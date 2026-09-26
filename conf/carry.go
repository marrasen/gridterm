package conf

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// CarryOwn makes the directory beside the copy of gridterm at exe and
// copies into it the files named from dir, then the file at key, so the
// copy starts with what the user has rather than with nothing.
//
// It reports a line per thing done, so what says it can list what
// happened rather than a count to take on trust. The window has to be
// started again before any of it is read: where the files are is
// decided once, when the window opens.
func CarryOwn(exe, dir string, names []string, key string) ([]string, error) {
	beside := Beside(exe)
	if err := os.MkdirAll(beside, 0o700); err != nil {
		return nil, fmt.Errorf("make %s: %w", beside, err)
	}
	done := []string{"Created " + beside}
	for _, name := range names {
		did, err := copyIfThere(filepath.Join(dir, name), filepath.Join(beside, name))
		if err != nil {
			return nil, err
		}
		if did {
			done = append(done, "Copied "+name)
		}
	}
	// The key last, and from wherever it lives: on Windows it is off the
	// roaming profile while a copy is not carrying its own files.
	did, err := copyIfThere(key, filepath.Join(beside, filepath.Base(key)))
	if err != nil {
		return nil, err
	}
	if did {
		done = append(done, "Copied "+filepath.Base(key))
	}
	return done, nil
}

// IsDir reports whether a directory is at path, and tells a missing
// one apart from a path that could not be looked at.
func IsDir(path string) (bool, error) { return isDir(path) }

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
