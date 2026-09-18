package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/grid"
)

// The corner radius the shader is given is held to what the box can
// take. Past half the shorter side the rounded-box distance field stops
// being a distance, and the rule draws as a lens instead of a border.
func TestARulesCornerIsHeldToItsBox(t *testing.T) {
	// A box far smaller than the radius asked for: a pane one cell wide
	// with the cell's own size as the corner.
	box := image.Rect(10, 10, 17, 40)
	s := &Stroke{Rect: box, Corner: 9, Width: 2, Colour: color.RGBA{A: 0xff}}

	got, ok := strokeUniforms(box, s)["Corner"].(float32)

	if !ok {
		t.Fatalf("the corner is %T, want a float32", strokeUniforms(box, s)["Corner"])
	}
	if want := float32(3.5); got != want {
		t.Errorf("a corner of %v on a %v box reaches the shader as %v, want %v",
			s.Corner, box, got, want)
	}
}

// A corner that already fits is passed through.
func TestACornerThatFitsIsLeftAlone(t *testing.T) {
	box := image.Rect(0, 0, 100, 60)
	s := &Stroke{Rect: box, Corner: 8, Width: 2, Colour: color.RGBA{A: 0xff}}

	if got := strokeUniforms(box, s)["Corner"]; got != float32(8) {
		t.Errorf("a corner of 8 on a %v box reaches the shader as %v, want 8", box, got)
	}
}

// Once a shader has failed to compile, nothing tries again.
//
// A shader that will not compile will not compile on the next frame
// either, and retrying means building and throwing away the other
// programs on every frame for as long as the window is open.
func TestAFailedShaderIsNotRetried(t *testing.T) {
	c := NewCompositor(newTestRenderer(t))
	l := &Layer{Strokes: []Stroke{{
		Rect: image.Rect(0, 0, 40, 20), Width: 2, Colour: color.RGBA{A: 0xff},
	}}}
	c.Add(l)
	c.shaderFailed = true

	c.Draw(ebiten.NewImage(320, 240))

	if got := c.Stats().Strokes; got != 0 {
		t.Errorf("it drew %d rules after the shader failed, want none", got)
	}
}

// A rule whose size is not a number draws nothing.
//
// A NaN never compares equal to itself, so one that reached the layer
// would read as a change on every frame and the compositor would never
// skip one again.
func TestARuleThatIsNotANumberDrawsNothing(t *testing.T) {
	nan := float32(math.NaN())
	box := image.Rect(0, 0, 40, 20)
	for what, s := range map[string]*Stroke{
		"a width that is not a number":  {Rect: box, Width: nan, Colour: color.RGBA{A: 0xff}},
		"a corner that is not a number": {Rect: box, Width: 2, Corner: nan, Colour: color.RGBA{A: 0xff}},
	} {
		if !s.Empty() {
			t.Errorf("a rule with %s reads as something to draw", what)
		}
	}
}

// Once a shader has failed to compile, the glass does not try again
// either.
func TestAFailedShaderLeavesTheGlassAlone(t *testing.T) {
	c := NewCompositor(newTestRenderer(t))
	l := &Layer{
		Grid:  grid.New(4, 2, fg, bg),
		Frost: &Frost{Rect: image.Rect(0, 0, 40, 20), Radius: 4, Corner: 4},
	}
	c.Add(l)
	c.shaderFailed = true

	c.Draw(ebiten.NewImage(320, 240))

	if got := c.Stats().Frosted; got != 0 {
		t.Errorf("it drew %d panels of glass after the shader failed, want none", got)
	}
}
