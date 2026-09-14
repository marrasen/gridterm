package main

import (
	"context"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/remote"
)

// esc is the byte that starts an escape sequence.
const esc = "\x1b"

// Whatever a server sends is printed to the terminal gridterm was
// started from, which obeys escape sequences. Left alone it could move
// that terminal's cursor, overwrite what was already there, or set its
// title.
func TestPlainlyStripsWhatATerminalWouldObey(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"an escape sequence", esc + "[2JCode:", "[2JCode:"},
		{"a carriage return", "Code:\rFAKE", "Code:FAKE"},
		{"a title sequence", esc + "]0;owned\x07", "]0;owned"},
		{"a tab", "a\tb", "a b"},
		{"ordinary words", "Enter your code", "Enter your code"},
		{"an accent", "Código", "Código"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := plainly(tc.in)
			if got != tc.want {
				t.Fatalf("plainly(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for _, r := range got {
				if r < ' ' {
					t.Fatalf("plainly(%q) left a control character %q", tc.in, r)
				}
			}
		})
	}
}

// Before the window opens there is no dialog to show a fingerprint in,
// so an unknown host is a hard failure — and the message has to carry
// the fingerprint so the user can do something about it.
func TestConsoleRefusesAnUnknownHostKey(t *testing.T) {
	key := testHostKey(t)
	ok, err := consoleAsk{}.TrustHostKey(context.Background(), key)
	if ok {
		t.Fatal("the console accepted an unknown host key")
	}
	if err == nil {
		t.Fatal("no reason was given")
	}
	for _, want := range []string{key.Addr, key.Fingerprint(), "known_hosts"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %q", err, want)
		}
	}
}

// A console question names the machine it came from before it prints a
// word the server chose, and the server's wording is stripped of
// anything the terminal would obey.
func TestConsoleQuestionIsAttributedBeforeTheServersWording(t *testing.T) {
	var out strings.Builder
	old := console
	console = &out
	t.Cleanup(func() { console = old })

	// Nothing to answer, so nothing is read from the console.
	if _, err := (consoleAsk{}).Question(context.Background(), remote.Question{
		User: "marcus", Host: "margit:22",
		Name:        esc + "[2JUnlock a private key",
		Instruction: "/home/marcus/.ssh/id_ed25519",
	}); err != nil {
		t.Fatalf("Question: %v", err)
	}

	printed := out.String()
	ours := strings.Index(printed, "marcus@margit:22 is asking")
	theirs := strings.Index(printed, "Unlock a private key")
	if ours < 0 {
		t.Fatalf("nothing said which machine asked: %q", printed)
	}
	if theirs >= 0 && theirs < ours {
		t.Fatalf("the server's wording was printed first: %q", printed)
	}
	if strings.ContainsRune(printed, 0x1b) {
		t.Fatalf("an escape sequence reached the terminal: %q", printed)
	}
}
