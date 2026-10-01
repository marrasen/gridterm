// Package view is kakel's window: it draws the State the program side
// publishes, and turns what the user does into intents for it.
package view

import (
	"fmt"
	"log"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/look"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/winkeys"
	"github.com/marrasen/kakel/words"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/secrets"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
	"github.com/marrasen/kakel/ui"
)

// The window: a sidebar listing the panes, beside the stage, which
// shows the focused pane's group, over a status line.

// The window's own tokens.
var (
	noGap     = theme.Length("kakel.nogap", 0)
	smallText = theme.Length("kakel.small", 12)
)

// Window is the view the program's state drives.
type Window struct {
	anim.Group
	top *widget.Flex
	bar *widget.Menubar
	// serversView is the Servers pane, which holds list, the machines
	// and what is open on them; rows are the list's rows as last
	// worked out, and lastWorked the pane last worked in here, other
	// than the Servers pane, whose row is lit.
	serversView *serversPane
	rows        []sideItem
	lastWorked  string
	// listShown says the list is in the tree, and entering is a pane
	// the keyboard came into, asked to be the one in front.
	listShown bool
	entering  string
	// thumbsMade is how many thumbnails had been made, as last
	// published.
	thumbsMade uint64
	// tabs is the tab bar, and titleRow the title bar it sits in, after
	// barBox, which holds the menu button.
	tabs     *tabBar
	titleRow *widget.Flex
	barBox   *widget.Sized
	// dock is where a tab dragged over the stage would join a split.
	dock tabDock
	// cards are the Servers pane's machines, a card each.
	cards  *serverCards
	stage  *stage
	status *statusLine
	shells *screen.Shells
	keys   *ui.Keymap
	terms  map[string]*term
	// termPads hold the terminals, by pane, a little in from the pane's
	// edges.
	termPads map[string]*termPad
	// browsers and readers are the file panes and readers, by pane.
	browsers map[string]*browser
	readers  map[string]*reader
	choosers map[string]*chooser
	// groups are how each pane is arranged, by pane, for the switcher.
	groups map[string]*app.Box
	// typeAll says what is typed goes to every terminal in the split on
	// stage, stageBox, as Type in All Panes asks; lastChips is the
	// state the chips were last shown from, to show them again as it
	// turns on or off.
	typeAll   bool
	stageBox  *app.Box
	lastChips app.State
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
	share app.Share
	// serving is the serving as last published.
	serving app.Serving
	// bells is the bells as last published, titles whether panes show
	// their titles, and captions the line over each pane that does.
	// stopQuiet cancels the wake for the next row's note to go quiet.
	stopQuiet func()
	// presenting says the window fills the screen with the stage alone:
	// the menu bar and the status line slide away. barShade holds the
	// menu bar, to slide it up.
	presenting bool
	barShade   *shade
	// savedCommands are the commands kept, and remoteWindows the
	// windows connected to, as last published.
	savedCommands []settings.SavedCommand
	remoteWindows []app.RemoteWindow
	// shellChoices are the shells here, and chosenShell the one kept,
	// as last published.
	shellChoices []app.ShellChoice
	chosenShell  string
	// termProgram is what new shells are told the terminal is called,
	// and launcherKey the launcher's key, as last published.
	termProgram string
	launcherKey string
	// update is where kakel stands on installing and updating.
	update app.Update
	// thisComputer is this computer's settings, as the program keeps
	// them.
	thisComputer app.ThisComputer
	// connected are the servers connected to, as last published.
	connected []machines.ID
	// help is the list of commands, once opened, and shortcutsRead
	// the shortcuts file's reads as last taken on.
	help          *helpPane
	copies        *copiesPane
	shortcutsRead uint64
	// secretsExist says there are secrets, to keep a new key's passphrase in.
	secretsExist bool
	bells        uint64
	// echo sends rings out past the window's edges, pings the counts
	// last sent for, and away says another program has the keyboard.
	echo         widget.Echo
	pings        app.Pings
	away         bool
	titles       bool
	captions     map[string]*captioned
	sharing      bool
	savedTunnels []settings.SavedTunnel
	// accounts are the machines with a connection log, as the palette
	// lists them.
	accounts []machines.ID
	splits   map[string]*widget.Split
	focused  string
	palette  *widget.Palette
	// drawings keep what each pane's node drew last, by pane, and drawn
	// is that node, for the switcher to show a pane of any kind, and one
	// off the stage as it was last seen.
	drawings    map[string]*gunim.Drawing
	themePicker *widget.Palette
	// keyPicker lists the saved keys, to remove one from the list.
	keyPicker *widget.Palette
	drawn     map[string]gunim.Node
	// vault is the secrets as last published, and afterUnlock a command
	// waiting for them to open.
	vault       app.Secrets
	afterUnlock string
	// title is the window's title as last set.
	title string
	// dropped are the machines whose connection went by itself.
	dropped []machines.ID
	// dialing are the servers being connected to, fileClip what the
	// file clipboard holds, and keyFiles the key files kept.
	dialing  []machines.ID
	fileClip app.FileClip
	keyFiles []string
	// fonts are the families on the Font menu, and font the one the
	// terminals are drawn in.
	fonts []string
	font  app.Font
	// zoomed gathers Ctrl and the wheel until it makes a point of font
	// size.
	zoomed float32
	// recent are the panes, the most recently used first; walk is a
	// Ctrl+Tab walk under way, walkList its list on screen, and keyMods
	// the modifiers of the key running the command now.
	recent   []string
	walk     *paneWalk
	walkList *walkList
	// walkMark is the ring round the pane a walk has reached, while that
	// pane sits in a split.
	walkMark *walkMark
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
	// machineList is what the program calls each machine, by its ID.
	machineList []machines.Info
	// servingAsked says Serve was pressed and the window is not served
	// yet, and served is the dialog saying it is, while that is open.
	servingAsked bool
	servingTries uint64
	served       *servedShown
	// panes are the panes as last published, sideOrder them as the
	// sidebar lists them, and sw the switcher while it is open.
	panes     []app.Pane
	sideOrder []string
	sw        *switcher
	// winID numbers the window among kakel's own, and behind says
	// another is in front. dropLit is how lit the window is for a pane
	// dragged over it from another window.
	winID   int
	behind  bool
	dropLit *anim.Float
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
	// secretCopies counts secrets copied, so only the last one copied
	// clears the clipboard when its time is up.
	secretCopies uint64
}

func NewWindow(sh *screen.Shells, keys *ui.Keymap, all []look.Themed) *Window {
	w := &Window{
		contents:    map[string]theme.Theme{},
		stage:       &stage{},
		shells:      sh,
		keys:        keys,
		terms:       map[string]*term{},
		termPads:    map[string]*termPad{},
		browsers:    map[string]*browser{},
		readers:     map[string]*reader{},
		choosers:    map[string]*chooser{},
		tunnelPanes: map[string]*tunnelPane{},
		splits:      map[string]*widget.Split{},
		captions:    map[string]*captioned{},
		dropLit:     anim.NewFloat(0),
	}
	w.Add(w.dropLit)
	w.status = newStatusLine()
	w.onStage = widget.NewThemed(w.stage, widget.Dark())
	main := widget.Column(w.onStage, w.status).Grow(w.onStage, 1)
	main.Cross, main.Gap = widget.CrossStretch, noGap
	w.cards = newServerCards(w)
	w.serversView = newServersPane(w)
	w.bar = widget.NewMenubar()
	// The menus behind one button, leaving the bar to move the window.
	w.bar.Compact = true
	w.bar.Title = app.ProgramName
	for _, m := range menus {
		bm := widget.BarMenu{Title: m.title}
		for i, it := range m.items {
			hint := ""
			if chord, ok := keys.ChordFor(it.id); ok && !it.caption {
				hint = chordLabel(chord)
			}
			bm.Items, bm.Hints = append(bm.Items, it.title), append(bm.Hints, hint)
			bm.Icons = append(bm.Icons, commandIcons[it.id])
			bm.Checked = append(bm.Checked, false)
			if it.caption {
				bm.Captions = append(bm.Captions, i)
			}
			if it.group || (it.caption && i > 0) {
				bm.Breaks = append(bm.Breaks, i)
			}
		}
		w.bar.Menus = append(w.bar.Menus, withAccessKeys(bm))
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
				u.Send(w, app.PickFont{Name: w.fonts[i]})
			}
		case m < len(menus) && i < len(menus[m].items):
			w.run(menus[m].items[i].id, u)
		}
	}
	w.toasts = &widget.Toasts{}
	w.chips = newChipBar()
	// The pin before minimize keeps the window above the others.
	controls := widget.NewWindowControls()
	controls.Pin = true
	w.tabs = newTabBar(w)
	w.barBox = widget.NewSized(w.bar, 0, 0)
	bar := widget.Row(newAppMark(), w.barBox, w.tabs, w.chips, controls).Grow(w.barBox, 1)
	bar.Cross, bar.Gap = widget.CrossStretch, noGap
	w.titleRow = bar
	w.barShade = newShade(bar)
	w.top = widget.Column(w.barShade, main).Grow(main, 1)
	w.top.Cross, w.top.Gap = widget.CrossStretch, noGap
	w.palette = &widget.Palette{Placeholder: "Type a command", Pick: func(i int, u *gunim.UI) {
		if i < len(w.paletteIDs) {
			w.run(w.paletteIDs[i], u)
		}
	}}
	// Its shortcut again closes it: the keys typed in the palette reach
	// the palette alone, which hands on the ones it does not use.
	w.palette.Key = func(k input.KeyPress, u *gunim.UI) bool {
		ev, ok := winkeys.Event(k)
		if !ok || k.Repeat {
			return false
		}
		if id, bound := w.keys.Lookup(ui.ChordOf(ev)); bound && id == "palette.open" {
			w.palette.Close(u)
			return true
		}
		return false
	}
	w.servers(nil)
	for _, t := range all {
		w.contents[t.Name] = t.Content
	}
	return w
}

