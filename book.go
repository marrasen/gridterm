package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// What a saved server's commands are called. The prefixes let the
// commands for one version of the list be taken away when it changes.
const (
	openPrefix  = "server.open."
	editPrefix  = "server.edit."
	termPrefix  = "conn.terminal."
	filesPrefix = "conn.files."
)

// refreshServers registers a command per saved machine and puts them on
// the menu bar.
//
// A command each rather than a list to pick from: the palette finds them
// by name, the menu shows them, and a key can be bound to one. It is the
// same shape the font families use.
func (a *app) refreshServers() {
	if a.root.Commands == nil {
		return
	}
	// Which of them are windows rather than machines, for the panel and
	// for the commands below.
	//
	// Before the shortcut: which machines there are and what each one is
	// are two different questions, and changing a machine from one kind
	// to the other changes no name at all. Reading this after the
	// shortcut left a window known as a machine for the rest of the
	// session, so asking for a terminal on it tried to log in to it.
	windows := make(map[string]bool)
	for _, h := range a.book.Hosts() {
		if h.Window {
			windows[h.Name] = true
		}
	}
	a.savedWindows = windows

	// Nothing more to do when the machines are the ones already
	// registered. This runs whenever a connection is made or lost, and
	// rebuilding takes the open menu down with it: a menu that vanished
	// while the user was reading it, because a connection they were not
	// watching dropped, is the window getting in their way.
	// The kinds go into the comparison as well as the names. What a
	// command is called depends on which kind a machine is -- "take
	// over" rather than "open a terminal on" -- and changing that
	// changes no name.
	want := make([]string, 0, len(a.everyHost())+len(a.savedHosts()))
	for _, host := range append(a.everyHost(), a.savedHosts()...) {
		if windows[host] {
			host += " (window)"
		}
		want = append(want, host)
	}
	if slices.Equal(want, a.serverHosts) {
		return
	}
	a.serverHosts = want
	// Whatever was registered for the old list goes first, or a server
	// that has been renamed would answer to both names.
	for _, id := range a.serverCommands {
		a.root.Commands.Unregister(id)
	}
	a.serverCommands = nil

	// A terminal and a file pane for every machine, this one included:
	// the palette is searched by name, and a name is the thing the user
	// has in mind. Connecting and editing are only for the saved ones --
	// there is nothing to connect to on the machine already running.
	for _, name := range a.everyHost() {
		host := name
		title := "Open a terminal on " + groupName(host)
		if a.savedWindows[host] && a.windows[host] == nil {
			// Nothing runs on a window until it is taken over, and
			// taking it over is what asking for a terminal on it means.
			title = "Take over " + groupName(host)
		}
		term := ui.Command{
			ID:    termPrefix + remote.CommandName(host),
			Title: title,
			Run:   func() error { return a.openTerminalOn(host) },
		}
		a.registerServerCommands(a.reporting(term))
		browse := ui.Command{
			ID:    filesPrefix + remote.CommandName(host),
			Title: "Browse files on " + groupName(host),
			Run:   func() error { return a.openFilesOn(host) },
		}
		a.registerServerCommands(a.reporting(browse))
	}

	var items []ui.MenuItem
	for _, host := range a.savedHosts() {
		open := ui.Command{
			ID:    openPrefix + remote.CommandName(host),
			Title: "Connect to " + host,
			Run:   func() error { return a.connectSaved(host) },
		}
		edit := ui.Command{
			ID:    editPrefix + remote.CommandName(host),
			Title: "Edit " + host + "…",
			Run:   func() error { return a.openEditServer(host) },
		}
		for _, cmd := range []ui.Command{a.reporting(open), a.reporting(edit)} {
			if err := a.root.Commands.Register(cmd); err != nil {
				// Two names that reduce to the same id, or one that
				// collides with a command already there. Neither is
				// worth losing the rest of the list over.
				a.logError(err)
				continue
			}
			a.serverCommands = append(a.serverCommands, cmd.ID)
		}
		// Only the ones that registered go on the menu: a line naming a
		// command that is not there is drawn greyed out and cannot be
		// chosen, which reads as a fault.
		if _, ok := a.root.Commands.Lookup(open.ID); ok {
			items = append(items, ui.MenuItem{Command: open.ID})
		}
	}
	a.refreshServerMenu(items)
}

