package serve

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/session"
)

// esc is the byte that starts an escape sequence.
const esc = "\x1b"

// One rule, whether the text arrives whole or in pieces.
//
// The one place the two differ is the end of the text: a character whose
// last byte has not arrived is held back by the streaming form and
// replaced by the other, which has the whole string in front of it.
func TestPlainDropsWhatATerminalWouldObey(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"an escape sequence", esc + "[2JCode:", "[2JCode:"},
		{"a title sequence", esc + "]0;owned\x07", "]0;owned"},
		{"a carriage return", "Code:\rFAKE", "Code:FAKE"},
		{"a line feed", "one\ntwo", "one\ntwo"},
		{"a tab", "a\tb", "a b"},
		{"a C1 control", "ab", "ab"},
		{"a byte that is not a character", "a\xffb", "a�b"},
		{"ordinary words", "Enter your code", "Enter your code"},
		{"an accent", "Código", "Código"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Plain(tc.in); got != tc.want {
				t.Fatalf("Plain(%q) = %q, want %q", tc.in, got, tc.want)
			}

			// And the same bytes one at a time, which is how they arrive
			// off a connection.
			var out bytes.Buffer
			p := &PlainWriter{To: &out}
			for i := 0; i < len(tc.in); i++ {
				if _, err := p.Write([]byte(tc.in[i : i+1])); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("written a byte at a time it came out %q, want %q", got, tc.want)
			}
		})
	}

	// The whole-string form has the end of the string in front of it, so
	// a character that stops part way through is replaced there.
	if got := Plain("a\xc3"); got != "a�" {
		t.Errorf("Plain of a truncated character = %q", got)
	}
}

// Whole characters survive the filtering, however the bytes of one are
// split between writes.
func TestFilteringKeepsCharactersSplitAcrossWrites(t *testing.T) {
	var got bytes.Buffer
	p := &PlainWriter{To: &got}

	// "héllo" with the two bytes of the é in different writes, and a
	// cursor-home sequence split in the middle of it.
	body := []byte("héllo\x1b[H there")
	for i := range body {
		if _, err := p.Write(body[i : i+1]); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if want := "héllo[H there"; got.String() != want {
		t.Fatalf("it wrote %q, want %q", got.String(), want)
	}
}

// A character whose last byte never arrives is not passed on: nothing
// half-decoded reaches the pane.
func TestFilteringHoldsBackACharacterThatNeverFinishes(t *testing.T) {
	var got bytes.Buffer
	p := &PlainWriter{To: &got}
	if _, err := p.Write([]byte("ok\xe6\x97")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got.String() != "ok" {
		t.Fatalf("it wrote %q, want only what was whole", got.String())
	}
}

// What the far end says goes into a pane, and a pane acts on escape
// sequences.
//
// A window at the other end that could move this one's cursor could
// overwrite the lines above what it wrote -- "its host key is accepted"
// among them -- and pass its own words off as this window's.
func TestWhatTheFarEndSaysCannotDriveThisTerminal(t *testing.T) {
	_, w := takenOverWith(t, nil, func(want Attached, cols, rows int) (session.Session, error) {
		return nil, errors.New("\x1b[2J\x1b[Hgridterm: its host key is accepted\x07")
	})

	sess, err := w.Attach(Open{ID: "1", Kind: "Terminal", Label: "bash"}, 80, 24)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = sess.Close() }()

	got := read(t, sess, "its host key is accepted")
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("the pane was sent %q, want nothing a terminal acts on", got)
	}
}
