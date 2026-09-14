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
// A line it cannot read fails the lot, and so does a line carrying
// restrictions. Both for the same reason: a user who wrote something
// there meant it. A key skipped is one they believe is trusted and is
// not, or one somebody else added that they cannot see the shape of. A
// key admitted with its from= or command= quietly dropped is worse
// still, because it is a key they believe is restricted and is not.
//
// Taken a line at a time rather than by handing the whole file to
// x/crypto, which skips what it cannot parse and would make a silent
// nothing of a key that was cut in half.
func ParseAllowed(raw []byte, from string) (*Allowed, error) {
	a := &Allowed{}
	for n, line := range strings.Split(string(raw), "\n") {
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("serve: %s line %d cannot be read: %w", from, n+1, err)
		}
		if len(options) > 0 {
			return nil, fmt.Errorf(
				"serve: %s line %d carries %s, which gridterm does not honour."+
					" Remove it, or keep that key for ssh only",
				from, n+1, strings.Join(options, ","))
		}
		if _, ok := key.(*ssh.Certificate); ok {
			// A certificate has an expiry, principals and a revocation
			// list behind it, and none of that is checked here. Taking
			// one as a plain key would honour it for ever.
			return nil, fmt.Errorf(
				"serve: %s line %d is a certificate, which gridterm does not check."+
					" List the key itself", from, n+1)
		}
		a.keys = append(a.keys, key)
		a.names = append(a.names, strings.TrimSpace(comment))
	}
	return a, nil
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
// about the key instead of the key. The bytes begin with the key's own
// algorithm name, so matching them is already matching the type.
func (a *Allowed) Who(offered ssh.PublicKey) (string, bool) {
	if a == nil || offered == nil {
		return "", false
	}
	want := offered.Marshal()
	for i, key := range a.keys {
		// ConstantTimeCompare is not wanted here and would be a
		// pretence: a public key is public.
		if !bytes.Equal(key.Marshal(), want) {
			continue
		}
		if a.names[i] != "" {
			return a.names[i], true
		}
		return Fingerprint(key), true
	}
	return "", false
}
