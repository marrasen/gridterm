package main

import (
	"fmt"
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

	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/settings"
	shellfind "github.com/marrasen/gridterm/shells"
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
	connected []string
	// help is the list of commands, once opened, and shortcutsRead
	// the shortcuts file's reads as last taken on.
	help          *helpPane
	copies        *copiesPane
	shortcutsRead uint64
	// secretsExist says there are secrets, to keep a new key's passphrase in.
	secretsExist bool
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
	// vault is the secrets as last published, and afterUnlock a command
	// waiting for them to open.
	vault       Secrets
	afterUnlock string
	// title is the window's title as last set.
	title string
	// dropped are the machines whose connection went by itself.
	dropped []string
	// dialing are the servers being connected to, fileClip what the
	// file clipboard holds, and keyFiles the key files kept.
	dialing  []string
	fileClip FileClip
	keyFiles []string
	// fonts are the families on the Font menu, and font the one the
	// terminals are drawn in.
	fonts []string
	font  Font
	// zoomed gathers Ctrl and the wheel until it makes a point of font
	// size.
	zoomed float32
	// recent are the panes, the most recently used first; walk is a
	// Ctrl+Tab walk under way, walkList its list on screen, and keyMods
	// the modifiers of the key running the command now.
	recent   []string
	walk     *paneWalk
	walkList *walkList
	keyMods  input.Mods
	// glowing is set while a shared pane's ring keeps frames coming,
	// and revealed is the pane whose row the sidebar last scrolled to.
	glowing  bool
	revealed string
	// chips are what the menu bar says the window is doing for others.
	chips *chipBar
	// permsAfter is a pane whose permissions open once it is shared.
	permsAfter string
	size       geom.Size
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
	w.bar.OnHighlight = func(m, i int, u *gunim.UI) {
		id := ""
		switch {
		case m < 0 || i < 0:
		case menus[m].title == "Servers":
			if i < len(w.serverIDs) {
				id = w.serverIDs[i]
			}
		case menus[m].title == "Font":
		case i < len(menus[m].items):
			id = menus[m].items[i].id
		}
		w.status.setHint(w.fullTitle(id, m, i), u)
	}
	w.bar.Pick = func(m, i int, u *gunim.UI) {
		switch {
		case m < len(menus) && menus[m].title == "Servers":
			if i < len(w.serverIDs) {
				w.run(w.serverIDs[i], u)
			}
		case m < len(menus) && menus[m].title == "Font":
			if i < len(w.fonts) {
				u.Send(w, PickFont{Name: w.fonts[i]})
			}
		case m < len(menus) && i < len(menus[m].items):
			w.run(menus[m].items[i].id, u)
		}
	}
	w.toasts = &widget.Toasts{}
	w.chips = newChipBar()
	bar := widget.Row(w.bar, w.chips).Grow(w.bar, 1)
	bar.Cross, bar.Gap = widget.CrossStretch, noGap
	w.top = widget.Column(bar, w.outer).Grow(w.outer, 1)
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
	if id != w.afterUnlock {
		// Another command since: the one waiting is let go.
		w.afterUnlock = ""
	}
	switch id {
	case "palette.open":
		w.palette.Open(w, geom.Rc(0, 48, w.size.W, 0), u)
		return true
	case "menu.open":
		w.bar.Open(0, u)
		return true
	case "view.switcher":
		w.openSwitcher(u)
		return true
	case "pane.next", "pane.previous":
		step := 1
		if id == "pane.previous" {
			step = -1
		}
		w.walkRecent(step, u)
		if w.keyMods&input.ModControl == 0 {
			// Run from the palette or the menu, with no Ctrl to let go
			// of: one step.
			w.endWalk(u)
		}
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
	case "view.theme":
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
	case "agent.typed":
		u.Send(w, ShowTyped{Pane: w.focused})
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
	case "sshkey.make":
		w.makeKeyDialog(u)
		return true
	case "help.shortcuts":
		u.Send(w, ShowHelp{})
		return true
	case "shortcuts.write":
		u.Send(w, WriteShortcuts{Bindings: w.keys.Bindings()})
		return true
	case "app.about":
		w.aboutDialog(u)
		return true
	case "help.files":
		w.fileLocationsDialog(u)
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
	case "agent.hand":
		// Shared, it opens its permissions, as gridterm's does: to open
		// them again for a pane shared already, or to tick more once the
		// share is made.
		if slices.ContainsFunc(w.share.Panes, func(p SharedPane) bool { return p.Pane == w.focused }) {
			w.permissionsDialog(w.share, u)
			return true
		}
		u.Send(w, SharePane{Pane: w.focused})
		w.permsAfter = w.focused
		return true
	case "agent.take":
		u.Send(w, UnsharePane{Pane: w.focused})
		return true
	case "secrets.change", "secrets.forget", "secrets.removeKey", "secrets.addPassphrase":
		w.secretsCommand(id, u)
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
	case "conn.tunnel", "conn.socks":
		w.tunnelDialog(id == "conn.socks", u)
		return true
	case "files.goTo":
		if b, ok := w.browsers[w.focused]; ok {
			b.askGoTo(u)
		}
		return true
	case "edit.paste":
		if t, ok := w.terms[w.focused]; ok {
			t.pasteClipboard(u)
		}
		return true
	case "edit.copy":
		if t, ok := w.terms[w.focused]; ok {
			t.copySelection(u)
		}
		return true
	}
	if w.runItem(id, u) {
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

// zoom makes the font a point larger for each notch the wheel turns
// away from the user, with Ctrl held, and smaller toward, as gridterm
// does.
func (w *window) zoom(s input.Scroll, u *gunim.UI) {
	w.zoomed += s.Delta.Y / zoomNotch
	steps := int(w.zoomed)
	if steps == 0 {
		return
	}
	w.zoomed -= float32(steps)
	u.Send(w, FontSize{Step: steps})
}

// zoomNotch is how far the wheel turns for a point of font size: a
// notch, as the drivers count it.
const zoomNotch = 40

// showFonts fills the Font menu, the family in use ticked, and draws
// every terminal and reader in that family.
func (w *window) showFonts(st State) {
	if st.Font.Name != w.font.Name {
		for _, t := range w.terms {
			t.cells.Faces = st.Font.Faces
		}
		for _, r := range w.readers {
			r.cells.Faces = st.Font.Faces
		}
	}
	w.fonts, w.font = st.Fonts, st.Font
	w.servers(w.saved)
	m := widget.BarMenu{Title: "Font"}
	for i, name := range st.Fonts {
		m.Items = append(m.Items, name)
		m.Checked = append(m.Checked, fontCommandID(name) == fontCommandID(st.Font.Name))
		if i == 1 {
			// A line under Go Mono, as in gridterm.
			m.Breaks = append(m.Breaks, i)
		}
	}
	for i := range w.bar.Menus {
		if w.bar.Menus[i].Title == "Font" {
			w.bar.Menus[i] = m
		}
	}
}

// programName is what the window is called, before the focused
// terminal's title.
const programName = "gunimterm"

// showTitle names the window after the focused terminal's title, as
// its program sets it. Read from the focused one only: a build running
// in a pane out of sight does not rename the window.
func (w *window) showTitle(st State, u *gunim.UI) {
	title := programName
	if t, ok := w.terms[st.Focus]; ok {
		if program := t.sh.t.Title(); program != "" {
			title = programName + " — " + program
		}
	}
	if title != w.title {
		w.title = title
		u.SetTitle(title)
	}
}

// secretsCommand carries out a command on the secrets that picks one
// first, or takes a passphrase. A locked vault is unlocked first, and
// the command carried on once it is open.
func (w *window) secretsCommand(id string, u *gunim.UI) {
	st := w.vault
	if !st.Open {
		w.afterUnlock = id
		u.Send(w, UnlockSecrets{})
		return
	}
	w.afterUnlock = ""
	switch id {
	case "secrets.change", "secrets.forget":
		if len(st.Items) == 0 {
			w.toasts.Show(widget.Toast{Title: "There are no secrets yet", Body: "Add Secret keeps the first one."}, u)
			return
		}
		titles := make([]string, len(st.Items))
		for i, it := range st.Items {
			titles[i] = it.Name
		}
		items := st.Items
		w.choose("Which secret?", titles, func(i int, u *gunim.UI) {
			it := items[i]
			if id == "secrets.change" {
				w.secretForm(it.Kind, &it, u)
				return
			}
			w.confirmRemoveSecret(it, u)
		}, u)
	case "secrets.removeKey":
		titles := make([]string, len(st.Keys))
		for i, k := range st.Keys {
			titles[i] = k.Name
		}
		keys := st.Keys
		w.choose("Which key?", titles, func(i int, u *gunim.UI) { w.confirmRemoveKey(st, keys[i], u) }, u)
	case "secrets.addPassphrase":
		if st.Passphrase {
			w.toasts.Show(widget.Toast{Title: "The secrets already take a passphrase", Body: "Remove Secrets Key takes it away first."}, u)
			return
		}
		w.passphraseForm(st, u)
	}
}

// choose offers titles in a palette, and runs then with the one picked.
func (w *window) choose(placeholder string, titles []string, then func(i int, u *gunim.UI), u *gunim.UI) {
	p := &widget.Palette{Placeholder: placeholder, Pick: then}
	for _, t := range titles {
		p.Items = append(p.Items, widget.PaletteItem{Title: t})
	}
	p.Open(w, geom.Rc(0, 48, w.size.W, 0), u)
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
	// Buttons that do something and leave the question up.
	for _, act := range q.Actions {
		if act == "Copy" {
			copied := q.Copy
			d.AddAction(act, func(u *gunim.UI) { u.SetClipboard(copied) })
			continue
		}
		d.AddAction(act, func(u *gunim.UI) { u.Send(w, AskAction{ID: q.ID, Action: act}) })
	}
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
	d.Danger, d.Careful = q.Danger, q.Careful
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
			w.serverIDs = append(w.serverIDs, "server.open."+remote.CommandName(h.Name))
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
	for _, h := range saved {
		for _, c := range []struct{ title, id string }{
			{"Connect to " + h.Name, "server.open."},
			{"Edit Server " + h.Name, "server.edit."},
			{"Remove Server " + h.Name, "server.remove."},
		} {
			id := c.id + remote.CommandName(h.Name)
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: c.title, Also: []string{h.Address}, Hint: hint(id)})
			w.paletteIDs = append(w.paletteIDs, id)
		}
	}
	for _, m := range w.accounts {
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Connection Log for " + m})
		w.paletteIDs = append(w.paletteIDs, "conn.log."+remote.CommandName(m))
	}
	// Every machine by name: a terminal there, its files, and each
	// folder saved for it.
	for _, m := range w.machines() {
		where := m
		if m == "" {
			where = "This Computer"
		}
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "New Terminal on " + where}, widget.PaletteItem{Title: "Browse Files on " + where})
		w.paletteIDs = append(w.paletteIDs, "conn.terminal."+remote.CommandName(m), "conn.files."+remote.CommandName(m))
		for i, f := range w.foldersOn(m) {
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Browse " + f + " on " + where})
			w.paletteIDs = append(w.paletteIDs, "conn.files."+remote.CommandName(m)+"."+strconv.Itoa(i+1))
		}
	}
	// With more than one shell here, a terminal with any of them, and
	// which new terminals start.
	if len(w.shellChoices) > 1 {
		ids := w.shellIDs()
		for i, s := range w.shellChoices {
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "New " + s.Title, Hint: hint(ids[i])})
			w.paletteIDs = append(w.paletteIDs, ids[i])
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
		w.paletteIDs = append(w.paletteIDs, "conn.saved."+strconv.Itoa(i+1))
	}
	for _, name := range w.fonts {
		id := fontCommandID(name)
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Font: " + name, Hint: hint(id)})
		w.paletteIDs = append(w.paletteIDs, id)
	}
	for i, it := range w.savedTunnelItems() {
		w.palette.Items = append(w.palette.Items, it)
		w.paletteIDs = append(w.paletteIDs, "conn.savedtunnel."+strconv.Itoa(i+1))
	}
}

