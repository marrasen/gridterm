package main

import (
	"testing"
	"time"

	"github.com/marrasen/gridterm/ui/term"
)

// aWindowOnOneClock is aWindowWatchingItsCursor with the window's own
// clock in the test's hands as well, which is the one the cursor's hide
// is measured against.
func aWindowOnOneClock(t *testing.T, seq string) (*testApp, *term.Terminal, *time.Time) {
	t.Helper()
	a, pane, at := aWindowWatchingItsCursor(t, seq)
	a.now = func() time.Time { return *at }
	return a, pane, at
}

// A program hiding and showing the cursor in one go never reaches the
// screen, which is what a local shell does: the repaint comes off the
// pty in one read, so the hide and the show are the same frame.
func TestAHideAndShowInOneReadNeverShows(t *testing.T) {
	a, pane, at := aWindowOnOneClock(t, "\x1b[1 q")
	*at = blinkStart.Add(100 * time.Millisecond)

	a.shells[0].out <- []byte("\x1b[?25l" + "x" + "\x1b[?25h")
	waitFor(t, a, "the repaint to land", func() bool {
		return screenOf(pane).At(5, 0).Rune == 'x'
	})
	frame(t, a)

	if !screenOf(pane).Cursor().Visible {
		t.Error("the cursor is hidden after a repaint that turned it back on")
	}
}

// The same repaint split across two reads keeps its cursor too.
//
// A pane on another machine is read off a connection rather than a pty,
// so the split is wherever the network put it. Without the grace the
// cursor goes out for those frames, which is a cursor that flickers
// while somebody types.
func TestAHideAndShowSplitAcrossReadsKeepsTheCursor(t *testing.T) {
	a, pane, at := aWindowOnOneClock(t, "\x1b[1 q")
	*at = blinkStart.Add(100 * time.Millisecond)

	// The first half: the program has hidden the cursor and not yet put
	// it back.
	a.shells[0].out <- []byte("\x1b[?25l" + "x")
	waitFor(t, a, "the first half to land", func() bool {
		return screenOf(pane).At(5, 0).Rune == 'x'
	})
	frame(t, a)
	if !screenOf(pane).Cursor().Visible {
		t.Error("the cursor went out between two reads of one repaint")
	}

	// A frame or two later it is still there, because the rest of the
	// repaint is still on its way.
	*at = blinkStart.Add(160 * time.Millisecond)
	frame(t, a)
	if !screenOf(pane).Cursor().Visible {
		t.Error("the cursor went out while the rest of the repaint was in flight")
	}

	// And the rest arrives.
	a.shells[0].out <- []byte("\x1b[?25h")
	waitFor(t, a, "the rest to land", func() bool { return true })
	*at = blinkStart.Add(200 * time.Millisecond)
	frame(t, a)
	if !screenOf(pane).Cursor().Visible {
		t.Error("the cursor never came back")
	}
}

// A program that means to hide the cursor still gets it hidden.
//
// The grace covers a repaint, not a pager sitting there with no cursor,
// so a cursor still hidden once the grace is up goes.
func TestACursorTheProgramMeantToHideGoes(t *testing.T) {
	a, pane, at := aWindowOnOneClock(t, "\x1b[1 q")
	*at = blinkStart.Add(100 * time.Millisecond)

	// A character after the hide, so the test can see the hide has been
	// parsed rather than guessing at it.
	a.shells[0].out <- []byte("\x1b[?25l" + "z")
	waitFor(t, a, "the hide to land", func() bool {
		return screenOf(pane).At(5, 0).Rune == 'z'
	})
	frame(t, a)

	// Still on, because a repaint would have put it back by now.
	if !screenOf(pane).Cursor().Visible {
		t.Error("the cursor went out at once, so a repaint would flicker")
	}

	// Past the grace it goes, and stays gone.
	*at = blinkStart.Add(100*time.Millisecond + term.CursorHideGrace + 10*time.Millisecond)
	frame(t, a)
	if screenOf(pane).Cursor().Visible {
		t.Error("the cursor is still on well after the program hid it")
	}
}
