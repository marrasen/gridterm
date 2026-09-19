package render

import (
	"image"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/grid"
)

// A layer carrying a picture blits it, and an idle frame after that is
// skipped.
func TestAPictureIsBlittedAndThenLeftAlone(t *testing.T) {
	c := NewCompositor(newTestRenderer(t))
	screen := ebiten.NewImage(320, 240)
	l := &Layer{
		Grid:    grid.New(20, 8, fg, bg),
		Picture: &Picture{Img: ebiten.NewImage(40, 20), Rect: image.Rect(0, 16, 160, 96)},
	}
	c.Add(l)

	c.Draw(screen)
	if got := c.Stats(); got.Skipped || got.Blits != 2 {
		t.Errorf("first frame = %+v, want the layer and its picture blitted", got)
	}

	c.Draw(screen)
	if got := c.Stats(); !got.Skipped {
		t.Errorf("the frame after = %+v, want it skipped: nothing changed", got)
	}
}

// A different picture on the same layer is drawn, although no grid
// changed.
func TestANewPictureIsDrawn(t *testing.T) {
	c := NewCompositor(newTestRenderer(t))
	screen := ebiten.NewImage(320, 240)
	l := &Layer{
		Grid:    grid.New(20, 8, fg, bg),
		Picture: &Picture{Img: ebiten.NewImage(40, 20), Rect: image.Rect(0, 16, 160, 96)},
	}
	c.Add(l)
	c.Draw(screen)

	l.Picture.Img = ebiten.NewImage(30, 30)
	c.Draw(screen)

	if got := c.Stats(); got.Skipped {
		t.Error("a frame showing a new picture was skipped")
	}
}

// A picture moved into a different box is drawn there.
func TestAPictureMovedIsDrawnAgain(t *testing.T) {
	c := NewCompositor(newTestRenderer(t))
	screen := ebiten.NewImage(320, 240)
	l := &Layer{
		Grid:    grid.New(20, 8, fg, bg),
		Picture: &Picture{Img: ebiten.NewImage(40, 20), Rect: image.Rect(0, 16, 160, 96)},
	}
	c.Add(l)
	c.Draw(screen)

	l.Picture.Rect = image.Rect(0, 16, 120, 96)
	c.Draw(screen)

	if got := c.Stats(); got.Skipped {
		t.Error("a frame whose picture moved was skipped")
	}
}

// A picture taken away wipes the screen, or its pixels would stay where
// they were for good.
func TestAPictureTakenAwayWipesTheScreen(t *testing.T) {
	c := NewCompositor(newTestRenderer(t))
	screen := ebiten.NewImage(320, 240)
	l := &Layer{
		Grid:    grid.New(20, 8, fg, bg),
		Picture: &Picture{Img: ebiten.NewImage(40, 20), Rect: image.Rect(0, 16, 160, 96)},
	}
	c.Add(l)
	c.Draw(screen)

	l.Picture = nil
	c.Draw(screen)

	got := c.Stats()
	if got.Skipped {
		t.Fatal("the frame that lost the picture was skipped")
	}
	if !got.Cleared {
		t.Error("the picture went and the screen was not wiped")
	}
}

// Hiding a layer takes its picture off screen too.
func TestHidingALayerTakesItsPictureAway(t *testing.T) {
	c := NewCompositor(newTestRenderer(t))
	screen := ebiten.NewImage(320, 240)
	l := &Layer{
		Grid:    grid.New(20, 8, fg, bg),
		Picture: &Picture{Img: ebiten.NewImage(40, 20), Rect: image.Rect(0, 16, 160, 96)},
	}
	c.Add(l)
	c.Draw(screen)

	l.Hidden = true
	c.Draw(screen)
	got := c.Stats()
	if got.Skipped || got.Blits != 0 {
		t.Errorf("hiding the layer gave %+v, want the screen put back with nothing on it", got)
	}
	if !got.Cleared {
		t.Error("the picture was hidden and the screen was not wiped, so its pixels stay")
	}

	// And it comes back with it.
	l.Hidden = false
	c.Draw(screen)
	if got := c.Stats(); got.Skipped || got.Blits != 2 {
		t.Errorf("showing it again gave %+v, want the layer and its picture", got)
	}
}