// runItem carries out a command on one thing of many, by gridterm's
// name for it: a saved server, a machine, a folder saved on one, a
// shell, a saved command or tunnel. It reports false for any other id.
func (w *window) runItem(id string, u *gunim.UI) bool {
	savedNamed := func(cmd string) (remote.Host, bool) {
		for _, h := range w.saved {
			if remote.CommandName(h.Name) == cmd {
				return h, true
			}
		}
		return remote.Host{}, false
	}
	machineNamed := func(cmd string) (string, bool) {
		for _, m := range append(w.machines(), w.accounts...) {
			if remote.CommandName(m) == cmd {
				return m, true
			}
		}
		if h, ok := savedNamed(cmd); ok {
			return h.Name, true
		}
		return "", false
	}
	nth := func(at string) (int, bool) {
		n, err := strconv.Atoi(at)
		return n - 1, err == nil && n > 0
	}
	switch {
	case strings.HasPrefix(id, "server.open."):
		if h, ok := savedNamed(strings.TrimPrefix(id, "server.open.")); ok {
			u.Send(w, ConnectTo{Saved: h.Name})
		}
	case strings.HasPrefix(id, "server.edit."):
		if h, ok := savedNamed(strings.TrimPrefix(id, "server.edit.")); ok {
			w.serverForm(&h, u)
		}
	case strings.HasPrefix(id, "server.remove."):
		if h, ok := savedNamed(strings.TrimPrefix(id, "server.remove.")); ok {
			w.confirmRemove(h.Name, u)
		}
	case strings.HasPrefix(id, "conn.log."):
		if m, ok := machineNamed(strings.TrimPrefix(id, "conn.log.")); ok {
			u.Send(w, ShowLog{Machine: m})
		}
	case strings.HasPrefix(id, "conn.terminal."):
		if m, ok := machineNamed(strings.TrimPrefix(id, "conn.terminal.")); ok {
			u.Send(w, OpenOn{Machine: m})
		}
	case strings.HasPrefix(id, "conn.files."):
		rest := strings.TrimPrefix(id, "conn.files.")
		if m, ok := machineNamed(rest); ok {
			u.Send(w, FilesOn{Machine: m})
			break
		}
		// A folder offered on a machine: its number follows the
		// machine's name, whose own name may hold stops.
		cut := strings.LastIndex(rest, ".")
		if cut < 0 {
			break
		}
		m, ok := machineNamed(rest[:cut])
		i, numbered := nth(rest[cut+1:])
		if folders := w.foldersOn(m); ok && numbered && i < len(folders) {
			u.Send(w, FilesOn{Machine: m, Path: folders[i]})
		}
	case strings.HasPrefix(id, shellfind.CommandPrefix):
		for i, sid := range w.shellIDs() {
			if sid == id {
				u.Send(w, OpenShellNamed{ID: w.shellChoices[i].ID})
			}
		}
	case strings.HasPrefix(id, "shell.pick."):
		u.Send(w, PickShell{ID: strings.TrimPrefix(id, "shell.pick.")})
	case strings.HasPrefix(id, "conn.saved."):
		if i, ok := nth(strings.TrimPrefix(id, "conn.saved.")); ok && i < len(w.savedCommands) {
			u.Send(w, RunSavedCommand{Saved: w.savedCommands[i]})
		}
	case strings.HasPrefix(id, "font.use."):
		for _, name := range w.fonts {
			if fontCommandID(name) == id {
				u.Send(w, PickFont{Name: name})
			}
		}
	case strings.HasPrefix(id, "conn.savedtunnel."):
		if i, ok := nth(strings.TrimPrefix(id, "conn.savedtunnel.")); ok && i < len(w.savedTunnels) {
			u.Send(w, OpenSavedTunnel{Saved: w.savedTunnels[i]})
		}
	default:
		return false
	}
	return true
}

