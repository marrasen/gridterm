package main

import (
	"errors"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui"
)

// hostKey marks a panel row as the name of a machine rather than one of
// its connections. A row's key has to be comparable and has to be
// unmistakable for any other kind of key, which a bare string is not.
type hostKey string

// hostMenus is the machine a menu opened from the sidebar is about and
// whether such a menu is up, kept here because ui.Command.Run takes no
// argument. Only the goroutine that draws touches it.
type hostMenus struct {
	// host is the machine the menu now up is about, and up says whether
	// one is up at all, because the local machine's name is empty.
	host string
	up   bool

	// far is the machine of a window taken over that the menu is about,
	// and onFar says the menu is one of those. Only "Files" is offered
	// there, and only openFilesHere acts on it; a.current calls that
	// machine hostFar, which every other command refuses.
	far   remoteHostKey
	onFar bool

	// opened counts the menus opened this way.
	opened int
}

// opening counts a menu about to open and gives back its number.
func (m *hostMenus) opening() int {
	m.opened++
	return m.opened
}

// nowAbout records the machine the menu now up is about.
func (m *hostMenus) nowAbout(host string) {
	m.host, m.up = host, true
	m.far, m.onFar = remoteHostKey{}, false
}

// nowAboutFar records the machine of a window taken over that the menu
// now up is about.
//
// The name is left empty and machine reports no menu, so nothing can read
// that machine as a name: a key can neither let go of the window nor reach
// this window's own machine of the same name. a.current answers for it
// instead, as hostFar.
func (m *hostMenus) nowAboutFar(key remoteHostKey) {
	m.host, m.up = "", false
	m.far, m.onFar = key, true
}

// forget takes the machine away, for a menu that has closed.
func (m *hostMenus) forget() {
	m.host, m.up = "", false
	m.far, m.onFar = remoteHostKey{}, false
}

// closed forgets the machine the menu numbered n named, unless a later
// menu has named another.
func (m *hostMenus) closed(n int) {
	if m.opened == n {
		m.forget()
	}
}

// machine is the machine the open menu is about. The second result says
// whether a menu is up at all, which an empty name cannot.
func (m *hostMenus) machine() (string, bool) { return m.host, m.up }

// farMachine is the machine of a window taken over the open menu is
// about. The second result says whether the menu is one of those.
func (m *hostMenus) farMachine() (remoteHostKey, bool) { return m.far, m.onFar }

