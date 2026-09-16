package render

import (
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/grid"
)

// blinkStart is a fixed hour, so a test reads as a time line rather than
// as arithmetic on whatever the clock said.
var blinkStart = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

// blinkStage is a compositor whose clock the test moves, with one layer
// carrying a blinking cursor on row 2.
func blinkStage(t *testing.T) (*Compositor, *Layer, *ebiten.Image, *time.Time) {
	t.Helper()
	c := NewCompositor(newTestRenderer(t))
	c.OnError = func(err error) { t.Errorf("compositor: %v", err) }
	at := blinkStart
	c.Now = func() time.Time { return at }
	l := &Layer{Grid: grid.New(20, 8, fg, bg)}
	l.Grid.SetString(0, 2, "hello", fg, bg, 0)
	l.Grid.SetCursor(grid.Cursor{X: 5, Y: 2, Visible: true, Blink: true})
	c.Add(l)
	return c, l, ebiten.NewImage(320, 240), &at
}

// A blinking cursor is on for half a second and off for the next half,
// and the frames in between cost nothing at all.
func TestABlinkingCursorGoesOnAndOffTwiceASecond(t *testing.T) {
	c, l, screen, at := blinkStage(t)

	c.Draw(screen)
	if !l.cursorShowing() {
		t.Fatal("the cursor is off on the frame it appeared; it should start on")
	}

	// Inside the on half nothing happens, so the frames are free.
	for _, d := range []time.Duration{100 * time.Millisecond, 200 * time.Millisecond} {
		*at = blinkStart.Add(d)
		c.Draw(screen)
		if got := c.Stats(); !got.Skipped || got.Repainted != 0 {
			t.Errorf("the frame at %v = %+v, want it skipped entirely", d, got)
		}
		if !l.cursorShowing() {
			t.Errorf("the cursor went off at %v, inside its on half", d)
		}
	}

	// Half way through, the cursor goes off. One row, and only one.
	*at = blinkStart.Add(600 * time.Millisecond)
	c.Draw(screen)
	off := c.Stats()
	if off.Skipped || off.Repainted != 1 || off.RowsDrawn != 1 {
		t.Fatalf("the frame at 600ms = %+v, want one layer repainting one row", off)
	}
	// Nothing moved, so the stack is blitted back over itself: wiping
	// the screen for a blink would cost the whole window.
	if off.Cleared {
		t.Error("the flip to off wiped the screen")
	}
	if l.cursorShowing() {
		t.Error("the cursor is still on 600ms in, which is the off half")
	}

	// And the off half is free too.
	*at = blinkStart.Add(700 * time.Millisecond)
	c.Draw(screen)
	if got := c.Stats(); !got.Skipped {
		t.Errorf("the frame at 700ms = %+v, want it skipped entirely", got)
	}

	// A second in it is back, and the row it is on is drawn again.
	*at = blinkStart.Add(time.Second)
	c.Draw(screen)
	on := c.Stats()
	if on.Skipped || on.Repainted != 1 || on.RowsDrawn != 1 {
		t.Fatalf("the frame at 1s = %+v, want one layer repainting one row", on)
	}
	if on.Cleared {
		t.Error("the flip back on wiped the screen")
	}
	if !l.cursorShowing() {
		t.Fatal("the cursor did not come back a second in")
	}
	// The same row drawn twice, once with the cursor's own quad and once
	// without it: that quad is the cursor being on screen.
	if on.Quads != off.Quads+1 {
		t.Errorf("the row drew %d quads with the cursor and %d without, want one more with",
			on.Quads, off.Quads)
	}
}