// shellIDs are gridterm's names for the commands that open a terminal
// with each of the shells here, in the order of shellChoices.
func (w *window) shellIDs() []string {
	list := make([]shellfind.Shell, len(w.shellChoices))
	for i, s := range w.shellChoices {
		list[i] = shellfind.Shell{ID: s.ID}
	}
	return shellfind.CommandIDs(list)
}

// savingClashes says what stands in the way of saving h in place of
// old, as gridterm checks it: a new name something is connected as, a
// window saved twice, a connected window moved.
func (w *window) savingClashes(h remote.Host, old *remote.Host) string {
	under := ""
	if old != nil {
		under = old.Name
	}
	if under != h.Name && (slices.Contains(w.connected, h.Name) || slices.ContainsFunc(w.remoteWindows, func(rw RemoteWindow) bool { return rw.Name == h.Name })) {
		return fmt.Sprintf("Something is already connected as %q; close it first.", h.Name)
	}
	if h.Window {
		for _, s := range w.saved {
			if s.Window && s.Name != under && s.ServeAddr() == h.ServeAddr() {
				return fmt.Sprintf("%s is already saved as the window at %s; a window has one entry in the list.", s.Name, h.ServeAddr())
			}
		}
	}
	for _, rw := range w.remoteWindows {
		if under != "" && rw.Name == under && (!h.Window || rw.Addr != h.ServeAddr()) {
			return fmt.Sprintf("%s is connected at %s; let go of it before changing where it is.", under, rw.Addr)
		}
	}
	return ""
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
	// The key files kept, so one is a pick away rather than a path to
	// remember.
	var kept *widget.Dropdown
	if len(w.keyFiles) > 0 {
		kept = widget.NewDropdown(append([]string{"Pick a kept key"}, w.keyFiles...)...)
		kept.Label = "Kept keys"
		files := w.keyFiles
		kept.OnPick(func(i int, u *gunim.UI) {
			if i > 0 {
				key.SetText(files[i-1])
				u.Invalidate()
			}
		})
	}
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
		// The key typed first, and the keys after it the entry had,
		// which the form does not show and so keeps.
		var rest []string
		if old != nil && len(old.Identities) > 1 {
			rest = old.Identities[1:]
		}
		if k := strings.TrimSpace(key.Text()); k != "" {
			h.Identities = []string{k}
		}
		for _, r := range rest {
			if !slices.Contains(h.Identities, r) {
				h.Identities = append(h.Identities, r)
			}
		}
		h.Via = ids[max(0, min(via.Selected, len(ids)-1))]
		h.Window = kind.Selected == 1
		h.Folders = remote.FoldersFrom(folders.Text())
		h.Setup, h.ForwardAgent = setup.On, forward.On && !h.Window
		if h.Window {
			h.Via = ""
		}
		if err := h.Validate(); err != nil {
			return h, upperFirst(err.Error()) + "."
		}
		if why := w.savingClashes(h, old); why != "" {
			return h, why
		}
		return h, ""
	}
	// A window has no account, nothing to go through and no session to
	// carry an agent over: those are greyed out while the type says
	// window, as in gridterm, rather than taken and dropped.
	applies := func() {
		window := kind.Selected == 1
		via.Disabled = window || len(ids) <= 1
		forward.Disabled = window
	}
	applies()
	kind.OnPick(func(int, *gunim.UI) { applies() })
	form := widget.NewForm().Add("Name", name).Add("Type", kind).Add("Address", addr).Add("Port", port).Add("User", user).
		Add("Through", via).Add("Key file", key)
	if kept != nil {
		form.Add("Kept keys", kept)
	}
	d := widget.NewDialog(title)
	d.Body = form.Add("Folders", folders).Add("", setup).Add("", forward)
	d.SetButtons("Save", "Cancel")
	if old != nil {
		d.AddAction("Remove…", func(u *gunim.UI) {
			d.Close(u)
			w.confirmRemove(under, u)
		})
	}
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
	if said := w.removeSays(name); said != "" {
		d.Body = widget.NewLabel(said)
	}
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.Accept = RemoveServer{Name: name}
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// filesKeyOf is the name a pane's files are kept under, as the program
// names it.
func (w *window) filesKeyOf(id string) string {
	for _, p := range w.panes {
		if p.ID == id {
			if p.On != "" {
				return p.Machine + farSep + p.On
			}
			return p.Machine
		}
	}
	return ""
}

