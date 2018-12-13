// Update protocol:
//
//   GET hk.heroku.com/hk/linux-amd64.json
//
//   200 ok
//   {
//       "Version": "2",
//       "Sha256": "..." // base64
//   }
//
// then
//
//   GET hkpatch.s3.amazonaws.com/hk/1/2/linux-amd64
//
//   200 ok
//   [bsdiff data]
//
// or
//
//   GET hkdist.s3.amazonaws.com/hk/2/linux-amd64.gz
//
//   200 ok
//   [gzipped executable data]
//
//
package selfupdate

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	update "github.com/inconshreveable/go-update"
)

const (
	upcktimePath = "cktime"
	plat         = runtime.GOOS + "-" + runtime.GOARCH
)

const devValidTime = 7 * 24 * time.Hour

var ErrHashMismatch = errors.New("new file hash mismatch after patch")

// Updater is the configuration and runtime data for doing an update.
//
// Note that ApiURL, BinURL and DiffURL should have the same value if all files are available at the same location.
//
// Example:
//
//  updater := &selfupdate.Updater{
//  	CurrentVersion: version,
//  	ApiURL:         "http://updates.yourdomain.com/",
//  	BinURL:         "http://updates.yourdownmain.com/",
//  	DiffURL:        "http://updates.yourdomain.com/",
//  	Dir:            "update/",
//  	CmdName:        "myapp", // app name
//  }
//  if updater != nil {
//  	go updater.BackgroundRun()
//  }
type Updater struct {
	CurrentVersion string // Currently running version.
	ApiURL         string // Base URL for API requests (json files).
	CmdName        string // Command name is appended to the ApiURL like http://apiurl/CmdName/. This represents one binary.
	BinURL         string // Base URL for full binary downloads.
	DiffURL        string // Base URL for diff downloads.
	Info           struct {
		Version string
		Sha256  []byte
	}
}

func (u *Updater) getExecRelativeDir(dir string) string {
	if dir[0] == '/' {
		return dir
	}
	filename, _ := os.Executable()
	path := filepath.Join(filepath.Dir(filename), dir)
	return path
}

// BackgroundRun checks and applies the update
func (u *Updater) Apply() error {
	err := u.FetchInfo()
	if err != nil {
		return err
	}
	if u.Info.Version == u.CurrentVersion {
		log.Println("update: no new version available")
		return nil
	}
	updateOptions := update.Options{
		Checksum: u.Info.Sha256,
		Patcher:  update.NewBSDiffPatcher(),
	}
	if err = updateOptions.CheckPermissions(); err != nil {
		// fail
		return err
	}

	log.Println("update: fetching binary patch")
	err = u.apply(u.DiffURL+u.CmdName+"/"+u.CurrentVersion+"/"+u.Info.Version+"/"+plat, updateOptions)
	if err == nil {
		log.Println("update: software updated correctly")
		return nil
	}

	log.Println("update: fetching full binary")
	updateOptions.Patcher = nil
	return u.applyGz(u.BinURL+u.CmdName+"/"+u.Info.Version+"/"+plat+".gz", updateOptions)
}

// FetchInfo downloads info about latest available version
func (u *Updater) FetchInfo() error {
	r, err := fetch(u.ApiURL + u.CmdName + "/" + plat + ".json")
	if err != nil {
		return err
	}
	defer r.Close()
	err = json.NewDecoder(r).Decode(&u.Info)
	if err != nil {
		return err
	}
	if len(u.Info.Sha256) != sha256.Size {
		return errors.New("bad cmd hash in info")
	}
	return nil
}

func (u *Updater) apply(url string, options update.Options) error {
	r, err := fetch(url)
	if err != nil {
		return err
	}
	defer r.Close()
	return update.Apply(r, options)
}

func (u *Updater) applyGz(url string, options update.Options) error {
	r, err := fetch(url)
	if err != nil {
		return err
	}
	defer r.Close()
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	return update.Apply(gz, options)
}

func fetch(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bad http status from %s: %v", url, resp.Status)
	}
	return resp.Body, nil
}
