package main

import (
	"image/color"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/ui"
)

// The window: a sidebar listing the panes, beside the stage, which
// shows the focused pane's group, over a status line.

// The window's own tokens.
var (
	sidebarFill = theme.Color("gunimterm.sidebar", color.NRGBA{R: 0x1b, G: 0x1e, B: 0x26, A: 0xff})
	rowActive   = theme.Color("gunimterm.row.active", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0x40})
	rowHover    = theme.Color("gunimterm.row.hover", color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x12})
	faint       = theme.Color("gunimterm.faint", color.NRGBA{R: 0x8a, G: 0x93, B: 0xa6, A: 0xff})
	noGap       = theme.Length("gunimterm.nogap", 0)
	smallText   = theme.Length("gunimterm.small", 12)
)

// window is the view the program's state drives.
type window struct {
	top     *widget.Flex
	bar     *widget.Menubar
	outer   *widget.Split
	side    *panel
	list    *widget.List
	stage   *stage
	status  *statusLine
	shells  *shells
	keys    *ui.Keymap
	terms   map[string]*term
	splits  map[string]*widget.Split
	focused string
	palette *widget.Palette
	size    geom.Size
	// panes are the panes as last published, and sw the switcher while
	// it is open.
	panes []Pane
	sw    *switcher
	// toasts shows the program's notices, and shown is the last one
	// shown.
	toasts *widget.Toasts
	shown  uint64
	// dialog is the dialog open over the window, which keeps the
	// keyboard until it starts to leave.
	dialog *widget.Dialog
}

func newWindow(sh *shells, keys *ui.Keymap) *window {
	w := &window{
		list:   widget.NewList(),
		stage:  &stage{},
		shells: sh,
		keys:   keys,
		terms:  map[string]*term{},
		splits: map[string]*widget.Split{},
	}
	heading := widget.NewLabel("This computer")
	heading.Size, heading.Color = smallText, faint
	side := widget.Column(widget.NewPad(heading), w.list)
	side.Cross = widget.CrossStretch
	w.status = newStatusLine()
	main := widget.Column(w.stage, w.status).Grow(w.stage, 1)
	main.Cross, main.Gap = widget.CrossStretch, noGap
	w.side = &panel{child: widget.NewScroll(side), least: 220}
	w.outer = widget.NewSplit(w.side, main)
	w.outer.Fixed = true
	w.outer.SetShare(220, nil)
	w.outer.OnMove = func(v float32) gunim.Intent { return SidebarMoved{Width: v} }
	w.bar = widget.NewMenubar()
	for _, m := range menus {
		bm := widget.BarMenu{Title: m.title}
		for i, it := range m.items {
			hint := ""
			if chord, ok := keys.ChordFor(it.id); ok {
				hint = chordLabel(chord)
			}
			bm.Items, bm.Hints = append(bm.Items, it.title), append(bm.Hints, hint)
			bm.Checked = append(bm.Checked, false)
			if it.group {
				bm.Breaks = append(bm.Breaks, i)
			}
		}
		w.bar.Menus = append(w.bar.Menus, bm)
	}
	w.bar.Pick = func(m, i int, u *gunim.UI) { w.run(menus[m].items[i].id, u) }
	w.toasts = &widget.Toasts{}
	w.top = widget.Column(w.bar, w.outer).Grow(w.outer, 1)
	w.top.Cross, w.top.Gap = widget.CrossStretch, noGap
	w.palette = &widget.Palette{Placeholder: "Type a command", Pick: func(i int, u *gunim.UI) {
		w.run(commands[i].id, u)
	}}
	for _, c := range commands {
		item := widget.PaletteItem{Title: c.title}
		if chord, ok := keys.ChordFor(c.id); ok {
			item.Hint = chordLabel(chord)
		}
		w.palette.Items = append(w.palette.Items, item)
	}
	return w
}

