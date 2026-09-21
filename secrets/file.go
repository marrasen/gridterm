package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Name is what the vault file is called inside gridterm's directory.
const Name = "secrets.json"

// fileVersion is the format written. A file from a later version is
// refused rather than guessed at: it holds the only copy of somebody's
// passwords.
const fileVersion = 1

// rename is os.Rename, replaced in a test that makes the last step of a
// save fail.
var rename = os.Rename

// file is the vault as it sits on disk.
//
// Everything here is either public or sealed. The slots say which keys
// open the vault and hold the data key wrapped for each; Sealed is the
// items under that data key.
type file struct {
	Version int    `json:"version"`
	Slots   []slot `json:"slots"`
	Nonce   []byte `json:"nonce"`
	Sealed  []byte `json:"sealed"`
}

// slot is one key that opens the vault.
//
// Challenge is what the key signs; Salt separates the slot key from the
// signature; Wrapped is the data key under that slot key. Fingerprint
// is there so the vault can say which key it wants without trying every
// key it is offered.
type slot struct {
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment,omitempty"`
	Challenge   []byte `json:"challenge"`
	Salt        []byte `json:"salt"`
	Nonce       []byte `json:"nonce"`
	Wrapped     []byte `json:"wrapped"`
}

// slotKindSSH is a slot opened by an SSH key. It is the only kind so
// far; a passphrase would be another, and would need no change to
// anything already written.
const slotKindSSH = "ssh"

// readFile reads a vault file. A missing file is reported as
// os.ErrNotExist for the caller to tell apart from a broken one.
func readFile(path string) (*file, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("secrets: read the vault %s: %w", path, err)
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("secrets: read the vault %s: %w", path, err)
	}
	if f.Version > fileVersion {
		return nil, fmt.Errorf(
			"secrets: the vault %s was written by a newer gridterm (version %d, this one reads %d)",
			path, f.Version, fileVersion)
	}
	if len(f.Slots) == 0 {
		return nil, fmt.Errorf("secrets: the vault %s has no key that opens it", path)
	}
	return &f, nil
}

// writeFile writes a vault file, replacing whatever was there.
//
// Through a temporary file in the same directory and a rename, so a
// crash leaves either the old vault or the new one and never half of
// either. Every failure is returned: a vault whose save went wrong and
// said nothing is a vault the user thinks holds something it does not.
func writeFile(path string, f *file) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("secrets: write the vault %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("secrets: make somewhere for the vault %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.new")
	if err != nil {
		return fmt.Errorf("secrets: write the vault %s: %w", path, err)
	}
	name := tmp.Name()
	// Only the user, before anything is in it.
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrInvalid) {
		return fmt.Errorf("secrets: write the vault %s: %w",
			path, errors.Join(err, tmp.Close(), os.Remove(name)))
	}
	if _, err := tmp.Write(raw); err != nil {
		return fmt.Errorf("secrets: write the vault %s: %w",
			path, errors.Join(err, tmp.Close(), os.Remove(name)))
	}
	// Flushed before the rename, or a crash can leave the new name
	// pointing at an empty file.
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("secrets: write the vault %s: %w",
			path, errors.Join(err, tmp.Close(), os.Remove(name)))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("secrets: write the vault %s: %w",
			path, errors.Join(err, os.Remove(name)))
	}
	if err := rename(name, path); err != nil {
		return fmt.Errorf("secrets: write the vault %s: %w",
			path, errors.Join(err, os.Remove(name)))
	}
	return nil
}
