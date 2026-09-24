// Package newfile writes a file that is not there yet, whole or not at
// all, and never over one that is.
//
// Two things are wanted at once. A file that is only half written -- the
// machine stopped, the disk filled -- must not be left at the name, or
// the next attempt finds the name taken and refuses it for good. And a
// file that another process wrote under the name a moment ago must not
// be written over: two windows making a host key at once would otherwise
// leave this machine answering to two.
//
// So the body goes to a file of its own beside the real one, is flushed,
// and is then linked to the real name, which fails when the name is
// taken. A filesystem with no hard links -- FAT32 and exFAT, which is
// what a USB stick usually is -- refuses the link whatever the name, so
// there the name is claimed with an exclusive create instead and the
// finished file renamed onto the claim.
package newfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// link is os.Link, a variable so a test can be a filesystem without hard
// links.
var link = os.Link

// StaleClaim is how long an empty file can sit at a name before it is
// taken for a claim whose writer never came back -- a process that died,
// a stick pulled -- and cleared. Nothing Write puts anywhere is empty,
// so an empty file there is only ever a claim.
var StaleClaim = 10 * time.Second

// Write puts body at path with the mode given, when nothing is at path.
// When something is, it writes nothing and returns an error that is
// fs.ErrExist.
//
// The directory must exist already.
func Write(path string, body []byte, perm fs.FileMode) error {
	clearStaleClaim(path)
	tmp, err := writeBeside(path, body, perm)
	if err != nil {
		return err
	}
	err = link(tmp, path)
	switch {
	case err == nil:
		if rm := removeSoon(tmp); rm != nil {
			// The file is written. What is left is a copy of it beside
			// it, which the user has to hear about: it may be a key.
			return fmt.Errorf("%s was written, but its copy %s could not be removed: %w",
				path, tmp, rm)
		}
		return nil
	case errors.Is(err, fs.ErrExist):
		_ = os.Remove(tmp)
		return fmt.Errorf("%s: %w", path, fs.ErrExist)
	}
	// No hard links here. The name is claimed so nothing else can take
	// it, and the finished file goes onto the claim: a rename replaces
	// the empty file this call made, and nothing anybody else wrote.
	claim, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		_ = os.Remove(tmp)
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s: %w", path, fs.ErrExist)
		}
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := claim.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path,
			errors.Join(err, os.Remove(tmp), os.Remove(path)))
	}
	if err := os.Rename(tmp, path); err != nil {
		// The claim is this call's and holds nothing, so it goes too:
		// left, it would be the empty file this is here to prevent.
		return fmt.Errorf("write %s: %w", path,
			errors.Join(err, os.Remove(tmp), os.Remove(path)))
	}
	return nil
}

// IsClaim reports whether the file at path is an empty one, which Write
// leaves at a name for the moment between claiming it and filling it
// on a filesystem without hard links.
func IsClaim(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() == 0
}

// clearStaleClaim takes away an empty file left at a name longer than a
// writer takes, so the name can be written again.
func clearStaleClaim(path string) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		return
	}
	if time.Since(info.ModTime()) < StaleClaim {
		return
	}
	_ = os.Remove(path)
}

// removeSoon removes a file, trying again for a moment: on Windows a
// file just closed can still be held open by whatever scans new files.
func removeSoon(path string) error {
	var err error
	for range 10 {
		if err = os.Remove(path); err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

// writeBeside writes body to a new file in path's directory and flushes
// it, and answers its name.
func writeBeside(path string, body []byte, perm fs.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	tmp := f.Name()
	fail := func(err error) (string, error) {
		return "", fmt.Errorf("write %s: %w", path, errors.Join(err, f.Close(), os.Remove(tmp)))
	}
	if _, err := f.Write(body); err != nil {
		return fail(err)
	}
	// Flushed before it takes the name, or a machine that stopped here
	// would leave the name holding an empty file.
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Chmod(perm); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write %s: %w", path, errors.Join(err, os.Remove(tmp)))
	}
	return tmp, nil
}
