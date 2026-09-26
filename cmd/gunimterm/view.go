package main

import (
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/settings"
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
	// tunnelPanes are the tunnels' panes, and savedTunnels the tunnels
	// kept, as the palette lists them.
	tunnelPanes map[string]*tunnelPane
	// jobs is the jobs pane, once it has been opened.
	jobs *jobsPane
	// secrets is the secrets pane, once opened, and lastTerm the
	// terminal pane that last had the keyboard.
	secrets  *secretsPane
	lastTerm string
	// share is the agent share as last published, and sharing says the
	// user just asked for one, so its dialog opens once it has a code.
	share Share
	// serving is the serving as last published.
	serving Serving
	// bells is the bells as last published, titles whether panes show
	// their titles, and captions the line over each pane that does.
	// sidebarShown is whether the sidebar shows, as last published.
	sidebarShown bool
	// savedCommands are the commands kept, and remoteWindows the
	// windows connected to, as last published.
	savedCommands []settings.SavedCommand
	remoteWindows []RemoteWindow
	// shellChoices are the shells here, and chosenShell the one kept,
	// as last published.
	shellChoices []ShellChoice
	chosenShell  string
	// termProgram is what new shells are told the terminal is called.
	termProgram string
	// connected are the servers connected to, as last published.
	connected    []string
	bells        uint64
	titles       bool
	captions     map[string]*captioned
	sharing      bool
	savedTunnels []settings.SavedTunnel
	// accounts are the machines with a connection log, as the palette
	// lists them.
	accounts []string
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
	// onStage gives the panes their theme's own colours. contents holds
	// each theme's colours for the stage, by name.
	onStage  *widget.Themed
	contents map[string]theme.Theme
	// fontSize is the terminals' font size, as last published, and
	// themes the themes on offer.
	fontSize float32
	themes   []string
	themeNow string
	// ask is the dialog asking a connection's question askID, and
	// saved are the saved servers; serverIDs and paletteIDs are the
	// commands of the Servers menu's items and the palette's.
	ask        *widget.Dialog
	askID      uint64
	saved      []remote.Host
	serverIDs  []string
	paletteIDs []string
}