// run carries out a command: the window's own here, and the program's
// by asking it.
func (w *Window) run(id string, u *gunim.UI) bool {
	if id != w.afterUnlock {
		// Another command since: the one waiting is let go.
		w.afterUnlock = ""
	}
	switch id {
	case "pane.splitRight", "pane.splitDown":
		w.askSplit(id == "pane.splitDown", u)
		return true
	case "palette.open":
		// Its shortcut again closes it, when the key reaches the window
		// rather than the palette, which hands it on through its Key.
		if w.palette.IsOpen() {
			w.palette.Close(u)
			return true
		}
		w.palette.Open(w, geom.Rc(0, 48, w.size.W, 0), u)
		return true
	case "menu.open":
		// The menus live in the menu bar, which comes back for them.
		w.present(false, u)
		w.bar.Open(0, u)
		return true
	case "view.switcher":
		w.openSwitcher(u)
		return true
	case "pane.nextInSidebar", "pane.previousInSidebar":
		// In the sidebar's own order, which the window has: grouped by
		// machine, as the rows are read.
		if next := w.paneInSidebar(id == "pane.previousInSidebar"); next != "" {
			u.Send(w, app.FocusPane{Pane: next})
		}
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
	case "server.import":
		u.Send(w, app.ImportSSHConfig{})
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
		u.Send(w, app.ShowLog{Machine: machine})
		return true
	case "agent.typed":
		u.Send(w, app.ShowTyped{Pane: w.focused})
		return true
	case "pane.scrollback":
		if k := w.kindOf(w.focused); k != app.KindTerminal && k != app.KindLog {
			w.toasts.Show(widget.Toast{Title: "The pane in front is not a terminal", Body: "Find in Scrollback searches what a terminal or a log has kept."}, u)
			return true
		}
		u.Send(w, app.ShowScrollback{Pane: w.focused})
		return true
	case "conn.command":
		w.commandDialog(u)
		return true
	case "sidebar.focus":
		w.focusSidebar(u)
		return true
	case "files.icons":
		if b, ok := w.browsers[w.focused]; ok {
			b.setIcons(!b.icons, u)
			w.tickSwitch(id, b.icons)
		}
		return true
	case "app.launcherKey":
		w.launcherKeyDialog(u)
		return true
	case "app.thisComputer":
		w.thisComputerDialog(u)
		return true
	case "app.install":
		w.installDialog(u)
		return true
	case "app.updates":
		w.updatesDialog(u)
		return true
	case "tab.newWindow", "servers.window", "secrets.window":
		// A little down and to the right of this window, as large.
		at, size := geom.Pt(40, 40), w.size
		switch id {
		case "servers.window":
			u.Send(w, app.ToolWindow{Kind: app.KindServers, At: at, Size: size})
		case "secrets.window":
			u.Send(w, app.ToolWindow{Kind: app.KindSecrets, At: at, Size: size})
		default:
			u.Send(w, app.TabToNewWindow{At: at, Size: size})
		}
		return true
	case "sidebar.closeRow":
		row, ok := u.Focused().(*sideRow)
		switch {
		case !ok:
			w.toasts.Show(widget.Toast{Title: "No row selected", Body: "Close Selected Row works on the row the Servers pane has the keyboard on."}, u)
		case row.closes == nil:
			w.toasts.Show(widget.Toast{Title: "That row cannot be closed", Body: "A machine's heading goes with Disconnect, from its menu."}, u)
		default:
			u.Send(row, row.closes)
		}
		return true
	case "server.editThis", "server.forget":
		m := w.machineOf(w.focused)
		i := slices.IndexFunc(w.saved, func(h remote.Host) bool { return machines.ID(h.ID) == m })
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
	case "sshkey.forget":
		w.removeSavedKey(u)
		return true
	case "help.shortcuts":
		u.Send(w, app.ShowHelp{})
		return true
	case "shortcuts.write":
		u.Send(w, app.WriteShortcuts{Bindings: w.keys.Bindings()})
		return true
	case "app.about":
		w.aboutDialog(u)
		return true
	case "help.files":
		w.fileLocationsDialog(u)
		return true
	case "view.fullScreen":
		w.present(!w.presenting, u)
		return true
	case "pane.typeAll":
		w.setTypeAll(!w.typeAll, u)
		return true
	case "view.pin":
		if err := u.SetPinned(!u.Pinned()); err != nil {
			w.failed("Couldn't keep the window on top", err.Error(), u)
		}
		w.tickSwitch(id, u.Pinned())
		return true
	case "shell.termProgram":
		w.termProgramDialog(u)
		return true
	case "pane.titles":
		u.Send(w, app.TogglePaneTitles{})
		return true
	case "serve.attach":
		w.connectWindowDialog(u)
		return true
	case "conn.disconnect":
		if m := w.machineOf(w.focused); m != "" {
			u.Send(w, app.Disconnect{Machine: m})
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
		// Shared, it opens its permissions: to open them again for a
		// pane shared already, or to tick more once the share is made.
		if slices.ContainsFunc(w.share.Panes, func(p app.SharedPane) bool { return p.Pane == w.focused }) {
			w.permissionsDialog(w.share, u)
			return true
		}
		u.Send(w, app.SharePane{Pane: w.focused})
		w.permsAfter = w.focused
		return true
	case "agent.take":
		u.Send(w, app.UnsharePane{Pane: w.focused})
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
	case "edit.selectAll":
		if t, ok := w.terms[w.focused]; ok {
			t.sh.T.SelectAll()
			t.sync()
			u.Invalidate()
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
	// The terminal in front's own, as the palette and the menus run
	// them: copying, pasting, scrolling back.
	if t, ok := w.terms[w.focused]; ok {
		return t.command(id, u)
	}
	if rd, ok := w.readers[w.focused]; ok && rd.r != nil && (id == "view.scrollUp" || id == "view.scrollDown") {
		page := -1
		if id == "view.scrollDown" {
			page = 1
		}
		rd.r.ScrollPages(page)
		rd.sync()
		u.Invalidate()
		return true
	}
	return false
}

// Children implements [gunim.Composite].
func (w *Window) Children() []gunim.Node { return []gunim.Node{w.top, w.toasts} }

// Layout implements [gunim.Node]. The switcher, while open, covers the
// window.
func (w *Window) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
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
func (w *Window) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(p)
	}
	w.paintDropLit(p, f, box)
	w.paintDock(p, f)
}

// openDialog shows d over the window, with the keyboard, until it
// closes.
func (w *Window) openDialog(d *widget.Dialog, u *gunim.UI) {
	if f, ok := d.Body.(*widget.Form); ok {
		// A form taller than the window scrolls in the room the dialog
		// can spare, rather than running off both ends of the screen
		// with the buttons.
		d.Body = &scrollingForm{Scroll: widget.NewScroll(f), form: f}
	}
	// The dialog takes the keyboard itself as it arrives, to its first
	// field, and holds it until it closes.
	u.Insert(w, d)
	w.dialog = d
	// The pane takes the keyboard back once the dialog has closed.
	w.focused = ""
}

// scrollingForm is a dialog's form in a scroll, which hands the dialog
// the form's fields, for Tab to go through them and the first to take
// the keyboard as the dialog opens.
type scrollingForm struct {
	*widget.Scroll
	form *widget.Form
}

// Focusables are the form's fields.
func (s *scrollingForm) Focusables() []gunim.Node { return s.form.Focusables() }

// formOf is the form a dialog's body shows, or nil.
func formOf(body gunim.Node) *widget.Form {
	switch b := body.(type) {
	case *widget.Form:
		return b
	case *scrollingForm:
		return b.form
	}
	return nil
}

// zoom makes the font a point larger for each notch the wheel turns
// away from the user, with Ctrl held, and smaller toward.
func (w *Window) zoom(s input.Scroll, u *gunim.UI) {
	w.zoomed += s.Delta.Y / zoomNotch
	steps := int(w.zoomed)
	if steps == 0 {
		return
	}
	w.zoomed -= float32(steps)
	u.Send(w, app.FontSize{Step: steps})
}

// zoomNotch is how far the wheel turns for a point of font size: a
// notch, as the drivers count it.
const zoomNotch = 40

// showFonts fills the Font menu, the family in use ticked, and draws
// every terminal and reader in that family.
func (w *Window) showFonts(st app.State) {
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
		m.Checked = append(m.Checked, app.FontCommandID(name) == app.FontCommandID(st.Font.Name))
		if i == 1 {
			// A line under Go Mono.
			m.Breaks = append(m.Breaks, i)
		}
	}
	if i := menuAt("Font"); i >= 0 && i < len(w.bar.Menus) {
		w.bar.Menus[i] = withAccessKeys(m)
	}
}

// showTitle names the window after the pane in front: "kakel" and the
// pane's title on the window's own title bar, and the two together for
// the taskbar. The title is the one its row in the sidebar shows, which
// follows what its program calls it, with a shell's path given as the
// shell's name.
func (w *Window) showTitle(st app.State, u *gunim.UI) {
	pane := ""
	for _, p := range st.Panes {
		if p.ID == st.Focus {
			pane = p.Title
		}
	}
	title := app.ProgramName
	if pane != "" {
		title = app.ProgramName + " — " + pane
	}
	if title != w.title {
		w.title = title
		u.SetTitle(title)
		u.Invalidate()
	}
	// The tab bar names the panes while it shows.
	if w.tabs.shown() {
		pane = ""
	}
	if w.bar.Subtitle != pane {
		w.bar.Subtitle = pane
		u.Invalidate()
	}
}

// secretsCommand carries out a command on the secrets that picks one
// first, or takes a passphrase. A locked vault is unlocked first, and
// the command carried on once it is open.
func (w *Window) secretsCommand(id string, u *gunim.UI) {
	st := w.vault
	if !st.Open {
		w.afterUnlock = id
		u.Send(w, app.UnlockSecrets{})
		return
	}
	w.afterUnlock = ""
	switch id {
	case "secrets.change", "secrets.forget":
		if len(st.Items) == 0 {
			w.toasts.Show(widget.Toast{Title: "No secrets yet", Body: "Add one with Add Secret."}, u)
			return
		}
		choices := make([]widget.PaletteItem, len(st.Items))
		for i, it := range st.Items {
			choices[i] = widget.PaletteItem{Title: it.Name, Hint: secretFor(it)}
		}
		items := st.Items
		w.chooseFrom("Which secret?", choices, func(i int, u *gunim.UI) {
			it := items[i]
			if id == "secrets.change" {
				w.secretForm(it.Kind, &it, u)
				return
			}
			w.confirmRemoveSecret(it, u)
		}, u)
	case "secrets.removeKey":
		choices := make([]widget.PaletteItem, len(st.Keys))
		for i, k := range st.Keys {
			var hint []string
			for _, part := range []string{k.Note, keyFingerprint(k)} {
				if part != "" {
					hint = append(hint, part)
				}
			}
			choices[i] = widget.PaletteItem{Title: k.Name, Hint: strings.Join(hint, " · ")}
		}
		keys := st.Keys
		w.chooseFrom("Which key?", choices, func(i int, u *gunim.UI) { w.confirmRemoveKey(st, keys[i], u) }, u)
	case "secrets.addPassphrase":
		if st.Passphrase {
			w.toasts.Show(widget.Toast{Title: "The secrets already take a passphrase", Body: "Remove Secrets Key takes it away first."}, u)
			return
		}
		w.passphraseForm(st, u)
	}
}

// chooseFrom offers items in a palette, and runs then with the one
// picked.
func (w *Window) chooseFrom(placeholder string, items []widget.PaletteItem, then func(i int, u *gunim.UI), u *gunim.UI) {
	p := &widget.Palette{Placeholder: placeholder, Pick: then, Items: items}
	p.Open(w, geom.Rc(0, 48, w.size.W, 0), u)
}

// pickTheme offers the themes in a palette, the one on marked.
func (w *Window) pickTheme(u *gunim.UI) {
	p := &widget.Palette{Placeholder: "Pick a theme"}
	for _, name := range w.themes {
		hint := ""
		if name == w.themeNow {
			hint = "in use"
		}
		p.Items = append(p.Items, widget.PaletteItem{Title: name, Hint: hint})
	}
	names := w.themes
	p.Pick = func(i int, u *gunim.UI) { u.Send(w, app.PickTheme{Name: names[i]}) }
	// The theme the highlight is on shows at once, the window and the
	// terminals in it, from the first move: the highlight the palette
	// opens with is not one the user put there. Closed with nothing
	// picked, the theme in use comes back.
	opened := false
	p.Hot = func(i int, u *gunim.UI) {
		if !opened {
			opened = true
			return
		}
		if i >= 0 && i < len(names) {
			u.Send(w, app.PreviewTheme{Name: names[i]})
		}
	}
	p.Cancel = func(u *gunim.UI) { u.Send(w, app.PreviewTheme{}) }
	w.themePicker = p
	p.Open(w, geom.Rc(0, 48, w.size.W, 0), u)
}

// removeSavedKey offers the saved key files, to take one off the list the
// server form offers.
func (w *Window) removeSavedKey(u *gunim.UI) {
	if len(w.keyFiles) == 0 {
		w.toasts.Show(widget.Toast{Title: "No saved keys", Body: "A key is saved when it is created here or chosen for a server."}, u)
		return
	}
	p := &widget.Palette{Placeholder: "Saved keys"}
	files := slices.Clone(w.keyFiles)
	for _, f := range files {
		p.Items = append(p.Items, widget.PaletteItem{Title: f, Icon: icon.KeyRound})
	}
	p.Pick = func(i int, u *gunim.UI) { u.Send(w, app.RemoveSavedKey{Path: files[i]}) }
	w.keyPicker = p
	p.Open(w, geom.Rc(0, 48, w.size.W, 0), u)
}

