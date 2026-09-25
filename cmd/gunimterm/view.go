package main

import (
	"image/color"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/syntax"
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
	top    *widget.Flex
	bar    *widget.Menubar
	outer  *widget.Split
	side   *panel
	list   *widget.List
	stage  *stage
	status *statusLine
	shells *shells
	keys   *ui.Keymap
	terms  map[string]*term
	// browsers and readers are the file panes and readers, by pane.
	browsers map[string]*browser
	readers  map[string]*reader
	splits   map[string]*widget.Split
	focused  string
	palette  *widget.Palette
	size     geom.Size
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
	// fontSize is the terminals' font size, as last published.
	fontSize float32
	// ask is the dialog asking a connection's question askID, and
	// saved are the saved servers; serverIDs and paletteIDs are the
	// commands of the Servers menu's items and the palette's.
	ask        *widget.Dialog
	askID      uint64
	saved      []remote.Host
	serverIDs  []string
	paletteIDs []string
}

func newWindow(sh *shells, keys *ui.Keymap) *window {
	w := &window{
		list:     widget.NewList(),
		stage:    &stage{},
		shells:   sh,
		keys:     keys,
		terms:    map[string]*term{},
		browsers: map[string]*browser{},
		readers:  map[string]*reader{},
		splits:   map[string]*widget.Split{},
	}
	side := widget.Column(w.list)
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
			if chord, ok := keys.ChordFor(it.id); ok && !it.caption {
				hint = chordLabel(chord)
			}
			bm.Items, bm.Hints = append(bm.Items, it.title), append(bm.Hints, hint)
			bm.Checked = append(bm.Checked, false)
			if it.caption {
				bm.Captions = append(bm.Captions, i)
			}
			if it.group || (it.caption && i > 0) {
				bm.Breaks = append(bm.Breaks, i)
			}
		}
		w.bar.Menus = append(w.bar.Menus, bm)
	}
	w.bar.Pick = func(m, i int, u *gunim.UI) {
		switch {
		case m < len(menus):
			w.run(menus[m].items[i].id, u)
		case i < len(w.serverIDs):
			w.run(w.serverIDs[i], u)
		}
	}
	w.bar.Menus = append(w.bar.Menus, widget.BarMenu{Title: "Servers"})
	w.toasts = &widget.Toasts{}
	w.top = widget.Column(w.bar, w.outer).Grow(w.outer, 1)
	w.top.Cross, w.top.Gap = widget.CrossStretch, noGap
	w.palette = &widget.Palette{Placeholder: "Type a command", Pick: func(i int, u *gunim.UI) {
		if i < len(w.paletteIDs) {
			w.run(w.paletteIDs[i], u)
		}
	}}
	w.servers(nil)
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
	case "server.connect":
		w.connectDialog(u)
		return true
	case "server.add":
		w.serverForm(nil, u)
		return true
	case "files.goTo":
		if b, ok := w.browsers[w.focused]; ok {
			b.askGoTo(u)
		}
		return true
	case "edit.paste":
		if t, ok := w.terms[w.focused]; ok {
			t.paste(u.Clipboard())
		}
		return true
	case "edit.copy":
		if t, ok := w.terms[w.focused]; ok {
			t.copySelection(u)
		}
		return true
	}
	if name, ok := strings.CutPrefix(id, "server.open:"); ok {
		u.Send(w, ConnectTo{Saved: name})
		return true
	}
	if name, ok := strings.CutPrefix(id, "server.edit:"); ok {
		for _, h := range w.saved {
			if h.Name == name {
				w.serverForm(&h, u)
			}
		}
		return true
	}
	if name, ok := strings.CutPrefix(id, "server.remove:"); ok {
		w.confirmRemove(name, u)
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

// openDialog shows d over the window, with the keyboard, until it
// closes.
func (w *window) openDialog(d *widget.Dialog, u *gunim.UI) {
	u.Insert(w, d)
	u.Focus(d)
	w.dialog = d
	// The pane takes the keyboard back once the dialog has closed.
	w.focused = ""
}

// connectDialog asks which server to connect to.
func (w *window) connectDialog(u *gunim.UI) {
	target := widget.NewTextField()
	target.Placeholder = "user@host or user@host:port"
	d := widget.NewDialog("Connect to a server")
	d.Body = widget.NewForm().Add("Server", target)
	d.SetButtons("Connect", "Cancel")
	d.OnAccept = func() gunim.Intent { return ConnectTo{Target: target.Text()} }
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// showAsk shows the oldest question a connection is waiting on, in a
// dialog, and takes the dialog away when its question goes, as when the
// connection gives up.
func (w *window) showAsk(asks []Ask, u *gunim.UI) {
	if w.ask != nil {
		for _, q := range asks {
			if q.ID == w.askID {
				return
			}
		}
		u.Remove(w.ask)
		w.ask = nil
	}
	if len(asks) == 0 || w.dialog != nil && u.Presence(w.dialog) != gunim.Exiting {
		return
	}
	q := asks[0]
	form := widget.NewForm()
	if q.Text != "" {
		note := widget.NewLabel(q.Text)
		form.Add("", note)
	}
	var fields []*widget.TextField
	for i, prompt := range q.Prompts {
		f := widget.NewTextField()
		f.Secret = i < len(q.Secret) && q.Secret[i]
		fields = append(fields, f)
		form.Add(strings.TrimSuffix(strings.TrimSpace(prompt), ":"), f)
	}
	var also *widget.Checkbox
	if q.Also != "" {
		also = widget.NewCheckbox(q.Also)
		form.Add("", also)
	}
	d := widget.NewDialog(q.Title)
	d.Body = form
	id := q.ID
	answer := func(choice string) gunim.Intent {
		answers := make([]string, len(fields))
		for i, f := range fields {
			answers[i] = f.Text()
		}
		if choice != "" {
			answers = append(answers, choice)
		}
		if also != nil {
			yes := ""
			if also.On {
				yes = "yes"
			}
			answers = append(answers, yes)
		}
		return AskAnswered{ID: id, Yes: true, Answers: answers}
	}
	no := q.No
	if no == "" {
		no = "Cancel"
	}
	if len(q.Choose) > 0 {
		d.SetButtons(q.Choose[0], no)
		d.OnAccept = func() gunim.Intent { return answer(q.Choose[0]) }
		for _, c := range q.Choose[1:] {
			d.AddButton(c, func() gunim.Intent { return answer(c) })
		}
	} else {
		d.SetButtons(q.Yes, no)
		d.OnAccept = func() gunim.Intent { return answer("") }
	}
	d.Dismiss = AskAnswered{ID: id}
	w.ask, w.askID = d, id
	w.openDialog(d, u)
}

// servers fills the Servers menu and the palette: the window's own
// commands, then the saved servers, each to connect to, and in the
// palette to edit or remove too.
func (w *window) servers(saved []remote.Host) {
	w.saved = saved
	hint := func(id string) string {
		if chord, ok := w.keys.ChordFor(id); ok {
			return chordLabel(chord)
		}
		return ""
	}
	m := widget.BarMenu{Title: "Servers",
		Items: []string{"Connect to Server…", "Add Server…"},
		Hints: []string{hint("server.connect"), ""}}
	w.serverIDs = []string{"server.connect", "server.add"}
	if len(saved) > 0 {
		m.Breaks, m.Captions = []int{2}, []int{2}
		m.Items, m.Hints = append(m.Items, "Saved"), append(m.Hints, "")
		w.serverIDs = append(w.serverIDs, "")
		for _, h := range saved {
			m.Items, m.Hints = append(m.Items, h.Name), append(m.Hints, "")
			w.serverIDs = append(w.serverIDs, "server.open:"+h.Name)
		}
	}
	for i := range w.bar.Menus {
		if w.bar.Menus[i].Title == "Servers" {
			w.bar.Menus[i] = m
		}
	}
	w.palette.Items, w.paletteIDs = nil, nil
	for _, c := range commands {
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: c.title, Hint: hint(c.id)})
		w.paletteIDs = append(w.paletteIDs, c.id)
	}
	w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Add Server"})
	w.paletteIDs = append(w.paletteIDs, "server.add")
	for _, h := range saved {
		for _, c := range []struct{ title, id string }{
			{"Connect to " + h.Name, "server.open:"},
			{"Edit Server " + h.Name, "server.edit:"},
			{"Remove Server " + h.Name, "server.remove:"},
		} {
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: c.title, Also: []string{h.Address}})
			w.paletteIDs = append(w.paletteIDs, c.id+h.Name)
		}
	}
}