// nextFilePane is the file pane after id, or before it with back, in
// the sidebar's order, and empty when id is the only one.
func (w *window) nextFilePane(id string, back bool) string {
	var files []string
	for _, p := range w.panes {
		if p.Kind == kindFiles {
			files = append(files, p.ID)
		}
	}
	at := slices.Index(files, id)
	if at < 0 || len(files) < 2 {
		return ""
	}
	step := 1
	if back {
		step = -1
	}
	return files[(at+step+len(files))%len(files)]
}

// foldersOn are the folders offered for a machine, as gridterm offers
// them: the ones saved for it, and on this computer each WSL
// distribution's, which Windows serves on a share of its own.
func (w *window) foldersOn(m string) []string {
	var out []string
	for _, h := range w.saved {
		if h.Name == m {
			out = append(out, h.Folders...)
		}
	}
	if m == "" {
		for _, s := range w.shellChoices {
			if s.Folder != "" {
				out = append(out, s.Folder)
			}
		}
	}
	return out
}

// removeSays is what removing a server closes, as gridterm says it, and
// nothing when it closes nothing.
func (w *window) removeSays(name string) string {
	switch {
	case slices.ContainsFunc(w.remoteWindows, func(rw RemoteWindow) bool { return rw.Name == name }):
		return name + " is connected. Removing it closes the connection and its panes."
	case slices.Contains(w.connected, name):
		said := name + " is connected. Removing it closes the connection"
		panes := 0
		for _, p := range w.panes {
			if p.Machine == name {
				panes++
			}
		}
		if panes > 0 {
			said += " and everything through it: " + count(panes, "pane")
		}
		return said + "."
	case slices.Contains(w.dialing, name):
		return "Removing it cancels the connection in progress."
	}
	return ""
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
	if s, ok := e.(input.Scroll); ok && s.Mods&input.ModControl != 0 {
		w.zoom(s, u)
		return true
	}
	if d, ok := e.(input.Drop); ok && len(d.Paths) > 0 {
		// Dropped somewhere that is no terminal: the sidebar, a file
		// pane, the menu bar. The focused pane is what the user is
		// working in, and is where the files are wanted.
		u.Send(w, DropFiles{Paths: d.Paths})
		return true
	}
	if r, ok := e.(input.KeyRelease); ok {
		if w.walk != nil && walkKey(r) {
			w.endWalk(u)
			return true
		}
		return false
	}
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
	w.keyMods = k.Mods
	defer func() { w.keyMods = 0 }()
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
	if !slices.Equal(st.Fonts, w.fonts) || st.Font.Name != w.font.Name {
		w.showFonts(st)
	}
	if st.FontSize != w.fontSize {
		w.fontSize = st.FontSize
		for _, t := range w.terms {
			t.cells.Size = st.FontSize
		}
		for _, r := range w.readers {
			r.cells.Size = st.FontSize
		}
	}
	saved := make([]string, 0, len(st.Saved))
	for _, h := range st.Saved {
		saved = append(saved, h.Name)
	}
	rows := sidebarRows(st.Panes, st.Tunnels, st.Share, st.Windows, saved, st.Dropped...)
	// The windows connected to this one, under this computer.
	for i, c := range st.Serving.Clients {
		at := slices.IndexFunc(rows, func(r sideItem) bool { return r.key == "machine:" }) + 1
		for at < len(rows) && !rows[at].heading {
			at++
		}
		item := sideItem{key: "client:" + c.Name + ":" + strconv.Itoa(i), text: "serving " + c.Name, note: "from " + c.From, local: func(u *gunim.UI) { w.servingDialog(w.serving, u) },
			closes: DisconnectClient(c)}
		rows = slices.Insert(rows, at, item)
	}
	rows = w.markRows(rows, st)
	// At the foot, as in gridterm, the way to a machine not yet listed.
	rows = append(rows, sideItem{key: "connect:new", text: "+ Connect to server…", local: w.connectDialog})
	widget.Sync(w.list, u, rows,
		func(r sideItem) widget.Key { return widget.Key(r.key) },
		func(r sideItem) *sideRow { return w.newSideRow(r) },
		func(row *sideRow, r sideItem, u *gunim.UI) { row.set(r) })
	for _, r := range rows {
		if row, ok := widget.RowOf[*sideRow](w.list, widget.Key(r.key)); ok && !r.heading {
			row.setActive(r.pane != "" && r.pane == st.Focus, u)
		}
	}
	if st.Focus != w.revealed {
		// The sidebar follows the stage, as gridterm's does: the row of
		// the pane in front scrolls into view.
		w.revealed = st.Focus
		if row, ok := widget.RowOf[*sideRow](w.list, widget.Key(st.Focus)); ok {
			u.Reveal(row)
		}
	}
	w.setSavedTunnels(st.SavedTunnels)
	w.share = st.Share
	if id := w.permsAfter; id != "" && slices.ContainsFunc(st.Share.Panes, func(p SharedPane) bool { return p.Pane == id }) {
		w.permsAfter = ""
		if id == w.focused {
			w.permissionsDialog(st.Share, u)
		}
	}
	w.sidebarShown = st.Sidebar
	w.termProgram = st.TermProgram
	w.secretsExist = st.Secrets.Exists
	if st.ShortcutsRead != w.shortcutsRead {
		w.shortcutsRead = st.ShortcutsRead
		if w.applyShortcuts(st.Shortcuts, u) && st.ShortcutsAgain {
			// Said only now: a file naming a command there is none of
			// is refused here, and "reloaded" would be untrue.
			w.toasts.Show(widget.Toast{Title: "Shortcuts reloaded"}, u)
		}
	}
	if st.Contents != nil {
		w.contents = st.Contents
		if c, ok := w.contents[st.Theme]; ok {
			w.onStage.Use(c)
		}
	}
	renamed := len(st.Windows) != len(w.remoteWindows)
	for i := 0; !renamed && i < len(st.Windows); i++ {
		renamed = st.Windows[i].Name != w.remoteWindows[i].Name
	}
	w.remoteWindows = st.Windows
	w.dialing = st.Dialing
	w.dropped = st.Dropped
	w.fileClip = st.FileClip
	w.keyFiles = st.KeyFiles
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
		r.show(st.Readers[id], u)
	}
	// Panes off stage are out of the tree, where nothing can be added
	// to them; each catches up as it comes back.
	if w.jobs != nil && u.Presence(w.jobs) != gunim.Exiting {
		w.jobs.show(st.Jobs, u)
	}
	if w.copies != nil && u.Presence(w.copies) != gunim.Exiting {
		w.copies.show(st.SavedCopies, u)
	}
	if w.help != nil && u.Presence(w.help) != gunim.Exiting {
		w.help.show(w, u)
	}
	if w.secrets != nil && u.Presence(w.secrets) != gunim.Exiting {
		w.secrets.show(st.Secrets, u)
	}
	w.vault = st.Secrets
	w.noteFocus(st.Focus)
	shared := map[string]bool{}
	for _, sp := range st.Share.Panes {
		shared[sp.Pane] = true
	}
	for _, p := range st.Panes {
		if t, ok := w.terms[p.ID]; ok {
			t.agent = shared[p.ID] && !p.Ended
			t.marks = st.Marks
		}
	}
	w.glow(u)
	w.showTitle(st, u)
	if id := w.afterUnlock; id != "" && st.Secrets.Open {
		w.run(id, u)
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
	w.showChips(st)
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
			switch {
			case where == "":
				where = "This computer"
			case p.On != "":
				where = p.On + " through " + p.Machine
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
	case kindCopies:
		if w.copies == nil {
			w.copies = newCopiesPane(w)
		}
		return w.copies
	case kindHelp:
		if w.help == nil {
			w.help = newHelpPane(w)
		}
		return w.help
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
			r = newReader(w, id)
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
	if w.kindOf(id) == kindJobs && w.jobs != nil {
		return w.jobs.clear
	}
	if w.kindOf(id) == kindSecrets && w.secrets != nil {
		return w.secrets.table
	}
	if w.kindOf(id) == kindHelp && w.help != nil {
		return w.help.table
	}
	if w.kindOf(id) == kindCopies && w.copies != nil {
		return w.copies.table
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
	t.cells.Faces = w.font.Faces
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
	// depth sets a row a step in, as a machine a window reached is.
	depth int
	// kind picks the row's icon, live says how it is doing now, for the
	// mark in front of it, and nil gives it none; fill is how far its
	// work has got, drawn while filling; traffic is what it carries,
	// drawn as a little graph.
	kind    string
	live    func(now time.Time) meter.State
	fill    float32
	filling bool
	traffic *meter.Meter
}

// sidebarRows lists the panes under their machines, this computer
// first, then each server in the order its first pane opened, and
// each server's tunnels after its panes. A tunnel's pane is lit on the
// tunnel's row.
func sidebarRows(panes []Pane, tunnels []Tunnel, share Share, windows []RemoteWindow, saved []string, dropped ...string) []sideItem {
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
	// The saved servers, as gridterm lists them, each under its heading
	// with its plus, which connects to it.
	for _, name := range saved {
		add(name)
	}
	// And the ones whose connection went, kept until cleared.
	for _, name := range dropped {
		add(name)
	}
	shown := map[string]bool{}
	for _, t := range tunnels {
		shown[t.ID] = true
	}
	paneRow := func(p Pane) sideItem {
		note := notes[p.ID]
		switch {
		case p.Ended:
			note = "ended"
		case p.Rang:
			note = "bell"
		case note == "" && p.Note != "":
			note = p.Note
		}
		return sideItem{key: p.ID, text: p.Title, note: note, pane: p.ID, click: FocusPane{Pane: p.ID}, closes: ClosePane{Pane: p.ID}, dim: p.Ended}
	}
	var out []sideItem
	for _, m := range machines {
		name := m
		if m == "" {
			name = "This computer"
		}
		out = append(out, sideItem{key: "machine:" + m, text: name, heading: true})
		for _, p := range panes {
			if p.Machine == m && p.On == "" && !shown[p.Tunnel] {
				out = append(out, paneRow(p))
			}
		}
		var far []string
		for _, w := range windows {
			if w.Name != m {
				continue
			}
			// What the window has open on its own machine, to work in
			// from here.
			for _, o := range w.Open {
				if o.Host == "" {
					out = append(out, sideItem{key: "window:" + m + ":" + o.ID, text: o.Label, note: "there", click: AttachWindow{Window: m, ID: o.ID}, dim: true})
				} else if !slices.Contains(far, o.Host) {
					far = append(far, o.Host)
				}
			}
		}
		for _, p := range panes {
			if p.Machine == m && p.On != "" && !slices.Contains(far, p.On) {
				far = append(far, p.On)
			}
		}
		// Each machine the window reached, under a heading of its own a
		// step in, as gridterm has them: this window's panes on it, and
		// what the window has open there.
		for _, host := range far {
			out = append(out, sideItem{key: "machine:" + m + farSep + host, text: host, heading: true, depth: 1})
			for _, p := range panes {
				if p.Machine == m && p.On == host {
					out = append(out, paneRow(p))
				}
			}
			for _, w := range windows {
				if w.Name != m {
					continue
				}
				for _, o := range w.Open {
					if o.Host == host {
						out = append(out, sideItem{key: "window:" + m + ":" + o.ID, text: o.Label, note: "there", click: AttachWindow{Window: m, ID: o.ID}, dim: true})
					}
				}
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
	// marks is what the row shows besides its words: its mark, icon,
	// fill and traffic.
	marks rowMarks
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
	r.marks.set(it)
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
	// Room at the end for the cross a closing row shows on hover, and
	// for a traffic graph.
	end := c.Max.W - padX - r.marks.endRoom(r)
	note := kids.At(1)
	ns := note.Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-2*padX)/2, height)})
	note.Place(geom.Pt(end-ns.W, (height-ns.H)/2))
	start := padX + r.marks.startRoom(r)
	room := end - start
	if ns.W > 0 {
		room -= ns.W + gap
	}
	if r.heading {
		room -= plusWidth
	}
	k := kids.At(0)
	s := k.Layout(gunim.Constraints{Max: geom.Sz(max(0, room), height)})
	k.Place(geom.Pt(start, (height-s.H)/2))
	r.marks.laid(padX, end, height, c.Max.W)
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
	r.marks.paintUnder(p, f, inset)
	kids.At(0).Paint(p)
	kids.At(1).Paint(p)
	r.marks.paint(p, f, r)
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
			if r.closes != nil && r.marks.onCross(e.Pos) {
				// The cross at the end: close the row, as gridterm's
				// does, rather than go to it.
				u.Send(r, r.closes)
				return true
			}
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
		case e.Key == input.KeyPageUp, e.Key == input.KeyPageDown:
			// A page of rows, as gridterm's list moves.
			step := sidebarPage
			if e.Key == input.KeyPageUp {
				step = -step
			}
			r.w.focusRowsAway(r.key, step, u)
		case e.Key == input.KeyHome:
			r.w.focusRow("", 1, u)
		case e.Key == input.KeyEnd:
			r.w.focusRowsAway(r.key, len(r.w.list.Keys()), u)
		case e.Key == input.KeyEnter || e.Key == input.KeyKPEnter, e.Key == input.KeySpace && e.Mods == 0:
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
//
// While a menu is open it says instead the full title of the item
// highlighted, as gridterm's bottom row does, over the bottom of the
// stage when the line has no room of its own: the terminal keeps its
// size while the pointer runs down a menu.
type statusLine struct {
	anim.Group
	label  *widget.Label
	height *anim.Float
	text   string
	hint   *widget.Label
	hinted string
}

func newStatusLine() *statusLine {
	l := widget.NewLabel("")
	l.Size, l.Color, l.MaxLines = smallText, faint, 1
	h := widget.NewLabel("")
	h.Size, h.MaxLines = smallText, 1
	s := &statusLine{label: l, height: anim.NewFloat(0), hint: h}
	s.Add(s.height)
	return s
}

// setHint shows a menu item's full title, or with "" the status again.
func (s *statusLine) setHint(text string, u *gunim.UI) {
	if text == s.hinted {
		return
	}
	s.hinted = text
	s.hint.SetText(text)
	u.Invalidate()
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
func (s *statusLine) Children() []gunim.Node { return []gunim.Node{s.label, s.hint} }

// statusHeight is the line's height once grown.
const statusHeight = 24

// Layout implements [gunim.Node].
func (s *statusLine) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	h := max(0, s.height.Value())
	room := gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-24), statusHeight)}
	k := kids.At(0)
	size := k.Layout(room)
	k.Place(geom.Pt(12, (statusHeight-size.H)/2))
	k = kids.At(1)
	size = k.Layout(room)
	k.Place(geom.Pt(12, h-statusHeight+(statusHeight-size.H)/2))
	return c.Constrain(geom.Sz(c.Max.W, h))
}

