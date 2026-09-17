package render

import (
	"image/color"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/grid"
)

// quad is where one pushed rectangle lands on the screen, and what it
// samples from the atlas.
type quad struct {
	x0, y0, x1, y1 float32
	sx0, sy0       float32
	sx1, sy1       float32
}

// glyphQuads are the rectangles drawing one character queues, in a grid
// whose columns and rows are padded as the table says.
func glyphQuads(t *testing.T, ch rune, colPad, rowPad grid.Pad) ([]quad, *Geometry) {
	t.Helper()
	r := newTestRenderer(t)
	g := grid.New(4, 3, fg, bg)
	// On column 1 and row 1, so there is a cell either side and the gaps
	// are the ones the table asks for rather than the grid's edge.
	g.SetColPad(1, colPad)
	g.SetRowPad(1, rowPad)
	geo := &Geometry{}
	r.Measure(g, geo)
	r.reset()

	r.pushGlyph(ebiten.NewImage(64, 64), 1, 1, ch, 0, color.RGBA{0xff, 0xff, 0xff, 0xff}, geo)

	var out []quad
	for i := range r.fg {
		v := r.fg[i].verts
		for at := 0; at+4 <= len(v); at += 4 {
			out = append(out, quad{
				x0: v[at].DstX, y0: v[at].DstY, x1: v[at+3].DstX, y1: v[at+3].DstY,
				sx0: v[at].SrcX, sy0: v[at].SrcY, sx1: v[at+3].SrcX, sy1: v[at+3].SrcY,
			})
		}
	}
	if len(out) == 0 {
		t.Fatalf("%q queued nothing to draw", ch)
	}
	return out, geo
}

// reach is how far the quads of one character go either side of its own
// cell.
func reach(q []quad, geo *Geometry, x, y int) (left, right, up, down float32) {
	cellL, cellT := float32(geo.CellX(x)), float32(geo.CellY(y))
	cellR, cellB := cellL+float32(geo.CellW()), cellT+float32(geo.CellH())
	for _, r := range q {
		left = max(left, cellL-r.x0)
		right = max(right, r.x1-cellR)
		up = max(up, cellT-r.y0)
		down = max(down, r.y1-cellB)
	}
	return left, right, up, down
}

// A corner reaches out on the two sides it has an arm on, and stays
// inside its cell on the other two. Drawn the other way it would sit
// beside the edge it should meet.
func TestACornerReachesOnlyOnTheSidesItHasAnArmOn(t *testing.T) {
	pad := grid.Pad{Before: 4, After: 4}
	for _, c := range []struct {
		ch                    rune
		left, right, up, down bool
	}{
		{'┌', false, true, false, true},
		{'┐', true, false, false, true},
		{'└', false, true, true, false},
		{'┘', true, false, true, false},
		{'├', false, true, true, true},
		{'┬', true, true, false, true},
	} {
		q, geo := glyphQuads(t, c.ch, pad, pad)
		left, right, up, down := reach(q, geo, 1, 1)

		for _, side := range []struct {
			what string
			got  float32
			want bool
		}{
			{"left", left, c.left},
			{"right", right, c.right},
			{"up", up, c.up},
			{"down", down, c.down},
		} {
			if side.want && side.got <= 0 {
				t.Errorf("%q does not reach %s, and it has an arm there", c.ch, side.what)
			}
			if !side.want && side.got > 0 {
				t.Errorf("%q reaches %g pixels %s, and it has no arm there",
					c.ch, side.got, side.what)
			}
		}
	}
}

// A rule reaches across the gap either side of it, so a line does not
// break where a dialog crosses the sidebar's margin.
func TestARuleReachesAcrossTheGapEitherSideOfIt(t *testing.T) {
	pad := grid.Pad{Before: 4, After: 6}
	q, geo := glyphQuads(t, '─', pad, grid.Pad{})

	left, right, up, down := reach(q, geo, 1, 1)

	before, after := geo.ColGap(1)
	if left != float32(before) || right != float32(after) {
		t.Errorf("it reaches %g left and %g right, want the %d and %d the grid leaves",
			left, right, before, after)
	}
	if up > 0 || down > 0 {
		t.Errorf("it reaches %g up and %g down, and a rule has no arm either way", up, down)
	}
}

// A character that is not a line stays in its own cell however the grid
// is padded.
func TestSomethingThatIsNotALineStaysInItsCell(t *testing.T) {
	pad := grid.Pad{Before: 4, After: 4}
	for _, ch := range []rune{'A', '█', '.'} {
		q, geo := glyphQuads(t, ch, pad, pad)

		left, right, up, down := reach(q, geo, 1, 1)

		if left > 0 || right > 0 || up > 0 || down > 0 {
			t.Errorf("%q reaches %g %g %g %g out of its cell", ch, left, right, up, down)
		}
	}
}

// With no padding nothing reaches anywhere, so an ordinary grid queues
// one rectangle a character.
func TestWithNoPaddingAGlyphIsOneRectangle(t *testing.T) {
	q, _ := glyphQuads(t, '┼', grid.Pad{}, grid.Pad{})

	if len(q) != 1 {
		t.Errorf("it queued %d rectangles, want the one: there is no gap to reach across", len(q))
	}
}

// An arm carried across a gap is one pixel of the glyph's own edge
// repeated, not the whole glyph squashed into the gap. Squashed, a
// corner would draw a second copy of itself in the margin.
func TestAnArmCarriedAcrossAGapIsOnePixelOfTheEdge(t *testing.T) {
	pad := grid.Pad{Before: 4, After: 4}
	q, geo := glyphQuads(t, '┬', pad, pad)

	cellL := float32(geo.CellX(1))
	cellR := cellL + float32(geo.CellW())
	var arms int
	for _, r := range q {
		if r.x0 >= cellL && r.x1 <= cellR {
			continue
		}
		arms++
		if got := r.sx1 - r.sx0; got != 1 {
			t.Errorf("an arm samples %g pixels of the glyph, want the one at its edge", got)
		}
	}
	if arms != 2 {
		t.Fatalf("%d quads reach out of the cell, want the left and right arms", arms)
	}
}

// And down the other axis.
func TestAnArmCarriedDownAGapIsOnePixelOfTheEdge(t *testing.T) {
	pad := grid.Pad{Before: 4, After: 4}
	q, geo := glyphQuads(t, '│', pad, pad)

	cellT := float32(geo.CellY(1))
	cellB := cellT + float32(geo.CellH())
	var arms int
	for _, r := range q {
		if r.y0 >= cellT && r.y1 <= cellB {
			continue
		}
		arms++
		if got := r.sy1 - r.sy0; got != 1 {
			t.Errorf("an arm samples %g pixels of the glyph, want the one at its edge", got)
		}
	}
	if arms != 2 {
		t.Fatalf("%d quads reach out of the cell, want the up and down arms", arms)
	}
}
