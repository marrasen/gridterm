package main

import (
	"slices"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/input/ebitenin"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vt"
)

// paneWalk is a walk back through the panes in the order they last had
// focus, while Ctrl is held.
//
// The list is frozen when the walk starts. One that re-ordered as the
// user stepped would swap the top two entries on the first press and
// then bounce between the same pair for ever.
type paneWalk struct {
	order []ui.Widget

	// at is the pane the walk is on, counted from the one that had focus
	// when it started.
	at int

	// hold is the modifiers that keep the walk alive: the ones the
	// shortcut that started it holds. Read from the keymap rather than
	// fixed, so a user who moves the walk to another chord keeps the
	// holding as well as the key.
	hold input.Mods
}

// on is the pane the walk is on, and nil when there is nothing left.
func (w *paneWalk) on() ui.Widget {
	if len(w.order) == 0 {
		return nil
	}
	return w.order[min(w.at, len(w.order)-1)]
}

// noteFocus moves whichever pane has the keys to the front of the order
// panes are remembered in. A pane that closes is dropped by
// forgetRecent, where it leaves the window.
//
// From what has focus rather than from whatever asked for it: a click
// moves the keys without going through focus, and so does a pane opening
// or closing.
func (a *app) noteFocus() {
	if a.walk != nil {
		// Frozen while the user is walking. The pane landed on goes to
		// the front when they let go, and not before.
		return
	}
	pane := ui.FocusedLeaf(a.root.Widget())
	if pane == nil || !a.isPane(pane) {
		return
	}
	if len(a.recent) > 0 && a.recent[0] == pane {
		// Already the newest, which is every frame but the one the keys
		// moved on. Walking the tree and building a slice here would be
		// work an idle window does sixty times a second.
		return
	}
	a.recent = append([]ui.Widget{pane}, slices.DeleteFunc(a.recent, func(w ui.Widget) bool {
		return w == pane
	})...)
}

// recentPanes are the panes of this window, the ones that have had focus
// first and in the order they last had it.
//
// A pane that has never had focus goes on the end in the order the tree
// holds it, so a window just opened walks in an order that is at least
// something the user can see.
func (a *app) recentPanes() []ui.Widget {
	live := map[ui.Widget]bool{}
	var tree []ui.Widget
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if a.isPane(leaf) {
			live[leaf] = true
			tree = append(tree, leaf)
		}
	}
	out := make([]ui.Widget, 0, len(tree))
	for _, w := range a.recent {
		if live[w] {
			out = append(out, w)
		}
	}
	for _, w := range tree {
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// walkRecent steps through the panes in the order they last had focus.
//
// The first press starts the walk and freezes the order. Letting Ctrl go
// lands on whatever the walk is on.
func (a *app) walkRecent(step int) error {
	if a.root.Modal() != nil {
		// A dialog has the keys. Walking would move the focus behind it
		// and mark a pane the user cannot type in.
		return nil
	}
	if a.walk == nil {
		a.walk = &paneWalk{order: a.recentPanes(), hold: a.walkHeld()}
		if on := ui.FocusedLeaf(a.root.Widget()); on == nil || !a.isPane(on) {
			// The keys are on the sidebar, so the first press steps in
			// rather than past: it lands on the pane used last.
			a.focus(a.walk.on())
			return nil
		}
	}
	if len(a.walk.order) < 2 {
		return nil
	}
	n := len(a.walk.order)
	a.walk.at = ((a.walk.at+step)%n + n) % n
	a.focus(a.walk.on())
	return nil
}

// stepWalk keeps a walk in step with the window: panes that have closed
// come off it, and letting Ctrl go ends it.
//
// Ended on the first frame Ctrl is not held rather than on a release,
// which may land in another window: alt+tab away mid-walk and gridterm
// never sees the key come up.
func (a *app) stepWalk() {
	if a.walk == nil {
		return
	}
	a.prune()
	if a.walk.hold != 0 && a.mods()&a.walk.hold != 0 && len(a.walk.order) > 0 {
		return
	}
	a.walk = nil
	a.markDirty()
}

// walkHeld is the modifiers a walk waits to be let go of: the ones the
// chords bound to walking hold.
//
// Shift is left out because it picks the direction rather than holding
// the walk open, and the two chords differ by it. Nothing bound leaves
// no modifier to hold, and the walk then ends on the next frame, which
// is what running it from the palette should do.
func (a *app) walkHeld() input.Mods {
	if a.root.Accelerators == nil {
		return 0
	}
	var held input.Mods
	for _, id := range []string{"pane.next", "pane.previous"} {
		if c, bound := a.root.Accelerators.ChordFor(id); bound {
			held |= c.Mods
		}
	}
	return held &^ input.ModShift
}

// prune takes panes that have closed off the walk, leaving the mark on
// the one after whichever went.
func (a *app) prune() {
	live := map[ui.Widget]bool{}
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if a.isPane(leaf) {
			live[leaf] = true
		}
	}
	was := a.walk.on()
	a.walk.order = slices.DeleteFunc(a.walk.order, func(w ui.Widget) bool { return !live[w] })
	if len(a.walk.order) == 0 {
		return
	}
	if at := slices.Index(a.walk.order, was); at >= 0 {
		a.walk.at = at
		return
	}
	// The one it was on has gone. Deleting it moved the one after it
	// into that place, and the list is a ring, so the end comes round to
	// the start.
	a.walk.at %= len(a.walk.order)
	a.focus(a.walk.on())
}

