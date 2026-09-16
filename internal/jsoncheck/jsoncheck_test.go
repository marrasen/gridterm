package jsoncheck

import (
	"strings"
	"testing"
)

// A key written twice is caught, wherever in the file it is.
func TestAKeyWrittenTwiceIsCaught(t *testing.T) {
	twice := []struct{ what, body, key string }{
		{"at the top", `{"version":1,"version":2}`, "version"},
		{"in a nested object", `{"a":{"name":"one","name":"two"}}`, "name"},
		{"in an object inside an array", `{"s":[{"p":1},{"p":2,"p":3}]}`, "p"},
	}
	for _, c := range twice {
		t.Run(c.what, func(t *testing.T) {
			err := NoRepeatedKeys([]byte(c.body))
			if err == nil {
				t.Fatal("the same key twice was read as one")
			}
			if !strings.Contains(err.Error(), c.key) {
				t.Errorf("it said %v, without naming %q", err, c.key)
			}
			if !strings.Contains(err.Error(), "twice") {
				t.Errorf("it said %v, without saying what is wrong", err)
			}
		})
	}
}

// A value that reads like a key is a value.
//
// The check reads the tokens in order and cannot tell a key from a value
// by looking at it: both can be strings, and a server whose name is its
// address has the same string as a key and as a value.
func TestAValueThatLooksLikeAKeyIsNotARepeat(t *testing.T) {
	fine := []struct{ what, body string }{
		{"a name that is also an address",
			`{"servers":[{"name":"www.skalarit.net","address":"www.skalarit.net"}]}`},
		{"the same key in two objects", `{"a":{"name":"one"},"b":{"name":"two"}}`},
		{"a string that is a key elsewhere", `{"name":"version","version":1}`},
		{"the same string twice in an array", `{"tags":["a","a"]}`},
		{"nothing at all", `{}`},
	}
	for _, c := range fine {
		t.Run(c.what, func(t *testing.T) {
			if err := NoRepeatedKeys([]byte(c.body)); err != nil {
				t.Errorf("it refused a file with nothing wrong with it: %v", err)
			}
		})
	}
}

// Something that is not JSON is left to the caller's own decode, which
// says what is wrong with it far better than this can.
func TestSomethingThatIsNotJSONIsLeftAlone(t *testing.T) {
	for _, body := range []string{"{", "", "not json", `{"a":}`} {
		if err := NoRepeatedKeys([]byte(body)); err != nil {
			t.Errorf("%q gave %v, want it left to the decoder", body, err)
		}
	}
}
