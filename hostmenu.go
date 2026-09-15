package main

import (
	"errors"

	"github.com/marrasen/gridterm/ui"
)

// hostKey marks a panel row as the name of a machine rather than one of
// its connections. A row's key has to be comparable and has to be
// unmistakable for any other kind of key, which a bare string is not.
type hostKey string

// hostMenus is the machine a menu opened from the sidebar is about, kept
// here because ui.Command.Run takes no argument. Only the goroutine that
// draws touches it.
type hostMenus struct {
	// host is the machine the menu now up is about, empty when no such
	// menu is up.
	host string

	// opened counts the menus opened this way.
	opened int
}

// opening counts a menu about to open and gives back its number.
func (m *hostMenus) opening() int {
	m.opened++
	return m.opened
}

// nowAbout records the machine the menu now up is about.
func (m *hostMenus) nowAbout(host string) { m.host = host }

// forget takes the machine away, for a menu that has closed.
func (m *hostMenus) forget() { m.host = "" }

// closed forgets the machine the menu numbered n named, unless a later
// menu has named another.
func (m *hostMenus) closed(n int) {
	if m.opened == n {
		m.forget()
	}
}

// machine is the machine the open menu is about, empty when no menu is
// up.
func (m *hostMenus) machine() string { return m.host }

// openHostMenu drops down what can be opened on a machine, under the row
// that names it.
//
// The lines name the same commands the menu bar and the keys use, and
// while this menu is up they act on the machine whose row was clicked.
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
		hostItems(hostAbout{
			here:   a.isHere(string(host)),
			window: a.isWindow(string(host)),
			saved:  a.isSaved(string(host)),
			serves: a.savedWindow(string(host)),
		}), func() {
			if hide != nil {
				hide()
			}
		})
	menu.Style = a.menuStyle()
	menu.Anchor = a.rowAnchor(row.Key)

	// Which menu this is, so a later one's machine is not taken away by
	// this one being forgotten.
	mine := a.hostMenus.opening()
	hide = a.showModal(menu, func() {
		// Not cleared here: a menu closes before it runs the line the
		// user chose, so this would take the machine away from under
		// the command. The pump is drained once a frame, which is after
		// the command has run.
		a.pump.post(func() { a.hostMenus.closed(mine) })
	})
	if a.root.Modal() != ui.Widget(menu) {
		// It never went up, so nothing will ever take it down and
		// nothing will clear the machine it was about.
		return errors.New("there is no room to show the menu")
	}
	// Set once the menu is really up, for the same reason.
	a.hostMenus.nowAbout(string(host))
	a.markDirty()
	return nil
}

// hostItems is what the plus on a machine's row offers.
//
// A machine gridterm has no connection of its own to gets the short
// list: a terminal there is one more pane, and there is nothing to
// tunnel or to close.
//
// A window taken over gets a list of its own: panes and files are what
// cross that connection, so a command, a tunnel and a proxy are left
// off.
//
// A machine the server list holds can also be edited and forgotten.
// This is where they belong: the row is the machine, so the plus on it
// is where everything about that machine is.

// hostAbout says what kind of machine a row stands for, which is what
// decides the menu on it.
type hostAbout struct {
	// here is the machine gridterm is running on.
	here bool

	// window is another gridterm, already taken over.
	window bool

	// saved is in the server list, and serves narrows that to one saved
	// as a gridterm window rather than a machine to log in to.
	saved, serves bool
}

func hostItems(about hostAbout) []ui.MenuItem {
	if about.here {
		return []ui.MenuItem{
			{Command: "tab.open", Title: "Terminal"},
			{Command: "conn.files", Title: "Files"},
		}
	}
	if about.window {
		items := []ui.MenuItem{
			{Command: "conn.terminal", Title: "Terminal"},
			{Command: "conn.files", Title: "Files"},
			ui.MenuSeparator(),
			{Command: "conn.disconnect", Title: "Let go of this window"},
		}
		if about.serves {
			items = append(items, ui.MenuSeparator(),
				ui.MenuItem{Command: "server.editThis", Title: "Edit this window…"},
				ui.MenuItem{Command: "server.forget", Title: "Forget this window…"})
		}
		return items
	}
	if about.serves {
		// Saved as a window and not taken over yet. Nothing that needs
		// a shell applies: it serves gridterm's own protocol and has no
		// shell to log in to.
		return []ui.MenuItem{
			{Command: "conn.terminal", Title: "Take it over"},
			ui.MenuSeparator(),
			{Command: "server.editThis", Title: "Edit this window…"},
			{Command: "server.forget", Title: "Forget this window…"},
		}
	}
	items := []ui.MenuItem{
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
	if about.saved {
		items = append(items, ui.MenuSeparator(),
			ui.MenuItem{Command: "server.editThis", Title: "Edit this server…"},
			ui.MenuItem{Command: "server.forget", Title: "Forget this server…"})
	}
	return items
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
		// The plus itself, not the whole row: the menu hangs from its
		// left edge and reaches out over whatever is beside the sidebar,
		// rather than being squeezed into the sidebar's own width.
		return ui.Rect{
			X: area.X + max(area.Cols-2, 0), Y: a.windowRow(y),
			Cols: 1, Rows: 1,
		}
	}
}