// savedHosts is what the book calls its machines.
func (a *app) savedHosts() []string { return a.book.Names() }

// registerServerCommands puts commands on the registry and remembers
// their ids, so the next list can take them off again.
//
// A name that reduces to an id already taken is dropped rather than
// losing the rest of the list over it.
func (a *app) registerServerCommands(cmds ...ui.Command) {
	for _, cmd := range cmds {
		if err := a.root.Commands.Register(cmd); err != nil {
			a.logError(err)
			continue
		}
		a.serverCommands = append(a.serverCommands, cmd.ID)
	}
}

// openTerminalOn opens a terminal on a machine: a tab here when it is
// this one, and a shell over the connection otherwise.
func (a *app) openTerminalOn(host string) error {
	switch {
	case a.isHere(host):
		// A new pane already runs there, which is what a new tab is.
		return a.openTab()
	case a.isWindow(host):
		return a.openOnWindow(host, nil)
	case a.savedWindow(host):
		// Nothing runs on a window until it is taken over, and there is
		// no shell on one to log in to: it serves gridterm's own
		// protocol and answers nothing else.
		return a.connectSaved(host)
	}
	return a.openOn(host, nil, nil)
}

// everyHost is every machine worth a command of its own: this one, the
// saved servers, and anything the window has connected to along the way.
//
// Local is on the list because "browse files on this machine" is the
// answer as often as any other, and it is the one machine the book never
// names.
func (a *app) everyHost() []string { return a.allHosts() }

// refreshServerMenu puts the saved machines on the menu bar, under their
// own title.
func (a *app) refreshServerMenu(items []ui.MenuItem) {
	if a.bar == nil {
		return
	}
	// Whatever is open holds the index of the title it hangs under, and
	// the list is about to change shape.
	a.bar.Close()

	if len(items) > 0 {
		items = append(items, ui.MenuSeparator())
	}
	items = append(items,
		ui.MenuItem{Command: "server.connect"},
		ui.MenuItem{Command: "server.add"},
		ui.MenuItem{Command: "server.reload"},
		ui.MenuSeparator(),
		ui.MenuItem{Command: "serve.window"},
		ui.MenuItem{Command: "serve.takeOver"},
		ui.MenuSeparator(),
		ui.MenuItem{Command: "agent.hand"},
		ui.MenuItem{Command: "agent.take"})

	def := ui.MenuDef{Title: "Servers", Items: items}
	for i, have := range a.bar.Menus {
		if have.Title == def.Title {
			a.bar.Menus[i] = def
			return
		}
	}
	a.bar.Menus = append(a.bar.Menus, def)
}

// kindMachine and kindWindow are what the Kind field offers: a machine
// to log in to, or another gridterm serving that this one takes over.
const (
	kindMachine = "Machine (SSH)"
	// The same words the message uses when an SSH connection turns out
	// to be a window, so the two cannot drift apart.
	kindWindow = remote.GridtermWindowKind
)

// whichKind reads the Kind field.
//
// Anything else is a mistake rather than an instruction. The field
// takes whatever is typed into it, and a typo quietly turning a window
// into a machine would leave the user with a saved entry that tries to
// log in to a port with no shell behind it.
func whichKind(text string) (window bool, err error) {
	switch strings.TrimSpace(text) {
	case kindMachine:
		return false, nil
	case kindWindow:
		return true, nil
	}
	return false, fmt.Errorf("the kind has to be %q or %q", kindMachine, kindWindow)
}

