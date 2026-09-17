// Package conf decides where gridterm keeps its files.
//
// There are two places. One is the directory the operating system gives
// a program for its settings, which is where they go by default. The
// other is a directory beside the executable, which lets one machine
// hold several copies of gridterm, each with files of its own.
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

// Name is what gridterm's directory under the operating system's own is
// called. Existing files are under this name, so it does not change.
const Name = "gridterm"

// BesideName is what the directory beside the executable is called. Not
// "gridterm", which on a system where the executable has no extension
// would be the executable itself.
const BesideName = "gridterm-files"

// Dir returns the directory gridterm keeps its files in.
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

// Private returns the directory gridterm keeps files nobody else may
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

// CarriesItsOwn reports whether gridterm keeps its files beside itself,
// and where that directory is either way.
func CarriesItsOwn() (bool, string, error) {
	d := decide()
	return d.own, d.beside, d.err
}

// Beside returns the directory a copy of gridterm at exe would carry its
// files in.
func Beside(exe string) string { return filepath.Join(filepath.Dir(exe), BesideName) }

// DirFor returns the directory a copy of gridterm at exe keeps its files
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

// PrivateFor returns the directory a copy of gridterm at exe keeps its
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

// CarriesItsOwnFor reports whether a copy of gridterm at exe keeps its
// files beside itself, and where that directory is either way.
func CarriesItsOwnFor(exe string) (bool, string, error) {
	there, err := isDir(Beside(exe))
	if err != nil {
		return false, "", err
	}
	return there, Beside(exe), nil
}

// system returns the directory the operating system gives gridterm for
// its settings.
func system() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("conf: no configuration directory: %w", err)
	}
	return filepath.Join(base, Name), nil
}

// localProfile returns the directory gridterm keeps private files in
// when it is not carrying its own: the local profile on Windows, where
// os.UserConfigDir gives the roaming one.
func localProfile() (string, error) {
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, Name), nil
		}
	}
	return system()
}

// choice is where gridterm keeps its files, worked out once.
type choice struct {
	// own says the files are beside this copy of gridterm, and beside is
	// that directory whether they are there or not.
	own    bool
	beside string

	err error
}

// made and once hold the answer for the life of the process.
//
// Worked out once so a directory made or deleted while gridterm is
// running cannot send part of a session's writes to the other place.
var (
	made choice
	once sync.Once
)

// findExe is the executable's own path. A test points it elsewhere.
var findExe = whereGridtermIs

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

// whereGridtermIs returns the executable's own path with any link
// followed, so a copy started through a link finds the files beside the
// real one.
func whereGridtermIs() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("conf: find gridterm's own path: %w", err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("conf: follow %s: %w", exe, err)
	}
	return real, nil
}