// The blink dirties the cursor's row when the phase turns over, and
// nothing at all in between.
func TestTheBlinkDirtiesOnlyTheCursorsRow(t *testing.T) {
	l := &Layer{Grid: grid.New(20, 8, fg, bg)}
	l.Grid.SetCursor(grid.Cursor{X: 4, Y: 3, Visible: true, Blink: true})
	l.stepCursorBlink(blinkStart)
	// Standing in for the repaint: the texture now holds the on half.
	l.noteCursorDrawn()
	l.Grid.ClearDirty()

	l.stepCursorBlink(blinkStart.Add(100 * time.Millisecond))
	if l.Grid.AnyDirty() {
		t.Fatal("a frame inside the on half dirtied a row")
	}

	l.stepCursorBlink(blinkStart.Add(600 * time.Millisecond))
	if !l.Grid.RowDirty(3) {
		t.Error("the cursor's row is clean, so the cursor would stay on screen")
	}
	for _, y := range []int{0, 1, 2, 4, 7} {
		if l.Grid.RowDirty(y) {
			t.Errorf("row %d was dirtied and the cursor is not on it", y)
		}
	}
}

// A steady cursor is the cursor as it was before any of this: drawn on
// every frame, and never a reason to repaint one.
func TestASteadyCursorNeverBlinks(t *testing.T) {
	c, l, screen, at := blinkStage(t)
	l.Grid.SetCursor(grid.Cursor{X: 5, Y: 2, Visible: true})

	c.Draw(screen)
	if !l.cursorShowing() {
		t.Fatal("a steady cursor was not drawn on the first frame")
	}
	for _, d := range []time.Duration{
		100 * time.Millisecond,
		600 * time.Millisecond,
		time.Second,
		3 * time.Second,
	} {
		*at = blinkStart.Add(d)
		c.Draw(screen)
		if got := c.Stats(); !got.Skipped || got.Repainted != 0 {
			t.Errorf("the frame at %v = %+v, want a steady cursor to cost nothing", d, got)
		}
		if !l.cursorShowing() {
			t.Errorf("a steady cursor went off at %v", d)
		}
	}
}

// A pane with no cursor on it has nothing to blink, which is what an
// unfocused terminal leaves behind: it places no cursor at all.
func TestAPaneWithNoCursorCostsNothing(t *testing.T) {
	c, l, screen, at := blinkStage(t)
	l.Grid.SetCursor(grid.Cursor{})

	c.Draw(screen)
	for _, d := range []time.Duration{600 * time.Millisecond, time.Second} {
		*at = blinkStart.Add(d)
		c.Draw(screen)
		if got := c.Stats(); !got.Skipped {
			t.Errorf("the frame at %v = %+v, want nothing to blink", d, got)
		}
	}
}

// Moving the cursor starts the phase again, so a cursor that has just
// jumped somewhere is on rather than caught mid-blink.
func TestMovingTheCursorPutsItBackOn(t *testing.T) {
	c, l, screen, at := blinkStage(t)
	c.Draw(screen)

	*at = blinkStart.Add(600 * time.Millisecond)
	c.Draw(screen)
	if l.cursorShowing() {
		t.Fatal("the cursor is on 600ms in, so this proves nothing")
	}

	l.Grid.SetCursor(grid.Cursor{X: 6, Y: 2, Visible: true, Blink: true})
	*at = blinkStart.Add(700 * time.Millisecond)
	c.Draw(screen)
	if !l.cursorShowing() {
		t.Fatal("the cursor stayed off after it moved")
	}

	// The phase now runs from the move, not from where it was.
	*at = blinkStart.Add(1100 * time.Millisecond)
	c.Draw(screen)
	if !l.cursorShowing() {
		t.Error("the cursor went off 400ms after it moved, so the phase did not start again")
	}
	*at = blinkStart.Add(1300 * time.Millisecond)
	c.Draw(screen)
	if l.cursorShowing() {
		t.Error("the cursor is still on 600ms after it moved")
	}
}