// serverForm asks for a server to save: a new one, or old edited.
func (w *window) serverForm(old *remote.Host, u *gunim.UI) {
	name, addr, port, user, key := widget.NewTextField(), widget.NewTextField(), widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	addr.Placeholder, port.Placeholder = "host name or address", "22"
	user.Placeholder, key.Placeholder = "your name here", "the usual keys in ~/.ssh"
	// Through lists the other saved servers, to reach this one through.
	through := []string{"Directly"}
	ids := []string{""}
	for _, h := range w.saved {
		if old == nil || h.ID != old.ID {
			through = append(through, h.Name)
			ids = append(ids, h.ID)
		}
	}
	via := widget.NewDropdown(through...)
	via.Label = "Through"
	title, under := "Add a server", ""
	if old != nil {
		title, under = "Edit "+old.Name, old.Name
		name.SetText(old.Name)
		addr.SetText(old.Address)
		if old.Port != 0 {
			port.SetText(strconv.Itoa(old.Port))
		}
		user.SetText(old.User)
		if len(old.Identities) > 0 {
			key.SetText(old.Identities[0])
		}
		for i, id := range ids {
			if id != "" && id == old.Via {
				via.Selected = i
			}
		}
	}
	host := func() (remote.Host, string) {
		h := remote.Host{Name: strings.TrimSpace(name.Text()), Address: strings.TrimSpace(addr.Text()), User: strings.TrimSpace(user.Text())}
		if old != nil {
			h = *old
			h.Name, h.Address, h.User = strings.TrimSpace(name.Text()), strings.TrimSpace(addr.Text()), strings.TrimSpace(user.Text())
			h.Port, h.Identities = 0, nil
		}
		if p := strings.TrimSpace(port.Text()); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n <= 0 || n > 65535 {
				return h, "The port is a number from 1 to 65535."
			}
			h.Port = n
		}
		if k := strings.TrimSpace(key.Text()); k != "" {
			h.Identities = []string{k}
		}
		h.Via = ids[max(0, min(via.Selected, len(ids)-1))]
		if err := h.Validate(); err != nil {
			return h, upperFirst(err.Error()) + "."
		}
		return h, ""
	}
	d := widget.NewDialog(title)
	d.Body = widget.NewForm().Add("Name", name).Add("Address", addr).Add("Port", port).Add("User", user).Add("Through", via).Add("Key file", key)
	d.SetButtons("Save", "Cancel")
	d.Check = func() string {
		_, problem := host()
		return problem
	}
	d.OnAccept = func() gunim.Intent {
		h, _ := host()
		return SaveServer{Host: h, Under: under}
	}
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// confirmRemove asks before forgetting a saved server.
func (w *window) confirmRemove(name string, u *gunim.UI) {
	d := widget.NewDialog("Remove " + name + "?")
	d.Body = widget.NewLabel("Its panes stay open. Connecting to it again takes its address.")
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.Accept = RemoveServer{Name: name}
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// upperFirst capitalises the first letter of s.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
	w.openDialog(d, u)
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
	if n := w.focusNode(w.focused); n != nil && back {
		u.Focus(n)
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
	if st.FontSize != w.fontSize {
		w.fontSize = st.FontSize
		for _, t := range w.terms {
			t.cells.Size = st.FontSize
		}
	}
	rows := sidebarRows(st.Panes)
	widget.Sync(w.list, u, rows,
		func(r sideItem) widget.Key { return widget.Key(r.key) },
		func(r sideItem) *sideRow { return newSideRow(r) },
		func(row *sideRow, r sideItem, u *gunim.UI) { row.title.SetText(r.text) })
	for _, r := range rows {
		if row, ok := widget.RowOf[*sideRow](w.list, widget.Key(r.key)); ok && !r.heading {
			row.setActive(r.key == st.Focus, u)
		}
	}
	w.showAsk(st.Asks, u)
	if !slices.EqualFunc(st.Saved, w.saved, func(a, b remote.Host) bool { return a.ID == b.ID && a.Name == b.Name && a.Address == b.Address }) {
		w.servers(st.Saved)
	}

	keep := map[string]bool{}
	w.stage.show(w.build(st.Stage, keep), u)
	for id := range w.splits {
		if !keep[id] {
			delete(w.splits, id)
		}
	}
	open := map[string]bool{}
	for _, p := range st.Panes {
		open[p.ID] = true
	}
	for id, b := range w.browsers {
		if !open[id] {
			delete(w.browsers, id)
			continue
		}
		b.show(st.Browsers[id], u)
	}
	for id, r := range w.readers {
		if !open[id] {
			delete(w.readers, id)
			continue
		}
		next := st.Readers[id]
		if next.Path != r.st.Path {
			r.colour = syntax.For(next.Path)
			r.follow = next.Follow
		}
		if next.Seq != r.seq && r.follow {
			// Following, the reader stays at the end as the file grows.
			r.top = len(next.Lines)
		}
		r.seq = next.Seq
		r.st = next
	}
	for id, t := range w.terms {
		if w.shells.get(id) == nil {
			delete(w.terms, id)
			continue
		}
		t.sync()
		if id == st.Focus {
			t.blink(u)
		}
	}
	if w.dialog != nil && u.Presence(w.dialog) == gunim.Exiting {
		w.dialog = nil
	}
	// The pane with the keyboard gets it when it changes, and back when
	// nothing has it, as when the split it sat in went away around it.
	if w.sw == nil && w.dialog == nil && (st.Focus != w.focused || u.Focused() == nil) {
		w.focused = st.Focus
		if n := w.focusNode(st.Focus); n != nil {
			u.Focus(n)
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
		return w.paneNode(b.Pane)
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

// kindOf returns the kind of pane id.
func (w *window) kindOf(id string) string {
	for _, p := range w.panes {
		if p.ID == id {
			return p.Kind
		}
	}
	return kindTerminal
}

// paneNode returns the node that shows pane id, made on first use.
func (w *window) paneNode(id string) gunim.Node {
	switch w.kindOf(id) {
	case kindFiles:
		b, ok := w.browsers[id]
		if !ok {
			b = newBrowser(w, id)
			w.browsers[id] = b
		}
		return b
	case kindReader:
		r, ok := w.readers[id]
		if !ok {
			r = newReader(id, w.fontSize)
			w.readers[id] = r
		}
		return r
	}
	return w.term(id)
}

// focusNode returns the node in pane id that takes the keyboard, or
// nil when there is none yet.
func (w *window) focusNode(id string) gunim.Node {
	if t, ok := w.terms[id]; ok {
		return t
	}
	if b, ok := w.browsers[id]; ok {
		return b.table
	}
	if r, ok := w.readers[id]; ok {
		return r
	}
	return nil
}

func (w *window) term(id string) *term {
	if t, ok := w.terms[id]; ok {
		return t
	}
	t := newTerm(id, w.shells.get(id), w.keys)
	if w.fontSize > 0 {
		t.cells.Size = w.fontSize
	}
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

// sideItem is one row of the sidebar: a machine's heading, or a pane
// under it.
type sideItem struct {
	key, text string
	heading   bool
}

// sidebarRows lists the panes under their machines: this computer
// first, then each server in the order its first pane opened.
func sidebarRows(panes []Pane) []sideItem {
	var machines []string
	seen := map[string]bool{"": true}
	machines = append(machines, "")
	for _, p := range panes {
		if !seen[p.Machine] {
			seen[p.Machine] = true
			machines = append(machines, p.Machine)
		}
	}
	var out []sideItem
	for _, m := range machines {
		name := m
		if m == "" {
			name = "This computer"
		}
		out = append(out, sideItem{key: "machine:" + m, text: name, heading: true})
		for _, p := range panes {
			if p.Machine == m {
				out = append(out, sideItem{key: p.ID, text: p.Title})
			}
		}
	}
	return out
}

// sideRow is a row in the sidebar. A pane's row shows its title, lit
// while the pane has the keyboard, and a click brings the pane
// forward. A machine's heading is small and dim.
type sideRow struct {
	anim.Group
	id      string
	heading bool
	title   *widget.Label
	active  *anim.Float
	hover   *anim.Float
	on      bool
}

func newSideRow(it sideItem) *sideRow {
	r := &sideRow{id: it.key, heading: it.heading, title: widget.NewLabel(it.text), active: anim.NewFloat(0), hover: anim.NewFloat(0)}
	r.title.MaxLines = 1
	if it.heading {
		r.title.Size, r.title.Color = smallText, faint
	}
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
	if r.heading {
		return false
	}
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
