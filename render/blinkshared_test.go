package render

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/grid"
)

// sharedBlinkStage is blinkStage with the border a shared pane carries:
// a second layer whose rule changes colour every frame, the way the glow
// does. The border is a reason to draw on its own, so no frame is ever
// skipped while it is there.
func sharedBlinkStage(t *testing.T) (*Compositor, *Layer, *Layer, *ebiten.Image, *time.Time) {
	t.Helper()
	c := NewCompositor(newTestRenderer(t))
	c.OnError = func(err error) { t.Errorf("compositor: %v", err) }
	at := blinkStart
	c.Now = func() time.Time { return at }
	l := &Layer{Grid: grid.New(20, 8, fg, bg)}
	l.Grid.SetString(0, 2, "hello", fg, bg, 0)
	l.Grid.SetCursor(grid.Cursor{X: 5, Y: 2, Visible: true, Blink: true})
	c.Add(l)
	border := &Layer{Strokes: []Stroke{{
		Rect: image.Rect(0, 0, 320, 240), Width: 2, Colour: color.RGBA{0, 0, 0xff, 0x50},
	}}}
	c.Add(border)
	return c, l, border, ebiten.NewImage(320, 240), &at
}

// glow moves the border's colour on, which is what one step of the
// shared pane's pulse does.
func glow(border *Layer, step uint8) { border.Strokes[0].Colour.A = 0x40 + step }

// The cursor keeps its half-second rhythm while a shared pane's border
// is glowing beside it.
//
// The glow is a reason to draw every frame, so the compositor stops
// skipping. The blink has to come from its own phase rather than from
// which frames happened to be drawn.
func TestTheBlinkKeepsItsRhythmBesideAGlowingBorder(t *testing.T) {
	c, l, border, screen, at := sharedBlinkStage(t)

	// A frame every 16ms for two full turns of the blink, with the glow
	// moving on each one.
	var flips []time.Duration
	was := true
	for ms := 0; ms <= 2000; ms += 16 {
		*at = blinkStart.Add(time.Duration(ms) * time.Millisecond)
		glow(border, uint8(ms/16%0x40))
		c.Draw(screen)
		if now := l.cursorShowing(); now != was {
			flips = append(flips, time.Duration(ms)*time.Millisecond)
			was = now
		}
	}

	// On at 0, off at 500, on at 1000, off at 1500, on at 2000: four
	// changes, each within a frame of the half second.
	want := []time.Duration{500, 1000, 1500, 2000}
	if len(flips) != len(want) {
		t.Fatalf("the cursor changed at %v, want four changes near %v", flips, want)
	}
	for i, w := range want {
		off := flips[i] - w*time.Millisecond
		if off < 0 || off > 16*time.Millisecond {
			t.Errorf("change %d was at %v, want it within a frame of %v",
				i, flips[i], w*time.Millisecond)
		}
	}
}

// The texture holds whichever half the phase says, on every frame, while
// the border glows.
//
// The blink dirties the cursor's row and the repaint puts the right half
// in the texture. A frame drawn for the glow alone must not leave the
// two disagreeing, or the cursor is missing until something else dirties
// that row.
func TestTheDrawnHalfKeepsUpWithTheBlinkBesideABorder(t *testing.T) {
	c, l, border, screen, at := sharedBlinkStage(t)

	for ms := 0; ms <= 2000; ms += 16 {
		*at = blinkStart.Add(time.Duration(ms) * time.Millisecond)
		glow(border, uint8(ms/16%0x40))
		c.Draw(screen)
		if l.blink.shown != l.cursorShowing() {
			t.Fatalf("at %dms the texture holds shown=%v while the phase says %v",
				ms, l.blink.shown, l.cursorShowing())
		}
	}
}

// Typing keeps the cursor on while a shared pane's border glows.
//
// A key wakes the cursor and starts its phase again, so a burst of
// typing leaves it solid. The glow must not let the phase run on
// underneath and put the cursor out mid-word.
func TestTypingKeepsTheCursorOnBesideAGlowingBorder(t *testing.T) {
	c, l, border, screen, at := sharedBlinkStage(t)

	// A key every 120ms for two seconds, which is faster than the blink.
	for ms := 0; ms <= 2000; ms += 16 {
		*at = blinkStart.Add(time.Duration(ms) * time.Millisecond)
		glow(border, uint8(ms/16%0x40))
		if ms%120 == 0 {
			c.WakeCursors()
		}
		c.Draw(screen)
		if !l.cursorShowing() {
			t.Fatalf("the cursor went out at %dms while somebody was typing", ms)
		}
	}
}
