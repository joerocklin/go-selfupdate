// Package selfupdate performs an update of the running process
//
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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/osext"
	"gopkg.in/inconshreveable/go-update.v0"
)

const (
	upcktimePath = "cktime"
	plat         = runtime.GOOS + "-" + runtime.GOARCH
)

// ErrHashMismatch represents a hash mismatch error post-patching
var ErrHashMismatch = errors.New("new file hash mismatch after patch")

// ErrInvalidHashLength represents a bad hash length
var ErrInvalidHashLength = errors.New("Invalid hash length")

type logInterface interface {
	Println(v ...interface{})
}

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
	CurrentVersion string    // Currently running version.
	ApiURL         string    // Base URL for API requests (json files).
	CmdName        string    // Command name is appended to the ApiURL like http://apiurl/CmdName/. This represents one binary.
	BinURL         string    // Base URL for full binary downloads.
	DiffURL        string    // Base URL for diff downloads.
	Dir            string    // Directory to store selfupdate state.
	Requester      Requester //Optional parameter to override existing http request handler
	Info           struct {
		Version string
		Sha256  []byte
	}
	Logger logInterface
}

func (u *Updater) getExecRelativeDir(dir string) string {
	filename, _ := osext.Executable()
	path := filepath.Join(filepath.Dir(filename), dir)
	return path
}

// BackgroundRun starts the update check and apply cycle.
func (u *Updater) BackgroundRun() error {
	os.MkdirAll(u.getExecRelativeDir(u.Dir), 0777)
	if u.wantUpdate() {
		u.Logger.Println("Update Wanted")
		if err := u.Update(); err != nil {
			return err
		}
	}
	return nil
}

func (u *Updater) wantUpdate() bool {
	path := u.getExecRelativeDir(u.Dir + upcktimePath)
	if u.CurrentVersion == "dev" || readTime(path).After(time.Now()) {
		return false
	}
	wait := 24*time.Hour + randDuration(24*time.Hour)
	return writeTime(path, time.Now().Add(wait))
}

// Update does an update check and performs an update - first attempting a binary
// patch, then attempting a full binary download
func (u *Updater) Update() error {
	if updateAvailable, err := u.UpdateAvailable(); updateAvailable == false {
		return err
	}

	up := update.New().ApplyPatch(update.PATCHTYPE_BSDIFF).VerifyChecksum(u.Info.Sha256)

	// Construct the Patch URL
	patchURL := u.DiffURL + u.CmdName + "/" + u.CurrentVersion + "/" + u.Info.Version + "/" + plat

	// Attempt to perform an update from the URL
	err, _ := up.FromUrl(patchURL)
	if err == nil {
		return nil
	}

	// Construct the full binary URL
	binURL := u.BinURL + u.CmdName + "/" + u.Info.Version + "/" + plat + ".gz"

	// Update by patching failed - let's try updating the full binary
	up.ApplyPatch(update.PATCHTYPE_NONE)
	err, errRecover := up.FromUrl(binURL)
	if errRecover != nil {
		return fmt.Errorf("update and recovery errors: %q %q", err, errRecover)
	}
	if err != nil {
		return err
	}

	return nil
}

// UpdateAvailable returns true if an update is available, and false otherwise.
// If an error is encountered during the checks, the error will be returned as well
func (u *Updater) UpdateAvailable() (bool, error) {
	err := u.fetchInfo()
	if err != nil {
		return false, err
	}
	if u.Info.Version == u.CurrentVersion {
		return false, nil
	}

	return true, nil
}

func (u *Updater) fetch(url string) (io.ReadCloser, error) {
	if u.Requester == nil {
		u.Requester = &HTTPRequester{}
	}

	readCloser, err := u.Requester.Fetch(url)
	if err != nil {
		return nil, err
	}

	if readCloser == nil {
		return nil, fmt.Errorf("Fetch was expected to return non-nil ReadCloser")
	}

	return readCloser, nil
}

func (u *Updater) fetchInfo() error {
	r, err := u.fetch(u.ApiURL + u.CmdName + "/" + plat + ".json")
	if err != nil {
		return err
	}
	defer r.Close()
	err = json.NewDecoder(r).Decode(&u.Info)
	if err != nil {
		return err
	}
	if len(u.Info.Sha256) != sha256.Size {
		return ErrInvalidHashLength
	}
	return nil
}
