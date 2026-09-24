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
	"runtime"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/internal/newfile"
	"golang.org/x/crypto/ssh"
)

// The files a serving window keeps, in the directory conf gives
// gridterm. The host key is private and goes wherever conf.Private says.
const (
	keyFile = "serve_host_key"

	// AuthFile lists the keys allowed to take this window over.
	AuthFile = "authorized_keys"
)

// Dir returns the directory a serving window keeps its public files in.
func Dir() (string, error) { return conf.Dir() }

// privateDir returns the directory the host key lives in.
func privateDir() (string, error) { return conf.Private() }

// HostKeyPath returns where the host key lives.
func HostKeyPath() (string, error) {
	at, err := privateDir()
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
	return filepath.Join(at, AuthFile), nil
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
	signer, err := readHostKey(path)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return signer, err
	}
	signer, err = makeHostKey(path)
	if !errors.Is(err, os.ErrExist) {
		return signer, err
	}
	// Another window made one between the read and the write. Theirs is
	// the one on disk, so theirs is the one this machine is known by.
	return readHostKey(path)
}

// modesMeanSomething is whether a file mode says who can read a file.
//
// Windows has none to read: what keeps the key private there is the
// directory it is in, which belongs to one account. A test sets this to
// pin the check on whichever platform it is running on.
var modesMeanSomething = runtime.GOOS != "windows"

// readHostKey reads the key from disk, refusing one anybody else can
// read.
func readHostKey(path string) (ssh.Signer, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("serve: read the host key %s: %w", path, err)
	}
	if modesMeanSomething && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf(
			"serve: the host key %s is readable by others (mode %04o)."+
				" Fix its permissions, or delete it and let gridterm make another",
			path, info.Mode().Perm())
	}
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("serve: read the host key %s: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("serve: read the host key %s: %w", path, err)
	}
	return signer, nil
}

// makeHostKey writes a new key and returns it.
//
// Written whole to a file of its own and then linked into place, so the
// key either exists complete or does not exist at all. Two windows
// starting together must not each think they made the key this machine
// is known by -- they would present different ones, which is the very
// alarm a host key is for -- and the one that loses the link reads what
// the winner wrote. Creating the real file and then filling it would
// leave the loser reading an empty one.
//
// A window killed part way leaves the temporary file behind and nothing
// else. The next start makes its own.
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
	// Whole or not at all, and never over a key another window made a
	// moment ago: this machine would answer to two. newfile also works
	// where there are no hard links, which a copy on a USB stick needs.
	if err := newfile.Write(path, pem.EncodeToMemory(block), 0o600); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, err
		}
		return nil, fmt.Errorf("serve: put the host key at %s: %w", path, err)
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
