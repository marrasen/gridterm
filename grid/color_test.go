package grid

import (
	"image/color"
	"testing"
)

// A blend runs from one colour to the other, and every channel goes with
// it.
func TestBlendRunsFromOneColourToTheOther(t *testing.T) {
	black := color.RGBA{A: 0}
	white := color.RGBA{R: 200, G: 200, B: 200, A: 100}

	if got := Blend(black, white, 0, 4); got != black {
		t.Errorf("no way along the blend is %v, want the first colour", got)
	}
	if got := Blend(black, white, 4, 4); got != white {
		t.Errorf("all the way along the blend is %v, want the second colour", got)
	}
	want := color.RGBA{R: 100, G: 100, B: 100, A: 50}
	if got := Blend(black, white, 2, 4); got != want {
		t.Errorf("halfway along the blend is %v, want %v: alpha is mixed too", got, want)
	}
}

// A blend over no distance is the colour it starts at, which is what a
// list of one row asks for.
func TestABlendOverNoDistanceIsWhereItStarts(t *testing.T) {
	from := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	to := color.RGBA{R: 200, A: 255}

	for _, of := range []int{0, -1} {
		if got := Blend(from, to, 1, of); got != from {
			t.Errorf("a blend of %d steps is %v, want %v", of, got, from)
		}
	}
}
