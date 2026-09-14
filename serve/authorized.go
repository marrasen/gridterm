package serve

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Allowed is the set of public keys that may take over this window.
//
// Read from a file in the same format as OpenSSH's authorized_keys, so
// a key can be put there by any of the tools people already have.
//
// An empty set lets nobody in. That is the point of it: a window with
// no keys listed has nobody it trusts, and must refuse everything
// rather than fall back to anything else.
type Allowed struct {
	keys []ssh.PublicKey

	// names are the comments the keys were written with, in the same
	// order, so the window can say who is connected by the name their
	// key carries rather than by its fingerprint.
	names []string
}

// LoadAllowed reads the keys that may connect.
//
// A file that is not there gives an empty set and no error: it is what
// a machine that has never been served from looks like, and the window
// says so rather than treating it as a fault.
//
// A file that cannot be read is an error, and serving does not start. A
// window that carried on would be one that trusts nobody while looking
// like one that trusts the people in a file it could not open.
func LoadAllowed(path string) (*Allowed, error) {
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &Allowed{}, nil
	case err != nil:
		return nil, fmt.Errorf("serve: read %s: %w", path, err)
	}
	return ParseAllowed(raw, path)
}

// ParseAllowed reads keys in the authorized_keys format.
//
// A line it cannot read fails the lot. Skipping it would leave the user
// believing a key is trusted when it is not, or -- worse the other way
// -- hide a line somebody else added that they cannot see the shape of.
func ParseAllowed(raw []byte, from string) (*Allowed, error) {
	a := &Allowed{}
	rest := raw
	line := 0
	for {
		rest = bytes.TrimLeft(rest, " \t\r\n")
		for len(rest) > 0 && rest[0] == '#' {
			if at := bytes.IndexByte(rest, '\n'); at >= 0 {
				rest = rest[at+1:]
			} else {
				rest = nil
			}
			rest = bytes.TrimLeft(rest, " \t\r\n")
		}
		if len(rest) == 0 {
			return a, nil
		}
		line++
		key, comment, _, next, err := ssh.ParseAuthorizedKey(rest)
		if err != nil {
			return nil, fmt.Errorf("serve: %s: key %d cannot be read: %w", from, line, err)
		}
		a.keys = append(a.keys, key)
		a.names = append(a.names, strings.TrimSpace(comment))
		rest = next
	}
}

// Len is how many keys may connect.
func (a *Allowed) Len() int {
	if a == nil {
		return 0
	}
	return len(a.keys)
}

// Names are the comments the keys carry, for showing who may connect.
// A key written without one is named by its fingerprint instead.
func (a *Allowed) Names() []string {
	if a == nil {
		return nil
	}
	out := make([]string, len(a.keys))
	for i, key := range a.keys {
		if a.names[i] != "" {
			out[i] = a.names[i]
			continue
		}
		out[i] = Fingerprint(key)
	}
	return out
}

// Who names the key offered, and reports whether it may connect.
//
// Compared by the bytes of the key rather than by its fingerprint: a
// fingerprint is a digest, and comparing digests is comparing something
// about the key instead of the key.
func (a *Allowed) Who(offered ssh.PublicKey) (string, bool) {
	if a == nil || offered == nil {
		return "", false
	}
	want := offered.Marshal()
	for i, key := range a.keys {
		// ConstantTimeCompare is not wanted here and would be a
		// pretence: a public key is public, and the lengths and types
		// differ anyway.
		if key.Type() == offered.Type() && bytes.Equal(key.Marshal(), want) {
			if a.names[i] != "" {
				return a.names[i], true
			}
			return Fingerprint(key), true
		}
	}
	return "", false
}