// Paint implements [gunim.Node]: the line shows as far as it has grown,
// and a menu's hint over it and the stage's bottom.
func (s *statusLine) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if s.hinted != "" {
		p.RRect(geom.Rc(0, box.H-statusHeight, box.W, statusHeight), 0, paint.Solid(widget.MenubarFill.Get(f.Theme)))
		kids.At(1).Paint(p)
		return
	}
	if box.H < 1 {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true})()
	kids.At(0).Paint(p)
}

// fullTitle is what a menu line does, in full: the palette's title for
// its command, which says the half a heading over the line leaves out,
// or else the line as the menu shows it. It is empty for no line.
func (w *window) fullTitle(id string, menu, item int) string {
	if menu < 0 || item < 0 || menu >= len(w.bar.Menus) || item >= len(w.bar.Menus[menu].Items) {
		return ""
	}
	if at := slices.Index(w.paletteIDs, id); id != "" && at >= 0 {
		return w.palette.Items[at].Title
	}
	return w.bar.Menus[menu].Items[item]
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

// sidebarPage is how many rows PageUp and PageDown move in the sidebar.
const sidebarPage = 8

// focusRowsAway gives the keyboard to the row n rows past from, or
// before it for a negative n, counting only rows that take it, and
// stopping at the last one there is.
func (w *window) focusRowsAway(from string, n int, u *gunim.UI) {
	keys := w.list.Keys()
	at := slices.Index(keys, widget.Key(from))
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	var last *sideRow
	for i := at + step; i >= 0 && i < len(keys) && n > 0; i += step {
		if row, ok := widget.RowOf[*sideRow](w.list, keys[i]); ok && !row.heading {
			last = row
			n--
		}
	}
	if last != nil {
		u.Focus(last)
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

// showMachineMenu opens a heading's menu of items, each doing its act.
func (w *window) showMachineMenu(r *sideRow, items []string, acts []func(*gunim.UI), u *gunim.UI) {
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
	for _, h := range w.saved {
		if h.Name == m && h.Window && !window {
			// Saved as a window and not connected: nothing that needs a
			// shell applies, as in gridterm.
			saved := h
			add("Connect", send(ConnectTo{Saved: m}))
			add("Edit This Window…", func(u *gunim.UI) { w.serverForm(&saved, u) })
			add("Remove This Window…", func(u *gunim.UI) { w.confirmRemove(m, u) })
			w.showMachineMenu(r, items, acts, u)
			return
		}
	}
	add("Terminal", send(OpenOn{Machine: m}))
	if m == "" && len(w.shellChoices) > 1 {
		// This computer's shells, each to open a terminal with, as
		// gridterm lists them.
		for _, sh := range w.shellChoices {
			add("Terminal: "+sh.Title, send(OpenShellNamed{ID: sh.ID}))
		}
	}
	if !window {
		add("Command…", func(u *gunim.UI) { w.commandDialogOn(m, u) })
	}
	add("Files", send(FilesOn{Machine: m}))
	for _, f := range w.foldersOn(m) {
		add("Files in "+f, send(FilesOn{Machine: m, Path: f}))
	}
	if m != "" && !window {
		add("Tunnel…", func(u *gunim.UI) { w.tunnelDialogOn(m, false, u) })
		add("SOCKS Proxy…", func(u *gunim.UI) { w.tunnelDialogOn(m, true, u) })
	}
	if slices.Contains(w.dropped, m) {
		// Its connection went: the row stays until this clears it.
		add("Clear", send(ClearMachine{Name: m}))
	}
	if m != "" {
		add("Connection Log", send(ShowLog{Machine: m}))
		add("Disconnect", send(Disconnect{Machine: m}))
		for _, h := range w.saved {
			if h.Name == m {
				saved := h
				what := "Server"
				if h.Window {
					what = "Window"
				}
				add("Edit This "+what+"…", func(u *gunim.UI) { w.serverForm(&saved, u) })
				add("Remove This "+what+"…", func(u *gunim.UI) { w.confirmRemove(m, u) })
			}
		}
	}
	w.showMachineMenu(r, items, acts, u)
}
