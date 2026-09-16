package main

import (
	"image/color"
	"slices"

	"github.com/marrasen/gridterm/ui"
)

// newMenubar puts a row of menu titles above the widget tree.
//
// The menus name commands rather than carrying actions of their own, so
// a line here, its key binding and its entry in the palette are three
// ways into one registry and cannot drift apart.
func (a *app) newMenubar(child ui.Widget) *ui.Menubar {
	bar := ui.NewMenubar(a.root.Commands, a.root.Accelerators, child)
	bar.Menus = []ui.MenuDef{
		{Title: "File", Items: []ui.MenuItem{
			{Command: "tab.open"},
			{Command: "pane.splitRight"},
			{Command: "pane.splitDown"},
			{Command: "pane.unsplit"},
			ui.MenuSeparator(),
			{Command: "keys.lock"},
			ui.MenuSeparator(),
			{Command: "pane.close"},
		}},
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
			{Command: "tab.next"},
			{Command: "tab.previous"},
			ui.MenuSeparator(),
			{Command: "palette.open"},
		}},
		{Title: helpMenu, Items: []ui.MenuItem{
			{Command: helpCommand},
		}},
	}
	bar.Style = ui.MenubarStyle{
		FG: a.colours.FG,
		// The sidebar's own ground, running across the bar rather than
		// down it. The two are one frame around the window, so they are
		// drawn in one colour.
		BG:     sidebarTop(a.colours),
		BGEnd:  sidebarFoot(a.colours),
		OpenFG: a.colours.BG,
		OpenBG: a.colours.FG,
	}
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

// helpMenu is the menu bar title the key list hangs under. It stays
// last, so the menus the window adds while it runs go in front of it.
const helpMenu = "Help"

// addMenu puts a menu on the bar, in front of Help.
//
// An open menu holds the index of the title it hangs under, so one at or
// after the new title is taken down rather than left hanging under
// another menu's name.
func (a *app) addMenu(def ui.MenuDef) {
	at := len(a.bar.Menus)
	for i, have := range a.bar.Menus {
		if have.Title == helpMenu {
			at = i
			break
		}
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
