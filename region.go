package main

import (
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
)

// region is a part of the window drawn on a grid of its own.
//
// A grid has one set of row heights. The sidebar wants a little room
// above each server's name, and the terminal beside it wants every line
// the same height as the one before, so the two cannot share a grid: a
// row made taller for a heading would put a gap through the middle of
// the shell's output.
//
// So the sidebar gets a grid to itself, on a layer over the part of the
// window it covers. It keeps its place in the widget tree, which is
// what still gives it its size, its keys and its clicks. Only the
// painting moves.
type region struct {
	w     ui.Widget
	g     *grid.Grid
	layer *render.Layer

	// rect is where the region sits in the window, in the window's
	// cells. Empty when it has nowhere to go.
	rect ui.Rect

	// left, top, width and height are the same in pixels.
	left, top, width, height int
}

// newRegion puts a widget on a grid of its own, over whatever the
// window has drawn there.
//
// Opaque, not transparent: the region covers its part of the window
// completely, and the cells of the window's own grid underneath it are
// left holding whatever was last painted there.
func newRegion(w ui.Widget, g *grid.Grid, geo *render.Geometry) *region {
	return &region{w: w, g: g, layer: &render.Layer{Grid: g, Hidden: true, Geom: geo}}
}

// place tells the region where it sits and sizes its grid to suit.
//
// The columns are the window's own, padding and all, so the region
// lines up to the pixel with the divider beside it. The rows are its
// own, which is the whole point of it.
func (r *region) place(rect ui.Rect, geo *render.Geometry, pads []grid.Pad) {
	if rect.Cols <= 0 || rect.Rows <= 0 {
		r.rect = ui.Rect{}
		r.left, r.top, r.width, r.height = 0, 0, 0, 0
		r.layer.Hidden = true
		return
	}
	r.rect = rect
	r.left, r.width = geo.ColBox(rect.X, rect.X+rect.Cols)
	r.top, r.height = geo.RowBox(rect.Y, rect.Y+rect.Rows)
	r.layer.X, r.layer.Y = r.left, r.top
	r.layer.Hidden = false

	r.g.Resize(rect.Cols, rect.Rows)
	var want padTable
	for x := 0; x < rect.Cols; x++ {
		if p := padOf(pads, rect.X+x); !p.Empty() {
			want.add(x, p)
		}
	}
	want.apply(rect.Cols, r.g.ColPads(), r.g.SetColPad)
}

// measure works out where the region's own grid lands in pixels.
//
// The columns come from the window, so the region fills its hole to the
// pixel and its text lines up with the text above it. The rows are its
// own, and the last of them is stretched to reach the bottom of the
// hole.
func (r *region) measure(src *render.Geometry, m glyph.Metrics, geo *render.Geometry) {
	geo.Layout(r.g, m)
	geo.TakeCols(src, r.rect.X, r.rect.X+r.rect.Cols)
	geo.FitRows(r.height)
}

// draw paints the region's widget onto its own grid.
func (r *region) draw() {
	if r.rect.Empty() || r.w == nil {
		return
	}
	r.w.Draw(r.g.View())
}

// contains reports whether a pixel is inside the region.
func (r *region) contains(px, py int) bool {
	if r.rect.Empty() || r.layer.Hidden {
		return false
	}
	return px >= r.left && px < r.left+r.width &&
		py >= r.top && py < r.top+r.height
}

// cellAt is which of the window's cells a pixel inside the region is
// on, measured by the region's own rows rather than the window's.
func (r *region) cellAt(px, py int, geo *render.Geometry) (col, row int) {
	col = min(max(geo.ColAt(px-r.left), 0), r.rect.Cols-1)
	row = min(max(geo.RowAt(py-r.top), 0), r.rect.Rows-1)
	return r.rect.X + col, r.rect.Y + row
}

// padOf reads a pad table that may stop short of the grid.
func padOf(pads []grid.Pad, i int) grid.Pad {
	if i < 0 || i >= len(pads) {
		return grid.Pad{}
	}
	return pads[i]
}

// placeRegions gives each region the part of the window its widget was
// laid out for.
//
// Every frame rather than on a resize: the sidebar changes width when
// the divider is dragged and goes away when it is closed, neither of
// which changes the size of the window.
func (a *app) placeRegions() {
	if a.sideRegion == nil {
		return
	}
	a.renderer.Measure(a.g, &a.geo)
	a.sideRegion.place(a.sidebarArea(), &a.geo, a.g.ColPads())
	a.sideRegion.measure(&a.geo, a.renderer.Metrics(), &a.sideGeo)
}

// sidebarArea is where the tree put the sidebar, in the window's cells,
// or nothing when the window has no room for one.
func (a *app) sidebarArea() ui.Rect {
	if a.side == nil || a.root.Widget() == nil {
		return ui.Rect{}
	}
	cols, rows := a.g.Size()
	whole := ui.Rect{Cols: cols, Rows: rows}
	area, ok := ui.AreaOf(a.root.Widget(), whole, a.side)
	if !ok {
		return ui.Rect{}
	}
	return area
}

// cellAt is which of the window's cells a pixel is on.
//
// A region has its own row heights, so a click inside one is measured
// by those rather than by the window's: asking the window would name a
// row the sidebar is not drawing there.
func (a *app) cellAt(px, py int) (col, row int) {
	if a.sideRegion != nil && a.sideRegion.contains(px, py) {
		return a.sideRegion.cellAt(px, py, &a.sideGeo)
	}
	return a.geo.CellAt(px, py)
}