// connectDialog asks for a server to connect to without saving it, a
// quick connection, or a saved one typed by its name.
func (w *Window) connectDialog(u *gunim.UI) {
	target := widget.NewTextField()
	target.Placeholder = "user@host or user@host:port"
	d := widget.NewDialog("Quick Connect")
	d.Body = widget.NewForm().Add("Server", target)
	d.SetButtons("Connect", "Cancel")
	d.OnAccept = func() gunim.Intent {
		typed := strings.TrimSpace(target.Text())
		for _, h := range w.saved {
			if strings.EqualFold(h.Name, typed) {
				return app.ConnectTo{Server: machines.ID(h.ID)}
			}
		}
		return app.ConnectTo{Target: typed}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// showAsk shows the oldest question a connection is waiting on, in a
// dialog, and takes the dialog away when its question goes, as when the
// connection gives up.
func (w *Window) showAsk(asks []app.Ask, u *gunim.UI) {
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
		if q.Preformatted {
			// Lines kept whole, as a key or a table means them, and
			// scrolled sideways where they are wider than the dialog.
			note.Face, note.Selectable, note.NoWrap = widget.MonoFont, true, true
		}
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
	}
	// A saved secret to answer with instead: picked, it stands for what
	// would be typed, and there is nothing typed to keep.
	var saved *widget.Dropdown
	if len(q.Saved) > 0 {
		saved = widget.NewDropdown(append([]string{"None, type it"}, q.Saved...)...)
		saved.Label = "Saved secret"
		saved.OnPick(func(i int, u *gunim.UI) {
			for _, f := range fields {
				f.Disabled = i > 0
			}
			if also != nil {
				also.Disabled = i > 0
			}
			u.Invalidate()
		})
		form.Add("Or use", saved)
	}
	if also != nil {
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
		d.AddAction(act, func(u *gunim.UI) { u.Send(w, app.AskAction{ID: q.ID, Action: act}) })
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
		if saved != nil {
			answers = append(answers, strconv.Itoa(saved.Selected-1))
		}
		return app.AskAnswered{ID: id, Yes: true, Answers: answers}
	}
	no := q.No
	if no == "" {
		no = "Cancel"
	}
	if len(q.Choose) > 0 {
		d.SetButtons(q.Choose[0], no)
		// Where the first choice is the safe one, such as Leave It
		// beside Replace, Tab from the fields reaches it first.
		d.DefaultFirst = q.FirstIsSafe
		d.OnAccept = func() gunim.Intent { return answer(q.Choose[0]) }
		for _, c := range q.Choose[1:] {
			d.AddButton(c, func() gunim.Intent { return answer(c) })
		}
	} else {
		d.SetButtons(q.Yes, no)
		d.OnAccept = func() gunim.Intent { return answer("") }
	}
	d.Dismiss = app.AskAnswered{ID: id}
	d.Danger, d.Careful = q.Danger, q.Careful
	d.Icon = askIcons[q.Icon]
	if q.Plain {
		d.SetButtons(q.Yes, "")
	}
	w.ask, w.askID = d, id
	w.openDialog(d, u)
}

// servers fills the Servers menu and the palette: the window's own
// commands, then the saved servers, each to connect to, and in the
// palette to edit or remove too.
func (w *Window) servers(saved []remote.Host) {
	w.saved = saved
	hint := func(id string) string {
		if chord, ok := w.keys.ChordFor(id); ok {
			return chordLabel(chord)
		}
		return ""
	}
	// The saved servers first, under Connect To, then what is
	// always there.
	m := widget.BarMenu{Title: "Servers"}
	w.serverIDs = nil
	if len(saved) > 0 {
		m.Captions = []int{0}
		m.Items, m.Hints = append(m.Items, "Connect To"), append(m.Hints, "")
		m.Icons = append(m.Icons, nil)
		w.serverIDs = append(w.serverIDs, "")
		for _, h := range saved {
			id := "server.open." + remote.CommandName(h.Name)
			m.Items, m.Hints = append(m.Items, h.Name), append(m.Hints, hint(id))
			m.Icons = append(m.Icons, icon.Server)
			w.serverIDs = append(w.serverIDs, id)
		}
		m.Breaks = []int{len(m.Items)}
	}
	m.Items = append(m.Items, "Quick Connect…", "Open Launcher", "Add Server…", "Import from SSH Config", "Reload Server List")
	m.Hints = append(m.Hints, hint("server.connect"), "", "", "", "")
	m.Icons = append(m.Icons, icon.Plug, icon.Search, icon.Plus, icon.FileInput, icon.RefreshCw)
	w.serverIDs = append(w.serverIDs, "server.connect", "app.launcher", "server.add", "server.import", "server.reload")
	if i := menuAt("Servers"); i >= 0 && i < len(w.bar.Menus) {
		// The lines always there first.
		n := len(m.Items)
		w.bar.Menus[i] = withAccessKeys(m, n-5, n-4, n-3, n-2, n-1)
	}
	w.palette.Items, w.paletteIDs = nil, nil
	for _, c := range commands {
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: c.title, Icon: commandIcons[c.id], Hint: hint(c.id), Also: commandAlso[c.id]})
		w.paletteIDs = append(w.paletteIDs, c.id)
	}
	for _, h := range saved {
		for _, c := range []struct {
			title, id string
			icon      *icon.Icon
		}{
			{"Connect to " + h.Name, "server.open.", icon.Plug},
			{"Edit Server " + h.Name, "server.edit.", icon.Pencil},
			{"Remove Server " + h.Name, "server.remove.", icon.Trash2},
		} {
			id := c.id + remote.CommandName(h.Name)
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: c.title, Icon: c.icon, Also: []string{h.Address}, Hint: hint(id)})
			w.paletteIDs = append(w.paletteIDs, id)
		}
	}
	for _, m := range w.accounts {
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Connection Log for " + w.nameOf(m), Icon: icon.ScrollText})
		w.paletteIDs = append(w.paletteIDs, "conn.log."+w.cmdName(m))
	}
	// Every machine by name: a terminal there, its files, and each
	// folder saved for it.
	for _, m := range w.machines() {
		where := w.nameOf(m)
		if m == "" {
			where = "This Computer"
		}
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "New Terminal on " + where, Icon: icon.SquareTerminal},
			widget.PaletteItem{Title: "Browse Files on " + where, Icon: icon.Folder})
		w.paletteIDs = append(w.paletteIDs, "conn.terminal."+w.cmdName(m), "conn.files."+w.cmdName(m))
		for i, f := range w.foldersOn(m) {
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Browse " + f + " on " + where, Icon: icon.Folder})
			w.paletteIDs = append(w.paletteIDs, "conn.files."+w.cmdName(m)+"."+strconv.Itoa(i+1))
		}
	}
	// With more than one shell here, a terminal with any of them, and
	// which new terminals start.
	if len(w.shellChoices) > 1 {
		ids := w.shellIDs()
		for i, s := range w.shellChoices {
			w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "New " + s.Title, Icon: icon.SquareTerminal, Hint: hint(ids[i])})
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
		id := app.FontCommandID(name)
		w.palette.Items = append(w.palette.Items, widget.PaletteItem{Title: "Font: " + name, Hint: hint(id)})
		w.paletteIDs = append(w.paletteIDs, id)
	}
	for i, it := range w.savedTunnelItems() {
		w.palette.Items = append(w.palette.Items, it)
		w.paletteIDs = append(w.paletteIDs, "conn.savedtunnel."+strconv.Itoa(i+1))
	}
}

// runItem carries out a command on one thing of many, by kakel's
// name for it: a saved server, a machine, a folder saved on one, a
// shell, a saved command or tunnel. It reports false for any other id.
func (w *Window) runItem(id string, u *gunim.UI) bool {
	savedNamed := func(cmd string) (remote.Host, bool) {
		for _, h := range w.saved {
			if remote.CommandName(h.Name) == cmd {
				return h, true
			}
		}
		return remote.Host{}, false
	}
	machineNamed := func(cmd string) (machines.ID, bool) {
		for _, m := range append(w.machines(), w.accounts...) {
			if w.cmdName(m) == cmd {
				return m, true
			}
		}
		if h, ok := savedNamed(cmd); ok {
			return machines.ID(h.ID), true
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
			u.Send(w, app.ConnectTo{Server: machines.ID(h.ID)})
		}
	case strings.HasPrefix(id, "server.edit."):
		if h, ok := savedNamed(strings.TrimPrefix(id, "server.edit.")); ok {
			w.serverForm(&h, u)
		}
	case strings.HasPrefix(id, "server.remove."):
		if h, ok := savedNamed(strings.TrimPrefix(id, "server.remove.")); ok {
			w.confirmRemove(machines.ID(h.ID), u)
		}
	case strings.HasPrefix(id, "conn.log."):
		if m, ok := machineNamed(strings.TrimPrefix(id, "conn.log.")); ok {
			u.Send(w, app.ShowLog{Machine: m})
		}
	case strings.HasPrefix(id, "conn.terminal."):
		if m, ok := machineNamed(strings.TrimPrefix(id, "conn.terminal.")); ok {
			u.Send(w, app.OpenOn{Machine: m})
		}
	case strings.HasPrefix(id, "conn.files."):
		rest := strings.TrimPrefix(id, "conn.files.")
		if m, ok := machineNamed(rest); ok {
			u.Send(w, app.FilesOn{Machine: m})
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
			u.Send(w, app.FilesOn{Machine: m, Path: folders[i]})
		}
	case strings.HasPrefix(id, shellfind.CommandPrefix):
		for i, sid := range w.shellIDs() {
			if sid == id {
				u.Send(w, app.OpenShellNamed{ID: w.shellChoices[i].ID})
			}
		}
	case strings.HasPrefix(id, "shell.pick."):
		u.Send(w, app.PickShell{ID: strings.TrimPrefix(id, "shell.pick.")})
	case strings.HasPrefix(id, "conn.saved."):
		if i, ok := nth(strings.TrimPrefix(id, "conn.saved.")); ok && i < len(w.savedCommands) {
			u.Send(w, app.RunSavedCommand{Saved: w.savedCommands[i]})
		}
	case strings.HasPrefix(id, "font.use."):
		for _, name := range w.fonts {
			if app.FontCommandID(name) == id {
				u.Send(w, app.PickFont{Name: name})
			}
		}
	case strings.HasPrefix(id, "conn.savedtunnel."):
		if i, ok := nth(strings.TrimPrefix(id, "conn.savedtunnel.")); ok && i < len(w.savedTunnels) {
			u.Send(w, app.OpenSavedTunnel{Saved: w.savedTunnels[i]})
		}
	default:
		return false
	}
	return true
}

// shellIDs are kakel's names for the commands that open a terminal
// with each of the shells here, in the order of shellChoices.
func (w *Window) shellIDs() []string {
	list := make([]shellfind.Shell, len(w.shellChoices))
	for i, s := range w.shellChoices {
		list[i] = shellfind.Shell{ID: s.ID}
	}
	return shellfind.CommandIDs(list)
}

// savingClashes says what stands in the way of saving h in place of
// old: a new name something is connected as, a window saved twice, a
// connected window moved.
func (w *Window) savingClashes(h remote.Host, old *remote.Host) string {
	was := ""
	if old != nil {
		was = old.ID
	}
	if h.Window {
		for _, s := range w.saved {
			if s.Window && (was == "" || s.ID != was) && s.ServeAddr() == h.ServeAddr() {
				return fmt.Sprintf("%s is already saved as the window at %s; a window has one entry in the list.", s.Name, h.ServeAddr())
			}
		}
	}
	for _, rw := range w.remoteWindows {
		if was != "" && rw.Name == machines.ID(was) && (!h.Window || rw.Addr != h.ServeAddr()) {
			return fmt.Sprintf("%s is connected at %s; let go of it before changing where it is.", old.Name, rw.Addr)
		}
	}
	return ""
}

