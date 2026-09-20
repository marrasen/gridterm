package vt

import "testing"

// st ends an OSC the way a string terminator does.
const st = "\x1b\\"

// A shell may say where it is as a plain path rather than as a URL.
// It is what the Command Prompt can send: its prompt is built from
// what cmd.exe substitutes and none of that makes a URL.
func TestAShellSaysWhereItIsAsAPath(t *testing.T) {
	for _, tc := range []struct{ sent, want string }{
		{"\x1b]9;9;" + `C:\Users\marcus\notes` + st, `C:\Users\marcus\notes`},
		// Windows Terminal quotes it, and some shells copy that.
		{"\x1b]9;9;" + `"C:\Work space"` + st, `C:\Work space`},
		{"\x1b]9;9;/home/marcus\x07", "/home/marcus"},
	} {
		term := New(40, 10, DefaultPalette(), 100, Callbacks{})

		feed(t, term, tc.sent)

		dir, host := term.Dir()
		if dir != tc.want {
			t.Errorf("%q gave the directory %q, want %q", tc.sent, dir, tc.want)
		}
		if host != "" {
			t.Errorf("%q named the machine %q, and a plain path names none", tc.sent, host)
		}
	}
}

// Nothing useful is not taken, and the last directory stays.
func TestNonsenseInsteadOfAPathIsRefused(t *testing.T) {
	for _, sent := range []string{
		"\x1b]9;9;\x07",
		"\x1b]9;9;   \x07",
		// OSC 9 on its own is a desktop notification, not a directory.
		"\x1b]9;a message\x07",
	} {
		term := New(40, 10, DefaultPalette(), 100, Callbacks{})
		feed(t, term, "\x1b]7;file://here/tmp\x07")

		feed(t, term, sent)

		if dir, _ := term.Dir(); dir != "/tmp" {
			t.Errorf("%q changed the directory to %q", sent, dir)
		}
	}
}
