package view

import (
	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// The Servers pane: every machine kakel knows, with how its connection
// is doing, and under each what is open on it in every window, with
// Quick Connect, Add Server and Connect to Window along the top. It is
// what the sidebar was, in a tab of its own. A pane's row in another
// window says so, and a click there brings that window to the front.

// serversPane holds the machines' list, which is the window's: its rows
// are the ones the keys and menus of the list work on.
type serversPane struct {
	w      *Window
	head   *widget.Label
	quick  *widget.Button
	add    *widget.Button
	attach *widget.Button
	// words are the buttons' labels, and headShown says the heading has
	// room beside them.
	words     [3]string
	headShown bool
	body      *widget.Scroll
}

func newServersPane(w *Window) *serversPane {
	p := &serversPane{
		w:      w,
		head:   widget.NewLabel("Servers"),
		quick:  iconButton(icon.Zap, "Quick Connect…"),
		add:    iconButton(icon.Plus, "Add Server…"),
		attach: iconButton(icon.Plug, "Connect to Window…"),
	}
	p.head.Size = widget.DialogTitleSize
	p.words = [3]string{p.quick.Label, p.add.Label, p.attach.Label}
	p.quick.OnActivate(func(u *gunim.UI) { w.run("server.connect", u) })
	p.add.OnActivate(func(u *gunim.UI) { w.run("server.add", u) })
	p.attach.OnActivate(func(u *gunim.UI) { w.run("serve.attach", u) })
	col := widget.Column(w.list)
	col.Cross = widget.CrossStretch
	p.body = widget.NewScroll(col)
	return p
}

// Handle implements [gunim.Handler]: the keyboard coming into the pane
// makes it the one in front.
func (p *serversPane) Handle(e gi.Event, u *gunim.UI) bool {
	if _, ok := e.(gi.FocusEntered); ok {
		p.w.entered(p.w.paneOfKind(app.KindServers), u)
	}
	return false
}

// Children implements [gunim.Composite].
func (p *serversPane) Children() []gunim.Node {
	return []gunim.Node{p.head, p.quick, p.add, p.attach, p.body}
}

// Layout implements [gunim.Node]: the heading and the buttons along the
// top, the list under them.
func (p *serversPane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const pad, top, gap = 16, 56, 8
	head, quick, add, attach, body := kids.At(0), kids.At(1), kids.At(2), kids.At(3), kids.At(4)
	buttons := []gunim.Child{attach, add, quick}
	place := func() float32 {
		x := c.Max.W - pad
		for _, b := range buttons {
			s := b.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, top)})
			x -= s.W
			b.Place(geom.Pt(x, (top-s.H)/2))
			x -= gap
		}
		return x
	}
	// Narrow, the buttons keep their icons alone, their words said as
	// the pointer rests on them; the heading shows where it fits.
	p.labelled(true)
	x := place()
	if x < pad+100 {
		p.labelled(false)
		x = place()
	}
	hs := head.Layout(gunim.Constraints{Max: geom.Sz(max(1, x-pad), top)})
	head.Place(geom.Pt(pad, (top-hs.H)/2))
	p.headShown = x-pad >= hs.W
	body.Layout(gunim.Tight(geom.Sz(c.Max.W, max(0, c.Max.H-top))))
	body.Place(geom.Pt(0, top))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *serversPane) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(look.SidebarFill.Get(f.Theme)))
	defer pt.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true})()
	for k := range kids.All {
		if k.Node() == p.head && !p.headShown {
			continue
		}
		k.Paint(pt)
	}
}

// labelled gives the buttons their words, or takes them away to leave
// the icons alone, the words said as the pointer rests on them.
func (p *serversPane) labelled(on bool) {
	for i, b := range []*widget.Button{p.quick, p.add, p.attach} {
		b.Label, b.Tooltip = p.words[i], ""
		if !on {
			b.Label, b.Tooltip = "", p.words[i]
		}
	}
}

// showServers brings the list up to date, while the Servers pane is
// in this window: out of the tree, nothing can be added to it, and it
// catches up as it comes back.
func (w *Window) showServers(st app.State, u *gunim.UI) {
	if w.serversView == nil || !w.listShown {
		return
	}
	rows := w.rows
	widget.Sync(w.list, u, rows,
		func(r sideItem) widget.Key { return widget.Key(r.key) },
		func(r sideItem) *sideRow { return w.newSideRow(r) },
		func(row *sideRow, r sideItem, u *gunim.UI) { row.set(r) })
	for _, r := range rows {
		if row, ok := widget.RowOf[*sideRow](w.list, widget.Key(r.key)); ok && !r.heading {
			row.setActive(r.pane != "" && r.pane == w.lastWorked, u)
		}
	}
	w.quietNotes(u)
	if w.lastWorked != w.revealed {
		// The list follows the panes: the row of the pane last worked
		// in scrolls into view.
		w.revealed = w.lastWorked
		if row, ok := widget.RowOf[*sideRow](w.list, widget.Key(w.lastWorked)); ok {
			u.Reveal(row)
		}
	}
}

// windowNotes says, on the rows of panes in another of kakel's windows
// than own, that they are there: a click on one brings it to the front.
func windowNotes(rows []sideItem, all []app.Pane, own int) []sideItem {
	in := map[string]int{}
	for _, p := range all {
		in[p.ID] = p.Window
	}
	for i, r := range rows {
		n, ok := in[r.pane]
		if !ok || n == own || r.heading || r.key != r.pane {
			continue
		}
		where := "another window"
		if r.note != "" {
			where += ", " + r.note
		}
		rows[i].note = where
	}
	return rows
}

// serversRow returns the row the keyboard goes to in the Servers pane:
// the one of the pane last worked in, or else the first that is not a
// heading, or else the first; never one on its way out.
func (w *Window) serversRow(u *gunim.UI) gunim.Node {
	staying := func(k widget.Key) (*sideRow, bool) {
		row, ok := widget.RowOf[*sideRow](w.list, k)
		if !ok || u.Presence(row) == gunim.Exiting {
			return nil, false
		}
		return row, true
	}
	if row, ok := staying(widget.Key(w.lastWorked)); ok {
		return row
	}
	var first gunim.Node
	for _, k := range w.list.Keys() {
		row, ok := staying(k)
		if !ok {
			continue
		}
		if !row.heading {
			return row
		}
		if first == nil {
			first = row
		}
	}
	return first
}
