package main

import (
	"fmt"
	"image/color"
	"slices"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vt"
)

// newMenubar puts a row of menu titles above the widget tree.
//
// The menus name commands rather than carrying actions of their own, so
// a line here, its key binding and its entry in the palette are three
// ways into one registry and cannot drift apart.
func (a *app) newMenubar(child ui.Widget) *ui.Menubar {
	bar := ui.NewMenubar(a.root.Commands, a.root.Accelerators, child)
	bar.Menus = []ui.MenuDef{
		// No shells yet: the looking for them is still running, and
		// refreshFileMenu puts them on when it lands.
		{Title: fileMenu, Items: fileItems(nil)},
		{Title: "Edit", Items: []ui.MenuItem{
			{Command: "edit.copy"},
			{Command: "edit.paste"},
			{Command: "edit.pasteImage", Title: "Paste Image as File…"},
			ui.MenuSeparator(),
			// Named for what a hand reaches for, and for where it
			// lands: the text goes to the file viewer in a pane of its
			// own, which "Find" alone would not lead anyone to expect.
			{Command: scrollbackCommand, Title: "Find in Scrollback…"},
		}},
		{Title: "View", Items: []ui.MenuItem{
			{Command: "sidebar.toggle", Title: "Sidebar"},
			{Command: "pane.titles", Title: "Pane Titles"},
			{Command: fullScreenCommand, Title: "Full Screen"},
			ui.MenuHeader("Font"),
			{Command: "font.increase", Title: "Larger"},
			{Command: "font.decrease", Title: "Smaller"},
			{Command: "font.reset", Title: "Reset"},
			ui.MenuHeader("Scrollback"),
			{Command: "view.scrollUp", Title: "Page Up"},
			{Command: "view.scrollDown", Title: "Page Down"},
		}},
		{Title: paneMenu, Items: []ui.MenuItem{
			ui.MenuHeader("Split"),
			{Command: "pane.splitRight", Title: "Right"},
			{Command: "pane.splitDown", Title: "Down"},
			// Pop Out rather than Unsplit: the pane leaves the split
			// and lands on the stage, which is somewhere rather than
			// nowhere.
			{Command: "pane.popOut", Title: "Pop Out"},
			ui.MenuHeader("Go To"),
			// The sidebar pair gets the plain names, because that
			// order is the one on screen to be seen.
			{Command: "pane.nextInSidebar", Title: "Next"},
			{Command: "pane.previousInSidebar", Title: "Previous"},
			{Command: "pane.next", Title: "Last Used"},
			{Command: "pane.previous", Title: "Last Used, Reversed"},
			{Command: switcherCommand, Title: "All Panes…"},
			{Command: "sidebar.focus", Title: "Sidebar"},
		}},
		{Title: machineMenu, Items: []ui.MenuItem{
			// The same list in the same order as the plus on a
			// machine's row in the sidebar: learn one, know the other.
			ui.MenuHeader("Open Here"),
			{Command: "conn.terminal", Title: "Terminal"},
			{Command: "conn.command", Title: "Command…"},
			{Command: "conn.files", Title: "Files"},
			{Command: "conn.tunnel", Title: "Tunnel…"},
			{Command: "conn.socks", Title: "SOCKS Proxy…"},
			ui.MenuHeader("Files"),
			{Command: "files.goTo", Title: "Go to Directory…"},
			{Command: copiesCommand, Title: "Remembered Copies…"},
			ui.MenuHeader("This Machine"),
			{Command: "conn.log", Title: "Connection Log"},
			{Command: "shell.setup", Title: "Shell Setup"},
		}},
		// Filled in by refreshServerMenu. Written down here so the bar
		// keeps its order: a menu added at run time would land
		// wherever addMenu put it.
		{Title: serversMenu, Items: serverItems(nil)},
		{Title: shareMenu, Items: shareItems("", false)},
		{Title: optionsMenu, Items: []ui.MenuItem{
			{Command: "view.theme", Title: "Theme…"},
			// gridterm is configured by three files and all three
			// follow one routine: write a starter, edit it, reload it.
			// Grouped by the step rather than by the file, so the
			// routine is what the menu shows.
			ui.MenuHeader("Starter Files"),
			{Command: "view.themesStart", Title: "New Theme File…"},
			{Command: keysCommand, Title: "New Shortcuts File"},
			ui.MenuHeader("Reload"),
			{Command: "view.themesReload", Title: "Themes"},
			{Command: keysReloadCommand, Title: "Shortcuts"},
			{Command: "server.reload", Title: "Server List"},
			ui.MenuSeparator(),
			{Command: filesCommand, Title: "File Locations"},
		}},
		{Title: helpMenu, Items: []ui.MenuItem{
			{Command: "palette.open", Title: "All Commands…"},
			{Command: helpCommand, Title: "Shortcuts and Commands"},
			ui.MenuSeparator(),
			{Command: logCommand, Title: "Window Log"},
			ui.MenuSeparator(),
			{Command: "app.about", Title: "About gridterm"},
		}},
	}
	bar.Style = a.menubarStyle()
	bar.MenuStyle = a.menuStyle()
	// The bar reaches the modal stack and the compositor only through
	// these: everything about which menu is showing stays in the widget.
	bar.Present = func(m *ui.Menu) func() {
		return a.showModal(m, bar.Close)
	}
	bar.Origin = func() ui.Rect {
		area, _ := a.root.AreaOf(bar)
		return area
	}
	return bar
}

