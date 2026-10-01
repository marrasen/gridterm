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

// serversPane holds the machines' cards, which are the window's: their
// rows are the ones the keys and menus of the list work on. Along the
// top are its title, a field that finds a machine as it is typed and
// connects to one typed as user@host, and Add, whose menu adds a
// server, connects to one, or to a window, or reads the SSH config.
type serversPane struct {
	w      *Window
	head   *widget.Label
	search *widget.TextField
	add    *widget.MenuButton
	// headShown says the title has room beside the field.
	headShown bool
	body      *widget.Scroll
}

// addItems are the Add menu's lines, and addCommands what each runs.
var (
	addItems    = []string{"Add Server…", "Quick Connect…", "Connect to Window…", "Import from SSH Config"}
	addCommands = []string{"server.add", "server.connect", "serve.attach", "server.import"}
)

func newServersPane(w *Window) *serversPane {
	p := &serversPane{
		w:      w,
		head:   widget.NewLabel("Servers"),
		search: widget.NewTextField(),
		add:    widget.NewMenuButton("Add", addItems...),
	}
	p.head.Size = widget.DialogTitleSize
	p.search.Icon = icon.Search
	p.search.Placeholder = "Find a machine, or user@host to connect"
	p.search.OnEdit = func(text string, u *gunim.UI) { w.cards.find(text, u) }
	p.search.OnSubmit = func(text string) gunim.Intent { return w.cards.submit(text) }
	p.add.Icon = icon.Plus
	p.add.Icons = []*icon.Icon{icon.Server, icon.Zap, icon.Plug, icon.FileInput}
	p.add.Picked = func(i int, u *gunim.UI) { w.run(addCommands[i], u) }
	p.body = widget.NewScroll(w.cards.grid)
	return p
}

// Handle implements [gunim.Handler]: the keyboard coming into the pane
// makes it the one in front, and a press on its empty room gives it the
// keyboard, on the row it would have, as a press in a terminal does.
func (p *serversPane) Handle(e gi.Event, u *gunim.UI) bool {
	switch e.(type) {
	case gi.FocusEntered:
		p.w.entered(p.w.paneOfKind(app.KindServers), u)
	case gi.PointerDown:
		if row := p.w.serversRow(u); row != nil {
			u.Focus(row)
			return true
		}
	}
	return false
}

// Children implements [gunim.Composite].
func (p *serversPane) Children() []gunim.Node {
	return []gunim.Node{p.head, p.search, p.add, p.body}
}

// Layout implements [gunim.Node]: the title, the field and Add along
// the top, the cards under them.
func (p *serversPane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const pad, top, gap = 16, 60, 10
	head, search, add, body := kids.At(0), kids.At(1), kids.At(2), kids.At(3)
	as := add.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, top)})
	x := c.Max.W - pad - as.W
	add.Place(geom.Pt(x, (top-as.H)/2))
	hs := head.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, top)})
	// The field takes what is left, up to a comfortable width; the
	// title shows where it still fits beside it.
	p.headShown = x-gap-pad-hs.W-gap >= 160
	left := float32(pad)
	if p.headShown {
		left += hs.W + 2*gap
	}
	fw := min(x-gap-left, 420)
	if p.headShown {
		// Kept against Add, the title at the start.
		left = x - gap - fw
	}
	ss := search.Layout(gunim.Constraints{Min: geom.Sz(max(0, fw), 0), Max: geom.Sz(max(0, fw), top)})
	search.Place(geom.Pt(left, (top-ss.H)/2))
	head.Place(geom.Pt(pad, (top-hs.H)/2))
	body.Layout(gunim.Tight(geom.Sz(c.Max.W, max(0, c.Max.H-top))))
	body.Place(geom.Pt(0, top))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *serversPane) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(look.TermBackground.Get(f.Theme)))
	defer pt.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true})()
	for k := range kids.All {
		if k.Node() == p.head && !p.headShown {
			continue
		}
		k.Paint(pt)
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
	w.cards.sync(rows, u)
	for _, r := range rows {
		if row, ok := w.cards.row(widget.Key(r.key)); ok && !r.heading {
			row.setActive(r.pane != "" && r.pane == w.lastWorked, u)
		}
	}
	w.quietNotes(u)
	if w.lastWorked != w.revealed {
		// The list follows the panes: the row of the pane last worked
		// in scrolls into view.
		w.revealed = w.lastWorked
		if row, ok := w.cards.row(widget.Key(w.lastWorked)); ok {
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
		row, ok := w.cards.row(k)
		if !ok || u.Presence(row) == gunim.Exiting {
			return nil, false
		}
		return row, true
	}
	if row, ok := staying(widget.Key(w.lastWorked)); ok {
		return row
	}
	var first gunim.Node
	for _, k := range w.cards.keys() {
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
