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
	for x := range 6 {
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

// A run that reaches past the last column keeps counting in whole
// cells. Nothing asks for one today, but a box that stopped at the edge
// would quietly draw a run of two as a run of one.
func TestARunPastTheEndKeepsCounting(t *testing.T) {
	g := geoGrid(4, 2)
	g.SetColPad(3, grid.Pad{After: 2})
	geo := measured(g)

	at, size := geo.ColBox(3, 6)
	if at != 24 || size != 8+4+8+8 {
		t.Errorf("the run from 3 to 6 is %d..%d, want 24..%d", at, at+size, 24+28)
	}
	if got := geo.CellX(6); got != geo.CellX(4)+2*8 {
		t.Errorf("the cell two past the end starts at %d", got)
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

	for x := range 5 {
		at, size := geo.ColBox(x, x+1)
		for px := at; px < at+size; px++ {
			if got := geo.ColAt(px); got != x {
				t.Fatalf("pixel %d is in column %d, want %d", px, got, x)
			}
		}
	}
	for y := range 4 {
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

// A grid a few pixels short of the window reaches it on both sides, so
// no strip of the window is left with nothing to paint it and the
// margin does not end up wider at one edge than the other.
func TestFillSharesTheSparePixelsBetweenTheEdges(t *testing.T) {
	g := geoGrid(4, 2)
	geo := measured(g)

	geo.Fill(35, 35)

	if got := geo.Width(); got != 35 {
		t.Errorf("the grid is %d wide, want 35", got)
	}
	if got := geo.Height(); got != 35 {
		t.Errorf("the grid is %d high, want 35", got)
	}
	if at, size := geo.ColBox(0, 1); at != 0 || size != 8+1 {
		t.Errorf("the first column is %d..%d, want 0..9", at, at+size)
	}
	if at, size := geo.ColBox(3, 4); at != 25 || size != 8+2 {
		t.Errorf("the last column is %d..%d, want 25..35", at, at+size)
	}
	// Every cell moved over by the share the first edge took.
	if got := geo.CellX(0); got != 1 {
		t.Errorf("the first cell starts at %d, want 1", got)
	}
	if got := geo.CellX(3); got != 25 {
		t.Errorf("the last cell starts at %d, want 25", got)
	}
}

// The grid reaches the window at every size, whatever the cell box.
//
// The room the padding needs is worked out before there is a grid to
// lay out, so the two have to agree to the pixel. They did not: the
// room was one division and the placing was a division per pad, and the
// pixels the truncations lost added up to a whole cell the window had
// set aside and the grid never used -- a bare strip a character wide
// down the right-hand edge, at one window width in nine.
func TestTheGridReachesTheWindowAtEverySize(t *testing.T) {
	// Odd cell boxes are where a division per pad loses a pixel.
	for _, cell := range []int{5, 7, 9, 11, 13, 17, 8, 12, 24} {
		m := glyph.Metrics{CellW: cell, CellH: cell, Ascent: cell * 4 / 5}
		// What the window asks for: a margin down each side and the
		// sidebar gap, split across three columns.
		const padX = 6
		for px := 100; px < 400; px++ {
			cols := max((px-padX*cell/grid.PadUnit)/cell, 1)
			g := geoGrid(cols, 4)
			// The three pads the window asks for, combined where two of
			// them land on the same column, which is what it does with
			// them. All six quarters are placed either way.
			want := make([]grid.Pad, cols)
			want[0].Before += 2
			want[min(3, cols-1)].After += 2
			want[cols-1].After += 2
			for at, p := range want {
				g.SetColPad(at, p)
			}

			geo := &Geometry{}
			geo.Layout(g, m)
			geo.Fill(px, px)

			if got := geo.Width(); got != px {
				t.Fatalf("cell %d, window %d: the grid is %d wide, leaving %d pixels bare",
					cell, px, got, px-got)
			}
		}
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

// A geometry nothing has laid out still answers. It is an exported
// value anyone can declare, and the cell size is nothing until Layout
// has run.
func TestAnUnmeasuredGeometryAnswers(t *testing.T) {
	var geo Geometry

	if got := geo.ColAt(40); got != 0 {
		t.Errorf("a pixel in an unmeasured geometry is in column %d", got)
	}
	if got := geo.RowAt(-40); got != 0 {
		t.Errorf("a pixel in an unmeasured geometry is in row %d", got)
	}
	if w, h := geo.Width(), geo.Height(); w < 1 || h < 1 {
		t.Errorf("an unmeasured geometry is %dx%d", w, h)
	}
}

// A layer is only entitled to the window below and to the right of its
// own corner. One stretched to the far edge whatever its corner would
// have a texture that much too big.
func TestALayerIsMeasuredFromItsOwnCorner(t *testing.T) {
	r := newTestRenderer(t)
	cellW, cellH := r.CellSize()
	cols, rows := 4, 3
	// A window a couple of pixels bigger than the grid, so a grid at
	// the corner is stretched to reach it and one moved over by those
	// pixels already does.
	off := max(min(cellW, cellH)/3, 1)
	r.SetWindow(cols*cellW+off, rows*cellH+off)

	var here, over Geometry
	g := geoGrid(cols, rows)
	r.MeasureAt(g, 0, 0, &here)
	r.MeasureAt(g, off, off, &over)

	if here.Width() != cols*cellW+off || here.Height() != rows*cellH+off {
		t.Errorf("a grid at the corner is %dx%d, want it stretched to %dx%d",
			here.Width(), here.Height(), cols*cellW+off, rows*cellH+off)
	}
	if over.Width() != cols*cellW || over.Height() != rows*cellH {
		t.Errorf("a grid %d pixels in is %dx%d, want its own %dx%d",
			off, over.Width(), over.Height(), cols*cellW, rows*cellH)
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

// A geometry that borrows another's columns puts its cells exactly
// where that one puts them.
//
// A window is rarely a whole number of cells across, and the odd pixels
// are shared between its two edges. A region laid out on its own would
// put that share somewhere else and its text would sit a few pixels off
// the text above it.
func TestBorrowedColumnsLandWhereTheyDidBefore(t *testing.T) {
	g := geoGrid(20, 4)
	g.SetColPad(0, grid.Pad{Before: 2})
	g.SetColPad(5, grid.Pad{Before: 1, After: 3})
	g.SetColPad(19, grid.Pad{After: 2})
	src := measured(g)
	// The odd pixels a window has over, which land on the first column.
	src.Fill(src.Width()+5, src.Height())

	var geo Geometry
	geo.Layout(geoGrid(6, 2), geoMetrics)
	geo.TakeCols(src, 0, 6)

	if got, want := geo.Width(), mustBox(src, 0, 6); got != want {
		t.Errorf("the borrowed columns are %d wide, want the %d they came from", got, want)
	}
	for x := range 6 {
		if got, want := geo.CellX(x), src.CellX(x); got != want {
			t.Errorf("column %d is at %d, want the %d it came from", x, got, want)
		}
		at, size := geo.ColBox(x, x+1)
		wantAt, wantSize := src.ColBox(x, x+1)
		if at != wantAt || size != wantSize {
			t.Errorf("column %d's box is %d..%d, want %d..%d",
				x, at, at+size, wantAt, wantAt+wantSize)
		}
	}
}

// Borrowing from a run that does not start at the first column moves it
// to the origin.
func TestBorrowedColumnsStartAtTheOrigin(t *testing.T) {
	g := geoGrid(20, 4)
	g.SetColPad(5, grid.Pad{Before: 2})
	src := measured(g)

	var geo Geometry
	geo.Layout(geoGrid(4, 2), geoMetrics)
	geo.TakeCols(src, 5, 9)

	base, _ := src.ColBox(5, 6)
	if at, _ := geo.ColBox(0, 1); at != 0 {
		t.Errorf("the run starts at %d, want the origin", at)
	}
	if got, want := geo.CellX(0), src.CellX(5)-base; got != want {
		t.Errorf("its first cell is at %d, want %d", got, want)
	}
}

// Borrowing from nothing leaves a geometry with no columns rather than
// reading through a nil.
func TestBorrowingFromNothingIsSafe(t *testing.T) {
	var geo Geometry
	geo.Layout(geoGrid(4, 2), geoMetrics)

	geo.TakeCols(nil, 0, 4)

	if got := geo.Cols(); got != 0 {
		t.Errorf("it has %d columns", got)
	}
	// And still answers, counting in whole cells the way an empty
	// geometry does.
	if got := geo.ColAt(30); got != 30/geoMetrics.CellW {
		t.Errorf("a pixel in it is in column %d", got)
	}
}

// mustBox is the width of a run of columns.
func mustBox(geo *Geometry, x0, x1 int) int {
	_, size := geo.ColBox(x0, x1)
	return size
}

// A line-drawing character is drawn across the padding beside its cell,
// so a rule crossing a padded column joins up rather than breaking.
//
// The padding is where the sidebar's margin goes. A menu opened over
// that column used to show a gap in every rule it drew, which read as a
// dark line down the menu.
func TestALineIsDrawnAcrossThePaddingBesideIt(t *testing.T) {
	g := grid.New(4, 1, color.RGBA{}, color.RGBA{})
	// A gap after the second column, the way the sidebar's margin sits.
	g.SetColPad(1, grid.Pad{After: grid.PadUnit})

	var geo Geometry
	geo.Layout(g, glyph.Metrics{CellW: 8, CellH: 16, Ascent: 12})

	// The cell itself stops short of the gap; the box around it does
	// not, and that is what a line is drawn into.
	cellLeft, cellW := geo.CellX(1), geo.CellW()
	boxLeft, boxW := geo.ColBox(1, 2)
	if boxLeft != cellLeft {
		t.Errorf("the box starts at %d and the cell at %d", boxLeft, cellLeft)
	}
	if boxW <= cellW {
		t.Fatalf("the box is %d wide and the cell %d, so there is no gap to cross", boxW, cellW)
	}
	// And it reaches the cell after it, with nothing in between.
	if next := geo.CellX(2); boxLeft+boxW != next {
		t.Errorf("the box ends at %d and the next cell starts at %d", boxLeft+boxW, next)
	}
}

// The cell box covers the glyphs and nothing else. A frosted panel sits
// on it, so a panel measured from the outer boxes would reach out over
// the padding and no longer line up with the border drawn on it.
func TestTheCellBoxCoversTheGlyphsAndNotThePadding(t *testing.T) {
	g := geoGrid(6, 3)
	g.SetColPad(0, grid.Pad{Before: 2})
	g.SetColPad(2, grid.Pad{After: 4})
	geo := measured(g)

	at, size := geo.CellsX(0, 3)

	if want := geo.CellX(0); at != want {
		t.Errorf("it starts at %d, want the %d the first glyph is drawn at", at, want)
	}
	if want := geo.CellX(2) + 8; at+size != want {
		t.Errorf("it ends at %d, want the %d the last glyph ends at", at+size, want)
	}
	// And the outer box does cover the padding, which is what a
	// background wants.
	outer, wide := geo.ColBox(0, 3)
	if outer >= at || outer+wide <= at+size {
		t.Errorf("the outer box is %d..%d and the cells are %d..%d, want the cells inside it",
			outer, outer+wide, at, at+size)
	}
}

// The cell box of one cell is that cell, and of none is nothing.
func TestTheCellBoxOfOneCellIsThatCell(t *testing.T) {
	g := geoGrid(4, 2)
	g.SetColPad(1, grid.Pad{Before: 2, After: 4})
	geo := measured(g)

	at, size := geo.CellsX(1, 2)

	if at != geo.CellX(1) || size != 8 {
		t.Errorf("the cell box of column 1 is %d..%d, want %d..%d",
			at, at+size, geo.CellX(1), geo.CellX(1)+8)
	}
	if _, none := geo.CellsX(2, 2); none != 0 {
		t.Errorf("no columns measure %d wide", none)
	}
}

// Rows work the same way.
func TestTheCellBoxOfRowsSkipsTheirPadding(t *testing.T) {
	g := geoGrid(3, 4)
	g.SetRowPad(0, grid.Pad{Before: 2})
	geo := measured(g)

	at, size := geo.CellsY(0, 2)

	if want := geo.CellY(0); at != want {
		t.Errorf("it starts at %d, want the %d the first row is drawn at", at, want)
	}
	if want := geo.CellY(1) + 16; at+size != want {
		t.Errorf("it ends at %d, want %d", at+size, want)
	}
}