// A layer hidden in the off half of the blink comes back with the
// cursor on it. Its texture is kept while it is away, and that texture
// has no cursor in it.
func TestALayerHiddenInTheOffHalfComesBackWithItsCursor(t *testing.T) {
	c, l, screen, at := blinkStage(t)
	c.Draw(screen)

	*at = blinkStart.Add(600 * time.Millisecond)
	c.Draw(screen)
	if l.cursorShowing() {
		t.Fatal("the cursor is on 600ms in, so this proves nothing")
	}

	l.Hidden = true
	*at = blinkStart.Add(700 * time.Millisecond)
	c.Draw(screen)

	l.Hidden = false
	*at = blinkStart.Add(800 * time.Millisecond)
	c.Draw(screen)
	if !l.cursorShowing() {
		t.Fatal("the cursor did not come back with the layer")
	}
	if got := c.Stats(); got.Repainted != 1 || got.RowsDrawn != 1 {
		t.Errorf("the frame the layer came back on = %+v, want the cursor's row repainted; "+
			"the texture it was hidden with has no cursor in it", got)
	}
}

// A key puts the cursor back on the moment it is pressed, because the
// window cannot see a key from a layer.
func TestAKeyPutsTheCursorBackOn(t *testing.T) {
	c, l, screen, at := blinkStage(t)
	c.Draw(screen)

	*at = blinkStart.Add(600 * time.Millisecond)
	c.Draw(screen)
	if l.cursorShowing() {
		t.Fatal("the cursor is on 600ms in, so this proves nothing")
	}

	c.WakeCursors()
	if !l.cursorShowing() {
		t.Fatal("a key left the cursor off")
	}
	if !l.Grid.RowDirty(2) {
		t.Error("the cursor's row is clean, so the cursor would not come back until the next flip")
	}

	c.Draw(screen)
	*at = blinkStart.Add(900 * time.Millisecond)
	c.Draw(screen)
	if !l.cursorShowing() {
		t.Error("the cursor went off 300ms after the key, so the phase did not start again")
	}
}

// A layer hidden in the on half comes back with nothing to do: its
// texture already holds the cursor, so putting it back is a blit.
func TestALayerHiddenInTheOnHalfComesBackForFree(t *testing.T) {
	c, l, screen, at := blinkStage(t)
	c.Draw(screen)

	l.Hidden = true
	*at = blinkStart.Add(200 * time.Millisecond)
	c.Draw(screen)

	l.Hidden = false
	*at = blinkStart.Add(300 * time.Millisecond)
	c.Draw(screen)
	if !l.cursorShowing() {
		t.Fatal("the cursor came back off, and it was on when the layer went away")
	}
	if got := c.Stats(); got.Repainted != 0 {
		t.Errorf("the frame the layer came back on = %+v, want no repaint: "+
			"its texture already holds the cursor", got)
	}
}

// A layer given a different grid starts the phase again, even when the
// new grid's cursor happens to sit exactly where the old one did. The
// texture is repainted from scratch, and it is repainted with a cursor.
func TestANewGridStartsTheBlinkAgain(t *testing.T) {
	c, l, screen, at := blinkStage(t)
	c.Draw(screen)

	*at = blinkStart.Add(600 * time.Millisecond)
	c.Draw(screen)
	if l.cursorShowing() {
		t.Fatal("the cursor is on 600ms in, so this proves nothing")
	}

	fresh := grid.New(20, 8, fg, bg)
	fresh.SetString(0, 2, "hello", fg, bg, 0)
	fresh.SetCursor(l.Grid.Cursor())
	l.Grid = fresh
	*at = blinkStart.Add(700 * time.Millisecond)
	c.Draw(screen)
	if !l.cursorShowing() {
		t.Fatal("the new grid was painted with no cursor, and its phase came from the old one")
	}

	// The phase now runs from the swap.
	*at = blinkStart.Add(1100 * time.Millisecond)
	c.Draw(screen)
	if !l.cursorShowing() {
		t.Error("the cursor went off 400ms after the swap, so the phase did not start again")
	}
}
