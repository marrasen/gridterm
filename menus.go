package main

import (
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
		}},
		{Title: "View", Items: []ui.MenuItem{
			{Command: "panel.toggle"},
			{Command: "panel.focus"},
			ui.MenuSeparator(),
			{Command: "font.increase"},
			{Command: "font.decrease"},
			{Command: "font.reset"},
			ui.MenuSeparator(),
			{Command: "view.theme"},
			{Command: "view.themesReload"},
			{Command: "view.themesStart"},
			{Command: "pane.titles"},
			ui.MenuSeparator(),
			{Command: "view.scrollUp"},
			{Command: "view.scrollDown"},
		}},
		{Title: "Connection", Items: []ui.MenuItem{
			{Command: "conn.terminal"},
			{Command: "conn.command"},
			{Command: "conn.tunnel"},
			{Command: "conn.socks"},
			{Command: "conn.files"},
			ui.MenuSeparator(),
			{Command: "conn.close"},
			{Command: "conn.clearFinished"},
		}},
		{Title: "Go", Items: []ui.MenuItem{
			{Command: "pane.next"},
			{Command: "pane.previous"},
			ui.MenuSeparator(),
			{Command: "pane.nextInSidebar"},
			{Command: "pane.previousInSidebar"},
			ui.MenuSeparator(),
			{Command: "palette.open"},
		}},
		{Title: helpMenu, Items: []ui.MenuItem{
			{Command: helpCommand},
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
	items := []ui.MenuItem{{Command: "pane.open"}}
	if len(shells) > 0 {
		items = append(items, ui.MenuSeparator())
		items = append(items, shells...)
		items = append(items, ui.MenuSeparator())
	}
	return append(items,
		ui.MenuItem{Command: "pane.splitRight"},
		ui.MenuItem{Command: "pane.splitDown"},
		ui.MenuItem{Command: "pane.unsplit"},
		ui.MenuSeparator(),
		ui.MenuItem{Command: "keys.lock"},
		ui.MenuSeparator(),
		ui.MenuItem{Command: "pane.close"})
}

// fileMenuShells are the shell lines the File menu offers. A window
// whose panes open on the machine -ssh named gets none: "New pane" goes
// there and a shell line comes back here, and no row behind the line
// says which machine it means.
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
	if a.agents.sharing() {
		chips = append(chips, ui.Chip{
			Text: shareTitle, FG: statusAgentFG(a.colours), BG: chipBG(a.colours),
			Do: a.showShare,
		})
	}
	if a.serving.on() {
		text, fg := "Serving", statusIdleFG(a.colours)
		if len(a.serving.clients()) > 0 {
			text, fg = "Remote controlled", statusTakenFG(a.colours)
		}
		chips = append(chips, ui.Chip{
			Text: text, FG: fg, BG: chipBG(a.colours), Do: a.showServing,
		})
	}
	return chips
}

// chipBG is the ground a chip on the menu bar sits on: black under a
// dark window, white under a light one, which is away from the bar in
// the direction the text on a chip leaves room for.
//
// A chip cannot meet grid.Contrast's 1.5:1 for a change of ground here.
// The bar is already as far from the window's background as the dimmer
// red on a chip can be read against, so the chip goes the other way, and
// this is as far as it goes.
func chipBG(p vt.Palette) color.RGBA {
	if grid.Contrast(chipWhite, p.BG) >= grid.Contrast(chipBlack, p.BG) {
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
		FG: a.colours.FG,
		// No background of its own: the frosted panel behind the menu is
		// the background, and an opaque fill would hide it.
		BG:         color.RGBA{},
		SelectedFG: a.colours.BG,
		SelectedBG: a.colours.FG,
		ChordFG:    a.colours.ANSI[8],
		DisabledFG: a.colours.Selection,
		// A rule around it, and a shadow under it. A menu over a
		// terminal is otherwise two lots of text with nothing between
		// them.
		BorderFG: a.colours.ANSI[8],
		ShadowBG: shadow,
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
// window changes scheme.
func (a *app) menubarStyle() ui.MenubarStyle {
	return ui.MenubarStyle{
		FG: a.colours.FG,
		// The sidebar's own ground, running across the bar rather than
		// down it. The two are one frame around the window, so they are
		// drawn in one colour.
		BG:     sidebarTop(a.colours),
		BGEnd:  sidebarFoot(a.colours),
		OpenFG: a.colours.BG,
		OpenBG: a.colours.FG,
	}
}
