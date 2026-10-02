// Package jsoncheck looks at raw JSON for things a decoder would resolve
// by itself, quietly.
package jsoncheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
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

// Member is one member of a JSON object: its key, and its value as it
// was written.
type Member struct {
	Key   string
	Value json.RawMessage
}

// SplitUnknown splits the object raw into the members the struct v, or
// the struct it points to, has a field for, written again as an object,
// and the rest, in the order they came: what a newer program wrote and
// this one doesn't know, kept to be written back with AppendUnknown.
// Only the object's own members are looked at: within them, a decoder
// still sees what it doesn't know.
func SplitUnknown(raw []byte, v any) (known []byte, rest []Member, err error) {
	names := jsonNames(v)
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil {
		return nil, nil, err
	} else if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, errors.New("not an object")
	}
	var out bytes.Buffer
	out.WriteByte('{')
	first := true
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, nil, errors.New("an object's key is not a string")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, nil, err
		}
		if !names[strings.ToLower(key)] {
			rest = append(rest, Member{Key: key, Value: value})
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(key)
		out.Write(k)
		out.WriteByte(':')
		out.Write(value)
	}
	if _, err := dec.Token(); err != nil {
		return nil, nil, err
	}
	out.WriteByte('}')
	// What follows the object is left to the caller, which reads raw
	// whole again for it.
	return out.Bytes(), rest, nil
}

// AppendUnknown writes rest back into out, an object as
// json.MarshalIndent writes it with two spaces, after its own members.
func AppendUnknown(out []byte, rest []Member) ([]byte, error) {
	if len(rest) == 0 {
		return out, nil
	}
	end := bytes.LastIndexByte(out, '}')
	if end < 0 {
		return nil, errors.New("not an object")
	}
	body := bytes.TrimRight(out[:end], " \t\r\n")
	var b bytes.Buffer
	b.Write(body)
	empty := bytes.HasSuffix(body, []byte("{"))
	for i, m := range rest {
		if i > 0 || !empty {
			b.WriteByte(',')
		}
		b.WriteString("\n  ")
		k, _ := json.Marshal(m.Key)
		b.Write(k)
		b.WriteString(": ")
		var v bytes.Buffer
		if err := json.Indent(&v, m.Value, "  ", "  "); err != nil {
			return nil, err
		}
		b.Write(v.Bytes())
	}
	b.WriteString("\n}")
	b.Write(out[end+1:])
	return b.Bytes(), nil
}

// jsonNames are the member names, lower case, the struct v or the one it
// points to decodes, as encoding/json names them: by tag, else by field.
func jsonNames(v any) map[string]bool {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	names := map[string]bool{}
	if t == nil || t.Kind() != reflect.Struct {
		return names
	}
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = f.Name
		}
		names[strings.ToLower(name)] = true
	}
	return names
}
