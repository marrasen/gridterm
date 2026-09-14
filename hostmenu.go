package main

import (
	"errors"

	"github.com/marrasen/gridterm/ui"
)

// hostKey marks a panel row as the name of a machine rather than one of
// its connections. A row's key has to be comparable and has to be
// unmistakable for any other kind of key, which a bare string is not.
type hostKey string

// openHostMenu drops down what can be opened on a machine, under the row
// that names it.
//
// The lines name commands, the same ones the menu bar and the keys use,
// so the three cannot drift apart. Those commands act on the machine the
// user is looking at, and while this menu is up that is the machine
// whose row was clicked.
func (a *app) openHostMenu(row ui.ListRow) error {
	host, ok := row.Key.(hostKey)
	if !ok {
		return nil
	}
	// The menu takes itself away, and what it hangs on is only known
	// once it is up, so it is reached through a variable filled in
	// below.
	var hide func()
	menu := ui.NewMenu(a.root.Commands, a.root.Accelerators,
		hostItems(a.isHere(string(host))), func() {
			if hide != nil {
				hide()
			}
		})
	menu.Style = a.menuStyle()
	menu.Anchor = a.rowAnchor(row.Key)

	// Which menu this is. The machine is forgotten a frame later, and by
	// then another menu may have opened and named a different one: a
	// clear that did not say which menu it belonged to would take that
	// machine away instead.
	a.menus++
	mine := a.menus
	hide = a.showModal(menu, func() {
		// Not cleared here: a menu closes before it runs the line the
		// user chose, so this would take the machine away from under
		// the command. The pump is drained once a frame, which is after
		// the command has run.
		a.pump.post(func() {
			if a.menus == mine {
				a.acting = false
			}
		})
	})
	if a.root.Modal() != ui.Widget(menu) {
		// It never went up, so nothing will ever take it down and
		// nothing will clear the machine it was about.
		return errors.New("there is no room to show the menu")
	}
	// Set once the menu is really up, for the same reason.
	a.actOn, a.acting = string(host), true
	a.markDirty()
	return nil
}

// hostItems is what the plus on a machine's row offers.
//
// A machine gridterm has no connection of its own to gets the short
// list: a terminal there is one more pane, and there is nothing to
// tunnel or to close.
func hostItems(here bool) []ui.MenuItem {
	if here {
		return []ui.MenuItem{
			{Command: "tab.open", Title: "Terminal"},
			{Command: "conn.files", Title: "Files"},
		}
	}
	return []ui.MenuItem{
		{Command: "conn.terminal", Title: "Terminal"},
		{Command: "conn.files", Title: "Files"},
		{Command: "conn.command", Title: "Command…"},
		ui.MenuSeparator(),
		{Command: "conn.tunnel", Title: "Tunnel…"},
		{Command: "conn.socks", Title: "SOCKS proxy…"},
		ui.MenuSeparator(),
		// Not conn.close: that one closes whatever the list has
		// selected, which is not the machine whose row was clicked.
		{Command: "conn.disconnect", Title: "Close the connection"},
	}
}

// rowAnchor is where a menu opened from a panel row points.
//
// Worked out afresh each time it is asked, because the window is laid
// out again after it is resized and the row will have moved.
func (a *app) rowAnchor(key any) func() ui.Rect {
	return func() ui.Rect {
		area, ok := a.root.AreaOf(a.side)
		if !ok {
			return ui.Rect{}
		}
		y := a.panel.RowTop(key)
		if y < 0 {
			// Scrolled out of sight while the menu was up. The sidebar
			// itself is still the right thing to point at.
			return area
		}
		return ui.Rect{X: area.X, Y: area.Y + y, Cols: area.Cols, Rows: 1}
	}
}