// connectSaved opens a terminal on a saved machine, reaching it through
// whatever it is saved as being behind.
//
// A saved window is taken over instead, which is the only way to reach
// one: it serves gridterm's own protocol and has no shell to log in to.
func (a *app) connectSaved(name string) error {
	h, ok := a.book.Lookup(name)
	if !ok {
		return fmt.Errorf("there is no saved server called %q", name)
	}
	if h.Window {
		if a.windows[h.Name] != nil {
			// Already taken over, so what was asked for is a terminal on
			// it rather than taking it over again.
			return a.openOnWindow(h.Name, nil)
		}
		return a.takeOver(h.ServeAddr(), h.KeyFile())
	}
	return a.openOn(h.Name, nil, nil)
}

// reloadBook reads the server list again, for a user who has repaired
// it since the window opened.
func (a *app) reloadBook() error {
	if err := a.book.Reload(); err != nil {
		return err
	}
	a.refreshServers()
	return nil
}

// openAddServer asks for a machine to save.
func (a *app) openAddServer() error { return a.openServerForm("") }

// openEditServer asks for changes to one already saved.
func (a *app) openEditServer(name string) error { return a.openServerForm(name) }

// openServerForm is the add and the edit dialog: the same fields, filled
// in from the saved machine when there is one.
func (a *app) openServerForm(under string) error {
	if err := a.book.Err(); err != nil {
		// Nothing typed here could be saved, so it is better to say so
		// than to take it and lose it.
		return err
	}
	var was remote.Host
	if under != "" {
		var ok bool
		if was, ok = a.book.Lookup(under); !ok {
			return fmt.Errorf("there is no saved server called %q", under)
		}
	}

	title := "Add a server"
	if under != "" {
		title = "Edit " + was.Name
	}
	f := a.newForm(title)
	name := f.AddField("Name", a.newField("what to call it", 0))
	kind := f.AddField("Kind", a.newField("", 0))
	kind.Options = []string{kindMachine, kindWindow}
	target := f.AddField("Server", a.newField("[user@]host[:port]", 0))
	key := f.AddField("Key file", a.newField("optional", 0))
	via := f.AddField("Through", a.newField("another saved server, optional", 0))
	// The machines already saved, so the field can be cycled rather than
	// typed from memory. Blank first: leaving it empty is the usual
	// answer, and it is what cycling comes back round to.
	via.Options = append([]string{""}, a.serverNames(under)...)
	f.Lines = append(f.Lines,
		"Kind steps with ctrl+down and ctrl+up. A gridterm window is one",
		"serving on another machine, taken over rather than logged in to.",
		viaHint(via.Options))

	name.SetText(was.Name)
	kind.SetText(kindMachine)
	if was.Window {
		kind.SetText(kindWindow)
	}
	target.SetText(was.Target())
	if len(was.Identities) > 0 {
		key.SetText(was.Identities[0])
	}
	via.SetText(was.Via)

	f.AddButton(ui.Button{Title: "Save", Do: func() error {
		window, err := whichKind(kind.Text())
		if err != nil {
			return err
		}
		h, err := remote.HostFromTarget(name.Text(), target.Text())
		if err != nil {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return err
		}
		h.Window = window
		if window {
			// Neither means anything to a window: there is no account to
			// log in to and no machine to go through. They are kept
			// rather than dropped, so turning it back into a machine
			// gives back what it had.
			h.User, h.Via = was.User, was.Via
			h.Window = true
			if h.User != "" || strings.TrimSpace(via.Text()) != was.Via {
				// Said plainly, because the fields are still on screen
				// and doing nothing would read as them having been
				// saved.
				f.Lines = append(f.Lines,
					"A window has no account and nothing to go through, so those are left as they were.")
			}
		} else {
			h.Via = strings.TrimSpace(via.Text())
		}
		// Kept whichever this is. A machine turned into a window and
		// back should come out the way it went in, and neither field is
		// something the dialog can show.
		h.Term = was.Term
		// The dialog edits the first key file. Any others the machine
		// had stay: a field that cannot show them must not delete them.
		rest := was.Identities
		if len(rest) > 0 {
			rest = rest[1:]
		}
		if path := strings.TrimSpace(key.Text()); path != "" {
			h.Identities = append([]string{path}, rest...)
		} else {
			h.Identities = rest
		}
		// Checked before the book is written, so the dialog stays open
		// with what was typed still in it. The window holds names the
		// book never saw -- a machine reached by typing a target, and
		// this machine itself -- and two connections under one name
		// would leave one of them open with nothing holding it.
		if under != "" && under != h.Name {
			if a.machines[h.Name] != nil || a.opening[h.Name] != nil || a.windows[h.Name] != nil {
				return fmt.Errorf("something is already connected as %q; close it first", h.Name)
			}
		}
		if t := a.windows[under]; t != nil && (!window || t.addr != h.ServeAddr()) {
			// The connection is to the machine it was made to, and
			// saying it is somewhere else does not move it.
			return fmt.Errorf("%s is taken over at %s; let go of it before changing where it is",
				under, t.addr)
		}
		if err := a.book.Put(h, under); err != nil {
			return err
		}
		// Exactly, not ignoring case: the window's own record is kept by
		// name and a change of capitals is a change of name to it.
		if under != "" && under != h.Name {
			a.renamedMachine(under, h)
		}
		return nil
	}})
	if under != "" {
		f.AddButton(ui.Button{Title: "Remove", Do: func() error {
			// Not from here: this dialog closes as soon as this returns,
			// and closing a dialog takes anything stacked on top of it.
			a.pump.post(func() { a.confirmRemoveServer(was.Name) })
			return nil
		}})
	}
	f.AddButton(ui.Button{Title: "Cancel"})

	a.showForm(f, a.refreshServers)
	return nil
}

