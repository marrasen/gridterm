package main

import (
	"errors"
	"image/color"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// switcherCommand shows every pane at once, and switcherTitle names the
// line that opens it.
const (
	switcherCommand = "view.switcher"
	switcherTitle   = "Show every pane"
)

// switcher is every pane drawn small at once, so one can be picked by
// eye. The tiles and their names are a ui.Tiles on the modal stack, and
// the picture inside each one is a layer of its own, shrunk by the GPU.
type switcher struct {
	tiles *ui.Tiles

	// panes are what the tiles stand for, in the order they are drawn.
	panes []ui.Widget

	// shown is the picture inside each tile, one per pane.
	shown map[ui.Widget]*tile
}

// tile is one pane drawn small, on a grid and a layer of its own.
type tile struct {
	what  ui.Widget
	g     *grid.Grid
	geo   render.Geometry
	layer *render.Layer
}

// newTile puts a pane on a grid and a layer of its own, hidden until it
// has been placed.
func newTile(what ui.Widget, fg, bg color.RGBA) *tile {
	t := &tile{what: what, g: grid.New(1, 1, fg, bg)}
	t.layer = &render.Layer{Grid: t.g, Hidden: true, Geom: &t.geo}
	return t
}

// place sizes the grid to the pane and fits the picture inside a tile.
// A tile with no room left is hidden: a scale of zero means none at all,
// so the picture would be blitted whole over the window.
func (t *tile) place(size ui.Size, box ui.Rect, geo *render.Geometry, m glyph.Metrics) {
	t.g.Resize(max(size.Cols, 1), max(size.Rows, 1))
	t.geo.Layout(t.g, m)
	left, width := geo.CellsX(box.X, box.X+box.Cols)
	top, height := geo.CellsY(box.Y, box.Y+box.Rows)
	if width <= 0 || height <= 0 {
		t.layer.Hidden = true
		return
	}
	t.layer.X, t.layer.Y, t.layer.Scale = fitInside(
		t.geo.Width(), t.geo.Height(), left, top, width, height)
	t.layer.Hidden = false
}

// draw paints the pane onto its own grid.
func (t *tile) draw() { ui.DrawApart(t.what, t.g.View()) }

// openSwitcher shows every pane at once and goes to the one picked.
func (a *app) openSwitcher() error {
	if a.switcher != nil {
		return nil
	}
	panes := a.panesInSidebarOrder()
	if len(panes) < 2 {
		return errors.New("there is only one pane. There is nothing to switch between")
	}
	cols, rows := a.g.Size()
	if !ui.TilesFit(len(panes), ui.Size{Cols: cols, Rows: rows}) {
		return errors.New("the window is too small to draw every pane at once")
	}

	names := make([]string, 0, len(panes))
	for _, pane := range panes {
		names = append(names, a.paneName(pane))
	}
	tiles := ui.NewTiles(names)
	tiles.Style = a.tilesStyle()
	tiles.Mark(indexOf(panes, ui.FocusedLeaf(a.root.Widget())))

	s := &switcher{tiles: tiles, panes: panes, shown: map[ui.Widget]*tile{}}
	var hide func()
	tiles.Close = func() {
		if hide != nil {
			hide()
		}
	}
	tiles.Pick = func(at int) error {
		if hide != nil {
			hide()
		}
		if at < 0 || at >= len(s.panes) {
			return nil
		}
		a.focus(s.panes[at])
		return nil
	}
	hide = a.showModal(tiles, func() { a.closeSwitcher() })
	if a.root.Modal() != ui.Widget(tiles) {
		return errors.New("the window would not show the panes")
	}
	a.switcher = s
	a.markDirty()
	return nil
}

// closeSwitcher takes the pictures off the window.
func (a *app) closeSwitcher() {
	if a.switcher == nil {
		return
	}
	for _, t := range a.switcher.shown {
		a.comp.Remove(t.layer)
	}
	a.switcher = nil
	a.markDirty()
}

// placeSwitcher draws a picture of each pane inside its tile, every
// frame, so the pictures are what the panes are doing now.
func (a *app) placeSwitcher() {
	if a.switcher == nil || a.comp == nil || a.g == nil {
		return
	}
	s := a.switcher
	// A pane that has closed takes its picture with it.
	for what, t := range s.shown {
		if !a.paneIsOpen(what) {
			a.comp.Remove(t.layer)
			delete(s.shown, what)
		}
	}
	// Hidden first and shown again below, so a tile the layout no longer
	// has a box for is not left where it was last frame.
	for _, t := range s.shown {
		t.layer.Hidden = true
	}
	a.renderer.Measure(a.g, &a.geo)
	m := a.renderer.Metrics()
	cellW, cellH := a.renderer.CellSize()
	for i, area := range s.tiles.Areas() {
		if i >= len(s.panes) {
			break
		}
		what := s.panes[i]
		if !a.paneIsOpen(what) {
			continue
		}
		size, ok := a.paneScreen(what)
		// A held screen is sized by somebody on another machine, so it is
		// bounded rather than trusted: one past a texture is left out.
		if !ok || !fitsATexture(size, cellW, cellH) {
			continue
		}
		t := s.shown[what]
		if t == nil {
			t = newTile(a.paneInside(what), a.colours.FG, a.colours.BG)
			s.shown[what] = t
			a.comp.Add(t.layer)
		}
		t.place(size, area, &a.geo, m)
		if !t.layer.Hidden {
			t.draw()
		}
	}
}

// paneIsOpen reports whether a widget is still one of the window's
// panes, for a switcher left open while one closed.
func (a *app) paneIsOpen(w ui.Widget) bool {
	if t, is := w.(*term.Terminal); is {
		_, live := a.panes[t]
		return live
	}
	return a.isPane(w)
}

// paneInside is what a tile draws. A terminal draws its whole screen
// rather than the blank the tree gets while the host is drawing it.
func (a *app) paneInside(w ui.Widget) ui.Widget {
	if t, is := w.(*term.Terminal); is {
		return fullScreen{t}
	}
	return w
}

// paneScreen is how big the picture inside a tile is, and whether there
// is one to draw. A terminal's own screen, and for anything else the
// room the tree gave it.
func (a *app) paneScreen(w ui.Widget) (ui.Size, bool) {
	if t, is := w.(*term.Terminal); is {
		return t.Size(), true
	}
	area, ok := a.paneArea(w)
	return area.Size(), ok
}

// tilesStyle colours the grid of tiles in the window's own scheme.
func (a *app) tilesStyle() ui.TilesStyle {
	return ui.TilesStyle{
		FG:       a.colours.FG,
		BG:       a.colours.Surface(),
		Border:   a.colours.ANSI[8],
		Marked:   a.colours.ANSI[6],
		MarkedFG: a.colours.BG,
		MarkedBG: a.colours.ANSI[6],
	}
}

// indexOf is where a widget sits in a list, and -1 when it is not in it.
func indexOf(in []ui.Widget, want ui.Widget) int {
	for i, have := range in {
		if have == want {
			return i
		}
	}
	return -1
}