// A picture can go on a layer with no grid of its own, which is how one
// is drawn over a pane the window painted itself.
func TestAPictureNeedsNoGrid(t *testing.T) {
	c := NewCompositor(newTestRenderer(t))
	screen := ebiten.NewImage(320, 240)
	under := &Layer{Grid: grid.New(20, 8, fg, bg)}
	over := &Layer{Picture: &Picture{Img: ebiten.NewImage(40, 20), Rect: image.Rect(8, 8, 80, 80)}}
	c.Add(under)
	c.Add(over)

	c.Draw(screen)

	if got := c.Stats(); got.Skipped || got.Blits != 2 {
		t.Errorf("first frame = %+v, want the grid and the picture", got)
	}
}

// A picture that draws nothing is left alone rather than reaching the
// screen.
func TestAnEmptyPictureDrawsNothing(t *testing.T) {
	for what, p := range map[string]*Picture{
		"nil":               nil,
		"no image":          {Rect: image.Rect(0, 0, 40, 40)},
		"no box":            {Img: ebiten.NewImage(40, 20)},
		"an inside-out box": {Img: ebiten.NewImage(40, 20), Rect: image.Rectangle{Min: image.Pt(40, 40)}},
	} {
		c := NewCompositor(newTestRenderer(t))
		screen := ebiten.NewImage(320, 240)
		l := &Layer{Grid: grid.New(20, 8, fg, bg), Picture: p}
		c.Add(l)

		c.Draw(screen)

		if got := c.Stats(); got.Blits != 1 {
			t.Errorf("%s: blitted %d, want only the layer itself", what, got.Blits)
		}
	}
}

// A picture is shrunk to fit its box, never blown up, and centred in it
// on whole pixels.
func TestFitInto(t *testing.T) {
	for what, tc := range map[string]struct {
		src, box  image.Rectangle
		wantScale float64
		wantAt    image.Point
	}{
		"smaller than the box": {
			src: image.Rect(0, 0, 40, 20), box: image.Rect(0, 0, 100, 100),
			wantScale: 1, wantAt: image.Pt(30, 40),
		},
		"wider than the box": {
			src: image.Rect(0, 0, 200, 100), box: image.Rect(0, 0, 100, 100),
			wantScale: 0.5, wantAt: image.Pt(0, 25),
		},
		"taller than the box": {
			src: image.Rect(0, 0, 100, 200), box: image.Rect(0, 0, 100, 100),
			wantScale: 0.5, wantAt: image.Pt(25, 0),
		},
		"the box moved": {
			src: image.Rect(0, 0, 40, 20), box: image.Rect(10, 10, 110, 110),
			wantScale: 1, wantAt: image.Pt(40, 50),
		},
		"an odd leftover lands on a whole pixel": {
			src: image.Rect(0, 0, 40, 20), box: image.Rect(0, 0, 41, 21),
			wantScale: 1, wantAt: image.Pt(0, 0),
		},
		"a picture measured from its own corner": {
			src: image.Rect(7, 7, 47, 27), box: image.Rect(0, 0, 100, 100),
			wantScale: 1, wantAt: image.Pt(30, 40),
		},
	} {
		scale, at := fitInto(tc.src, tc.box)
		if scale != tc.wantScale || at != tc.wantAt {
			t.Errorf("%s: fitInto(%v, %v) = %v at %v, want %v at %v",
				what, tc.src, tc.box, scale, at, tc.wantScale, tc.wantAt)
		}
	}
}

// Whatever the box, the picture lands inside it.
func TestAPictureLandsInsideItsBox(t *testing.T) {
	box := image.Rect(5, 9, 105, 69)
	for _, src := range []image.Rectangle{
		image.Rect(0, 0, 1, 1),
		image.Rect(0, 0, 4000, 10),
		image.Rect(0, 0, 10, 4000),
		image.Rect(0, 0, 99, 59),
		image.Rect(0, 0, 101, 61),
	} {
		scale, at := fitInto(src, box)
		w := int(float64(src.Dx())*scale + 0.5)
		h := int(float64(src.Dy())*scale + 0.5)
		if at.X < box.Min.X || at.Y < box.Min.Y || at.X+w > box.Max.X || at.Y+h > box.Max.Y {
			t.Errorf("a picture of %v landed at %v and is %d by %d, outside %v", src, at, w, h, box)
		}
	}
}