// fileMenu is the menu bar title the panes hang under.
const fileMenu = "File"

// fileItems is what the File menu offers: the panes, and a line per
// shell a new pane here can open on.
func fileItems(shells []ui.MenuItem) []ui.MenuItem {
	items := []ui.MenuItem{{Command: "pane.open", Title: "New Terminal"}}
	if len(shells) > 0 {
		items = append(items, ui.MenuHeader("New Terminal In"))
		items = append(items, shells...)
	}
	// A ladder, smallest first. These four are easy to confuse by
	// title alone; under one header what each takes is the difference
	// between one line and the next.
	return append(items,
		ui.MenuHeader("Close"),
		ui.MenuItem{Command: "pane.close", Title: "Pane"},
		ui.MenuItem{Command: "sidebar.closeRow", Title: "Selected Row"},
		ui.MenuItem{Command: "conn.disconnect", Title: "Machine"},
		ui.MenuItem{Command: "conn.clearFinished", Title: "All Finished"},
		ui.MenuSeparator(),
		ui.MenuItem{Command: "app.exit", Title: "Exit"})
}

// shareItems are the Share menu's lines. The one that shows the share
// is only there while there is one: a line that opens nothing is a
// line the user reads and tries.
func shareItems(hand string, sharing bool) []ui.MenuItem {
	items := []ui.MenuItem{
		ui.MenuHeader("Windows"),
		{Command: "serve.window", Title: "Serve This One…"},
		{Command: "serve.attach", Title: "Attach to Another…"},
		ui.MenuHeader("Agent"),
		{Command: "agent.hand", Title: hand},
		{Command: "agent.take", Title: "Remove Pane"},
	}
	if sharing {
		items = append(items, ui.MenuItem{Command: "agent.share", Title: "Show Share…"})
	}
	return append(items, ui.MenuItem{Command: typedCommand, Title: "Typing History…"})
}

// refreshShareMenu rebuilds the Share menu, whose agent lines change
// with whether a share is open.
func (a *app) refreshShareMenu() {
	if a.bar == nil {
		return
	}
	want := shareItems(a.shareItem(), a.agents.sharing())
	for i, have := range a.bar.Menus {
		if have.Title != shareMenu {
			continue
		}
		if slices.Equal(have.Items, want) {
			// Nothing has changed, and rebuilding takes an open menu
			// down with it: a menu that vanished while the user was
			// reading it is the window getting in their way.
			return
		}
		a.bar.Close()
		a.bar.Menus[i].Items = want
		return
	}
}

// fileMenuShells are the shell lines the File menu offers. A window
// whose panes open on the machine -ssh named gets none: "New terminal"
// goes there and a shell line comes back here, and no row behind the
// line says which machine it means.
func (a *app) fileMenuShells() []ui.MenuItem {
	if a.homeMachine() != nil {
		return nil
	}
	return a.shellPick.lines(false)
}