// mods are the modifier keys held now.
func (a *app) mods() input.Mods {
	if a.modsNow != nil {
		return a.modsNow()
	}
	return ebitenin.Mods()
}

// walkOverlay is the list of panes drawn while Ctrl is held, on a layer
// of its own over the window.
type walkOverlay struct {
	g     *grid.Grid
	geo   render.Geometry
	layer *render.Layer

	// was is what it last drew, so a frame that would draw the same
	// thing draws nothing.
	was []string
	at  int
}

// walkOverlayWidth is how wide the list is, in cells. Wide enough for a
// pane's name and the machine it is on, and narrow enough to sit over a
// split without covering it.
const walkOverlayWidth = 44

// newWalkOverlay puts the list on a grid and a layer of its own, hidden
// until it has been placed.
func newWalkOverlay(pal vt.Palette) *walkOverlay {
	o := &walkOverlay{g: grid.New(1, 1, pal.FG, pal.ANSI[0])}
	o.layer = &render.Layer{Grid: o.g, Hidden: true, Geom: &o.geo}
	return o
}

// placeWalk puts the list of panes over the window while a walk is on,
// and takes it away again when the walk ends.
func (a *app) placeWalk() {
	if a.comp == nil || a.g == nil {
		return
	}
	lines, at := a.walkLines()
	if len(lines) == 0 {
		if a.overlay != nil {
			a.comp.Remove(a.overlay.layer)
			a.overlay = nil
			a.markDirty()
		}
		return
	}
	if a.overlay == nil {
		a.overlay = newWalkOverlay(a.colours)
		a.addUnderModals(a.overlay.layer)
		a.markDirty()
	}
	a.overlay.place(lines, at, a)
}

// walkLines are the panes a walk is on, and which of them is marked.
func (a *app) walkLines() ([]string, int) {
	if a.walk == nil || len(a.walk.order) < 2 {
		return nil, 0
	}
	lines := make([]string, 0, len(a.walk.order))
	for _, w := range a.walk.order {
		lines = append(lines, a.walkName(w))
	}
	return lines, min(a.walk.at, len(lines)-1)
}

// walkName is what the list calls a pane.
func (a *app) walkName(w ui.Widget) string {
	if e := a.entryOf(w); e != nil {
		return agentLabel(e)
	}
	return "a pane"
}

// place sizes the grid to the list and puts it in the middle of the
// window.
func (o *walkOverlay) place(lines []string, at int, a *app) {
	// Never bigger than the window: a list taller than the rows there
	// are would push the marked line off the bottom.
	winCols, winRows := a.g.Size()
	cols := min(walkOverlayWidth, max(winCols, 1))
	rows := min(len(lines)+2, max(winRows, 1))
	o.g.Resize(cols, rows)
	o.geo.Layout(o.g, a.renderer.Metrics())
	winW, winH := a.geo.Width(), a.geo.Height()
	o.layer.X = max((winW-o.geo.Width())/2, 0)
	o.layer.Y = max((winH-o.geo.Height())/2, 0)
	o.layer.Hidden = false
	o.draw(lines, at, a.colours)
}

// draw paints the list, and leaves the grid alone when it would draw the
// same thing again.
func (o *walkOverlay) draw(lines []string, at int, pal vt.Palette) {
	if at == o.at && slices.Equal(lines, o.was) {
		return
	}
	o.was, o.at = slices.Clone(lines), at
	cols, rows := o.g.Size()
	fg, bg := pal.FG, pal.Surface()
	o.g.View().Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
	for i, line := range lines {
		if i+1 >= rows-1 {
			// No more room. The list is drawn from the top, so what is
			// left off is the oldest.
			break
		}
		rowFG, rowBG := fg, bg
		if i == at {
			rowFG, rowBG = bg, fg
		}
		row := o.g.View().Sub(1, i+1, cols-2, 1)
		row.Fill(grid.Cell{Rune: ' ', FG: rowFG, BG: rowBG, Width: 1})
		row.SetString(1, 0, grid.TrimTail(line, cols-4), rowFG, rowBG, 0)
	}
}

// forgetRecent takes a pane out of the order panes were last used in,
// for a pane the window has let go of.
func (a *app) forgetRecent(w ui.Widget) {
	a.recent = slices.DeleteFunc(a.recent, func(have ui.Widget) bool { return have == w })
}
