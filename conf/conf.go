// Package conf decides where kakel keeps its files.
//
// There are two places. One is the directory the operating system gives
// a program for its settings, which is where they go by default. The
// other is a directory beside the executable, which lets one machine
// hold several copies of kakel, each with files of its own.
package conf

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Name is what kakel's directory under the operating system's own is
// called.
const Name = "kakel"

// BesideName is what the directory beside the executable is called. Not
// "kakel", which on a system where the executable has no extension
// would be the executable itself.
const BesideName = "kakel-files"

// kakel was called gridterm, and kept its files under these names. The
// first time kakel looks for one of its directories and finds only the
// old one, it renames the old one; see adopt.
const (
	oldName       = "gridterm"
	oldBesideName = "gridterm-files"
)

// Dir returns the directory kakel keeps its files in.
func Dir() (string, error) {
	d := decide()
	if d.err != nil {
		return "", d.err
	}
	if d.own {
		return d.beside, nil
	}
	return system()
}

// Private returns the directory kakel keeps files nobody else may
// read in: the key a serving window proves itself with.
func Private() (string, error) {
	d := decide()
	if d.err != nil {
		return "", d.err
	}
	if d.own {
		return d.beside, nil
	}
	return localProfile()
}

// CarriesItsOwn reports whether kakel keeps its files beside itself,
// and where that directory is either way.
func CarriesItsOwn() (bool, string, error) {
	d := decide()
	return d.own, d.beside, d.err
}

// Beside returns the directory a copy of kakel at exe would carry its
// files in. A copy that carried gridterm's files beside it carries them
// on under the new name.
func Beside(exe string) string {
	dir := filepath.Dir(exe)
	return adopt(filepath.Join(dir, BesideName), filepath.Join(dir, oldBesideName))
}

// DirFor returns the directory a copy of kakel at exe keeps its files
// in: the one beside it when the user has made that directory, and
// otherwise the one the operating system gives a program.
func DirFor(exe string) (string, error) {
	there, err := isDir(Beside(exe))
	if err != nil {
		return "", err
	}
	if there {
		return Beside(exe), nil
	}
	return system()
}

// PrivateFor returns the directory a copy of kakel at exe keeps its
// private files in: the one beside it when it carries its own, and
// otherwise the local profile rather than the roaming one, which a
// domain account copies to a file server at every logon.
func PrivateFor(exe string) (string, error) {
	there, err := isDir(Beside(exe))
	if err != nil {
		return "", err
	}
	if there {
		return Beside(exe), nil
	}
	return localProfile()
}

// CarriesItsOwnFor reports whether a copy of kakel at exe keeps its
// files beside itself, and where that directory is either way.
func CarriesItsOwnFor(exe string) (bool, string, error) {
	there, err := isDir(Beside(exe))
	if err != nil {
		return false, "", err
	}
	return there, Beside(exe), nil
}

// system returns the directory the operating system gives kakel for
// its settings.
func system() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("conf: no configuration directory: %w", err)
	}
	return adopt(filepath.Join(base, Name), filepath.Join(base, oldName)), nil
}

// localProfile returns the directory kakel keeps private files in
// when it is not carrying its own: the local profile on Windows, where
// os.UserConfigDir gives the roaming one.
func localProfile() (string, error) {
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return adopt(filepath.Join(local, Name), filepath.Join(local, oldName)), nil
		}
	}
	return system()
}

// choice is where kakel keeps its files, worked out once.
type choice struct {
	// own says the files are beside this copy of kakel, and beside is
	// that directory whether they are there or not.
	own    bool
	beside string

	err error
}

// made and once hold the answer for the life of the process.
//
// Worked out once so a directory made or deleted while kakel is
// running cannot send part of a session's writes to the other place.
var (
	made choice
	once sync.Once
)

// findExe is the executable's own path. A test points it elsewhere.
var findExe = whereKakelIs

// decide works out where the files go, the first time anything asks.
func decide() choice {
	once.Do(func() {
		exe, err := findExe()
		if err != nil {
			made = choice{err: err}
			return
		}
		own, beside, err := CarriesItsOwnFor(exe)
		made = choice{own: own, beside: beside, err: err}
	})
	return made
}

// isDir reports whether a directory is at a path. A disk that cannot be
// read is neither yes nor no: it is a failure.
func isDir(path string) (bool, error) {
	switch info, err := os.Stat(path); {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("conf: look at %s: %w", path, err)
	default:
		return info.IsDir(), nil
	}
}

// whereKakelIs returns the executable's own path with any link
// followed, so a copy started through a link finds the files beside the
// real one.
func whereKakelIs() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("conf: find kakel's own path: %w", err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("conf: follow %s: %w", exe, err)
	}
	return real, nil
}

// adopt returns the directory to use for dir, taking over old, the
// directory the same files had under gridterm's name. With dir there,
// or old missing, it is dir. With only old there, old is renamed to
// dir, so the servers, secrets, themes and keys kept there carry on.
// Where the rename fails, as on Windows while an old gridterm still has
// a file in it open, it is old, used where it is, and the rename is
// tried again next time.
func adopt(dir, old string) string {
	if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
		return dir
	}
	info, err := os.Lstat(old)
	if err != nil || !info.IsDir() {
		return dir
	}
	if err := os.Rename(old, dir); err != nil {
		return old
	}
	return dir
}