// run carries out a command: the window's own here, and the program's
// by asking it.
func (w *window) run(id string, u *gunim.UI) bool {
	switch id {
	case "palette.open":
		w.palette.Open(w, geom.Rc(0, 48, w.size.W, 0), u)
		return true
	case "menu.open":
		w.bar.Open(0, u)
		return true
	case "pane.switch":
		w.openSwitcher(u)
		return true
	case "pane.rename":
		w.rename(u)
		return true
	case "edit.paste":
		if t, ok := w.terms[w.focused]; ok {
			t.paste(u.Clipboard())
		}
		return true
	}
	if in, ok := commandIntent(id); ok {
		u.Send(w, in)
		return true
	}
	return false
}

// Children implements [gunim.Composite].
func (w *window) Children() []gunim.Node { return []gunim.Node{w.top, w.toasts} }

// Layout implements [gunim.Node]. The switcher, while open, covers the
// window.
func (w *window) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	w.size = c.Max
	for k := range kids.All {
		if k.Node() == w.toasts {
			// The toasts sit in the bottom right corner.
			const margin = 16
			s := k.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W-2*margin, c.Max.H-2*margin)})
			k.Place(geom.Pt(c.Max.W-margin-s.W, c.Max.H-margin-s.H))
			continue
		}
		k.Layout(gunim.Tight(c.Max))
		k.Place(geom.Point{})
	}
	return c.Max
}

// Paint implements [gunim.Node].
func (w *window) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(p)
	}
}

// rename asks for a new name for the pane with the keyboard.
func (w *window) rename(u *gunim.UI) {
	id := w.focused
	current := ""
	for _, p := range w.panes {
		if p.ID == id {
			current = p.Title
		}
	}
	if id == "" {
		return
	}
	name := widget.NewTextField()
	name.SetText(current)
	name.Placeholder = "The shell's own title"
	d := widget.NewDialog("Rename the pane")
	d.Body = widget.NewForm().Add("Name", name)
	d.SetButtons("Rename", "Cancel")
	d.OnAccept = func() gunim.Intent { return RenamePane{Pane: id, Title: name.Text()} }
	d.Dismiss = DialogClosed{}
	u.Insert(w, d)
	u.Focus(d)
	w.dialog = d
	// The pane takes the keyboard back once the dialog has closed.
	w.focused = ""
}

// openSwitcher shows every pane, shrunk into a grid over the window.
func (w *window) openSwitcher(u *gunim.UI) {
	if w.sw != nil || len(w.panes) == 0 {
		return
	}
	w.sw = newSwitcher(w, w.panes, w.focused, u)
	u.Insert(w, w.sw)
	u.Focus(w.sw)
	w.sw.light(w.sw.hot, u)
}

// closeSwitcher lets the switcher go. With back, the keyboard goes back
// to the pane that had it; otherwise to the pane picked, once the
// program has put it on stage.
func (w *window) closeSwitcher(back bool, u *gunim.UI) {
	if w.sw == nil {
		return
	}
	u.Remove(w.sw)
	w.sw = nil
	if t, ok := w.terms[w.focused]; ok && back {
		u.Focus(t)
		return
	}
	w.focused = ""
}

// Handle implements [gunim.Handler]: the window's shortcuts, which the
// focused pane passes on.
func (w *window) Handle(e input.Event, u *gunim.UI) bool {
	k, ok := e.(input.KeyPress)
	if !ok {
		return false
	}
	ev, ok := keyEvent(k)
	if !ok {
		return false
	}
	id, ok := w.keys.Lookup(ui.ChordOf(ev))
	if !ok {
		return false
	}
	return w.run(id, u)
}