func newWindow(sh *shells, keys *ui.Keymap, all []themed) *window {
	w := &window{
		contents:    map[string]theme.Theme{},
		list:        widget.NewList(),
		stage:       &stage{},
		shells:      sh,
		keys:        keys,
		terms:       map[string]*term{},
		browsers:    map[string]*browser{},
		readers:     map[string]*reader{},
		tunnelPanes: map[string]*tunnelPane{},
		splits:      map[string]*widget.Split{},
		captions:    map[string]*captioned{},
	}
	side := widget.Column(w.list)
	side.Cross = widget.CrossStretch
	w.status = newStatusLine()
	w.onStage = widget.NewThemed(w.stage, widget.Dark())
	main := widget.Column(w.onStage, w.status).Grow(w.onStage, 1)
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
		case m < len(menus) && menus[m].title == "Servers":
			if i < len(w.serverIDs) {
				w.run(w.serverIDs[i], u)
			}
		case m < len(menus) && i < len(menus[m].items):
			w.run(menus[m].items[i].id, u)
		}
	}
	w.toasts = &widget.Toasts{}
	w.top = widget.Column(w.bar, w.outer).Grow(w.outer, 1)
	w.top.Cross, w.top.Gap = widget.CrossStretch, noGap
	w.palette = &widget.Palette{Placeholder: "Type a command", Pick: func(i int, u *gunim.UI) {
		if i < len(w.paletteIDs) {
			w.run(w.paletteIDs[i], u)
		}
	}}
	w.servers(nil)
	for _, t := range all {
		w.contents[t.name] = t.content
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
	case "server.connect":
		w.connectDialog(u)
		return true
	case "server.add":
		w.serverForm(nil, u)
		return true
	case "theme.pick":
		w.pickTheme(u)
		return true
	case "conn.log":
		machine := w.machineOf(w.focused)
		if machine == "" {
			w.toasts.Show(widget.Toast{Title: "Connection logs belong to servers", Body: "Open one from a pane on a server."}, u)
			return true
		}
		u.Send(w, ShowLog{Machine: machine})
		return true
	case "pane.scrollback":
		if w.kindOf(w.focused) != kindTerminal {
			w.toasts.Show(widget.Toast{Title: "The pane in front is not a terminal", Body: "Find in Scrollback searches what a terminal has kept."}, u)
			return true
		}
		u.Send(w, ShowScrollback{Pane: w.focused})
		return true
	case "conn.command":
		w.commandDialog(u)
		return true
	case "sidebar.focus":
		w.focusSidebar(u)
		return true
	case "sidebar.closeRow":
		if row, ok := u.Focused().(*sideRow); ok && row.closes != nil {
			u.Send(row, row.closes)
		}
		return true
	case "server.editThis", "server.forget":
		m := w.machineOf(w.focused)
		i := slices.IndexFunc(w.saved, func(h remote.Host) bool { return h.Name == m })
		if i < 0 {
			w.toasts.Show(widget.Toast{Title: "This pane is on no saved server", Body: "Edit This Server works on a pane on a server from the Servers menu."}, u)
			return true
		}
		if id == "server.editThis" {
			h := w.saved[i]
			w.serverForm(&h, u)
		} else {
			w.confirmRemove(m, u)
		}
		return true
	case "view.fullScreen":
		u.SetFullScreen(!u.FullScreen())
		return true
	case "shell.termProgram":
		w.termProgramDialog(u)
		return true
	case "pane.titles":
		u.Send(w, TogglePaneTitles{})
		return true
	case "serve.attach":
		w.connectWindowDialog(u)
		return true
	case "conn.disconnect":
		if m := w.machineOf(w.focused); m != "" {
			u.Send(w, Disconnect{Machine: m})
		} else {
			w.toasts.Show(widget.Toast{Title: "This pane is on this computer", Body: "Disconnect closes the connection to a server or a window."}, u)
		}
		return true
	case "serve.window":
		w.servingDialog(w.serving, u)
		return true
	case "agent.share":
		w.shareDialog(w.share, u)
		return true
	case "agent.permissions":
		w.permissionsDialog(w.share, u)
		return true
	case "secrets.export":
		w.exportForm(u)
		return true
	case "secrets.import":
		w.importForm(u)
		return true
	case "secrets.add", "secrets.addNote":
		kind := secrets.Password
		if id == "secrets.addNote" {
			kind = secrets.Note
		}
		w.secretForm(kind, nil, u)
		return true
	case "tunnel.open", "tunnel.socks":
		w.tunnelDialog(id == "tunnel.socks", u)
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
	if m, ok := strings.CutPrefix(id, "conn.log:"); ok {
		u.Send(w, ShowLog{Machine: m})
		return true
	}
	if m, ok := strings.CutPrefix(id, "on.terminal:"); ok {
		u.Send(w, OpenOn{Machine: m})
		return true
	}
	if m, ok := strings.CutPrefix(id, "on.files:"); ok {
		u.Send(w, FilesOn{Machine: m})
		return true
	}
	if rest, ok := strings.CutPrefix(id, "on.folder:"); ok {
		at, m, _ := strings.Cut(rest, ":")
		i, _ := strconv.Atoi(at)
		for _, h := range w.saved {
			if h.Name == m && i < len(h.Folders) {
				u.Send(w, FilesOn{Machine: m, Path: h.Folders[i]})
			}
		}
		return true
	}
	if s, ok := strings.CutPrefix(id, "shell.open."); ok {
		u.Send(w, OpenShellNamed{ID: s})
		return true
	}
	if s, ok := strings.CutPrefix(id, "shell.pick."); ok {
		u.Send(w, PickShell{ID: s})
		return true
	}
	if at, ok := strings.CutPrefix(id, "command.saved:"); ok {
		w.runSavedCommand(at, u)
		return true
	}
	if at, ok := strings.CutPrefix(id, "tunnel.saved:"); ok {
		w.runSavedTunnel(at, u)
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

// pickTheme offers the themes in a palette, the one on marked.
func (w *window) pickTheme(u *gunim.UI) {
	p := &widget.Palette{Placeholder: "Pick a theme"}
	for _, name := range w.themes {
		hint := ""
		if name == w.themeNow {
			hint = "in use"
		}
		p.Items = append(p.Items, widget.PaletteItem{Title: name, Hint: hint})
	}
	names := w.themes
	p.Pick = func(i int, u *gunim.UI) { u.Send(w, PickTheme{Name: names[i]}) }
	p.Open(w, geom.Rc(0, 48, w.size.W, 0), u)
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
	d.Danger = q.Danger
	if q.Plain {
		d.SetButtons(q.Yes, "")
	}
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
		Items: []string{"Connect to Server…", "Add Server…", "Reload Server List"},
		Hints: []string{hint("server.connect"), "", ""}}
	w.serverIDs = []string{"server.connect", "server.add", "server.reload"}
	if len(saved) > 0 {
		m.Breaks, m.Captions = []int{3}, []int{3}
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
	for _, m := range w.accounts {
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Connection Log for " + m})
		w.paletteIDs = append(w.paletteIDs, "conn.log:"+m)
	}
	// Every machine by name: a terminal there, its files, and each
	// folder saved for it.
	for _, m := range w.machines() {
		where := m
		if m == "" {
			where = "This Computer"
		}
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "New Terminal on " + where}, widget.PaletteItem{Title: "Browse Files on " + where})
		w.paletteIDs = append(w.paletteIDs, "on.terminal:"+m, "on.files:"+m)
		for _, h := range w.saved {
			if h.Name != m {
				continue
			}
			for i, f := range h.Folders {
				w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Browse " + f + " on " + where})
				w.paletteIDs = append(w.paletteIDs, "on.folder:"+strconv.Itoa(i)+":"+m)
			}
		}
	}
	// With more than one shell here, a terminal with any of them, and
	// which new terminals start.
	if len(w.shellChoices) > 1 {
		for _, s := range w.shellChoices {
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "New " + s.Title})
			w.paletteIDs = append(w.paletteIDs, "shell.open."+s.ID)
			if s.ID != w.chosenShell {
				w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Start " + s.Title + " in New Terminals"})
				w.paletteIDs = append(w.paletteIDs, "shell.pick."+s.ID)
			}
		}
		if w.chosenShell != "" {
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Start The Default Shell in New Terminals"})
			w.paletteIDs = append(w.paletteIDs, "shell.pick.")
		}
	}
	for i, it := range w.savedCommandItems() {
		w.palette.Items = append(w.palette.Items, it)
		w.paletteIDs = append(w.paletteIDs, "command.saved:"+strconv.Itoa(i))
	}
	for i, it := range w.savedTunnelItems() {
		w.palette.Items = append(w.palette.Items, it)
		w.paletteIDs = append(w.paletteIDs, "tunnel.saved:"+strconv.Itoa(i))
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
	kind := widget.NewDropdown("Server", "gridterm window")
	kind.Label = "Type"
	folders := widget.NewTextField()
	folders.Placeholder = "optional: paths to open files at, with commas"
	setup := widget.NewCheckbox("Teach its shell to say what it is doing")
	forward := widget.NewCheckbox("Forward this machine's SSH agent to it")
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
		if old.Window {
			kind.Selected = 1
		}
		folders.SetText(old.FoldersJoined())
		setup.On, forward.On = old.Setup, old.ForwardAgent
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
		h.Window = kind.Selected == 1
		h.Folders = remote.FoldersFrom(folders.Text())
		h.Setup, h.ForwardAgent = setup.On, forward.On
		if err := h.Validate(); err != nil {
			return h, upperFirst(err.Error()) + "."
		}
		return h, ""
	}
	d := widget.NewDialog(title)
	d.Body = widget.NewForm().Add("Name", name).Add("Type", kind).Add("Address", addr).Add("Port", port).Add("User", user).
		Add("Through", via).Add("Key file", key).Add("Folders", folders).Add("", setup).Add("", forward)
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
	if st.Theme != w.themeNow {
		if c, ok := w.contents[st.Theme]; ok {
			w.onStage.Use(c)
		}
	}
	w.themes, w.themeNow = st.Themes, st.Theme
	if st.FontSize != w.fontSize {
		w.fontSize = st.FontSize
		for _, t := range w.terms {
			t.cells.Size = st.FontSize
		}
	}
	rows := sidebarRows(st.Panes, st.Tunnels, st.Share, st.Windows)
	// The windows connected to this one, under this computer.
	for i, c := range st.Serving.Clients {
		at := slices.IndexFunc(rows, func(r sideItem) bool { return r.key == "machine:" }) + 1
		for at < len(rows) && !rows[at].heading {
			at++
		}
		item := sideItem{key: "client:" + c.Name + ":" + strconv.Itoa(i), text: "serving " + c.Name, note: "from " + c.From, local: func(u *gunim.UI) { w.servingDialog(w.serving, u) }}
		rows = slices.Insert(rows, at, item)
	}
	widget.Sync(w.list, u, rows,
		func(r sideItem) widget.Key { return widget.Key(r.key) },
		func(r sideItem) *sideRow { return w.newSideRow(r) },
		func(row *sideRow, r sideItem, u *gunim.UI) { row.set(r) })
	for _, r := range rows {
		if row, ok := widget.RowOf[*sideRow](w.list, widget.Key(r.key)); ok && !r.heading {
			row.setActive(r.pane != "" && r.pane == st.Focus, u)
		}
	}
	w.setSavedTunnels(st.SavedTunnels)
	w.share = st.Share
	w.sidebarShown = st.Sidebar
	w.termProgram = st.TermProgram
	renamed := len(st.Windows) != len(w.remoteWindows)
	for i := 0; !renamed && i < len(st.Windows); i++ {
		renamed = st.Windows[i].Name != w.remoteWindows[i].Name
	}
	w.remoteWindows = st.Windows
	if renamed || !slices.Equal(st.Connected, w.connected) {
		w.connected = st.Connected
		w.servers(w.saved)
	}
	w.setSavedCommands(st.SavedCommands)
	if !slices.Equal(st.Shells, w.shellChoices) || st.ChosenShell != w.chosenShell {
		w.shellChoices, w.chosenShell = st.Shells, st.ChosenShell
		w.servers(w.saved)
	}
	if st.Bells > w.bells {
		w.bells = st.Bells
		u.RequestAttention()
	}
	if st.PaneTitles != w.titles {
		w.titles = st.PaneTitles
		// Every pane is built again, with its line or without.
		clear(w.splits)
	}
	w.serving = st.Serving
	if w.sharing && st.Share.Code != "" {
		w.sharing = false
		if w.dialog == nil {
			w.shareDialog(st.Share, u)
		}
	}
	if !slices.Equal(st.Accounts, w.accounts) {
		w.accounts = st.Accounts
		w.servers(w.saved)
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
			// Opened at a line, such as one a link named.
			if next.Line > 0 {
				r.top = min(next.Line-1, max(0, len(next.Lines)-1))
			}
			if next.Find {
				r.openBar(false, u)
			}
		}
		if next.Seq != r.seq && r.follow {
			// Following, the reader stays at the end as the file grows.
			r.top = len(next.Lines)
		}
		r.seq = next.Seq
		r.st = next
	}
	// Panes off stage are out of the tree, where nothing can be added
	// to them; each catches up as it comes back.
	if w.jobs != nil && u.Presence(w.jobs) != gunim.Exiting {
		w.jobs.show(st.Jobs, u)
	}
	if w.secrets != nil && u.Presence(w.secrets) != gunim.Exiting {
		w.secrets.show(st.Secrets, u)
	}
	if w.kindOf(st.Focus) == kindTerminal {
		w.lastTerm = st.Focus
	}
	for id, p := range w.tunnelPanes {
		if !open[id] {
			delete(w.tunnelPanes, id)
			continue
		}
		if u.Presence(p) == gunim.Exiting {
			continue
		}
		var t Tunnel
		ok := false
		for _, pane := range st.Panes {
			if pane.ID == id {
				i := slices.IndexFunc(st.Tunnels, func(t Tunnel) bool { return t.ID == pane.Tunnel })
				if ok = i >= 0; ok {
					t = st.Tunnels[i]
				}
			}
		}
		p.bar.show(t, ok, u)
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
			if n.Forget {
				copied := n.Clipboard
				u.After(clipboardHolds*time.Second, func(u *gunim.UI) {
					// Only the secret goes; what was copied since stays.
					if u.Clipboard() == copied {
						u.SetClipboard("")
					}
				})
			}
		}
		w.toasts.Show(widget.Toast{Title: n.Title, Body: n.Body}, u)
	}
	// The View menu ticks the sidebar while it shows.
	for m := range menus {
		for i, it := range menus[m].items {
			switch it.id {
			case "sidebar.toggle":
				w.bar.Menus[m].Checked[i] = st.Sidebar
			case "pane.titles":
				w.bar.Menus[m].Checked[i] = st.PaneTitles
			case "view.fullScreen":
				w.bar.Menus[m].Checked[i] = u.FullScreen()
			case "shell.setup":
				w.bar.Menus[m].Checked[i] = st.ShellSetup
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

// paneNode returns the node that shows pane id, made on first use,
// under a line naming it while panes show their titles.
func (w *window) paneNode(id string) gunim.Node {
	n := w.bareNode(id)
	if !w.titles {
		return n
	}
	c, ok := w.captions[id]
	if !ok || c.pane != n {
		c = newCaptioned(n)
		w.captions[id] = c
	}
	for _, p := range w.panes {
		if p.ID == id {
			where := p.Machine
			if where == "" {
				where = "This computer"
			}
			c.label.SetText(where + ": " + p.Title)
		}
	}
	return c
}

// bareNode returns the node that shows pane id, made on first use.
func (w *window) bareNode(id string) gunim.Node {
	switch w.kindOf(id) {
	case kindFiles:
		b, ok := w.browsers[id]
		if !ok {
			b = newBrowser(w, id)
			w.browsers[id] = b
		}
		return b
	case kindSecrets:
		if w.secrets == nil {
			w.secrets = newSecretsPane(w)
		}
		return w.secrets
	case kindJobs:
		if w.jobs == nil {
			w.jobs = newJobsPane()
		}
		return w.jobs
	case kindTunnel:
		p, ok := w.tunnelPanes[id]
		if !ok {
			p = newTunnelPane(w.term(id))
			w.tunnelPanes[id] = p
		}
		return p
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
		// Its find bar, when it is open, as it is for the scrollback.
		if r.open {
			return r.bar
		}
		return r
	}
	if w.kindOf(id) == kindJobs && w.jobs != nil {
		return w.jobs.clear
	}
	if w.kindOf(id) == kindSecrets && w.secrets != nil {
		return w.secrets.table
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
// or a tunnel under it. pane is the pane that lights the row while it
// has the keyboard, click what a click asks for, and note is said
// small at the end. dim marks a tunnel that has stopped.
type sideItem struct {
	key, text, note string
	pane            string
	click           gunim.Intent
	// closes is what closing the row asks the program, when it can be
	// closed from the sidebar.
	closes gunim.Intent
	// local is what a click does in the window, for a row whose click
	// asks the program nothing.
	local        func(*gunim.UI)
	heading, dim bool
}

// sidebarRows lists the panes under their machines, this computer
// first, then each server in the order its first pane opened, and
// each server's tunnels after its panes. A tunnel's pane is lit on the
// tunnel's row.
func sidebarRows(panes []Pane, tunnels []Tunnel, share Share, windows []RemoteWindow) []sideItem {
	notes := map[string]string{}
	for _, p := range share.Panes {
		notes[p.Pane] = p.Note
	}
	machines := []string{""}
	seen := map[string]bool{"": true}
	add := func(m string) {
		if !seen[m] {
			seen[m] = true
			machines = append(machines, m)
		}
	}
	for _, p := range panes {
		add(p.Machine)
	}
	for _, t := range tunnels {
		add(t.Machine)
	}
	for _, w := range windows {
		add(w.Name)
	}
	shown := map[string]bool{}
	for _, t := range tunnels {
		shown[t.ID] = true
	}
	var out []sideItem
	for _, m := range machines {
		name := m
		if m == "" {
			name = "This computer"
		}
		out = append(out, sideItem{key: "machine:" + m, text: name, heading: true})
		for _, p := range panes {
			if p.Machine == m && !shown[p.Tunnel] {
				note := notes[p.ID]
				switch {
				case p.Ended:
					note = "ended"
				case p.Rang:
					note = "bell"
				}
				out = append(out, sideItem{key: p.ID, text: p.Title, note: note, pane: p.ID, click: FocusPane{Pane: p.ID}, closes: ClosePane{Pane: p.ID}, dim: p.Ended})
			}
		}
		for _, w := range windows {
			if w.Name != m {
				continue
			}
			// What the window has open, to work in from here.
			for _, o := range w.Open {
				note := "there"
				if o.Host != "" {
					note = "on " + o.Host
				}
				out = append(out, sideItem{key: "window:" + m + ":" + o.ID, text: o.Label, note: note, click: AttachWindow{Window: m, ID: o.ID}, dim: true})
			}
		}
		for _, t := range tunnels {
			if t.Machine == m {
				out = append(out, sideItem{key: "tunnel:" + t.ID, text: t.Label, note: t.Note, pane: t.Pane, click: ShowTunnel{ID: t.ID}, closes: CloseTunnel{ID: t.ID}, dim: !t.Live})
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
	w *window
	// machine is the machine a heading heads, which its plus opens a
	// menu for; menu is that menu while it is open, which holds the
	// keyboard, and back what had the keyboard before.
	machine string
	menu    *widget.Menu
	popup   *gunim.Popup
	back    gunim.Node
	key     string
	heading bool
	click   gunim.Intent
	closes  gunim.Intent
	local   func(*gunim.UI)
	// ring grows while the row has the keyboard.
	ring   *anim.Float
	title  *widget.Label
	note   *widget.Label
	active *anim.Float
	hover  *anim.Float
	on     bool
}

func (w *window) newSideRow(it sideItem) *sideRow {
	r := &sideRow{w: w, heading: it.heading, title: widget.NewLabel(""), note: widget.NewLabel(""), active: anim.NewFloat(0), hover: anim.NewFloat(0)}
	r.title.MaxLines, r.note.MaxLines = 1, 1
	r.note.Size, r.note.Color = smallText, faint
	if it.heading {
		r.title.Size, r.title.Color = smallText, faint
	}
	r.set(it)
	r.ring = anim.NewFloat(0)
	r.Add(r.active, r.hover, r.ring)
	return r
}

// set shows it on the row.
func (r *sideRow) set(it sideItem) {
	r.title.SetText(it.text)
	r.note.SetText(it.note)
	r.click, r.local, r.closes, r.key = it.click, it.local, it.closes, it.key
	if m, ok := strings.CutPrefix(it.key, "machine:"); ok && it.heading {
		r.machine = m
	}
	if !it.heading {
		r.title.Color = widget.Ink
		if it.dim {
			r.title.Color = faint
		}
	}
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
func (r *sideRow) Children() []gunim.Node { return []gunim.Node{r.title, r.note} }

// Layout implements [gunim.Node]: the title at the start, the note at
// the end.
func (r *sideRow) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const padX, gap, height = 12, 8, 28
	note := kids.At(1)
	ns := note.Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-2*padX)/2, height)})
	note.Place(geom.Pt(c.Max.W-padX-ns.W, (height-ns.H)/2))
	room := c.Max.W - 2*padX
	if ns.W > 0 {
		room -= ns.W + gap
	}
	if r.heading {
		room -= plusWidth
	}
	k := kids.At(0)
	s := k.Layout(gunim.Constraints{Max: geom.Sz(max(0, room), height)})
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
	if t := r.ring.Value(); t > 0.01 {
		c := widget.Accent.Get(f.Theme)
		c.A = uint8(float32(c.A) * min(t, 1))
		p.RRectStroke(inset, 6, paint.Fill{}, paint.Stroke{Width: 1.5, Color: c})
	}
	kids.At(0).Paint(p)
	kids.At(1).Paint(p)
	if r.heading {
		r.paintPlus(p, f, box)
	}
}

// Focusable implements [gunim.Focusable]: every row but a heading, for
// working the sidebar from the keyboard.
func (r *sideRow) Focusable() bool { return !r.heading || r.popup != nil }

// activate does what a click on the row does.
func (r *sideRow) activate(u *gunim.UI) {
	if r.local != nil {
		r.local(u)
	} else if r.click != nil {
		u.Send(r, r.click)
	}
}

// Handle implements [gunim.Handler].
func (r *sideRow) Handle(e input.Event, u *gunim.UI) bool {
	if r.heading {
		return r.handleHeading(e, u)
	}
	switch e := e.(type) {
	case input.PointerEnter:
		r.hover.Animate(1, widget.Quick.Get(u.Theme()))
	case input.PointerLeave:
		r.hover.Animate(0, widget.Settle.Get(u.Theme()))
	case input.PointerDown:
		if e.Button == input.ButtonPrimary {
			r.activate(u)
			return true
		}
		return false
	case input.FocusGained:
		r.ring.Animate(1, widget.Quick.Get(u.Theme()))
	case input.FocusLost:
		r.ring.Animate(0, widget.Settle.Get(u.Theme()))
	case input.KeyPress:
		switch {
		case e.Key == input.KeyUp, e.Key == input.KeyDown:
			step := 1
			if e.Key == input.KeyUp {
				step = -1
			}
			r.w.focusRow(r.key, step, u)
		case e.Key == input.KeyEnter || e.Key == input.KeyKPEnter:
			r.activate(u)
		case e.Key == input.KeyDelete && r.closes != nil:
			u.Send(r, r.closes)
		case e.Key == input.KeyEscape:
			// Back to the pane the keyboard came from.
			if n := r.w.focusNode(r.w.focused); n != nil {
				u.Focus(n)
			}
		default:
			return false
		}
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

// focusRow gives the keyboard to the row step rows from the one keyed
// from, passing over headings, or to the first row when from is "".
func (w *window) focusRow(from string, step int, u *gunim.UI) {
	keys := w.list.Keys()
	at := slices.Index(keys, widget.Key(from))
	if at < 0 {
		at, step = -1, 1
	}
	for i := at + step; i >= 0 && i < len(keys); i += step {
		if row, ok := widget.RowOf[*sideRow](w.list, keys[i]); ok && !row.heading {
			u.Focus(row)
			return
		}
	}
}

// focusSidebar gives the keyboard to the sidebar's row for the focused
// pane, or to its first row, showing the sidebar first.
func (w *window) focusSidebar(u *gunim.UI) {
	if !w.sidebarShown {
		u.Send(w, ToggleSidebar{})
	}
	if row, ok := widget.RowOf[*sideRow](w.list, widget.Key(w.focused)); ok {
		u.Focus(row)
		return
	}
	w.focusRow("", 1, u)
}

// machines are the machines panes can open on: this computer, the
// servers connected to, and the windows connected to.
func (w *window) machines() []string {
	out := []string{""}
	out = append(out, w.connected...)
	for _, rw := range w.remoteWindows {
		out = append(out, rw.Name)
	}
	return out
}

// plusWidth is the room the plus takes at the end of a heading.
const plusWidth = 28

// handleHeading shows a heading's plus under the pointer, and opens the
// machine's menu from it.
func (r *sideRow) handleHeading(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerEnter:
		r.hover.Animate(1, widget.Quick.Get(u.Theme()))
	case input.PointerLeave:
		r.hover.Animate(0, widget.Settle.Get(u.Theme()))
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		r.w.openMachineMenu(r, u)
		return true
	case input.KeyPress:
		if r.menu == nil {
			return false
		}
		if e.Key == input.KeyEscape || e.Key == input.KeyTab {
			r.closeMenu(u)
			return true
		}
		return r.menu.Key(e, u)
	default:
		return false
	}
	return false
}

// closeMenu closes a heading's menu, and gives the keyboard back.
func (r *sideRow) closeMenu(u *gunim.UI) {
	if r.popup == nil {
		return
	}
	r.popup.Close()
	r.popup, r.menu = nil, nil
	if r.back != nil {
		u.Focus(r.back)
		r.back = nil
	}
}

// paintPlus draws a heading's plus, faint, and brighter under the
// pointer.
func (r *sideRow) paintPlus(p *paint.Painter, f gunim.Frame, box geom.Size) {
	c := faint.Get(f.Theme)
	if t := r.hover.Value(); t > 0.01 {
		c = anim.Mix(anim.ColorCodec, c, widget.Accent.Get(f.Theme), min(t, 1))
	}
	cx, cy := box.W-6-plusWidth/2, box.H/2
	p.RRect(geom.Rect{Min: geom.Pt(cx-5, cy-0.75), Max: geom.Pt(cx+5, cy+0.75)}, 0.75, paint.Solid(c))
	p.RRect(geom.Rect{Min: geom.Pt(cx-0.75, cy-5), Max: geom.Pt(cx+0.75, cy+5)}, 0.75, paint.Solid(c))
}

// openMachineMenu opens the menu of what can be opened on a heading's
// machine, under the heading.
func (w *window) openMachineMenu(r *sideRow, u *gunim.UI) {
	m := r.machine
	var items []string
	var acts []func(*gunim.UI)
	add := func(title string, act func(*gunim.UI)) {
		items = append(items, title)
		acts = append(acts, act)
	}
	send := func(in gunim.Intent) func(*gunim.UI) { return func(u *gunim.UI) { u.Send(w, in) } }
	window := slices.ContainsFunc(w.remoteWindows, func(rw RemoteWindow) bool { return rw.Name == m })
	add("Terminal", send(OpenOn{Machine: m}))
	if !window {
		add("Command…", func(u *gunim.UI) { w.commandDialogOn(m, u) })
	}
	add("Files", send(FilesOn{Machine: m}))
	for _, h := range w.saved {
		if h.Name == m {
			for _, f := range h.Folders {
				add("Files in "+f, send(FilesOn{Machine: m, Path: f}))
			}
		}
	}
	if m != "" && !window {
		add("Tunnel…", func(u *gunim.UI) { w.tunnelDialogOn(m, false, u) })
		add("SOCKS Proxy…", func(u *gunim.UI) { w.tunnelDialogOn(m, true, u) })
	}
	if m != "" {
		add("Connection Log", send(ShowLog{Machine: m}))
		add("Disconnect", send(Disconnect{Machine: m}))
		for _, h := range w.saved {
			if h.Name == m {
				saved := h
				add("Edit This Server…", func(u *gunim.UI) { w.serverForm(&saved, u) })
				add("Remove This Server…", func(u *gunim.UI) { w.confirmRemove(m, u) })
			}
		}
	}
	r.closeMenu(u)
	menu := widget.NewMenu(items...)
	menu.Pick = func(i int, u *gunim.UI) {
		r.closeMenu(u)
		if i >= 0 && i < len(acts) {
			acts[i](u)
		}
	}
	box, _ := u.Bounds(r)
	r.menu = menu
	r.back = u.Focused()
	r.popup = u.OpenPopup(r, menu, gunim.PopupOptions{
		Anchor:  geom.Rect{Min: geom.Pt(box.Size().W-plusWidth-6, 0), Max: box.Size().Point()},
		Max:     geom.Sz(360, 480),
		Dismiss: r.closeMenu,
	})
	// The heading holds the keyboard while its menu is open, and
	// passes keys to it.
	u.Focus(r)
}
