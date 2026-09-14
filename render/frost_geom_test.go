package render

import (
	"image"
	"testing"
)

// The frost arithmetic is pulled out of drawFrost because nothing else
// can check it. Outside ebiten's game loop a draw only queues a command
// that is never run, so a test can build a frosted layer, call Draw, and
// learn only that the call did not panic. These are the numbers that
// decide whether the glass lands on the dialog or beside it.

func TestFrostRegionSurroundsThePanel(t *testing.T) {
	screen := image.Rect(0, 0, 800, 600)
	panel := image.Rect(300, 200, 500, 400)

	src, _, step := frostRegion(panel, 8, screen)

	if step <= 0 {
		t.Fatalf("step = %v, want a blur", step)
	}
	if !src.In(screen) {
		t.Errorf("src %v is not inside the screen %v", src, screen)
	}
	if !panel.In(src) {
		t.Errorf("src %v does not cover the panel %v", src, panel)
	}
	// The margin has to be at least as far as the blur reaches, or the
	// taps clamp against the edge and smear it outward.
	reach := int(step * blurTaps)
	if got := panel.Min.X - src.Min.X; got < reach {
		t.Errorf("margin is %d pixels, want at least the %d the blur reaches", got, reach)
	}
	if got := src.Max.Y - panel.Max.Y; got < reach {
		t.Errorf("margin is %d pixels, want at least the %d the blur reaches", got, reach)
	}
}

// TestFrostRegionAtTheScreenEdge checks a panel with nothing to widen
// into. The region is clipped rather than running off the screen, and
// the offset the caller derives from it stays right.
func TestFrostRegionAtTheScreenEdge(t *testing.T) {
	screen := image.Rect(0, 0, 800, 600)
	panel := image.Rect(0, 0, 200, 100)

	src, inner, _ := frostRegion(panel, 8, screen)

	if !src.In(screen) {
		t.Errorf("src %v runs off the screen %v", src, screen)
	}
	if !panel.In(src) {
		t.Errorf("src %v does not cover the panel %v", src, panel)
	}
	// The caller reads the blurred panel back at this offset.
	if inner.Min.X < 0 || inner.Min.Y < 0 {
		t.Errorf("the panel sits at %v inside the region, which is behind its origin", inner.Min)
	}
	if inner.Size() != panel.Size() {
		t.Errorf("the panel is %v inside the region but %v on screen", inner.Size(), panel.Size())
	}
}

// TestFrostRegionWithNoBlur checks that a radius of zero means no blur,
// as the field says, rather than a four-pixel smear of the panel's own
// edge.
func TestFrostRegionWithNoBlur(t *testing.T) {
	screen := image.Rect(0, 0, 800, 600)
	panel := image.Rect(300, 200, 500, 400)

	src, inner, step := frostRegion(panel, 0, screen)

	if step != 0 {
		t.Errorf("step = %v, want no blur", step)
	}
	if src != panel {
		t.Errorf("src = %v, want just the panel %v", src, panel)
	}
	if want := image.Rect(0, 0, panel.Dx(), panel.Dy()); inner != want {
		t.Errorf("inner = %v, want %v", inner, want)
	}
}

// TestFrostRegionInnerIsWhereThePanelLands pins the offset the blurred
// panel is read back at, with numbers worked out by hand. Getting it
// wrong draws the glass shifted by the padding, which is the kind of
// fault that only shows up on screen.
func TestFrostRegionInnerIsWhereThePanelLands(t *testing.T) {
	screen := image.Rect(0, 0, 800, 600)

	// Radius 8: the taps sit 2 apart and reach 8, so the region is the
	// panel grown by 8 on every side and the panel starts 8 in.
	_, inner, step := frostRegion(image.Rect(300, 200, 500, 400), 8, screen)
	if step != 2 {
		t.Fatalf("step = %v, want 2", step)
	}
	if want := image.Rect(8, 8, 208, 208); inner != want {
		t.Errorf("inner = %v, want %v", inner, want)
	}

	// Against the top-left corner there is no room to grow into, so the
	// panel starts at the region's own origin.
	_, inner, _ = frostRegion(image.Rect(0, 0, 200, 200), 8, screen)
	if want := image.Rect(0, 0, 200, 200); inner != want {
		t.Errorf("at the corner, inner = %v, want %v", inner, want)
	}

	// Against the right edge it is padded on the left but clipped on the
	// right, so the offset survives on one axis and not the other.
	_, inner, _ = frostRegion(image.Rect(600, 0, 800, 200), 8, screen)
	if want := image.Rect(8, 0, 208, 200); inner != want {
		t.Errorf("at the right edge, inner = %v, want %v", inner, want)
	}
}

// TestFrostRegionMarginMatchesTheReach is the check that caught the
// padding being four times the blur: the region is widened by what the
// taps actually reach, not by the radius times the tap count.
func TestFrostRegionMarginMatchesTheReach(t *testing.T) {
	screen := image.Rect(0, 0, 4000, 4000)
	panel := image.Rect(1000, 1000, 1200, 1200)

	for _, radius := range []float32{1, 2, 7, 16, 40} {
		src, _, step := frostRegion(panel, radius, screen)
		reach := step * blurTaps
		margin := float64(panel.Min.X - src.Min.X)
		if margin < reach {
			t.Errorf("radius %v: margin %v is short of the %v reach", radius, margin, reach)
		}
		if margin > reach+1 {
			t.Errorf("radius %v: margin %v is wider than the %v reach, which is blurred for nothing",
				radius, margin, reach)
		}
	}
}

// TestFrostCornerFitsInThePanel checks the clamp. The rounded-box
// distance field is only a distance while the radius fits inside the
// box; past that the panel draws as a lens instead of a rectangle.
func TestFrostCornerFitsInThePanel(t *testing.T) {
	for _, tc := range []struct {
		corner float32
		panel  image.Rectangle
		want   float32
	}{
		{12, image.Rect(0, 0, 200, 100), 12}, // room to spare
		{12, image.Rect(0, 0, 200, 20), 10},  // held to half the short side
		{12, image.Rect(0, 0, 7, 15), 3.5},   // a one-cell dialog
		{-5, image.Rect(0, 0, 200, 100), 0},  // never negative
		{1000, image.Rect(0, 0, 200, 100), 50},
	} {
		if got := frostCorner(tc.corner, tc.panel); got != tc.want {
			t.Errorf("frostCorner(%v, %v) = %v, want %v", tc.corner, tc.panel, got, tc.want)
		}
	}
}