// serverForm asks for a server to save: a new one, or old edited.
func (w *Window) serverForm(old *remote.Host, u *gunim.UI) {
	name, addr, port, user, key := widget.NewTextField(), widget.NewTextField(), widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	addr.Placeholder, port.Placeholder = "host name or address", "22"
	user.Placeholder, key.Placeholder = "your name here", "the usual keys in ~/.ssh"
	// Through lists the other saved servers, to reach this one through.
	// A kakel window is no jump host: it opens no SSH way on.
	through := []string{"Directly"}
	ids := []string{""}
	for _, h := range w.saved {
		if (old == nil || h.ID != old.ID) && !h.Window {
			through = append(through, h.Name)
			ids = append(ids, h.ID)
		}
	}
	via := widget.NewDropdown(through...)
	via.Label = "Through"
	kind := widget.NewDropdown("Server", remote.WindowKind)
	kind.Label = "Type"
	folders := widget.NewTextField()
	folders.Placeholder = "optional: paths to open files at, with commas"
	// The key files kept, so one is a pick away rather than a path to
	// remember.
	var kept *widget.Dropdown
	if len(w.keyFiles) > 0 {
		kept = widget.NewDropdown(append([]string{"Choose a saved key"}, w.keyFiles...)...)
		kept.Label = "Saved keys"
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
		// Left as they were, they stay as saved: a folder with a comma in
		// its path would be split in two by reading the line back.
		// Changed, such a folder is refused, as it would be lost.
		switch {
		case old != nil && folders.Text() == old.FoldersJoined():
			h.Folders = old.Folders
		case old != nil && commaFolderKept(old.Folders, folders.Text()):
			return h, "A saved folder has a comma in its path, which this line cannot keep apart. Leave the folders as they were, or take that folder out."
		default:
			h.Folders = remote.FoldersFrom(folders.Text())
		}
		// A window keeps the agent tick it had, unused, so switching it
		// back to a server brings it back. Not its route: a window
		// naming a jump host would keep that host from being removed,
		// for a route it never takes.
		h.Setup, h.ForwardAgent = setup.On, forward.On
		if h.Window {
			h.Via = ""
		}
		if err := h.Validate(); err != nil {
			return h, words.UpperFirst(err.Error()) + "."
		}
		if why := w.savingClashes(h, old); why != "" {
			return h, why
		}
		return h, ""
	}
	// A window has no account, nothing to go through and no session to
	// carry an agent over: those are greyed out while the type says
	// window, rather than taken and dropped.
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
		form.Add("Saved keys", kept)
	}
	d := widget.NewDialog(title)
	d.Body = form.Add("Folders", folders).Add("", setup).Add("", forward)
	d.SetButtons("Save", "Cancel")
	if old != nil {
		d.AddAction("Remove…", func(u *gunim.UI) {
			d.Close(u)
			w.confirmRemove(machines.ID(old.ID), u)
		})
	}
	d.Check = func() string {
		_, problem := host()
		return problem
	}
	d.OnAccept = func() gunim.Intent {
		h, _ := host()
		return app.SaveServer{Host: h, Under: under}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// confirmFarDisconnect asks before closing another window's connection
// to the machine m it reaches: whoever uses that window loses what is
// open there too.
func (w *Window) confirmFarDisconnect(m machines.ID, u *gunim.UI) {
	window, _, _ := m.Far()
	d := widget.NewDialog("Disconnect " + w.farHostName(m) + " on " + w.nameOf(window) + "?")
	d.Body = widget.NewLabel("This closes " + w.nameOf(window) + "'s own connection to " + w.farHostName(m) +
		", and everything open on it there and on any server reached through it, for anyone working in that window too.")
	d.SetButtons("Disconnect", "Cancel")
	d.Danger = true
	d.Accept = app.Disconnect{Machine: m}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// confirmRemove asks before forgetting the saved server with ID id.
func (w *Window) confirmRemove(id machines.ID, u *gunim.UI) {
	d := widget.NewDialog("Remove " + w.nameOf(id) + "?")
	if said := w.removeSays(id); said != "" {
		d.Body = widget.NewLabel(said)
	}
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.Accept = app.RemoveServer{ID: id}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// filesKeyOf is the name a pane's files are kept under, as the program
// names it.
func (w *Window) filesKeyOf(id string) machines.ID {
	for _, p := range w.panes {
		if p.ID == id {
			if p.On != "" {
				return machines.FarID(p.Machine, p.On)
			}
			return p.Machine
		}
	}
	return ""
}

// nextFilePane is the file pane after id, or before it with back, in
// the sidebar's order, and empty when id is the only one.
func (w *Window) nextFilePane(id string, back bool) string {
	var files []string
	for _, p := range w.panes {
		if p.Kind == app.KindFiles {
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

// foldersOn are the folders offered for a machine: the ones saved for
// it, and on this computer each WSL distribution's, which Windows serves
// on a share of its own.
func (w *Window) foldersOn(m machines.ID) []string {
	if window, key, far := m.Far(); far {
		// Beyond a window: the ones it saved for the machine.
		for _, rw := range w.remoteWindows {
			if rw.Name == window {
				return rw.Folders[key]
			}
		}
		return nil
	}
	var out []string
	for _, h := range w.saved {
		if machines.ID(h.ID) == m {
			out = append(out, h.Folders...)
		}
	}
	if m == "" {
		out = append(out, w.thisComputer.Folders...)
		for _, s := range w.shellChoices {
			if s.Folder != "" {
				out = append(out, s.Folder)
			}
		}
	}
	return out
}

// removeSays is what removing a server closes, and nothing when it
// closes nothing.
func (w *Window) removeSays(name machines.ID) string {
	called := w.nameOf(name)
	switch {
	case slices.ContainsFunc(w.remoteWindows, func(rw app.RemoteWindow) bool { return rw.Name == name }):
		return called + " is connected. Removing it closes the connection and its panes."
	case slices.Contains(w.connected, name):
		said := called + " is connected. Removing it closes the connection"
		panes := 0
		for _, p := range w.panes {
			if p.Machine == name {
				panes++
			}
		}
		if panes > 0 {
			said += " and everything through it: " + words.Count(panes, "pane")
		}
		return said + "."
	case slices.Contains(w.dropped, name):
		left := 0
		for _, p := range w.panes {
			if p.Machine == name && p.Kind != app.KindLog {
				left++
			}
		}
		said := "Its connection was lost."
		if slices.Contains(w.dialing, name) {
			said += " Removing it cancels the reconnect in progress"
			if left > 0 {
				said += " and closes its " + words.Count(left, "ended pane")
			}
			return said + "."
		}
		if left > 0 {
			return said + " Removing it closes its " + words.Count(left, "ended pane") + "."
		}
	case slices.Contains(w.dialing, name):
		return "Removing it cancels the connection in progress."
	}
	return ""
}

// rename asks for a new name for the pane with the keyboard.
func (w *Window) rename(u *gunim.UI) {
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
	d.OnAccept = func() gunim.Intent { return app.RenamePane{Pane: id, Title: name.Text()} }
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// nameOf is what machine id is called, as the program says: a saved
// server's name, a quick connection's address, "this computer" for "".
func (w *Window) nameOf(id machines.ID) string {
	if win, _, far := id.Far(); far {
		return w.farHostName(id) + " through " + w.nameOf(win)
	}
	if id == "" {
		return "this computer"
	}
	for _, m := range w.machineList {
		if m.ID == id {
			return m.Name
		}
	}
	return string(id)
}

// farHostName is what a window calls the machine beyond it that key,
// window and the window's own key for it joined by farSep, names.
func (w *Window) farHostName(key machines.ID) string {
	for _, m := range w.machineList {
		if m.ID == key {
			return m.Name
		}
	}
	_, host, _ := key.Far()
	return host
}

// quick reports whether machine id is a quick connection.
func (w *Window) quick(id machines.ID) bool {
	return slices.ContainsFunc(w.machineList, func(m machines.Info) bool { return m.ID == id && m.Quick })
}

// named is nameOf for the sidebar: a machine's name, and whether it is
// a quick connection.
func (w *Window) named(id machines.ID) (string, bool) {
	if _, _, far := id.Far(); far {
		// Under its window's heading: its own name alone.
		return w.farHostName(id), false
	}
	return w.nameOf(id), w.quick(id)
}

// cmdName is what a machine is called in a command's ID, as a
// shortcuts file names it: by its name, not its ID.
func (w *Window) cmdName(id machines.ID) string {
	if id == "" {
		return remote.CommandName("")
	}
	return remote.CommandName(w.nameOf(id))
}

// paneInSidebar is the pane after the focused one in the sidebar, or
// the one before with back, going round.
func (w *Window) paneInSidebar(back bool) string {
	n := len(w.sideOrder)
	if n == 0 {
		return ""
	}
	i := slices.Index(w.sideOrder, w.focused)
	switch {
	case i < 0:
		return w.sideOrder[0]
	case back:
		return w.sideOrder[(i+n-1)%n]
	}
	return w.sideOrder[(i+1)%n]
}

// openSwitcher shows every pane, shrunk into a grid over the window.
func (w *Window) openSwitcher(u *gunim.UI) {
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
func (w *Window) closeSwitcher(back bool, u *gunim.UI) {
	if w.sw == nil {
		return
	}
	u.Remove(w.sw)
	w.sw = nil
	if n := w.focusNode(w.focused, u); n != nil && back {
		u.Focus(n)
		return
	}
	w.focused = ""
}

// ctrlHeld tells every terminal that Ctrl went down or came up; the one
// under the pointer lights or unlights its link.
func (w *Window) ctrlHeld(k input.Key, mods input.Mods, down bool, u *gunim.UI) {
	for _, t := range w.terms {
		t.ctrlHeld(k, mods, down, u)
	}
}

// CatchKey implements [gunim.KeyCatcher]: the window's shortcuts while
// nothing has the keyboard, as after a click on room that takes none.
// The keys then go to gunim's root, above the window, and would reach no
// shortcut. With anything focused, Handle has heard them already.
func (w *Window) CatchKey(e input.Event, u *gunim.UI) bool {
	if u.Focused() != nil {
		return false
	}
	return w.shortcut(e, u)
}

// Handle implements [gunim.Handler]: the window's shortcuts, which the
// focused pane passes on.
func (w *Window) Handle(e input.Event, u *gunim.UI) bool {
	if s, ok := e.(input.Scroll); ok && s.Mods&input.ModControl != 0 {
		w.zoom(s, u)
		return true
	}
	if w.paneDrop(e, u) || w.tabDrop(e, u) {
		return true
	}
	if d, ok := e.(input.Drop); ok && len(d.Paths) > 0 {
		// Dropped somewhere that is no terminal: the sidebar, a file
		// pane, the menu bar. The focused pane is what the user is
		// working in, and is where the files are wanted.
		u.Send(w, app.DropFiles{Paths: d.Paths})
		return true
	}
	// Ctrl lights the link under the pointer in whichever pane it is
	// over, whatever has the keyboard.
	switch k := e.(type) {
	case input.WindowFocusGained:
		w.away = false
		for _, t := range w.terms {
			t.setAway(false, u)
		}
		if w.behind {
			u.Send(w, app.WindowFocused{})
		}
	case input.WindowFocusLost:
		w.away = true
		for _, t := range w.terms {
			t.setAway(true, u)
		}
		// Another program took the keyboard, as a screenshot tool does,
		// and no release of Ctrl will come: the walk ends where it is,
		// and no link stays lit.
		w.endWalk(u)
		w.ctrlHeld(input.KeyLeftControl, 0, false, u)
		return false
	case input.KeyPress:
		w.ctrlHeld(k.Key, k.Mods, true, u)
	case input.KeyRelease:
		w.ctrlHeld(k.Key, k.Mods, false, u)
	}
	if r, ok := e.(input.KeyRelease); ok {
		if w.walk != nil && walkKey(r) {
			w.endWalk(u)
			return true
		}
		return false
	}
	return w.shortcut(e, u)
}

// shortcut runs the window's command e is the shortcut for, and reports
// whether there was one.
func (w *Window) shortcut(e input.Event, u *gunim.UI) bool {
	k, ok := e.(input.KeyPress)
	if !ok {
		return false
	}
	ev, ok := winkeys.Event(k)
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

// Update shows st.
func (w *Window) Update(st app.State, u *gunim.UI) {
	w.panes = st.Panes
	w.groups = st.Groups
	w.machineList = st.Machines
	w.winID, w.behind = st.Window, st.Behind
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
	saved := make([]machines.ID, 0, len(st.Saved))
	for _, h := range st.Saved {
		saved = append(saved, machines.ID(h.ID))
	}
	all := st.AllPanes
	if all == nil {
		// Published by hand, as in a test: this window's alone.
		all = st.Panes
	}
	// The Servers pane lists the rest, not itself.
	all = slices.DeleteFunc(slices.Clone(all), func(p app.Pane) bool { return p.Kind == app.KindServers })
	rows := sidebarRows(all, st.Tunnels, st.Share, st.Windows, saved, w.named, st.Dropped...)
	// The windows connected to this one, under this computer.
	for i, c := range st.Serving.Clients {
		at := slices.IndexFunc(rows, func(r sideItem) bool { return r.key == "machine:" }) + 1
		for at < len(rows) && !rows[at].heading {
			at++
		}
		key := "client:" + c.Name + ":" + strconv.Itoa(i)
		item := sideItem{key: key, text: "serving " + c.Name, note: "from " + c.From, local: func(u *gunim.UI) { w.servingDialog(w.serving, u) },
			closes: app.DisconnectClient(c)}
		rows = slices.Insert(rows, at, item)
		// Under it, a row for each tunnel it holds through this window.
		for j, t := range st.Serving.Tunnels {
			if t.Client != c.Name || t.From != c.From {
				continue
			}
			at++
			rows = slices.Insert(rows, at, sideItem{key: key + ":tunnel:" + strconv.Itoa(j), text: t.Label, note: "for " + c.Name + ", on " + t.On,
				local: func(u *gunim.UI) { w.servingDialog(w.serving, u) }})
		}
	}
	rows = w.markRows(rows, st)
	rows = windowNotes(rows, st.AllPanes, st.Window)
	w.rows = rows
	w.sideOrder = w.sideOrder[:0]
	for _, r := range rows {
		// This window's own: a tunnel's row may name a pane in another.
		if r.pane != "" && !r.heading && slices.ContainsFunc(st.Panes, func(p app.Pane) bool { return p.ID == r.pane }) {
			w.sideOrder = append(w.sideOrder, r.pane)
		}
	}
	// The pane last worked in, in whichever window; this window's own
	// when nothing says.
	switch {
	case st.Working != "":
		w.lastWorked = st.Working
	case slices.ContainsFunc(st.Panes, func(p app.Pane) bool { return p.ID == st.Focus && p.Kind != app.KindServers }):
		w.lastWorked = st.Focus
	case !slices.ContainsFunc(all, func(p app.Pane) bool { return p.ID == w.lastWorked }):
		w.lastWorked = ""
	}
	w.setSavedTunnels(st.SavedTunnels)
	w.share = st.Share
	if id := w.permsAfter; id != "" && slices.ContainsFunc(st.Share.Panes, func(p app.SharedPane) bool { return p.Pane == id }) {
		w.permsAfter = ""
		if id == w.focused {
			w.permissionsDialog(st.Share, u)
		}
	}
	w.termProgram = st.TermProgram
	w.launcherKey = st.LauncherKey
	w.update = st.Update
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
	if !slices.Equal(st.Shells, w.shellChoices) || st.ChosenShell != w.chosenShell ||
		st.ThisComputer.StartFolder != w.thisComputer.StartFolder || !slices.Equal(st.ThisComputer.Folders, w.thisComputer.Folders) {
		w.shellChoices, w.chosenShell, w.thisComputer = st.Shells, st.ChosenShell, st.ThisComputer
		w.servers(w.saved)
	}
	w.echoFor(st, u)
	if st.Bells > w.bells {
		// The window the bell rang in, in front or not.
		w.bells = st.Bells
		u.RequestAttention()
	}
	if st.PaneTitles != w.titles {
		w.titles = st.PaneTitles
		// Every pane is built again, with its line or without.
		clear(w.splits)
	}
	w.serving = st.Serving
	w.showServed(st.Serving, u)
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
	// Every field: Edit This Server fills its form from these.
	if !reflect.DeepEqual(st.Saved, w.saved) {
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
	for id, c := range w.choosers {
		if !open[id] {
			delete(w.choosers, id)
			continue
		}
		// What it offers follows the panes, the shells and the machines.
		c.refresh(u)
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
	if st.ThumbsMade != w.thumbsMade {
		// Thumbnails have arrived for the icon views to draw.
		w.thumbsMade = st.ThumbsMade
		u.Invalidate()
	}
	w.listShown = slices.ContainsFunc(boxLeaves(st.Stage, nil), func(id string) bool { return w.kindOf(id) == app.KindServers }) &&
		u.Presence(w.cards.grid) != gunim.Exiting
	w.showServers(st, u)
	w.vault = st.Secrets
	w.noteFocus(st.Focus)
	if w.walk != nil {
		// Once the stage has laid the pane reached out, the ring goes to
		// it.
		focus, split := st.Focus, st.Stage != nil && st.Stage.Pane == ""
		u.After(0, func(u *gunim.UI) { w.markWalk(focus, split, u) })
	}
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
	w.keepDrawings(st, u)
	w.showTabs(st, u)
	w.showTitle(st, u)
	if id := w.afterUnlock; id != "" && st.Secrets.Open {
		w.run(id, u)
	}
	if w.kindOf(st.Focus) == app.KindTerminal {
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
		var t app.Tunnel
		ok := false
		for _, pane := range st.Panes {
			if pane.ID == id {
				i := slices.IndexFunc(st.Tunnels, func(t app.Tunnel) bool { return t.ID == pane.Tunnel })
				if ok = i >= 0; ok {
					t = st.Tunnels[i]
				}
			}
		}
		p.bar.show(t, ok, u)
	}
	for id, t := range w.terms {
		if w.shells.Get(id) == nil || !open[id] {
			// Ended, or moved to another window, which draws it from
			// now on.
			delete(w.terms, id)
			delete(w.termPads, id)
			continue
		}
		t.sync()
		t.lookAgain(u)
		if id == st.Focus {
			t.blink(u)
		}
	}
	if w.dialog != nil && u.Presence(w.dialog) == gunim.Exiting {
		w.dialog = nil
	}
	// The pane with the keyboard gets it when it changes, and back when
	// nothing has it, as when the split it sat in went away around it.
	if w.entering != "" && (st.Focus == w.entering || !slices.ContainsFunc(st.Panes, func(p app.Pane) bool { return p.ID == w.entering })) {
		if st.Focus == w.entering {
			w.focused = st.Focus
		}
		w.entering = ""
	}
	if w.entering == "" && w.sw == nil && w.dialog == nil && (st.Focus != w.focused || u.Focused() == nil) {
		w.focused = st.Focus
		if n := w.focusNode(st.Focus, u); n != nil {
			u.Focus(n)
		}
	}

	w.status.set(st.Status, u)
	w.stageBox = st.Stage
	w.lastChips = st
	w.showTypeAll(u)
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
				w.secretCopies++
				mine := w.secretCopies
				u.After(app.ClipboardHolds*time.Second, func(u *gunim.UI) {
					// Only the secret goes; what was copied since stays,
					// and a copy of it made since has its own time.
					if u.Clipboard() == copied && w.secretCopies == mine {
						u.SetClipboard("")
					}
				})
			}
		}
		w.toasts.Show(widget.Toast{Title: n.Title, Body: n.Body, Kind: toastKinds[n.Kind]}, u)
	}
	// The menus and the palette tick a switch while it is on, such as
	// the sidebar while it shows.
	for m := range menus {
		if len(menus[m].items) == 0 {
			continue
		}
		off := make([]bool, len(menus[m].items))
		for i, it := range menus[m].items {
			if on, isSwitch := w.switchOn(it.id, st, u); isSwitch {
				w.bar.Menus[m].Checked[i] = on
			}
			off[i] = !it.caption && !w.applies(it.id)
		}
		w.bar.Menus[m].Disabled = off
	}
	for i, id := range w.paletteIDs {
		if on, isSwitch := w.switchOn(id, st, u); isSwitch && i < len(w.palette.Items) {
			w.palette.Items[i].Checked = on
		}
	}
	u.Invalidate()
}

// applies reports whether command id can act on the pane in front now,
// for the menus to grey out the lines that cannot.
func (w *Window) applies(id string) bool {
	switch id {
	case "conn.log", "conn.disconnect":
		return w.machineOf(w.focused) != ""
	case "server.editThis", "server.forget":
		m := w.machineOf(w.focused)
		return m != "" && slices.ContainsFunc(w.saved, func(h remote.Host) bool { return machines.ID(h.ID) == m })
	case "pane.scrollback":
		k := w.kindOf(w.focused)
		return w.focused != "" && (k == app.KindTerminal || k == app.KindLog)
	case "files.goTo", "files.icons":
		_, ok := w.browsers[w.focused]
		return ok
	case "edit.copy", "edit.paste", "edit.selectAll":
		_, ok := w.terms[w.focused]
		return ok
	case "view.scrollUp", "view.scrollDown":
		_, term := w.terms[w.focused]
		rd, reader := w.readers[w.focused]
		return term || reader && rd.r != nil
	case "pane.close", "pane.rename":
		return w.focused != ""
	case "sshkey.forget":
		return len(w.keyFiles) > 0
	case "tab.next", "tab.previous", "tab.moveLeft", "tab.moveRight", "tab.newWindow":
		// A window's only tab has nowhere to go; it is its window.
		return len(w.tabs.tabs) > 1
	case "tab.close":
		return w.focused != ""
	case "app.install":
		return w.update.Installable && !w.update.Installed
	case "app.autostart":
		return w.update.Installed
	}
	return true
}

// tickSwitch ticks the menus' and the palette's rows for the switch id
// at once, for a switch the window turns itself, which no new state
// follows.
func (w *Window) tickSwitch(id string, on bool) {
	for m := range menus {
		for i, it := range menus[m].items {
			if it.id == id {
				w.bar.Menus[m].Checked[i] = on
			}
		}
	}
	for i, pid := range w.paletteIDs {
		if pid == id && i < len(w.palette.Items) {
			w.palette.Items[i].Checked = on
		}
	}
}

// switchOn reports whether the command id switches something, and
// whether that is on now.
func (w *Window) switchOn(id string, st app.State, u *gunim.UI) (on, isSwitch bool) {
	switch id {
	case "pane.typeAll":
		return w.typeAll, true
	case "sidebar.toggle":
		return slices.ContainsFunc(st.AllPanes, func(p app.Pane) bool { return p.Kind == app.KindServers }), true
	case "pane.titles":
		return st.PaneTitles, true
	case "view.fullScreen":
		return u.FullScreen(), true
	case "view.pin":
		return u.Pinned(), true
	case "shell.setup":
		return st.ShellSetup, true
	case "files.icons":
		b, ok := w.browsers[st.Focus]
		return ok && b.icons, true
	case "app.tray":
		return st.InTray, true
	case "app.autostart":
		return st.Update.Autostart, true
	}
	return false, false
}

// build returns the nodes for b, making the ones it lacks. Terminals
// are kept by pane, so a pane moving about keeps its screen. A split
// is kept while its panes stay the same, and made afresh once they
// change, so a change anywhere makes a new tree above it, which the
// stage swaps in whole.
func (w *Window) build(b *app.Box, keep map[string]bool) gunim.Node {
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
	sp.OnMove = func(v float32) gunim.Intent { return app.SplitMoved{Split: id, Share: v} }
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
func (w *Window) kindOf(id string) string {
	for _, p := range w.panes {
		if p.ID == id {
			return p.Kind
		}
	}
	return app.KindTerminal
}

// paneNode returns the node that shows pane id, made on first use,
// under a line naming it while panes show their titles.
func (w *Window) paneNode(id string) gunim.Node {
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
			c.label.SetText(w.captionOf(p))
		}
	}
	return c
}

// captionOf is the line over pane p while panes show their titles:
// where it runs, and its title. A file pane's is where it runs alone:
// the path under it names the folder already.
func (w *Window) captionOf(p app.Pane) string {
	where := w.nameOf(p.Machine)
	switch {
	case p.Machine == "":
		where = "This computer"
	case p.On != "":
		where = w.nameOf(machines.FarID(p.Machine, p.On))
	}
	if p.Kind == app.KindFiles {
		return where
	}
	return where + ": " + p.Title
}

// bareNode returns the node that shows pane id, made on first use.
func (w *Window) bareNode(id string) gunim.Node {
	switch w.kindOf(id) {
	case app.KindServers:
		return w.serversView
	case app.KindFiles:
		b, ok := w.browsers[id]
		if !ok {
			b = newBrowser(w, id)
			w.browsers[id] = b
		}
		return b
	case app.KindCopies:
		if w.copies == nil {
			w.copies = newCopiesPane(w)
		}
		return w.copies
	case app.KindHelp:
		if w.help == nil {
			w.help = newHelpPane(w)
		}
		return w.help
	case app.KindSecrets:
		if w.secrets == nil {
			w.secrets = newSecretsPane(w)
		}
		return w.secrets
	case app.KindJobs:
		if w.jobs == nil {
			w.jobs = newJobsPane()
			w.jobs.w = w
		}
		return w.jobs
	case app.KindTunnel:
		p, ok := w.tunnelPanes[id]
		if !ok {
			if w.shellGone(id) {
				return blankPane{}
			}
			p = newTunnelPane(w.term(id))
			w.tunnelPanes[id] = p
		}
		return p
	case app.KindReader:
		r, ok := w.readers[id]
		if !ok {
			r = newReader(w, id)
			w.readers[id] = r
		}
		return r
	case app.KindChooser:
		c, ok := w.choosers[id]
		if !ok {
			c = newChooser(w, id)
			w.choosers[id] = c
		}
		return c
	}
	if w.shellGone(id) {
		return blankPane{}
	}
	t := w.term(id)
	pad, ok := w.termPads[id]
	if !ok || pad.term != t {
		pad = &termPad{term: t}
		w.termPads[id] = pad
	}
	return pad
}

// entered takes pane id as the one in front, as the keyboard has come
// into it: a click in a list or on a button in a pane says so no other
// way, and the menus grey out what can't act on the pane in front.
//
// Not while no pane is in front, as while a dialog is open: the
// keyboard coming back to a pane as the dialog closes is gunim's, and
// the program may have put another pane in front meanwhile.
func (w *Window) entered(id string, u *gunim.UI) {
	if id != "" && w.focused != "" && id != w.focused {
		u.Send(w, app.FocusPane{Pane: id})
		// The keyboard is in it already, where it was clicked: the
		// answer must not move it to the pane's own first node.
		w.entering = id
	}
}

// paneOfKind is the pane of a kind there is one of, such as Help, or "".
func (w *Window) paneOfKind(kind string) string {
	for _, p := range w.panes {
		if p.Kind == kind {
			return p.ID
		}
	}
	return ""
}

// focusNode returns the node in pane id that takes the keyboard, or
// nil when there is none yet.
func (w *Window) focusNode(id string, u *gunim.UI) gunim.Node {
	if t, ok := w.terms[id]; ok {
		return t
	}
	if b, ok := w.browsers[id]; ok {
		return b.focusable()
	}
	if r, ok := w.readers[id]; ok {
		return r
	}
	if c, ok := w.choosers[id]; ok {
		return c.first()
	}
	if w.kindOf(id) == app.KindServers {
		return w.serversRow(u)
	}
	if w.kindOf(id) == app.KindJobs && w.jobs != nil {
		return w.jobs.clear
	}
	if w.kindOf(id) == app.KindSecrets && w.secrets != nil {
		return w.secrets.table
	}
	if w.kindOf(id) == app.KindHelp && w.help != nil {
		return w.help.table
	}
	if w.kindOf(id) == app.KindCopies && w.copies != nil {
		return w.copies.table
	}
	return nil
}

// shellGone reports whether pane id has no shell to show, nor a
// terminal made already. The state the window shows may be older than
// the shells: a pane closed since, as a connection's when the
// connection is given up, still names one that has gone.
func (w *Window) shellGone(id string) bool {
	_, made := w.terms[id]
	return !made && w.shells.Get(id) == nil
}

// blankPane stands in for a pane that has nothing to show yet, or any
// more: it takes its room and draws nothing.
type blankPane struct{}

// Layout implements [gunim.Node].
func (blankPane) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Max
}

// Paint implements [gunim.Node].
func (blankPane) Paint(*paint.Painter, gunim.Frame, geom.Size, gunim.Children) {}

func (w *Window) term(id string) *term {
	if t, ok := w.terms[id]; ok {
		return t
	}
	t := newTerm(id, w.shells.Get(id), w.keys)
	t.ctrl = w.ctrlHeld
	t.away = w.away
	t.alongWith = func() []*term { return w.typingAlong(t) }
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
	// shared says, for a pane's row, whether an agent works in the pane
	// and another window watches it, for the row's stripe to glow in
	// their colours, as the pane's rings do.
	shared func() (agent, watched bool)
	hues   look.Marks
}

// sidebarRows lists the panes under their machines, this computer
// first, then each server in the order its first pane opened, and
// each server's tunnels after its panes. A tunnel's pane is lit on the
// tunnel's row.
func sidebarRows(panes []app.Pane, tunnels []app.Tunnel, share app.Share, windows []app.RemoteWindow, saved []machines.ID, named func(machines.ID) (string, bool), dropped ...machines.ID) []sideItem {
	if named == nil {
		named = func(m machines.ID) (string, bool) { return string(m), false }
	}
	notes := map[string]string{}
	for _, p := range share.Panes {
		notes[p.Pane] = p.Note
	}
	order := []machines.ID{machines.Local}
	seen := map[machines.ID]bool{machines.Local: true}
	add := func(m machines.ID) {
		if !seen[m] {
			seen[m] = true
			order = append(order, m)
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
	// The saved servers, each under its heading with its plus, which
	// connects to it.
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
	paneRow := func(p app.Pane) sideItem {
		// An agent's note and the pane's own, such as who else is
		// watching it, both.
		note := notes[p.ID]
		switch {
		case p.Ended:
			note = "ended"
		case p.Rang:
			note = "bell"
		case note == "":
			note = p.Note
		case p.Note != "":
			note += ", " + p.Note
		}
		return sideItem{key: p.ID, text: p.Title, note: note, pane: p.ID, click: app.FocusPane{Pane: p.ID}, closes: app.ClosePane{Pane: p.ID}, dim: p.Ended}
	}
	var out []sideItem
	for _, m := range order {
		name, quick := named(m)
		if m == "" {
			name = "This computer"
		}
		if quick {
			// Not saved: forgotten once nothing is open on it. Said in
			// the heading, whose right is its plus.
			name += " (quick)"
		}
		out = append(out, sideItem{key: "machine:" + string(m), text: name, heading: true})
		for _, p := range panes {
			if p.Machine == m && p.On == "" && !shown[p.Tunnel] {
				out = append(out, paneRow(p))
			}
		}
		// Its tunnels, before the machines a window reaches, which have
		// headings of their own.
		for _, t := range tunnels {
			if t.Machine == m {
				out = append(out, sideItem{key: "tunnel:" + t.ID, text: t.Label, note: t.Note, pane: t.Pane, click: app.ShowTunnel{ID: t.ID}, closes: app.CloseTunnel{ID: t.ID}, dim: !t.Live})
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
				if o.Key() == "" {
					out = append(out, sideItem{key: "window:" + string(m) + ":" + o.ID, text: o.Label, note: "there", click: app.AttachWindow{Window: m, ID: o.ID}, dim: true})
				} else if !slices.Contains(far, o.Key()) {
					far = append(far, o.Key())
				}
			}
		}
		for _, p := range panes {
			if p.Machine == m && p.On != "" && !slices.Contains(far, p.On) {
				far = append(far, p.On)
			}
		}
		// And the machines it is connected to with nothing open there.
		for _, w := range windows {
			if w.Name != m {
				continue
			}
			for _, key := range w.Machines {
				if !slices.Contains(far, key) {
					far = append(far, key)
				}
			}
		}
		// Each machine the window reached, under a heading of its own a
		// step in: this window's panes on it, and what the window has
		// open there.
		for _, host := range far {
			hostName, _ := named(machines.FarID(m, host))
			out = append(out, sideItem{key: "machine:" + string(machines.FarID(m, host)), text: hostName, heading: true, depth: 1})
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
					if o.Key() == host {
						out = append(out, sideItem{key: "window:" + string(m) + ":" + o.ID, text: o.Label, note: "there", click: app.AttachWindow{Window: m, ID: o.ID}, dim: true})
					}
				}
			}
		}
	}
	return out
}

// quietNotes takes the notes that have stood long enough off their
// rows, and wakes again for the next to.
func (w *Window) quietNotes(u *gunim.UI) {
	if w.stopQuiet != nil {
		w.stopQuiet()
		w.stopQuiet = nil
	}
	now := time.Now()
	var next time.Duration
	for _, key := range w.cards.keys() {
		row, ok := w.cards.row(key)
		if !ok || row.said == "" || row.pointed || row.typed {
			continue
		}
		row.showNote(now)
		if left := noteFor - now.Sub(row.saidAt); left > 0 && (next == 0 || left < next) {
			next = left
		}
	}
	u.Invalidate()
	if next > 0 {
		w.stopQuiet = u.After(next, w.quietNotes)
	}
}

// sideRow is a row in the sidebar. A pane's row shows its title, lit
// while the pane has the keyboard, and a click brings the pane
// forward. A machine's heading is small and dim.
type sideRow struct {
	anim.Group
	w *Window
	// machine is the machine a heading heads, which its plus opens a
	// menu for; menu is that menu while it is open, which holds the
	// keyboard, and back what had the keyboard before.
	machine machines.ID
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
	// said is the row's note, said since saidAt; once it has stood for
	// noteFor it comes off the row, giving the name its width back,
	// and the pointer or the keyboard on the row brings it out again.
	said           string
	saidAt         time.Time
	pointed, typed bool
	// dim says the row's thing has ended, and its words are faint.
	dim bool
	// card is what a machine's heading shows as a card's header, nil for
	// a plain row; chipHot is its button under the pointer, -1 for none.
	card    *cardInfo
	chipHot int
}

// noteFor is how long a sidebar row's note stands before it goes quiet.
const noteFor = 4 * time.Second

// quiet reports whether the row's note has stood long enough, at now,
// to come off the row.
func (r *sideRow) quiet(now time.Time) bool {
	return r.said != "" && now.Sub(r.saidAt) >= noteFor && !r.pointed && !r.typed
}

// showNote shows the note, or nothing once it has gone quiet.
func (r *sideRow) showNote(now time.Time) {
	text := r.said
	if r.quiet(now) {
		text = ""
	}
	if r.note.Text != text {
		r.note.SetText(text)
	}
}

func (w *Window) newSideRow(it sideItem) *sideRow {
	r := &sideRow{w: w, heading: it.heading, title: widget.NewLabel(""), note: widget.NewLabel(""), active: anim.NewFloat(0), hover: anim.NewFloat(0), chipHot: -1}
	r.title.MaxLines, r.note.MaxLines = 1, 1
	r.note.Size, r.note.Color = smallText, look.Faint
	if it.heading {
		r.title.Size, r.title.Color = smallText, look.Faint
	}
	r.set(it)
	r.ring = anim.NewFloat(0)
	r.Add(r.active, r.hover, r.ring)
	return r
}

// set shows it on the row.
func (r *sideRow) set(it sideItem) {
	r.title.SetText(it.text)
	if it.note != r.said {
		r.said, r.saidAt = it.note, time.Now()
	}
	r.showNote(time.Now())
	r.click, r.local, r.closes, r.key = it.click, it.local, it.closes, it.key
	r.marks.set(it)
	if m, ok := strings.CutPrefix(it.key, "machine:"); ok && it.heading {
		r.machine = machines.ID(m)
	}
	if !it.heading {
		r.dim = it.dim
		r.inkTitle()
	}
}

// inkTitle colours the row's words: faint when dim, the theme's colour
// for whatever is in front while it is, and the usual ink otherwise.
func (r *sideRow) inkTitle() {
	switch {
	case r.heading:
	case r.dim:
		r.title.Color = look.Faint
	case r.on:
		r.title.Color = look.RowActiveInk
	default:
		r.title.Color = widget.Ink
	}
}

func (r *sideRow) setActive(on bool, u *gunim.UI) {
	if on == r.on {
		return
	}
	r.on = on
	r.inkTitle()
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
func (r *sideRow) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	if r.card != nil {
		return r.layoutCard(c, f, kids)
	}
	// As tall, and with as much room at the ends, as the theme says.
	const gap = 8
	padX, height := look.SidebarPad.Get(f.Theme), look.SidebarRow.Get(f.Theme)
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
	if r.card != nil {
		r.paintCard(p, f, box, kids)
		return
	}
	inset := geom.Rect{Min: geom.Pt(6, 1), Max: geom.Pt(box.W-6, box.H-1)}
	radius := look.RowRadius.Get(f.Theme)
	if t := r.hover.Value(); t > 0.01 {
		c := look.RowHover.Get(f.Theme)
		c.A = uint8(float32(c.A) * min(t, 1))
		p.RRect(inset, radius, paint.Solid(c))
	}
	if t := r.active.Value(); t > 0.01 {
		c := look.RowActive.Get(f.Theme)
		c.A = uint8(float32(c.A) * min(t, 1))
		p.RRect(inset, radius, paint.Solid(c))
	}
	if t := r.ring.Value(); t > 0.01 {
		c := widget.Accent.Get(f.Theme)
		c.A = uint8(float32(c.A) * min(t, 1))
		p.RRectStroke(inset, radius, paint.Fill{}, paint.Stroke{Width: 1.5, Color: c})
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
func (r *sideRow) Focusable() bool { return r.takesKeys() || r.popup != nil }

// takesKeys reports whether the keyboard goes to the row as it moves
// along the list: every row but a plain heading, a card's header too.
func (r *sideRow) takesKeys() bool { return !r.heading || r.card != nil }

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
	if r.card != nil {
		return r.handleCard(e, u)
	}
	if r.heading {
		return r.handleHeading(e, u)
	}
	switch e := e.(type) {
	case input.PointerEnter:
		r.hover.Animate(1, widget.Quick.Get(u.Theme()))
		r.pointed = true
		r.showNote(time.Now())
	case input.PointerLeave:
		r.hover.Animate(0, widget.Settle.Get(u.Theme()))
		r.pointed = false
		r.showNote(time.Now())
		r.w.quietNotes(u)
	case input.PointerDown:
		if e.Button == input.ButtonPrimary {
			if r.closes != nil && r.marks.onCross(e.Pos) {
				// The cross at the end: close the row, rather than go to it.
				u.Send(r, r.closes)
				return true
			}
			r.activate(u)
			return true
		}
		return false
	case input.FocusGained:
		r.ring.Animate(1, widget.Quick.Get(u.Theme()))
		r.typed = true
		r.showNote(time.Now())
	case input.FocusLost:
		r.ring.Animate(0, widget.Settle.Get(u.Theme()))
		r.typed = false
		r.showNote(time.Now())
		r.w.quietNotes(u)
	case input.KeyPress:
		switch {
		case e.Key == input.KeyUp, e.Key == input.KeyDown:
			step := 1
			if e.Key == input.KeyUp {
				step = -1
			}
			r.w.focusRow(r.key, step, u)
		case e.Key == input.KeyLeft, e.Key == input.KeyRight:
			r.w.cards.stepCard(r.key, e.Key == input.KeyRight, u)
		case e.Key == input.KeyPageUp, e.Key == input.KeyPageDown:
			// A page of rows.
			step := sidebarPage
			if e.Key == input.KeyPageUp {
				step = -step
			}
			r.w.focusRowsAway(r.key, step, u)
		case e.Key == input.KeyHome:
			r.w.focusRow("", 1, u)
		case e.Key == input.KeyEnd:
			r.w.focusRowsAway(r.key, len(r.w.cards.keys()), u)
		case e.Key == input.KeyEnter || e.Key == input.KeyKPEnter, e.Key == input.KeySpace && e.Mods == 0:
			r.activate(u)
		case e.Key == input.KeyDelete && r.closes != nil:
			u.Send(r, r.closes)
		case e.Key == input.KeyEscape:
			// Back to the pane last worked in, wherever it is.
			if id := r.w.lastWorked; id != "" && id != r.w.focused {
				u.Send(r, app.FocusPane{Pane: id})
			} else if n := r.w.focusNode(r.w.focused, u); n != nil {
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
// highlighted, over the bottom of the stage when the line has no room of
// its own: the terminal keeps its size while the pointer runs down a
// menu.
type statusLine struct {
	anim.Group
	label  *widget.Label
	height *anim.Float
	text   string
	hint   *widget.Label
	hinted string
	// hidden folds the line away whatever it says, while the window is
	// presenting.
	hidden bool
}

func newStatusLine() *statusLine {
	l := widget.NewLabel("")
	l.Size, l.Color, l.MaxLines = smallText, look.Faint, 1
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
	if text != "" {
		s.label.SetText(text)
	}
	s.fold(u)
}

// hide folds the line away while on is set, whatever it says.
func (s *statusLine) hide(on bool, u *gunim.UI) {
	s.hidden = on
	s.fold(u)
}

// fold grows the line to its height while it has something to say and
// shows, and folds it away otherwise.
func (s *statusLine) fold(u *gunim.UI) {
	to := float32(0)
	if s.text != "" && !s.hidden {
		to = statusHeight
	}
	if s.height.Target() != to {
		s.height.Animate(to, widget.Settle.Get(u.Theme()))
	}
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
func (w *Window) fullTitle(id string, menu, item int) string {
	if menu < 0 || item < 0 || menu >= len(w.bar.Menus) || item >= len(w.bar.Menus[menu].Items) {
		return ""
	}
	if at := slices.Index(w.paletteIDs, id); id != "" && at >= 0 {
		return w.palette.Items[at].Title
	}
	return shownText(w.bar.Menus[menu].Items[item])
}

// focusRow gives the keyboard to the row step rows from the one keyed
// from, passing over headings, or to the first row when from is "".
func (w *Window) focusRow(from string, step int, u *gunim.UI) {
	keys := w.cards.keys()
	at := slices.Index(keys, widget.Key(from))
	if at < 0 {
		at, step = -1, 1
	}
	for i := at + step; i >= 0 && i < len(keys); i += step {
		if row, ok := w.cards.row(keys[i]); ok && row.takesKeys() {
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
func (w *Window) focusRowsAway(from string, n int, u *gunim.UI) {
	keys := w.cards.keys()
	at := slices.Index(keys, widget.Key(from))
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	var last *sideRow
	for i := at + step; i >= 0 && i < len(keys) && n > 0; i += step {
		if row, ok := w.cards.row(keys[i]); ok && row.takesKeys() {
			last = row
			n--
		}
	}
	if last != nil {
		u.Focus(last)
	}
}

// focusSidebar gives the keyboard to the Servers pane's list: to the
// row of the pane last worked in, or its first row, opening the pane
// first, or going to it where it is.
func (w *Window) focusSidebar(u *gunim.UI) {
	if id := w.paneOfKind(app.KindServers); id != "" && id == w.focused {
		if n := w.serversRow(u); n != nil {
			u.Focus(n)
		}
		return
	}
	// Opened, or brought here from where it is; the keyboard goes to
	// its list as it comes on stage.
	u.Send(w, app.ShowServers{})
}

// machines are the machines panes can open on: this computer, the
// servers connected to, and the windows connected to.
func (w *Window) machines() []machines.ID {
	out := []machines.ID{machines.Local}
	out = append(out, w.connected...)
	for _, rw := range w.remoteWindows {
		out = append(out, rw.Name)
	}
	return out
}

// showMachineMenu opens a heading's menu of items, each with its icon and doing its act;
// an item with no act is a caption over the group under it.
func (w *Window) showMachineMenu(r *sideRow, items []string, icons []*icon.Icon, acts []func(*gunim.UI), u *gunim.UI) {
	r.closeMenu(u)
	menu := widget.NewMenu(items...)
	menu.Icons = icons
	for i, act := range acts {
		if act == nil {
			menu.Captions = append(menu.Captions, i)
			if i > 0 {
				menu.Breaks = append(menu.Breaks, i)
			}
		}
	}
	menu.Pick = func(i int, u *gunim.UI) {
		r.closeMenu(u)
		if i >= 0 && i < len(acts) && acts[i] != nil {
			acts[i](u)
		}
	}
	box, _ := u.Bounds(r)
	r.menu = menu
	r.back = u.Focused()
	r.popup = u.OpenPopup(r, menu, gunim.PopupOptions{
		Anchor:  r.menuAnchor(box.Size(), u.Theme()),
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

// askIcons are the icons a question can show before its title, by
// their Lucide names.
var askIcons = map[string]*icon.Icon{
	"shield-alert": icon.ShieldAlert,
	"key-round":    icon.KeyRound,
	"log-in":       icon.LogIn,
	"unplug":       icon.Unplug,
	"lock":         icon.Lock,
}

// toastKinds are the toasts for the kinds of notice.
var toastKinds = map[app.NoticeKind]widget.ToastKind{
	app.NoticePlain:  widget.ToastPlain,
	app.NoticeWorked: widget.ToastSuccess,
	app.NoticeFailed: widget.ToastError,
}

// paintPlus draws a heading's plus, faint, and brighter under the
// pointer.
func (r *sideRow) paintPlus(p *paint.Painter, f gunim.Frame, box geom.Size) {
	c := look.Faint.Get(f.Theme)
	if t := r.hover.Value(); t > 0.01 {
		c = anim.Mix(anim.ColorCodec, c, widget.Accent.Get(f.Theme), min(t, 1))
	}
	// Lucide's plus spans 14 of its 24 units: 17 across draws it 10 wide.
	cx, cy := box.W-6-plusWidth/2, box.H/2
	drawIcon(p, icon.Plus, geom.Rc(cx-8.5, cy-8.5, 17, 17), c, 1.5)
}

// openMachineMenu opens the menu of what can be opened on a heading's
// machine, under the heading.
func (w *Window) openMachineMenu(r *sideRow, u *gunim.UI) {
	m := r.machine
	var items []string
	var icons []*icon.Icon
	var acts []func(*gunim.UI)
	add := func(ic *icon.Icon, title string, act func(*gunim.UI)) {
		items, icons = append(items, title), append(icons, ic)
		acts = append(acts, act)
	}
	send := func(in gunim.Intent) func(*gunim.UI) { return func(u *gunim.UI) { u.Send(w, in) } }
	window := slices.ContainsFunc(w.remoteWindows, func(rw app.RemoteWindow) bool { return rw.Name == m })
	for _, h := range w.saved {
		if machines.ID(h.ID) == m && h.Window && !window {
			// Saved as a window and not connected: nothing that needs a
			// shell applies.
			saved := h
			add(icon.Plug, "Connect", send(app.ConnectTo{Server: m}))
			add(icon.Pencil, "Edit This Window…", func(u *gunim.UI) { w.serverForm(&saved, u) })
			add(icon.Trash2, "Remove This Window…", func(u *gunim.UI) { w.confirmRemove(m, u) })
			w.showMachineMenu(r, items, icons, acts, u)
			return
		}
	}
	// Grouped under headings: what opens here, then the files, then
	// what goes through the connection, then the connection and the
	// saved server.
	heading := func(title string) { add(nil, title, nil) }
	heading("Terminal")
	add(icon.SquareTerminal, "New Terminal", send(app.OpenOn{Machine: m}))
	add(icon.SquareChevronRight, "Command…", func(u *gunim.UI) { w.commandDialogOn(m, u) })
	if m == "" && len(w.shellChoices) > 1 {
		// This computer's shells, each to open a terminal with.
		heading("Shells")
		for _, sh := range w.shellChoices {
			add(icon.SquareTerminal, sh.Title, send(app.OpenShellNamed{ID: sh.ID}))
		}
	}
	heading("Files")
	add(icon.House, "Home", send(app.FilesOn{Machine: m}))
	for _, f := range w.foldersOn(m) {
		add(icon.Folder, f, send(app.FilesOn{Machine: m, Path: f}))
	}
	if m != "" {
		heading("Forward")
		add(icon.Cable, "Tunnel…", func(u *gunim.UI) { w.tunnelDialogOn(m, false, u) })
		add(icon.Network, "SOCKS Proxy…", func(u *gunim.UI) { w.tunnelDialogOn(m, true, u) })
	}
	if m != "" {
		heading("Connection")
		add(icon.ScrollText, "Connection Log", send(app.ShowLog{Machine: m}))
		if slices.Contains(w.dropped, m) {
			// Its connection went: the row stays until this clears it.
			add(icon.X, "Clear", send(app.ClearMachine{ID: m}))
		} else {
			if _, _, far := m.Far(); far {
				// Another window's connection: asked about first.
				add(icon.Unplug, "Disconnect…", func(u *gunim.UI) { w.confirmFarDisconnect(m, u) })
			} else {
				add(icon.Unplug, "Disconnect", send(app.Disconnect{Machine: m}))
			}
		}
		for _, h := range w.saved {
			if machines.ID(h.ID) == m {
				saved := h
				what := "Server"
				if h.Window {
					what = "Window"
				}
				heading(what)
				add(icon.Pencil, "Edit This "+what+"…", func(u *gunim.UI) { w.serverForm(&saved, u) })
				add(icon.Trash2, "Remove This "+what+"…", func(u *gunim.UI) { w.confirmRemove(m, u) })
			}
		}
	}
	if m == "" {
		heading("This Computer")
		add(icon.Pencil, "Edit This Computer…", func(u *gunim.UI) { w.thisComputerDialog(u) })
	}
	w.showMachineMenu(r, items, icons, acts, u)
}

// askSplit splits the focused pane at once, to the right or below with
// vertical, and puts a chooser in the new half, where a new terminal or
// a pane to move there is picked. With no pane to split, it opens a
// terminal.
func (w *Window) askSplit(vertical bool, u *gunim.UI) {
	if w.focused == "" {
		u.Send(w, app.SplitPane{Vertical: vertical})
		return
	}
	u.Send(w, app.ChooseSplit{Vertical: vertical})
}

// keepDrawings keeps what each pane draws, and lets go of the drawings
// of panes that have closed.
func (w *Window) keepDrawings(st app.State, u *gunim.UI) {
	if w.drawings == nil {
		w.drawings, w.drawn = map[string]*gunim.Drawing{}, map[string]gunim.Node{}
	}
	live := map[string]bool{}
	for _, p := range st.Panes {
		live[p.ID] = true
		n := w.madeNode(p.ID)
		if n == nil || w.drawn[p.ID] == n {
			continue
		}
		if old := w.drawn[p.ID]; old != nil {
			u.ForgetDrawing(old)
		}
		w.drawn[p.ID], w.drawings[p.ID] = n, u.KeepDrawing(n)
	}
	for id, n := range w.drawn {
		if !live[id] {
			u.ForgetDrawing(n)
			delete(w.drawn, id)
			delete(w.drawings, id)
		}
	}
}

// madeNode is the node that shows pane id, or nil when none has been
// made: bareNode without the making.
func (w *Window) madeNode(id string) gunim.Node {
	var n gunim.Node
	switch w.kindOf(id) {
	case app.KindServers:
		n = w.serversView
	case app.KindFiles:
		if b, ok := w.browsers[id]; ok {
			n = b
		}
	case app.KindCopies:
		if w.copies != nil {
			n = w.copies
		}
	case app.KindHelp:
		if w.help != nil {
			n = w.help
		}
	case app.KindSecrets:
		if w.secrets != nil {
			n = w.secrets
		}
	case app.KindJobs:
		if w.jobs != nil {
			n = w.jobs
		}
	case app.KindTunnel:
		if p, ok := w.tunnelPanes[id]; ok {
			n = p
		}
	case app.KindReader:
		if r, ok := w.readers[id]; ok {
			n = r
		}
	case app.KindChooser:
		if c, ok := w.choosers[id]; ok {
			n = c
		}
	default:
		if t, ok := w.terms[id]; ok {
			n = t
		}
	}
	return n
}

// failed says something the window itself tried failed, as the
// program's failures are said: in a toast, with a red echo, and in the
// Window Log, where it stays once the toast has gone.
func (w *Window) failed(title, why string, u *gunim.UI) {
	log.Printf("%s: %s", title, why)
	w.toasts.Show(widget.Toast{Title: title, Body: why, Kind: widget.ToastError}, u)
	w.echo.Ping(u, widget.EchoProblem)
}

// echoFor sends an echo out past the window's edges for each count in
// st.Pings that went up: a failure, work finished, or a bell out of
// sight. While another program has the keyboard, everything is out of
// sight: a bell in any pane, and a long command finishing in the pane in
// front. A
// faint one goes out again and again while a connection is being made.
func (w *Window) echoFor(st app.State, u *gunim.UI) {
	// Each window counts its own: a pane's from the window it is in.
	was := w.pings
	w.pings = st.Pings
	if st.Pings.Problems > was.Problems || st.Pings.FrontProblems > was.FrontProblems && w.away {
		w.echo.Ping(u, widget.EchoProblem)
	}
	if st.Pings.Dones > was.Dones || st.Pings.FrontDones > was.FrontDones && w.away {
		w.echo.Ping(u, widget.EchoDone)
	}
	if st.Pings.Calls > was.Calls || st.Bells > w.bells && w.away {
		w.echo.Ping(u, widget.EchoCall)
	}
	w.echo.Wait(u, widget.EchoWait, len(st.Dialing) > 0)
}

// present fills the screen with the stage, the pane or split in front,
// with on set, as F11 asks: the window goes full screen, the menu bar
// slides up, the sidebar slides away to the left and the status line
// folds. With on unset all of it comes back.
func (w *Window) present(on bool, u *gunim.UI) {
	if on == w.presenting {
		return
	}
	w.presenting = on
	u.SetFullScreen(on)
	spring := widget.Settle.Get(u.Theme())
	w.barShade.show(!on, spring)
	w.status.hide(on, u)
	if on {
		w.toasts.Show(widget.Toast{Title: "Full screen", Body: "F11 brings the menus back."}, u)
	}
	u.Invalidate()
}

// shade holds a strip across the window, such as the menu bar, that
// slides up out of sight and back down. Its child keeps its own height
// all the while, and is clipped to what shows.
type shade struct {
	anim.Group
	child gunim.Node
	// open is how much shows, from 0 to 1.
	open *anim.Float
}

func newShade(child gunim.Node) *shade {
	s := &shade{child: child, open: anim.NewFloat(1)}
	s.Add(s.open)
	return s
}

// show slides the strip down into sight, or with on unset up out of it.
func (s *shade) show(on bool, spring anim.Spring) {
	to := float32(0)
	if on {
		to = 1
	}
	s.open.Animate(to, spring)
}

// Children implements [gunim.Composite].
func (s *shade) Children() []gunim.Node { return []gunim.Node{s.child} }

// Layout implements [gunim.Node].
func (s *shade) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	size := k.Layout(gunim.Constraints{Min: geom.Sz(c.Max.W, 0), Max: c.Max})
	open := min(max(s.open.Value(), 0), 1)
	h := size.H * open
	k.Place(geom.Pt(0, h-size.H))
	return geom.Sz(size.W, h)
}

// Paint implements [gunim.Node].
func (s *shade) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, kids gunim.Children) {
	if box.H < 0.5 {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true})()
	kids.At(0).Paint(p)
}

// commaFolderKept reports whether the folders line still holds a saved
// folder with a comma in its path, which reading the line back would
// split in two. One taken out of the line is no longer in the way.
func commaFolderKept(saved []string, line string) bool {
	for _, f := range saved {
		if !remote.FoldersRoundTrip([]string{f}) && strings.Contains(line, f) {
			return true
		}
	}
	return false
}

// setTypeAll turns Type in All Panes on or off: what is typed goes to
// every terminal in the split on stage, each showing its cursor.
func (w *Window) setTypeAll(on bool, u *gunim.UI) {
	w.typeAll = on
	w.showTypeAll(u)
	w.tickSwitch("pane.typeAll", on)
	w.showChips(w.lastChips)
	u.Invalidate()
}

// showTypeAll has the terminals on stage type along while Type in All
// Panes is on, and the rest not.
func (w *Window) showTypeAll(u *gunim.UI) {
	on := map[string]bool{}
	if w.typeAll {
		for _, id := range boxLeaves(w.stageBox, nil) {
			on[id] = true
		}
	}
	for id, t := range w.terms {
		t.setAlong(on[id], u)
	}
}

// typingAlong is the other terminals what is typed in t goes to: those
// in the split on stage with it, while Type in All Panes is on.
func (w *Window) typingAlong(t *term) []*term {
	if !w.typeAll {
		return nil
	}
	leaves := boxLeaves(w.stageBox, nil)
	if !slices.Contains(leaves, t.id) {
		return nil
	}
	var out []*term
	for _, id := range leaves {
		if o, ok := w.terms[id]; ok && o != t && !o.sh.T.Exited() {
			out = append(out, o)
		}
	}
	return out
}

// boxLeaves adds the panes an arrangement shows to out.
func boxLeaves(b *app.Box, out []string) []string {
	switch {
	case b == nil:
		return out
	case b.Pane != "":
		return append(out, b.Pane)
	}
	return boxLeaves(b.B, boxLeaves(b.A, out))
}
