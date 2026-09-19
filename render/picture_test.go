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
	if got := c.Stats(); got.Skipped || got.Blits != 0 {
		t.Errorf("hiding the layer gave %+v, want the screen put back with nothing on it", got)
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
