package main

import (
	"testing"
	"time"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui/term"
)

// blinkStart is a fixed hour, so a test reads as a time line rather than
// as arithmetic on whatever the clock said.
var blinkStart = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

// aWindowWatchingItsCursor is a window drawing a pane, with the clock
// the cursor blinks on in the test's hands.
func aWindowWatchingItsCursor(t *testing.T, seq string) (*testApp, *term.Terminal, *time.Time) {
	t.Helper()
	a := newTestApp(t, 40, 12)
	withScreen(t, a)
	pane := onlyPaneOn(t, a)
	at := blinkStart
	a.comp.Now = func() time.Time { return at }

	a.shells[0].out <- []byte(seq + "hello")
	waitFor(t, a, "the shell to say something", func() bool {
		return screenOf(pane).At(0, 0).Rune == 'h'
	})
	// Three: the first frame puts the stack up, the second settles it,
	// and the third is the one that proves an idle window is idle.
	frame(t, a)
	frame(t, a)
	frame(t, a)
	if got := a.comp.Stats(); !got.Skipped {
		t.Fatalf("the window has not settled: %+v", got)
	}
	return a, pane, &at
}

// repaintsOverASecond runs a second of frames, twenty a second, and
// counts the ones that were not skipped.
func repaintsOverASecond(t *testing.T, a *testApp, at *time.Time) int {
	t.Helper()
	from := *at
	painted := 0
	for step := 1; step <= 20; step++ {
		*at = from.Add(time.Duration(step) * 50 * time.Millisecond)
		frame(t, a)
		got := a.comp.Stats()
		if got.Skipped {
			continue
		}
		painted++
		if got.Repainted != 1 || got.RowsDrawn != 1 {
			t.Errorf("a frame at %v repainted %d layers and %d rows, want one layer and only the cursor's row",
				time.Duration(step)*50*time.Millisecond, got.Repainted, got.RowsDrawn)
		}
	}
	return painted
}

// DECSCUSR 1 asks for a blinking block, and the window blinks it: two
// one-row repaints a second on a window where nothing else is going on.
func TestAShellCanAskForABlinkingCursor(t *testing.T) {
	a, pane, at := aWindowWatchingItsCursor(t, "\x1b[1 q")

	if !screenOf(pane).Cursor().Blink {
		t.Fatal("the shell asked for a blinking cursor and the pane has a steady one")
	}
	if !a.g.Cursor().Blink {
		t.Fatal("the pane's blinking cursor did not reach the window's grid")
	}

	if got := repaintsOverASecond(t, a, at); got != 2 {
		t.Errorf("a second of idle frames repainted %d times, want two: on and off", got)
	}
}

// DECSCUSR 2 asks for a steady block, and an idle window then costs
// nothing at all.
func TestAShellCanAskForASteadyCursor(t *testing.T) {
	a, pane, at := aWindowWatchingItsCursor(t, "\x1b[2 q")

	if screenOf(pane).Cursor().Blink {
		t.Fatal("the shell asked for a steady cursor and the pane has a blinking one")
	}

	if got := repaintsOverASecond(t, a, at); got != 0 {
		t.Errorf("a second of idle frames repainted %d times, want none at all", got)
	}
}

// Typing puts the cursor back on, so somebody at the keyboard is never
// looking at a window that seems to have stopped answering.
func TestAKeyPutsTheBlinkingCursorBackOn(t *testing.T) {
	a, _, at := aWindowWatchingItsCursor(t, "\x1b[1 q")

	// Half a second in it goes off.
	*at = blinkStart.Add(600 * time.Millisecond)
	frame(t, a)
	if a.comp.Stats().Skipped {
		t.Fatal("the cursor did not go off half a second in, so this proves nothing")
	}
	*at = blinkStart.Add(700 * time.Millisecond)
	frame(t, a)
	if !a.comp.Stats().Skipped {
		t.Fatal("the window is not idle in the off half")
	}

	a.handleKeys([]input.Event{input1('x')})

	frame(t, a)
	if a.comp.Stats().Skipped {
		t.Error("the frame after the key was skipped, so the cursor stayed off")
	}
	// And the phase now runs from the key: it would otherwise have
	// turned over again a second after the cursor first appeared.
	for _, d := range []time.Duration{900 * time.Millisecond, 1100 * time.Millisecond} {
		*at = blinkStart.Add(d)
		frame(t, a)
		if got := a.comp.Stats(); !got.Skipped {
			t.Errorf("the frame at %v = %+v, want the cursor still on after the key", d, got)
		}
	}
	*at = blinkStart.Add(1250 * time.Millisecond)
	frame(t, a)
	if a.comp.Stats().Skipped {
		t.Error("the cursor never went off again, so it stopped blinking after the key")
	}
}

// Letting go of a key is not typing, so it does not put the cursor back
// on: a window nobody is at goes on blinking to its own clock.
func TestAKeyReleaseDoesNotWakeTheCursor(t *testing.T) {
	a, _, at := aWindowWatchingItsCursor(t, "\x1b[1 q")

	*at = blinkStart.Add(600 * time.Millisecond)
	frame(t, a)
	if a.comp.Stats().Skipped {
		t.Fatal("the cursor did not go off half a second in, so this proves nothing")
	}

	*at = blinkStart.Add(700 * time.Millisecond)
	a.handleKeys([]input.Event{{Kind: input.KeyRelease, Key: input.KeyX}})

	frame(t, a)
	if !a.comp.Stats().Skipped {
		t.Error("a key coming back up woke the cursor")
	}
	// The phase is still the one the cursor started with, so it turns
	// over a second in rather than half a second after the release.
	*at = blinkStart.Add(1050 * time.Millisecond)
	frame(t, a)
	if a.comp.Stats().Skipped {
		t.Error("the cursor did not come back a second in, so the release moved the phase")
	}
}
