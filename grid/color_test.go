package grid

import (
	"image/color"
	"math"
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

// A blend towards a darker colour rounds down, the way a blend towards a
// lighter one does.
//
// Go's own division rounds towards zero, which rounds a negative step
// up. Every dim cell on a dark ground moved by one when that went
// unnoticed.
func TestABlendTowardsADarkerColourRoundsDown(t *testing.T) {
	// 7 down to 0, 55 hundredths of the way, is 3.15. Rounding down
	// gives 3; rounding towards zero gives 4.
	bright := color.RGBA{R: 7, G: 7, B: 7, A: 255}
	dark := color.RGBA{A: 255}

	want := color.RGBA{R: 3, G: 3, B: 3, A: 255}
	if got := Blend(bright, dark, 55, 100); got != want {
		t.Errorf("Blend(%v, %v, 55, 100) = %v, want %v", bright, dark, got, want)
	}
}

// Dimming is what the renderer does with a blend, and it has to give the
// same colour it gave before the mixer was shared: the float mixer it
// replaced rounded down at every step.
func TestABlendMatchesTheFloatMixerItReplaced(t *testing.T) {
	const t55 = 0.55
	for x := 0; x < 256; x++ {
		for y := 0; y < 256; y++ {
			from := color.RGBA{R: uint8(x), A: 255}
			to := color.RGBA{R: uint8(y), A: 255}
			want := uint8(float64(x)*(1-t55) + float64(y)*t55)
			if got := Blend(from, to, 55, 100).R; got != want {
				t.Fatalf("%d blended 55/100 towards %d is %d, want %d", x, y, got, want)
			}
		}
	}
}

// The two ends of the contrast scale, and the rule that it does not
// matter which way round the two colours are given.
func TestContrast(t *testing.T) {
	black := color.RGBA{A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	if got := Contrast(black, white); math.Abs(got-21) > 0.01 {
		t.Errorf("black against white is %.4f:1, want 21", got)
	}
	if got := Contrast(white, black); math.Abs(got-21) > 0.01 {
		t.Errorf("white against black is %.4f:1, want the same 21 either way round", got)
	}
	for _, c := range []color.RGBA{black, white, {R: 224, G: 108, B: 117, A: 255}} {
		if got := Contrast(c, c); math.Abs(got-1) > 0.0001 {
			t.Errorf("%v against itself is %.4f:1, want 1", c, got)
		}
	}
	// Symmetric for any pair, not only for the ends.
	one := color.RGBA{R: 97, G: 175, B: 239, A: 255}
	two := color.RGBA{R: 28, G: 32, B: 38, A: 255}
	if a, b := Contrast(one, two), Contrast(two, one); a != b {
		t.Errorf("the pair reads %.4f:1 one way and %.4f:1 the other", a, b)
	}
	// Alpha is ignored: a see-through colour is measured as if it were
	// opaque, which is why a blend comes first.
	clear := one
	clear.A = 0
	if got, want := Contrast(clear, two), Contrast(one, two); got != want {
		t.Errorf("with no alpha it reads %.4f:1 and with full alpha %.4f:1, want alpha ignored", got, want)
	}
}
