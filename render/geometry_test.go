package render

import (
	"image/color"
	"testing"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
)

var geoMetrics = glyph.Metrics{CellW: 8, CellH: 16, Ascent: 13}

func geoGrid(cols, rows int) *grid.Grid {
	return grid.New(cols, rows, color.RGBA{}, color.RGBA{})
}

func measured(g *grid.Grid) *Geometry {
	geo := &Geometry{}
	geo.Layout(g, geoMetrics)
	return geo
}

// With nothing padded a grid is a grid: every column is where
// multiplying by the cell width would put it.
func TestGeometryWithoutPaddingIsPlainMultiplication(t *testing.T) {
	geo := measured(geoGrid(10, 4))

	for x := 0; x <= 10; x++ {
		if got := geo.CellX(x); got != x*8 {
			t.Errorf("column %d starts at %d, want %d", x, got, x*8)
		}
	}
	for y := 0; y <= 4; y++ {
		if got := geo.CellY(y); got != y*16 {
			t.Errorf("row %d starts at %d, want %d", y, got, y*16)
		}
	}
	if w, h := geo.Width(), geo.Height(); w != 80 || h != 64 {
		t.Errorf("the grid is %dx%d, want 80x64", w, h)
	}
}

// Padding is room the grid gains, so a padded column moves every column
// after it and the grid gets wider.
func TestPaddingMovesWhatComesAfterIt(t *testing.T) {
	g := geoGrid(4, 2)
	g.SetColPad(0, grid.Pad{Before: 2}) // half a cell, four pixels
	geo := measured(g)

	if got := geo.CellX(0); got != 4 {
		t.Errorf("column 0 starts at %d, want 4", got)
	}
	if got := geo.CellX(1); got != 12 {
		t.Errorf("column 1 starts at %d, want 12", got)
	}
	if got := geo.Width(); got != 4+4*8 {
		t.Errorf("the grid is %d wide, want %d", got, 4+4*8)
	}
}

// The outer box covers the padding and the cell does not. A background
// fills the outer box, so the padding takes the colour of what it pads
// rather than showing a seam.
func TestTheOuterBoxCoversThePaddingAndTheCellDoesNot(t *testing.T) {
	g := geoGrid(4, 2)
	g.SetColPad(1, grid.Pad{Before: 2, After: 4})
	geo := measured(g)

	at, size := geo.ColBox(1, 2)
	if at != 8 || size != 4+8+8 {
		t.Errorf("the box of column 1 is %d..%d, want 8..28", at, at+size)
	}
	if got := geo.CellX(1); got != 12 {
		t.Errorf("the cell of column 1 starts at %d, want 12", got)
	}
}

// Boxes tile the grid: a run of columns is exactly the columns in it,
// with no pixel counted twice or left out.
func TestBoxesTileTheGrid(t *testing.T) {
	g := geoGrid(6, 3)
	g.SetColPad(0, grid.Pad{Before: 2})
	g.SetColPad(3, grid.Pad{Before: 1, After: 3})
	g.SetColPad(5, grid.Pad{After: 2})
	g.SetRowPad(0, grid.Pad{Before: 1, After: 1})
	geo := measured(g)

	at, size := geo.ColBox(0, 6)
	if at != 0 || size != geo.Width() {
		t.Errorf("every column together is %d..%d, want 0..%d", at, at+size, geo.Width())
	}
	for x := 0; x < 6; x++ {
		a, w := geo.ColBox(x, x+1)
		b, _ := geo.ColBox(x+1, x+2)
		if a+w != b {
			t.Errorf("column %d ends at %d and %d starts at %d", x, a+w, x+1, b)
		}
	}
	at, size = geo.RowBox(0, 3)
	if at != 0 || size != geo.Height() {
		t.Errorf("every row together is %d..%d, want 0..%d", at, at+size, geo.Height())
	}
}

// An empty run has no width, whatever it is asked about.
func TestAnEmptyRunHasNoWidth(t *testing.T) {
	g := geoGrid(4, 2)
	g.SetColPad(2, grid.Pad{Before: 2})
	geo := measured(g)

	for _, x := range []int{0, 2, 4} {
		if _, size := geo.ColBox(x, x); size != 0 {
			t.Errorf("the run from %d to %d is %d wide", x, x, size)
		}
	}
}