// update shows st.
func (w *window) update(st State, u *gunim.UI) {
	th := u.Theme()
	w.panes = st.Panes
	widget.Sync(w.list, u, st.Panes,
		func(p Pane) widget.Key { return widget.Key(p.ID) },
		func(p Pane) *sideRow { return newSideRow(p) },
		func(r *sideRow, p Pane, u *gunim.UI) { r.title.SetText(p.Title) })
	for _, p := range st.Panes {
		if r, ok := widget.RowOf[*sideRow](w.list, widget.Key(p.ID)); ok {
			r.setActive(p.ID == st.Focus, u)
		}
	}

	keep := map[string]bool{}
	w.stage.show(w.build(st.Stage, keep), u)
	for id := range w.splits {
		if !keep[id] {
			delete(w.splits, id)
		}
	}
	for id, t := range w.terms {
		if w.shells.get(id) == nil {
			delete(w.terms, id)
			continue
		}
		t.sync()
	}
	if w.dialog != nil && u.Presence(w.dialog) == gunim.Exiting {
		w.dialog = nil
	}
	if st.Focus != w.focused && w.sw == nil && w.dialog == nil {
		w.focused = st.Focus
		if t, ok := w.terms[st.Focus]; ok {
			u.Focus(t)
		}
	}

	width := float32(0)
	if st.Sidebar {
		width = st.SidebarWidth
		w.side.least = st.SidebarWidth
	}
	if w.outer.Share() != width {
		w.outer.SetShare(width, widget.Settle.Get(th))
	}
	w.status.set(st.Status, u)
	for _, n := range st.Notices {
		if n.ID <= w.shown {
			continue
		}
		w.shown = n.ID
		if n.Clipboard != "" {
			u.SetClipboard(n.Clipboard)
		}
		w.toasts.Show(widget.Toast{Title: n.Title, Body: n.Body}, u)
	}
	// The View menu ticks the sidebar while it shows.
	for m := range menus {
		for i, it := range menus[m].items {
			if it.id == "sidebar.toggle" {
				w.bar.Menus[m].Checked[i] = st.Sidebar
			}
		}
	}
	u.Invalidate()
}

// build returns the nodes for b, making the ones it lacks. Terminals
// are kept by pane, so a pane moving about keeps its screen. A split
// is kept while its panes stay the same, and made afresh once they
// change, so a change anywhere makes a new tree above it, which the
// stage swaps in whole.
func (w *window) build(b *Box, keep map[string]bool) gunim.Node {
	switch {
	case b == nil:
		return nil
	case b.Pane != "":
		return w.term(b.Pane)
	}
	a, c := w.build(b.A, keep), w.build(b.B, keep)
	keep[b.ID] = true
	if sp, ok := w.splits[b.ID]; ok {
		if first, second := sp.Panes(); first == a && second == c {
			if !sp.Held() && sp.Share() != b.Share {
				sp.SetShare(b.Share, widget.Settle.Default())
			}
			return sp
		}
	}
	sp := widget.NewSplit(a, c)
	sp.Vertical = b.Vertical
	id := b.ID
	sp.OnMove = func(v float32) gunim.Intent { return SplitMoved{Split: id, Share: v} }
	if b.Opening {
		// The new pane, second, slides in from the edge.
		sp.SetShare(1, nil)
		sp.SetShare(b.Share, widget.Settle.Default())
	} else {
		sp.SetShare(b.Share, nil)
	}
	w.splits[b.ID] = sp
	return sp
}

func (w *window) term(id string) *term {
	if t, ok := w.terms[id]; ok {
		return t
	}
	t := newTerm(id, w.shells.get(id), w.keys)
	w.terms[id] = t
	return t
}

// stage holds the arrangement on screen, and fills the space it has.
type stage struct {
	shown gunim.Node
}

// show puts n on stage in place of what was there. The old nodes leave
// before the new arrive, so a pane in both moves across and stays.
func (s *stage) show(n gunim.Node, u *gunim.UI) {
	if n == s.shown {
		return
	}
	if s.shown != nil {
		u.Remove(s.shown)
	}
	s.shown = n
	if n != nil {
		u.Insert(s, n)
	}
}

// Layout implements [gunim.Node].
func (s *stage) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	for k := range kids.All {
		k.Layout(gunim.Tight(c.Max))
		k.Place(geom.Point{})
	}
	return c.Max
}

// Paint implements [gunim.Node].
func (s *stage) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(p)
	}
}

// panel fills its space with the sidebar's colour, behind its child.
// Its child keeps at least the sidebar's width, cut off at the panel's
// edge, so the sidebar slides away whole as the panel narrows.
type panel struct {
	child gunim.Node
	least float32
}

// Children implements [gunim.Composite].
func (p *panel) Children() []gunim.Node { return []gunim.Node{p.child} }

