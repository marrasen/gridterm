package vt

import "testing"

// feedOSC writes an OSC sequence to a terminal, the way a shell does.
func feedOSC(t *testing.T, term *Terminal, body string) {
	t.Helper()
	term.Write([]byte("\x1b]" + body + "\x07"))
}

// A shell says where it is with OSC 7, and the pane remembers.
func TestOSC7SaysWhereTheShellIs(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})

	feedOSC(t, term, "7;file://kettle/home/marcus/work")

	dir, host := term.Dir()
	if dir != "/home/marcus/work" {
		t.Errorf("it says %q, want the path", dir)
	}
	if host != "kettle" {
		t.Errorf("it says the machine is %q, want kettle", host)
	}
}

// A Windows path comes with the slash a URL puts in front of the
// drive letter, and that slash is the URL's rather than the path's.
func TestOSC7UnwindsAWindowsPath(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})

	feedOSC(t, term, "7;file:///C:/Users/marcus/work")

	if dir, _ := term.Dir(); dir != `C:/Users/marcus/work` {
		t.Errorf("it says %q, want the path without the URL's slash", dir)
	}
}

// The path is percent-encoded, because a URL cannot hold a space.
func TestOSC7DecodesWhatTheURLEncoded(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})

	feedOSC(t, term, "7;file:///home/marcus/two%20words")

	if dir, _ := term.Dir(); dir != "/home/marcus/two words" {
		t.Errorf("it says %q, want the space decoded", dir)
	}
}

// A path holding a semicolon survives. The parser cuts parameters on
// semicolons, so what was sent has to be joined back up.
func TestOSC7KeepsASemicolonInThePath(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})

	feedOSC(t, term, "7;file:///home/marcus/a;b")

	if dir, _ := term.Dir(); dir != "/home/marcus/a;b" {
		t.Errorf("it says %q, want the semicolon kept", dir)
	}
}

// A shell with no name for its machine leaves the host empty, which
// is what one on this machine sends.
func TestOSC7WithNoHostLeavesItEmpty(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})

	feedOSC(t, term, "7;file:///home/marcus")

	dir, host := term.Dir()
	if dir != "/home/marcus" {
		t.Errorf("it says %q", dir)
	}
	if host != "" {
		t.Errorf("it says the machine is %q, want nothing", host)
	}
}

// An empty payload means the shell no longer knows where it is.
func TestOSC7WithNothingForgetsWhereItWas(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})
	feedOSC(t, term, "7;file:///home/marcus")

	feedOSC(t, term, "7;")

	if dir, _ := term.Dir(); dir != "" {
		t.Errorf("it still says %q", dir)
	}
}

// Something that is not a file URL is not believed, and what the
// shell last said stays rather than being replaced by nonsense.
func TestOSC7KeepsTheLastGoodOne(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})
	feedOSC(t, term, "7;file:///home/marcus")

	feedOSC(t, term, "7;https://example.com/not-a-directory")

	if dir, _ := term.Dir(); dir != "/home/marcus" {
		t.Errorf("it says %q, want the last one it believed", dir)
	}
}

// A bare path with no scheme is taken as one: some shells send that.
func TestOSC7TakesABarePath(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})

	feedOSC(t, term, "7;/home/marcus/work")

	if dir, _ := term.Dir(); dir != "/home/marcus/work" {
		t.Errorf("it says %q, want the bare path", dir)
	}
}

// A terminal reset forgets it, the same as it forgets the title.
func TestAResetForgetsWhereTheShellWas(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})
	feedOSC(t, term, "7;file:///home/marcus")

	term.Write([]byte("\x1bc"))

	if dir, _ := term.Dir(); dir != "" {
		t.Errorf("it still says %q after a reset", dir)
	}
}

// Nothing said means nothing known, which is what every shell that
// has not been set up for this leaves.
func TestAShellThatSaysNothingLeavesNoDirectory(t *testing.T) {
	term := New(20, 5, DefaultPalette(), 10, Callbacks{})

	term.Write([]byte("hello\r\n"))

	if dir, host := term.Dir(); dir != "" || host != "" {
		t.Errorf("it says %q on %q, want nothing", dir, host)
	}
}