// openHostMenu is what the button at the end of a sidebar row does.
//
// On the row that names a machine it drops a menu down under the row,
// with what can be opened there. The lines name the same commands the
// menu bar and the keys use, and while this menu is up they act on the
// machine whose row was clicked.
//
// On a finished connection's row the button is the cross that clears the
// row, and there is no menu: clearing is the one thing left to do.
func (a *app) openHostMenu(row ui.ListRow) error {
	// What the row offers, and what to remember the menu is about while
	// it is up.
	var (
		items []ui.MenuItem
		about func()
	)
	switch key := row.Key.(type) {
	case hostKey:
		items = hostItems(a.about(string(key)), a.shellPick.lines(true), a.folderItems(string(key)))
		about = func() { a.hostMenus.nowAbout(string(key)) }
	case remoteHostKey:
		if key.window == nil {
			// The only guard there is: nothing else re-checks, because
			// nothing else can produce one of these.
			return nil
		}
		items, about = farItems(), func() { a.hostMenus.nowAboutFar(key) }
	case *conns.Entry:
		// The button on a connection's row clears a finished one or
		// closes the pane a row stands for. Neither needs a menu. Which
		// of the two is whichever cross the row drew: a row that carries
		// one whatever the pointer is doing is the clearing one.
		if row.Button != 0 {
			return a.clearRow(key)
		}
		return a.closePaneRow(key)
	default:
		return nil
	}
	// The menu takes itself away, and what it hangs on is only known
	// once it is up, so it is reached through a variable filled in
	// below.
	var hide func()
	menu := ui.NewMenu(a.root.Commands, a.root.Accelerators, items, func() {
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
	about()
	a.markDirty()
	return nil
}

// closePaneRow closes the pane a row stands for, which is what the cross
// on a pane's row does.
//
// Asked of the panes rather than of paneRows: that set is a frame old,
// and a key in the same frame can have closed the pane already.
func (a *app) closePaneRow(e *conns.Entry) error {
	if !a.stillAPane(e) || e.Close == nil {
		return nil
	}
	if err := e.Close(); err != nil {
		// Shown here rather than returned, so the notice says which
		// button was pressed instead of "that could not be done".
		a.reportError("Could not close that pane", err)
	}
	a.markDirty()
	return nil
}

// stillAPane reports whether a row stands for a pane the window still
// holds.
func (a *app) stillAPane(e *conns.Entry) bool {
	for _, have := range a.panes {
		if have == e {
			return true
		}
	}
	if a.files == nil {
		return false
	}
	for _, have := range a.files.rows {
		if have == e {
			return true
		}
	}
	return false
}

// clearRow takes a finished row off the panel, which is what the cross at
// the end of it does.
//
// It clears the row and nothing else. What the row named is never ended
// from here: a pane keeps what it printed after its program has gone,
// and closing the pane or "clear finished connections" is what takes it
// away.
//
// A row with nothing to clear draws no clearing cross, so nothing here
// asks whether it has finished.
func (a *app) clearRow(e *conns.Entry) error {
	if e.Clear == nil {
		return nil
	}
	err := e.Clear()
	// Before the reason is shown, because a clear that failed part way
	// still changed the panel.
	a.markDirty()
	if err != nil {
		// Shown here rather than returned, so the notice says which
		// button was pressed instead of "that could not be done".
		a.reportError("Could not clear that row", err)
	}
	return nil
}

// farItems is what the plus on a machine of a window taken over offers.
//
// Files and nothing else. That machine is reached through the window,
// which has the shells and the tunnels on it: a pane reading its files
// is the one thing this window can open over there.
func farItems() []ui.MenuItem {
	return []ui.MenuItem{{Command: "conn.files", Title: "Files"}}
}

// hostItems is what the plus on a machine's row offers. shells are the
// lines that open a pane on a named shell, which only this machine has.
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
func hostItems(about hostFacts, shells, folders []ui.MenuItem) []ui.MenuItem {
	if about.kind == hostHere {
		items := []ui.MenuItem{{Command: "conn.terminal", Title: "Terminal"}}
		// Under Terminal, which already says a pane here is what opens,
		// so each line says only which shell it opens on.
		if len(shells) > 0 {
			items = append(items, ui.MenuSeparator())
			items = append(items, shells...)
			items = append(items, ui.MenuSeparator())
		}
		items = append(items, filesLines(folders)...)
		return append(items, ui.MenuItem{Command: "conn.command", Title: "Command…"})
	}
	if about.kind == hostWindow {
		items := []ui.MenuItem{{Command: "conn.terminal", Title: "Terminal"}}
		items = append(items, filesLines(folders)...)
		items = append(items,
			ui.MenuSeparator(),
			ui.MenuItem{Command: "conn.disconnect", Title: "Let go of this window"})
		items = withTheLog(items, about)
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
		return withTheLog([]ui.MenuItem{
			{Command: "conn.terminal", Title: "Take it over"},
			ui.MenuSeparator(),
			{Command: "server.editThis", Title: "Edit this window…"},
			{Command: "server.forget", Title: "Forget this window…"},
		}, about)
	}
	items := []ui.MenuItem{{Command: "conn.terminal", Title: "Terminal"}}
	items = append(items, filesLines(folders)...)
	items = append(items,
		ui.MenuItem{Command: "conn.command", Title: "Command…"},
		ui.MenuSeparator(),
		ui.MenuItem{Command: "conn.tunnel", Title: "Tunnel…"},
		ui.MenuItem{Command: "conn.socks", Title: "SOCKS proxy…"},
		ui.MenuSeparator(),
		// Not sidebar.closeRow: that one closes whatever the list has
		// selected, which is not the machine whose row was clicked.
		ui.MenuItem{Command: "conn.disconnect", Title: "Close the connection"})
	items = withTheLog(items, about)
	if about.saved {
		items = append(items, ui.MenuSeparator(),
			ui.MenuItem{Command: "server.editThis", Title: "Edit this server…"},
			ui.MenuItem{Command: "server.forget", Title: "Forget this server…"})
	}
	return items
}

// withTheLog adds the line that opens the account of how the machine was
// reached, for a name that kept one.
func withTheLog(items []ui.MenuItem, about hostFacts) []ui.MenuItem {
	if about.log() == nil {
		return items
	}
	return append(items, ui.MenuSeparator(),
		ui.MenuItem{Command: "conn.log", Title: "How it was reached"})
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
		// Past the sidebar rather than at the plus, so the menu reaches
		// out over whatever is beside it rather than down the sidebar's
		// own width.
		return ui.Rect{X: area.X + area.Cols, Y: a.windowRow(y), Cols: 1, Rows: 1}
	}
}

// filesLines is what the plus offers for reading files: "Files", which
// opens at home or at the one folder saved, and a line per folder for a
// machine with several.
func filesLines(folders []ui.MenuItem) []ui.MenuItem {
	return append([]ui.MenuItem{{Command: "conn.files", Title: "Files"}}, folders...)
}
