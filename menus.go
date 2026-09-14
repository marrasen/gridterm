package main

import (
	"image/color"

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
	}
	bar.Style = ui.MenubarStyle{
		FG:     a.colours.FG,
		BG:     a.colours.ANSI[0],
		OpenFG: a.colours.BG,
		OpenBG: a.colours.FG,
	}
	bar.MenuStyle = ui.MenuStyle{
		FG: a.colours.FG,
		// No background of its own: the frosted panel behind the menu is
		// the background, and an opaque fill would hide it.
		BG:         color.RGBA{},
		SelectedFG: a.colours.BG,
		SelectedBG: a.colours.FG,
		ChordFG:    a.colours.ANSI[8],
		DisabledFG: a.colours.Selection,
	}
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