// confirmRemoveServer asks before forgetting a machine.
func (a *app) confirmRemoveServer(name string) {
	f := a.newConfirm("Remove "+name+"?", []string{
		"It is only forgotten here. Nothing on the machine changes.",
	})
	f.AddButton(ui.Button{Title: "Remove", Do: func() error {
		return a.book.Remove(name)
	}})
	f.AddButton(ui.Button{Title: "Keep it"})
	// Opens on the button that changes nothing.
	f.FocusButton(1)
	a.showForm(f, a.refreshServers)
}

// reportBookError tells the user their server list could not be read,
// once the window is up to tell them in.
func (a *app) reportBookError() {
	err := a.book.Err()
	if err == nil {
		return
	}
	a.pump.post(func() {
		a.reportError("The server list could not be read", errors.Join(err,
			errors.New("gridterm will not write over it until it is repaired")))
	})
}

// serverNames is every saved machine except one, for the Through field
// of the dialog editing that one.
//
// A machine reached through itself is a machine nothing can reach, so it
// is not on the list. A longer loop is still possible and is caught when
// the connection is made, which is the only place the whole chain is
// known.
func (a *app) serverNames(except string) []string {
	var out []string
	for _, h := range a.book.Hosts() {
		if h.Name == except {
			continue
		}
		out = append(out, h.Name)
	}
	return out
}

// viaHint says how to fill the Through field in, and with what.
func viaHint(options []string) string {
	named := options
	if len(named) > 0 && named[0] == "" {
		named = named[1:]
	}
	if len(named) == 0 {
		return "Through: nothing else is saved yet, so there is nothing to go through."
	}
	return "Through: ctrl+down and ctrl+up step through " + strings.Join(named, ", ") + "."
}

// editThisServer opens the dialog for the machine whose row was
// clicked, rather than for one picked from a list.
func (a *app) editThisServer() error {
	host := a.currentHost()
	if !a.isSaved(host) {
		return fmt.Errorf("%s is not in the server list", groupName(host))
	}
	return a.openEditServer(host)
}

// forgetThisServer takes the machine whose row was clicked out of the
// server list, once the user has said so.
//
// Asked from here rather than done: a server is a few minutes of typing
// and the list is the only record of it.
func (a *app) forgetThisServer() error {
	host := a.currentHost()
	if !a.isSaved(host) {
		return fmt.Errorf("%s is not in the server list", groupName(host))
	}
	a.confirmRemoveServer(host)
	return nil
}