// refreshFileMenu rebuilds the File menu for the shells a scan has since
// found. An open menu holds its own copy of the items, so this cannot
// disturb one the user is reading.
func (a *app) refreshFileMenu(shells []ui.MenuItem) {
	if a.bar == nil {
		return
	}
	for i, def := range a.bar.Menus {
		if def.Title != fileMenu {
			continue
		}
		a.bar.Menus[i].Items = fileItems(shells)
		return
	}
}

// updateStatus puts the chips on the right of the menu bar, and leaves
// it blank when the window is neither served nor shared.
//
// Called every frame, so the chips are only built when what they are
// made of has changed.
func (a *app) updateStatus() {
	if a.bar == nil {
		return
	}
	key := statusKey{sharing: a.agents.sharing()}
	if a.serving.on() {
		key.addr = a.serving.addr()
		// The clients the chip is read off, not the panel rows: the
		// server takes a client on its own goroutine and the row follows
		// a frame later.
		key.clients = len(a.serving.clients())
		key.changes = a.serving.changes()
	}
	if key == a.statusWas {
		return
	}
	a.statusWas = key
	a.bar.Chips = a.statusChips()
}

// statusKey is what the menu bar chips are made of: two frames with the
// same key say the same thing.
//
// It counts the comings and goings rather than holding the client it last
// named: a key that held one would keep a connection that has gone alive,
// and one window replaced by another of the same name still moves the
// count.
type statusKey struct {
	addr    string
	clients int
	changes uint64
	sharing bool
}

// statusChips is what the menu bar says this window is doing: whether an
// agent has been let in, and whether somebody is working in it from
// another machine. There are none when neither is true.
//
// Each is a word or two and a press. Who is connected, from where, and
// which panes an agent has are the dialogs' to say.
//
// Serving goes last, against the right edge, because it is the one the
// bar drops last when the window is too narrow for both.
func (a *app) statusChips() []ui.Chip {
	var chips []ui.Chip
	// A picture on its way goes first. It is the only one of these that
	// is about something happening now rather than about how the window
	// stands, and it is gone in a moment.
	if text := a.sendingText(); text != "" {
		chips = append(chips, ui.Chip{
			Text: text, FG: statusIdleFG(a.colours), BG: a.chipBG(),
		})
	}
	if a.agents.sharing() {
		chips = append(chips, ui.Chip{
			Text: shareTitle, FG: statusAgentFG(a.colours), BG: a.chipBG(),
			Do: a.showShare,
		})
	}
	if a.serving.on() {
		text, fg := "Serving", statusIdleFG(a.colours)
		if len(a.serving.clients()) > 0 {
			text, fg = "Remote controlled", statusTakenFG(a.colours)
		}
		chips = append(chips, ui.Chip{
			Text: text, FG: fg, BG: a.chipBG(), Do: a.showServing,
		})
	}
	return chips
}

// sendingText is what the bar says while a picture is on its way, and
// nothing when none is.
//
// A picture is megabytes and the machine it is going to may be a long
// way off, so there is a moment with nothing else to show for the paste.
func (a *app) sendingText() string {
	switch {
	case a.sending <= 0:
		return ""
	case a.sending == 1 && a.sendingTo != "":
		return "Sending a picture to " + groupName(a.sendingTo)
	case a.sending == 1:
		return "Sending a picture"
	}
	return fmt.Sprintf("Sending %d pictures", a.sending)
}

// chipBG is the ground a chip on the menu bar sits on: black under a
// dark window, white under a light one, which is away from the bar in
// the direction the text on a chip leaves room for.
//
// It is picked against the window's ground rather than the bar's,
// because the colours written on a chip are lifted away from the
// window's ground too, and the two have to agree.
//
// A chip cannot meet grid.Contrast's 1.5:1 for a change of ground here.
// The bar is already as far from the window's background as the dimmer
// red on a chip can be read against, so the chip goes the other way, and
// this is as far as it goes.
func (a *app) chipBG() color.RGBA {
	if grid.Contrast(chipWhite, a.colours.BG) >= grid.Contrast(chipBlack, a.colours.BG) {
		return chipBlack
	}
	return chipWhite
}

