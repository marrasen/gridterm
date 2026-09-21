package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
)

// carryOwnTitle is what the button on the files notice says, and
// carriedOwnTitle heads the notice that says it is done.
//
// Two strings rather than one: a button says what pressing it will do
// and a title says what has happened, and one word cannot be both.
const (
	carryOwnTitle   = "Make Portable"
	carriedOwnTitle = "Made Portable"
)

// carryOwnFiles makes the directory beside this copy of gridterm and
// copies into it everything the window is reading now, so the copy
// starts with what the user has rather than with nothing.
//
// It reports what it did, for the notice that follows. The window has
// to be started again before any of it is read: where the files are is
// decided once, when the window opens.
func (a *app) carryOwnFiles() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("where this copy of gridterm is: %w", err)
	}
	beside := conf.Beside(exe)
	if err := os.MkdirAll(beside, 0o700); err != nil {
		return "", fmt.Errorf("make %s: %w", beside, err)
	}

	from, err := conf.Dir()
	if err != nil {
		return "", err
	}
	// A line per thing done, so the notice is a list of what happened
	// rather than a count to take on trust.
	done := []string{"Created " + beside}
	for _, name := range []string{
		settings.File, remote.BookFile, themes.File, keys.File,
		serve.AuthFile, knownWindowsFile,
	} {
		did, err := copyIfThere(filepath.Join(from, name), filepath.Join(beside, name))
		if err != nil {
			return "", err
		}
		if did {
			done = append(done, "Copied "+name)
		}
	}

	// The key last, and from wherever it lives: on Windows it is off the
	// roaming profile while a copy is not carrying its own files.
	key, err := serve.HostKeyPath()
	if err != nil {
		return "", err
	}
	did, err := copyIfThere(key, filepath.Join(beside, filepath.Base(key)))
	if err != nil {
		return "", err
	}
	if did {
		done = append(done, "Copied "+filepath.Base(key))
	}
	done = append(done, "", "Restart gridterm to use them.")
	return strings.Join(done, "\n"), nil
}

// copyIfThere copies one file, and reports whether there was one. A
// file that is not there yet is not a failure: the window writes each
// of them the first time it has something to say.
func copyIfThere(from, to string) (bool, error) {
	raw, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", from, err)
	}
	// Refused rather than written over: a directory that already holds
	// files is one somebody set up, and this would take what is in it
	// away without asking.
	if _, err := os.Stat(to); err == nil {
		return false, fmt.Errorf("%s is already there, so nothing was copied over it", to)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("look at %s: %w", to, err)
	}
	if err := os.WriteFile(to, raw, 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", to, err)
	}
	return true, nil
}

// isDir reports whether there is a directory at path, and tells a
// missing one apart from a path that could not be looked at.
func isDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look at %s: %w", path, err)
	}
	return info.IsDir(), nil
}
