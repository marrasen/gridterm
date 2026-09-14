package render

import (
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// padded returns a geometry for a grid with a margin down each side, a
// gap two columns in, and room around the first row: the shape the
// window itself asks for.
func paddedGeo(cols, rows int) (*grid.Grid, *Geometry) {
	g := geoGrid(cols, rows)
	g.SetColPad(0, grid.Pad{Before: 2})
	g.SetColPad(2, grid.Pad{After: 2})
	g.SetColPad(cols-1, grid.Pad{After: 2})
	g.SetRowPad(0, grid.Pad{Before: 1, After: 1})
	return g, measured(g)
}

// A background fills the outer box, so the padding around a cell takes
// the colour of the cell it pads. Anything less leaves a seam of
// whatever the texture held before.
func TestABackgroundFillsThePadding(t *testing.T) {
	_, geo := paddedGeo(6, 3)

	b := bgRect(geo, 0, 1, 0)
	if b.X != 0 || b.Y != 0 {
		t.Errorf("the first cell's background starts at %v,%v, want the corner", b.X, b.Y)
	}
	if b.W != 4+8 {
		t.Errorf("it is %v wide, want the cell and its margin", b.W)
	}
	if b.H != 4+16+4 {
		t.Errorf("it is %v high, want the row and the room around it", b.H)
	}
	// Runs meet with nothing between them.
	left := bgRect(geo, 0, 3, 1)
	right := bgRect(geo, 3, 6, 1)
	if left.X+left.W != right.X {
		t.Errorf("one run ends at %v and the next starts at %v", left.X+left.W, right.X)
	}
}

// A rule goes under the characters, not under the room around them.
// Padding is the window's furniture, and a line sticking out past what
// it underlines is what a background filling the same space is not: a
// visible mistake.
func TestARuleStopsAtTheCharacters(t *testing.T) {
	_, geo := paddedGeo(6, 3)

	b := ruleRect(geo, 0, 3, 1, 13, 1)
	if b.X != float32(geo.CellX(0)) {
		t.Errorf("the rule starts at %v, want the first cell at %d", b.X, geo.CellX(0))
	}
	if want := float32(geo.CellX(2) + geo.CellW()); b.X+b.W != want {
		t.Errorf("the rule ends at %v, want the third cell's right edge %v", b.X+b.W, want)
	}
	// Measured down from the cell, because it is drawn against the
	// baseline rather than against the room around the row.
	if b.Y != float32(geo.CellY(1))+13 {
		t.Errorf("the rule sits at %v, want %d", b.Y, geo.CellY(1)+13)
	}
}

// A block cursor inverts the whole cell, padding included, or a notch of
// ordinary background is left beside an inverted one.
func TestABlockCursorCoversThePadding(t *testing.T) {
	g, geo := paddedGeo(6, 3)
	g.SetCursor(grid.Cursor{X: 0, Y: 0, Visible: true, Style: grid.CursorBlock})

	b := cursorRect(g, g.Cursor(), geo)
	if want := bgRect(geo, 0, 1, 0); b != want {
		t.Errorf("the block is %+v, want the cell's background %+v", b, want)
	}
}

// A bar or an underline belongs to the character. Drawn from the outer
// box, a bar in the window's first column would float out in the margin,
// away from the text it marks.
func TestAThinCursorSitsOnTheCharacter(t *testing.T) {
	g, geo := paddedGeo(6, 3)

	g.SetCursor(grid.Cursor{X: 0, Y: 0, Visible: true, Style: grid.CursorBar})
	b := cursorRect(g, g.Cursor(), geo)
	if b.X != float32(geo.CellX(0)) {
		t.Errorf("the bar is at %v, want the cell at %d", b.X, geo.CellX(0))
	}

	g.SetCursor(grid.Cursor{X: 0, Y: 0, Visible: true, Style: grid.CursorUnderline})
	b = cursorRect(g, g.Cursor(), geo)
	if want := float32(geo.CellY(0) + geo.CellH()); b.Y+b.H != want {
		t.Errorf("the underline ends at %v, want the foot of the cell %v", b.Y+b.H, want)
	}
	if b.X != float32(geo.CellX(0)) {
		t.Errorf("the underline is at %v, want the cell at %d", b.X, geo.CellX(0))
	}
}

// The spare pixels a window has over go to the outer box. A cursor on
// the last column must not grow with them: it marks a character, and
// the character did not get any wider.
func TestAThinCursorIgnoresTheSparePixels(t *testing.T) {
	g, geo := paddedGeo(6, 3)
	geo.Fill(geo.Width()+5, geo.Height()+5)
	g.SetCursor(grid.Cursor{X: 5, Y: 2, Visible: true, Style: grid.CursorBar})

	b := cursorRect(g, g.Cursor(), geo)
	if b.H != float32(geo.CellH()) {
		t.Errorf("the bar is %v high, want one cell %d", b.H, geo.CellH())
	}
	if b.Y != float32(geo.CellY(2)) {
		t.Errorf("the bar starts at %v, want the cell at %d", b.Y, geo.CellY(2))
	}
}

// A cursor on the lead half of a double-width character covers both
// columns, matching where the glyph actually is.
func TestACursorOnAWideCharacterCoversBoth(t *testing.T) {
	g, geo := paddedGeo(6, 3)
	g.SetWide(1, 1, grid.Cell{Rune: '漢', Width: 2})
	g.SetCursor(grid.Cursor{X: 1, Y: 1, Visible: true, Style: grid.CursorBlock})

	b := cursorRect(g, g.Cursor(), geo)
	if want := bgRect(geo, 1, 3, 1); b != want {
		t.Errorf("the block is %+v, want both columns %+v", b, want)
	}
}
