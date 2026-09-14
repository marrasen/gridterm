package render

import (
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
)

// iconUnits is the grid an icon is drawn on. Every shape below is given
// in whole units of it, and the units are scaled to whatever the cell
// turns out to be.
const iconUnits = 8

// unit is one rectangle of an icon, in units of iconUnits.
type unit struct{ X, Y, W, H int }

// icons are the shapes, one list of rectangles each.
//
// Kept small and square. At the sizes a terminal font gives, an icon has
// about eight pixels to say what it is with, so each is the plainest
// thing that still reads: a screen with a prompt in it, a triangle for
// something that ran, a root with things under it, arrows both ways.
var icons = [numIconKinds][]unit{
	grid.IconTerminal: {
		// The screen: four edges of a box.
		{X: 0, Y: 0, W: 8, H: 1},
		{X: 0, Y: 7, W: 8, H: 1},
		{X: 0, Y: 1, W: 1, H: 6},
		{X: 7, Y: 1, W: 1, H: 6},
		// The prompt inside it: a chevron and the cursor under it.
		{X: 2, Y: 2, W: 1, H: 1},
		{X: 3, Y: 3, W: 1, H: 1},
		{X: 2, Y: 4, W: 1, H: 1},
		{X: 4, Y: 5, W: 2, H: 1},
	},
	grid.IconCommand: {
		// A triangle pointing the way a thing that runs points.
		{X: 2, Y: 1, W: 1, H: 6},
		{X: 3, Y: 2, W: 1, H: 4},
		{X: 4, Y: 3, W: 1, H: 2},
		{X: 5, Y: 4, W: 1, H: 1},
	},
	grid.IconFiles: {
		// Three files down the right and the branches reaching them: a
		// trunk with things hanging off it, rather than three bars and a
		// stem, which reads as a letter.
		{X: 3, Y: 0, W: 5, H: 2},
		{X: 3, Y: 3, W: 5, H: 2},
		{X: 3, Y: 6, W: 5, H: 2},
		{X: 0, Y: 1, W: 1, H: 6},
		{X: 1, Y: 4, W: 2, H: 1},
		{X: 1, Y: 7, W: 2, H: 1},
	},
	grid.IconTunnel: {
		// One arrow each way, which is what a tunnel is.
		{X: 1, Y: 2, W: 6, H: 1},
		{X: 5, Y: 1, W: 1, H: 1},
		{X: 5, Y: 3, W: 1, H: 1},
		{X: 1, Y: 5, W: 6, H: 1},
		{X: 2, Y: 4, W: 1, H: 1},
		{X: 2, Y: 6, W: 1, H: 1},
	},
}

// numIconKinds is how many icons there are to draw.
const numIconKinds = 4

// iconBars is where an icon's rectangles go inside one cell.
//
// Square, as wide as the cell allows and no taller than the letters
// beside it, sitting on the baseline so it lines up with the text.
//
// Every rectangle is at least a pixel each way. Quads are not
// antialiased, so one narrower than a pixel is drawn only when a pixel
// centre happens to fall inside it, and half an icon would go missing at
// the sizes where an icon is doing the most work.
func iconBars(art grid.Art, x, y int, m glyph.Metrics) []bar {
	kind, ok := art.Icon()
	if !ok || int(kind) >= len(icons) {
		return nil
	}
	// No taller than a capital letter, and never wider than the cell.
	side := min(m.CellW, m.Ascent*7/10)
	if side < 2 {
		return nil
	}
	left := x*m.CellW + (m.CellW-side)/2
	top := y*m.CellH + m.Ascent - side

	shape := icons[kind]
	out := make([]bar, 0, len(shape))
	for _, u := range shape {
		x0 := left + u.X*side/iconUnits
		x1 := left + (u.X+u.W)*side/iconUnits
		y0 := top + u.Y*side/iconUnits
		y1 := top + (u.Y+u.H)*side/iconUnits
		out = append(out, bar{
			X: float32(x0), Y: float32(y0),
			W: float32(max(x1-x0, 1)), H: float32(max(y1-y0, 1)),
		})
	}
	return out
}
