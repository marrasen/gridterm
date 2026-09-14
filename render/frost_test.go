package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// TestFrostShadersCompile is the tripwire for the Kage sources. They are
// strings, so nothing else in the build would notice a typo in one until
// a dialog opened.
func TestFrostShadersCompile(t *testing.T) {
	var s shaders

	if err := s.compile(); err != nil {
		t.Fatalf("compile: %v", err)
	}

	if s.blur == nil || s.frost == nil {
		t.Fatal("compile reported success and left a shader nil")
	}
	// Asking again must not build them twice.
	blur, frost := s.blur, s.frost
	if err := s.compile(); err != nil {
		t.Fatalf("compile again: %v", err)
	}
	if s.blur != blur || s.frost != frost {
		t.Error("the shaders were rebuilt on the second call")
	}
}

func TestAnyFrosted(t *testing.T) {
	c := NewCompositor(nil)
	plain := &Layer{Grid: grid.New(4, 2, fg, bg)}
	c.Add(plain)

	if c.anyFrosted() {
		t.Error("a layer with no frost reported frosted")
	}

	glass := &Layer{
		Grid:  grid.New(4, 2, fg, bg),
		Frost: &Frost{Rect: image.Rect(0, 0, 10, 10)},
	}
	c.Add(glass)
	if !c.anyFrosted() {
		t.Error("a frosted layer was not noticed")
	}

	// A panel with no area is nothing to draw.
	glass.Frost.Rect = image.Rectangle{}
	if c.anyFrosted() {
		t.Error("an empty panel counted as frosted")
	}

	// And one nobody can see costs nothing.
	glass.Frost.Rect = image.Rect(0, 0, 10, 10)
	glass.Hidden = true
	if c.anyFrosted() {
		t.Error("a hidden layer forced the screen to be rebuilt")
	}
}

func TestRGBAToFloats(t *testing.T) {
	got := rgbaToFloats(color.RGBA{0x00, 0x80, 0xff, 0xff})

	want := []float32{0, 128.0 / 255, 1, 1}
	if len(got) != 4 {
		t.Fatalf("got %d components, want 4", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("component %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestScratchGrowsAndIsKept checks the pair of offscreen images the blur
// bounces between. A dialog changes size as it is typed into, so
// reallocating on every change would allocate once a keystroke.
func TestScratchGrowsAndIsKept(t *testing.T) {
	var s scratch

	s.ensure(40, 20)
	first := s.a
	if s.a == nil || s.b == nil {
		t.Fatal("ensure left an image nil")
	}
	if b := s.a.Bounds(); b.Dx() < 40 || b.Dy() < 20 {
		t.Errorf("image is %v, want at least 40x20", b)
	}

	// Smaller is still big enough.
	s.ensure(10, 5)
	if s.a != first {
		t.Error("a smaller panel reallocated the scratch")
	}

	// Bigger has to grow, and must keep room for what came before.
	s.ensure(10, 60)
	if s.a == first {
		t.Fatal("a taller panel did not grow the scratch")
	}
	if b := s.a.Bounds(); b.Dx() < 40 || b.Dy() < 60 {
		t.Errorf("image is %v, want at least 40x60: growing lost a dimension", b)
	}
}

// TestScratchZeroSizeIsSafe checks the degenerate case: ebiten refuses a
// zero-sized image.
func TestScratchZeroSizeIsSafe(t *testing.T) {
	var s scratch

	s.ensure(0, 0)

	if s.a == nil || s.b == nil {
		t.Fatal("ensure left an image nil")
	}
	if b := s.a.Bounds(); b.Dx() < 1 || b.Dy() < 1 {
		t.Errorf("image is %v, want at least 1x1", b)
	}
}