// Every pixel of a column's box reports that column back. This is how a
// click finds the row it landed on, and padding is exactly where a
// division by the cell width would answer one out.
func TestEveryPixelReportsTheColumnItIsIn(t *testing.T) {
	g := geoGrid(5, 4)
	g.SetColPad(0, grid.Pad{Before: 2})
	g.SetColPad(4, grid.Pad{After: 2})
	g.SetRowPad(0, grid.Pad{Before: 1, After: 1})
	geo := measured(g)

	for x := 0; x < 5; x++ {
		at, size := geo.ColBox(x, x+1)
		for px := at; px < at+size; px++ {
			if got := geo.ColAt(px); got != x {
				t.Fatalf("pixel %d is in column %d, want %d", px, got, x)
			}
		}
	}
	for y := 0; y < 4; y++ {
		at, size := geo.RowBox(y, y+1)
		for py := at; py < at+size; py++ {
			if got := geo.RowAt(py); got != y {
				t.Fatalf("pixel %d is in row %d, want %d", py, got, y)
			}
		}
	}
}

// A pixel outside the grid keeps counting in whole cells, so a drag
// that has left the window still says which way it went.
func TestPixelsOutsideTheGridKeepCounting(t *testing.T) {
	g := geoGrid(4, 2)
	g.SetColPad(0, grid.Pad{Before: 2})
	geo := measured(g)

	if got := geo.ColAt(-1); got != -1 {
		t.Errorf("a pixel one left of the grid is in column %d, want -1", got)
	}
	if got := geo.ColAt(-9); got != -2 {
		t.Errorf("a pixel nine left of the grid is in column %d, want -2", got)
	}
	if got := geo.ColAt(geo.Width()); got != 4 {
		t.Errorf("a pixel one right of the grid is in column %d, want 4", got)
	}
	if got := geo.RowAt(geo.Height() + 16); got != 3 {
		t.Errorf("a pixel a row below the grid is in row %d, want 3", got)
	}
}

// A grid a few pixels short of the window is stretched to reach it, so
// no strip of the window is left with nothing to paint it.
func TestFillStretchesTheLastColumnAndRow(t *testing.T) {
	g := geoGrid(4, 2)
	geo := measured(g)

	geo.Fill(35, 35)

	if got := geo.Width(); got != 35 {
		t.Errorf("the grid is %d wide, want 35", got)
	}
	if got := geo.Height(); got != 35 {
		t.Errorf("the grid is %d high, want 35", got)
	}
	if _, size := geo.ColBox(3, 4); size != 8+3 {
		t.Errorf("the last column is %d wide, want 11", size)
	}
	// The cell itself did not move, so nothing is drawn anywhere new.
	if got := geo.CellX(3); got != 24 {
		t.Errorf("the last cell starts at %d, want 24", got)
	}
}

// A grid genuinely smaller than the window keeps its own size: only the
// odd pixels left over by a division are worth taking.
func TestFillLeavesASmallGridAlone(t *testing.T) {
	g := geoGrid(4, 2)
	geo := measured(g)

	geo.Fill(800, 600)

	if w, h := geo.Width(), geo.Height(); w != 32 || h != 32 {
		t.Errorf("the grid is %dx%d, want 32x32", w, h)
	}
}

// A window smaller than the grid takes nothing away: the grid is drawn
// as it is and the window clips it.
func TestFillNeverShrinks(t *testing.T) {
	g := geoGrid(4, 2)
	geo := measured(g)

	geo.Fill(10, 10)

	if w, h := geo.Width(), geo.Height(); w != 32 || h != 32 {
		t.Errorf("the grid is %dx%d, want 32x32", w, h)
	}
}

// An empty grid still measures, because a texture is made from it.
func TestAnEmptyGridStillHasASize(t *testing.T) {
	geo := measured(geoGrid(0, 0))

	if w, h := geo.Width(), geo.Height(); w < 1 || h < 1 {
		t.Errorf("an empty grid is %dx%d", w, h)
	}
	if geo.ColAt(20) != 2 {
		t.Errorf("a pixel in an empty grid is in column %d", geo.ColAt(20))
	}
}

// Laying a geometry out again reuses its slices rather than keeping the
// last grid's columns.
func TestLayoutStartsAgain(t *testing.T) {
	geo := measured(geoGrid(10, 4))
	geo.Layout(geoGrid(3, 2), geoMetrics)

	if geo.Cols() != 3 || geo.Rows() != 2 {
		t.Errorf("the geometry is %dx%d cells, want 3x2", geo.Cols(), geo.Rows())
	}
	if got := geo.Width(); got != 24 {
		t.Errorf("the grid is %d wide, want 24", got)
	}
}