// Layout implements [gunim.Node].
func (p *panel) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	w := max(c.Max.W, p.least)
	k.Layout(gunim.Tight(geom.Sz(w, c.Max.H)))
	// Narrowed, it slides out to the left.
	k.Place(geom.Pt(c.Max.W-w, 0))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *panel) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if box.W < 1 {
		return
	}
	defer pt.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true})()
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(sidebarFill.Get(f.Theme)))
	kids.At(0).Paint(pt)
}

// sideRow is a pane's row in the sidebar: its title, lit while its pane
// has the keyboard. A click on it brings the pane forward.
type sideRow struct {
	anim.Group
	id     string
	title  *widget.Label
	active *anim.Float
	hover  *anim.Float
	on     bool
}

func newSideRow(p Pane) *sideRow {
	r := &sideRow{id: p.ID, title: widget.NewLabel(p.Title), active: anim.NewFloat(0), hover: anim.NewFloat(0)}
	r.title.MaxLines = 1
	r.Add(r.active, r.hover)
	return r
}

func (r *sideRow) setActive(on bool, u *gunim.UI) {
	if on == r.on {
		return
	}
	r.on = on
	to := float32(0)
	if on {
		to = 1
	}
	r.active.Animate(to, widget.Quick.Get(u.Theme()))
}

// Children implements [gunim.Composite].
func (r *sideRow) Children() []gunim.Node { return []gunim.Node{r.title} }

// Layout implements [gunim.Node].
func (r *sideRow) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const padX, height = 12, 28
	k := kids.At(0)
	s := k.Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-2*padX), height)})
	k.Place(geom.Pt(padX, (height-s.H)/2))
	return c.Constrain(geom.Sz(c.Max.W, height))
}

// Paint implements [gunim.Node].
func (r *sideRow) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	inset := geom.Rect{Min: geom.Pt(6, 1), Max: geom.Pt(box.W-6, box.H-1)}
	if t := r.hover.Value(); t > 0.01 {
		c := rowHover.Get(f.Theme)
		c.A = uint8(float32(c.A) * min(t, 1))
		p.RRect(inset, 6, paint.Solid(c))
	}
	if t := r.active.Value(); t > 0.01 {
		c := rowActive.Get(f.Theme)
		c.A = uint8(float32(c.A) * min(t, 1))
		p.RRect(inset, 6, paint.Solid(c))
	}
	kids.At(0).Paint(p)
}

// Handle implements [gunim.Handler].
func (r *sideRow) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerEnter:
		r.hover.Animate(1, widget.Quick.Get(u.Theme()))
	case input.PointerLeave:
		r.hover.Animate(0, widget.Settle.Get(u.Theme()))
	case input.PointerDown:
		if e.Button == input.ButtonPrimary {
			u.Send(r, FocusPane{Pane: r.id})
			return true
		}
		return false
	default:
		return false
	}
	return false
}

// statusLine says what just happened, under the stage. Empty, it takes
// no room; given something to say, it grows into place.
type statusLine struct {
	anim.Group
	label  *widget.Label
	height *anim.Float
	text   string
}

func newStatusLine() *statusLine {
	l := widget.NewLabel("")
	l.Size, l.Color, l.MaxLines = smallText, faint, 1
	s := &statusLine{label: l, height: anim.NewFloat(0)}
	s.Add(s.height)
	return s
}

func (s *statusLine) set(text string, u *gunim.UI) {
	if text == s.text {
		return
	}
	s.text = text
	to := float32(0)
	if text != "" {
		to = 24
		s.label.SetText(text)
	}
	s.height.Animate(to, widget.Settle.Get(u.Theme()))
}

// Children implements [gunim.Composite].
func (s *statusLine) Children() []gunim.Node { return []gunim.Node{s.label} }

// Layout implements [gunim.Node].
func (s *statusLine) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	h := max(0, s.height.Value())
	k := kids.At(0)
	size := k.Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-24), 24)})
	k.Place(geom.Pt(12, (24-size.H)/2))
	return c.Constrain(geom.Sz(c.Max.W, h))
}

// Paint implements [gunim.Node]: the line shows as far as it has grown.
func (s *statusLine) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, kids gunim.Children) {
	if box.H < 1 {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true})()
	kids.At(0).Paint(p)
}
