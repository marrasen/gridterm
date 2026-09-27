package vt

import "testing"

// DECRQM says whether a mode is set, reset, or unknown, as a program
// asks before it uses one.
func TestAModeQueryIsAnswered(t *testing.T) {
	h := newHarness(t, 20, 4)
	ask := func(q, want string) {
		t.Helper()
		h.replies = nil
		h.write(q)
		if len(h.replies) != 1 || h.replies[0] != want {
			t.Fatalf("%q was answered %q, want %q", q, h.replies, want)
		}
	}
	ask("\x1b[?2026$p", "\x1b[?2026;2$y")
	h.write("\x1b[?2026h")
	ask("\x1b[?2026$p", "\x1b[?2026;1$y")
	h.write("\x1b[?2026l")
	ask("\x1b[?2004$p", "\x1b[?2004;2$y")
	h.write("\x1b[?2004h")
	ask("\x1b[?2004$p", "\x1b[?2004;1$y")
	ask("\x1b[?7$p", "\x1b[?7;1$y")
	ask("\x1b[?9999$p", "\x1b[?9999;0$y")
	ask("\x1b[4$p", "\x1b[4;2$y")
	ask("\x1b[20$p", "\x1b[20;0$y")
}
