package render

import (
	"image/color"
	"testing"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
)

// cellSizes are the ones a real font gives: a small window, the default,
// and a large one. The narrow ones are where a bar can fall between
// pixels.
var cellSizes = []glyph.Metrics{
	{CellW: 5, CellH: 11, Ascent: 9},
	{CellW: 7, CellH: 15, Ascent: 12},
	{CellW: 9, CellH: 19, Ascent: 15},
	{CellW: 12, CellH: 25, Ascent: 20},
	{CellW: 24, CellH: 48, Ascent: 38},
}

// Every bar is a whole pixel wide and they tile the cell exactly.
//
// Quads are not antialiased, so a bar narrower than a pixel is only
// drawn when a pixel centre happens to fall inside it -- and the same
// seconds vanish every time, which is the one thing a graph of a run
// must not do.
func TestGraphBarsAreWholePixelsAndTileTheCell(t *testing.T) {
	heights := make([]int, grid.ArtGraphBars)
	for i := range heights {
		heights[i] = grid.ArtGraphMax
	}
	art := grid.Graph(heights)

	for _, m := range cellSizes {
		for _, at := range [][2]int{{0, 0}, {3, 2}} {
			bars := graphBars(art, at[0], at[1], m)
			if len(bars) == 0 {
				t.Fatalf("%+v: no bars at all", m)
			}
			want := min(grid.ArtGraphBars, m.CellW)
			if len(bars) != want {
				t.Fatalf("%+v: %d bars, want %d", m, len(bars), want)
			}
			left := float32(at[0] * m.CellW)
			at := left
			for i, b := range bars {
				if b.W < 1 {
					t.Fatalf("%+v: bar %d is %v wide, want at least a pixel", m, i, b.W)
				}
				if b.X != at {
					t.Fatalf("%+v: bar %d starts at %v, want %v", m, i, b.X, at)
				}
				at = b.X + b.W
			}
			if right := left + float32(m.CellW); at != right {
				t.Fatalf("%+v: the bars end at %v, want the cell's edge %v", m, at, right)
			}
		}
	}
}

// Only the seconds really measured are drawn, so a run of two is not
// drawn as ten quiet ones and two busy.
func TestGraphDrawsOnlyWhatWasMeasured(t *testing.T) {
	m := glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38}
	for _, n := range []int{1, 2, 5, grid.ArtGraphBars} {
		heights := make([]int, n)
		for i := range heights {
			heights[i] = 8
		}
		if got := len(graphBars(grid.Graph(heights), 0, 0, m)); got != n {
			t.Fatalf("a run of %d seconds drew %d bars", n, got)
		}
	}
	if got := graphBars(grid.Art{}, 0, 0, m); got != nil {
		t.Fatalf("art that is not a graph drew %v", got)
	}
}

// The bars stay inside the cell, and sit on the baseline.
func TestGraphStaysInsideItsCell(t *testing.T) {
	art := grid.Graph([]int{0, grid.ArtGraphMax, 4})
	for _, m := range cellSizes {
		foot := float32(2*m.CellH + m.Ascent)
		for i, b := range graphBars(art, 1, 2, m) {
			if b.H < 1 {
				t.Errorf("%+v: bar %d is %v high, want at least a pixel", m, i, b.H)
			}
			if b.Y+b.H != foot {
				t.Errorf("%+v: bar %d ends at %v, want the baseline %v", m, i, b.Y+b.H, foot)
			}
			if b.Y < float32(2*m.CellH) {
				t.Errorf("%+v: bar %d starts at %v, above its own row", m, i, b.Y)
			}
		}
	}
}

// Art follows the same rules a glyph on that cell would: hidden is not
// drawn, and dim is mixed towards the background.
func TestArtColourFollowsTheCell(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{A: 255}
	g := grid.New(4, 2, white, black)
	art := grid.Graph([]int{1})

	plain := grid.Cell{Rune: ' ', FG: white, BG: black, Width: 1, Art: art}
	g.Set(0, 0, plain)
	full, ok := artColour(g, 0, 0, plain)
	if !ok {
		t.Fatal("an ordinary cell's art is not drawn")
	}

	dim := plain
	dim.Attr = grid.AttrDim
	g.Set(1, 0, dim)
	dimmed, ok := artColour(g, 1, 0, dim)
	if !ok {
		t.Fatal("a dim cell's art is not drawn at all")
	}
	if dimmed == full {
		t.Fatal("a dim cell's art is drawn at full brightness")
	}

	hidden := plain
	hidden.Attr = grid.AttrHidden
	g.Set(2, 0, hidden)
	if _, ok := artColour(g, 2, 0, hidden); ok {
		t.Fatal("a hidden cell's art is drawn")
	}
}
