// Package serve lets one gridterm window take over another's.
//
// A window can listen for a client on a port of its own. What crosses
// the wire is what the host has open and the bytes of whatever the
// client attaches to, never the screen: the two machines have screens
// of different sizes, and often of different shapes, so the client
// draws its own.
//
// Nothing here runs unless the user turns it on. A window that is not
// serving holds no listener, no keys and no port.
package serve

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// Where a serving window keeps its own files, under the OS config
// directory.
const (
	dir      = "gridterm"
	keyFile  = "serve_host_key"
	authFile = "authorized_keys"
)

// Dir returns the directory a serving window keeps its files in.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("serve: no configuration directory: %w", err)
	}
	return filepath.Join(base, dir), nil
}

// HostKeyPath returns where the host key lives.
func HostKeyPath() (string, error) {
	at, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(at, keyFile), nil
}

// AuthorizedKeysPath returns where the keys allowed to connect live.
func AuthorizedKeysPath() (string, error) {
	at, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(at, authFile), nil
}

// HostKey reads the window's own key, making one the first time.
//
// It is what a client checks this machine by, the way known_hosts
// checks any other server, so it has to be the same key every time the
// window opens. A window that made a fresh one on each start would look
// to its own client exactly like a machine in the middle.
//
// Written before it is used, and a failure to write is a failure to
// serve. A key that only exists in memory would be that fresh key again
// the next time.
func HostKey(path string) (ssh.Signer, error) {
	pemBytes, err := os.ReadFile(path)
	switch {
	case err == nil:
		signer, err := ssh.ParsePrivateKey(pemBytes)
		if err != nil {
			return nil, fmt.Errorf("serve: read the host key %s: %w", path, err)
		}
		return signer, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("serve: read the host key %s: %w", path, err)
	}
	return makeHostKey(path)
}

// makeHostKey writes a new key and returns it.
func makeHostKey(path string) (ssh.Signer, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("serve: make a host key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(key, "gridterm")
	if err != nil {
		return nil, fmt.Errorf("serve: encode the host key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("serve: make %s: %w", filepath.Dir(path), err)
	}
	// 0600, and written through OpenFile rather than WriteFile so the
	// mode is set as the file is made. WriteFile applies it only when it
	// creates the file, which leaves a key readable by anyone if one was
	// already there with a wider mode.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("serve: write the host key %s: %w", path, err)
	}
	if _, err := f.Write(pem.EncodeToMemory(block)); err != nil {
		f.Close()
		return nil, fmt.Errorf("serve: write the host key %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("serve: write the host key %s: %w", path, err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("serve: use the host key: %w", err)
	}
	return signer, nil
}

// Fingerprint is how the host key is spelled when it is shown to
// somebody, which is the way ssh and ssh-keygen spell it.
//
// There is nothing else to check a first connection against, so it has
// to read the same here as it does in the tools the user already has.
func Fingerprint(key ssh.PublicKey) string { return ssh.FingerprintSHA256(key) }
