// Package jsoncheck looks at raw JSON for things a decoder would resolve
// by itself, quietly.
package jsoncheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// NoRepeatedKeys reports an object holding the same key twice, which the
// decoder would otherwise resolve by keeping the last.
func NoRepeatedKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	// What is open, innermost last. Inside an object the tokens are a
	// key, then its value, then a key again, and that alternation is
	// the only thing that tells the two apart: both can be strings, and
	// a server whose name is its address has the same string as a key
	// and as a value.
	type scope struct {
		object  bool
		wantKey bool
		keys    map[string]bool
	}
	var scopes []scope
	// filled says the token just read was a value, so the object around
	// it is back to expecting a key.
	filled := func() {
		if n := len(scopes); n > 0 && scopes[n-1].object {
			scopes[n-1].wantKey = true
		}
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// Not readable as JSON at all, which the caller's own decode
			// says far better than this can.
			return nil
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{':
				filled()
				scopes = append(scopes, scope{object: true, wantKey: true, keys: map[string]bool{}})
			case '[':
				filled()
				scopes = append(scopes, scope{})
			case '}', ']':
				if len(scopes) > 0 {
					scopes = scopes[:len(scopes)-1]
				}
			}
			continue
		}
		n := len(scopes)
		if n == 0 || !scopes[n-1].object || !scopes[n-1].wantKey {
			filled()
			continue
		}
		key, _ := tok.(string)
		if scopes[n-1].keys[key] {
			return fmt.Errorf("%q is in it twice", key)
		}
		scopes[n-1].keys[key] = true
		scopes[n-1].wantKey = false
	}
}
