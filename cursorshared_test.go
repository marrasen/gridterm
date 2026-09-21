package main

import (
	"testing"
	"time"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui/term"
)

// aWatchedWindowWatchingItsCursor is aWindowWatchingItsCursor with
// somebody else reading the pane, which is what gives it a border.
//
// Both clocks are the test's: the cursor blinks on the compositor's and
// the border glows on the window's, and a test that moved only one
// would drift.
func aWatchedWindowWatchingItsCursor(t *testing.T, seq string) (*testApp, *term.Terminal, *time.Time) {
	t.Helper()
	a := newTestApp(t, 40, 12)
	withScreen(t, a)
	pane := onlyPaneOn(t, a)
	at := blinkStart
	a.comp.Now = func() time.Time { return at }
	a.now = func() time.Time { return at }
	watchPane(t, pane)

	a.shells[0].out <- []byte(seq + "hello")
	waitFor(t, a, "the shell to say something", func() bool {
		return screenOf(pane).At(0, 0).Rune == 'h'
	})
	frame(t, a)
	frame(t, a)
	frame(t, a)
	return a, pane, &at
}

// cursorFlips runs frames over a stretch and returns the moments the
// cursor's row was drawn, which is the blink turning over.
//
// The border's glow draws frames of its own, so a frame that was not
// skipped proves nothing. The cursor's row being redrawn does.
func cursorFlips(t *testing.T, a *testApp, at *time.Time, over, every time.Duration) []time.Duration {
	t.Helper()
	from := *at
	var flips []time.Duration
	for step := time.Duration(1); step*every <= over; step++ {
		*at = from.Add(step * every)
		frame(t, a)
		if a.comp.Stats().RowsDrawn > 0 {
			flips = append(flips, step*every)
		}
	}
	return flips
}

// A cursor on a pane somebody else is reading blinks to the same clock
// as one nobody is.
//
// The border glowing beside it draws frames the window would otherwise
// skip. The blink has to keep its own half-second phase through them.
func TestAWatchedPanesCursorBlinksOnTheHalfSecond(t *testing.T) {
	a, _, at := aWatchedWindowWatchingItsCursor(t, "\x1b[1 q")

	flips := cursorFlips(t, a, at, 2*time.Second, 25*time.Millisecond)

	// Off at 500ms, on at 1s, off at 1.5s, on at 2s.
	want := []time.Duration{
		500 * time.Millisecond, time.Second,
		1500 * time.Millisecond, 2 * time.Second,
	}
	if len(flips) != len(want) {
		t.Fatalf("the cursor turned over at %v, want four times at %v", flips, want)
	}
	for i, w := range want {
		if off := flips[i] - w; off < 0 || off > 25*time.Millisecond {
			t.Errorf("turn %d was at %v, want it within a frame of %v", i, flips[i], w)
		}
	}
}

// Typing keeps a watched pane's cursor on, the same as any other.
//
// A key starts the phase again, so a cursor never reaches the off half
// while somebody is typing into it.
func TestTypingKeepsAWatchedPanesCursorOn(t *testing.T) {
	a, _, at := aWatchedWindowWatchingItsCursor(t, "\x1b[1 q")

	from := *at
	// A key every 150ms for two seconds, which is well inside the
	// half second the cursor would otherwise go out after.
	for step := 1; step <= 80; step++ {
		*at = from.Add(time.Duration(step) * 25 * time.Millisecond)
		if step%6 == 0 {
			a.handleKeys([]input.Event{input1('x')})
		}
		frame(t, a)
		if got := a.comp.Stats(); got.RowsDrawn > 0 && step%6 != 0 {
			t.Fatalf("the cursor's row was redrawn at %v with nothing typed on that frame,"+
				" so the cursor blinked while somebody was typing",
				time.Duration(step)*25*time.Millisecond)
		}
	}
}