// The two grounds a chip picks between.
var (
	chipBlack = color.RGBA{A: 0xff}
	chipWhite = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// statusTakenFG is the red the bar says "somebody is working in this
// window" in, and statusIdleFG the quieter one for a window that is only
// listening.
//
// Neither is dimmed towards the window's background, because they are
// read against the bar and a chip rather than against it. The bright red
// is lifted a fifth of the way to white, which puts the two far enough
// apart to read as a change of colour rather than as the same red
// twice.
func statusTakenFG(p vt.Palette) color.RGBA {
	return grid.Blend(p.ANSI[9], farFrom(p), 2, 5)
}

// farFrom is whichever of black and white the window's ground is not, so
// a colour lifted towards it is lifted away from what it is read on.
func farFrom(p vt.Palette) color.RGBA {
	if grid.Contrast(chipWhite, p.BG) >= grid.Contrast(chipBlack, p.BG) {
		return chipWhite
	}
	return chipBlack
}
func statusIdleFG(p vt.Palette) color.RGBA { return p.ANSI[1] }

// statusAgentFG is the colour the bar says an agent has been let into
// this window in: a cyan, away from the reds that say somebody is
// working here from another machine.
func statusAgentFG(p vt.Palette) color.RGBA { return p.ANSI[6] }

// helpMenu is the menu bar title the key list hangs under. It stays
// last, so the menus the window adds while it runs go in front of it.
const helpMenu = "Help"

// addMenu puts a menu on the bar in front of Help, taking down an open
// menu whose index the insert moves.
func (a *app) addMenu(def ui.MenuDef) {
	at := -1
	for i, have := range a.bar.Menus {
		if have.Title == helpMenu {
			at = i
			break
		}
	}
	if at < 0 {
		// Every menu goes in front of Help, so there has to be one to go
		// in front of.
		panic("the menu bar has no " + helpMenu + " title to add a menu in front of")
	}
	if a.bar.OpenIndex() >= at {
		a.bar.Close()
	}
	a.bar.Menus = slices.Insert(a.bar.Menus, at, def)
}

// menuStyle colours a drop-down menu, wherever it was opened from.
func (a *app) menuStyle() ui.MenuStyle {
	return ui.MenuStyle{
		FG: a.frameFG(),
		// No background of its own unless the theme asked for a flat
		// one: the frosted panel behind the menu is the background, and
		// an opaque fill would hide it.
		BG:         a.panelBG(),
		SelectedFG: a.activeFG(),
		SelectedBG: a.activeBG(),
		ChordFG:    a.panelDimFG(),
		DisabledFG: a.disabledFG(),
		// A rule around it, and a shadow under it. A menu over a
		// terminal is otherwise two lots of text with nothing between
		// them.
		BorderFG: a.panelBorderFG(),
		ShadowBG: a.panelShadow(),
		Rule:     a.panelRule(),
	}
}

// openMenu drops down the first menu, or closes whichever is showing.
func (a *app) openMenu() error {
	if a.bar == nil {
		return nil
	}
	if a.bar.OpenIndex() >= 0 {
		a.bar.Close()
		return nil
	}
	a.bar.Open(0)
	return nil
}

// menubarStyle is the menu bar's colours, built afresh whenever the
// window changes theme.
func (a *app) menubarStyle() ui.MenubarStyle {
	return ui.MenubarStyle{
		FG: a.frameFG(),
		// The sidebar's own ground, running across the bar rather than
		// down it. The two are one frame around the window, so they are
		// drawn in one colour unless the theme asked for two.
		BG:     a.barTop(),
		BGEnd:  a.barFoot(),
		OpenFG: a.activeFG(),
		OpenBG: a.activeBG(),
	}
}

// paneMenu, machineMenu, shareMenu and optionsMenu are the rest of the
// bar's titles.
//
// The order of the bar runs from the smallest thing a command acts on
// to the largest: the text, what is drawn, the pane, the machine the
// pane is on, the address book, other windows and agents, and the
// program itself.
const (
	paneMenu    = "Pane"
	machineMenu = "Machine"
	shareMenu   = "Share"
	optionsMenu = "Options"
)
