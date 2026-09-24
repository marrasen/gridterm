package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/gridterm/internal/newfile"
	"golang.org/x/crypto/ssh"
)

// KeyPerm is the mode a new private key file is made with.
//
// It says who may read the key only where a mode says anything. On
// Windows a file has no mode to read and the key is as private as the
// directory holding it, which is why MakeKey insists on a full path
// rather than writing one wherever gridterm was started.
const KeyPerm = 0o600

// PubPerm is the mode the public half is made with. It is public, and it
// is pasted into files other people read.
const PubPerm = 0o644

// NewKey is a key pair that has just been written to disk.
type NewKey struct {
	// Path is the private half, and Pub the public half beside it.
	Path string
	Pub  string

	// Line is the public half as one line, which is what goes into an
	// authorized_keys file.
	Line string
}

// MakeKey writes a new ed25519 key pair at path, with the public half at
// path with ".pub" on the end.
//
// ed25519 because it is what ssh-keygen makes by default now, every
// server gridterm can reach takes it, and it has no size to choose.
//
// The path has to be an absolute one. A relative path would put a
// private key wherever gridterm happened to be started, which on Windows
// is also the whole of what keeps it private.
//
// An empty passphrase leaves the key unencrypted, the way ssh-keygen
// does when the user presses return at the prompt.
func MakeKey(path, comment, passphrase string) (NewKey, error) {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		return NewKey{}, errors.New("the key needs somewhere to be written")
	case !filepath.IsAbs(path):
		return NewKey{}, fmt.Errorf(
			"%s is not a full path. A key needs one, because where it sits is"+
				" part of what keeps it private", path)
	}
	pub := path + ".pub"
	// Before the key is made: locking a passphrase takes a moment, and
	// the likeliest answer is that one of the two files is already there.
	if err := errors.Join(free(path), free(pub)); err != nil {
		return NewKey{}, err
	}

	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return NewKey{}, fmt.Errorf("make a key: %w", err)
	}
	block, err := marshalKey(priv, comment, passphrase)
	if err != nil {
		return NewKey{}, fmt.Errorf("lock the key: %w", err)
	}
	signer, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return NewKey{}, fmt.Errorf("read back the key just made: %w", err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer)))
	if comment != "" {
		line += " " + comment
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return NewKey{}, fmt.Errorf("make %s: %w", filepath.Dir(path), err)
	}
	if err := writeWhole(path, pem.EncodeToMemory(block), KeyPerm); err != nil {
		return NewKey{}, err
	}
	if err := writeWhole(pub, []byte(line+"\n"), PubPerm); err != nil {
		// The private half is of no use with nothing saying what it is,
		// and leaving it would refuse the same path next time.
		if rm := os.Remove(path); rm != nil {
			return NewKey{}, errors.Join(err, fmt.Errorf(
				"a private key was left at %s and should be deleted: %w", path, rm))
		}
		return NewKey{}, err
	}
	return NewKey{Path: path, Pub: pub, Line: line}, nil
}

// free reports that nothing is at a path, or says what is.
//
// A key file that is already there is one somebody is using, and the
// private half cannot be got back.
func free(path string) error {
	switch _, err := os.Lstat(path); {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("look at %s: %w", path, err)
	}
	return fmt.Errorf("%s is already there, and a key file is not written over", path)
}

// marshalKey turns a private key into the file ssh reads, locked with a
// passphrase when there is one.
func marshalKey(priv ed25519.PrivateKey, comment, passphrase string) (*pem.Block, error) {
	if passphrase == "" {
		return ssh.MarshalPrivateKey(priv, comment)
	}
	return ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(passphrase))
}

// writeWhole writes a file that is not there, and refuses one that is.
//
// Through newfile, the way the serving window's host key is, so the key
// either exists complete or does not exist at all, on any filesystem. Creating the real file and then filling it
// would leave a half-written key at the name ssh looks for, and that
// name would then be refused for ever.
func writeWhole(path string, body []byte, perm os.FileMode) error {
	err := newfile.Write(path, body, perm)
	switch {
	case errors.Is(err, os.ErrExist):
		return fmt.Errorf("%s is already there, and a key file is not written over", path)
	case err != nil:
		return fmt.Errorf("put the key at %s: %w", path, err)
	}
	return nil
}

// DefaultKeyPath is where a new key goes when the user names nowhere:
// beside the keys ssh already looks for, under a name gridterm's own.
//
// Its own name so the first thing the dialog offers is not the one path
// most likely to be taken already.
func DefaultKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find your home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "id_ed25519_gridterm"), nil
}
