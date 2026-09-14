package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// What a saved server's commands are called. The prefixes let the
// commands for one version of the list be taken away when it changes.
const (
	openPrefix = "server.open."
	editPrefix = "server.edit."
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
	// Whatever was registered for the old list goes first, or a server
	// that has been renamed would answer to both names.
	for _, id := range a.serverCommands {
		a.root.Commands.Unregister(id)
	}
	a.serverCommands = nil

	hosts := a.book.Hosts()
	var items []ui.MenuItem
	for _, h := range hosts {
		open := ui.Command{
			ID:    openPrefix + remote.CommandName(h.Name),
			Title: "Connect to " + h.Name,
			Run:   func() error { return a.connectSaved(h.Name) },
		}
		edit := ui.Command{
			ID:    editPrefix + remote.CommandName(h.Name),
			Title: "Edit " + h.Name + "…",
			Run:   func() error { return a.openEditServer(h.Name) },
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
		ui.MenuItem{Command: "server.reload"})

	def := ui.MenuDef{Title: "Servers", Items: items}
	for i, have := range a.bar.Menus {
		if have.Title == def.Title {
			a.bar.Menus[i] = def
			return
		}
	}
	a.bar.Menus = append(a.bar.Menus, def)
}

// connectSaved opens a terminal on a saved machine, reaching it through
// whatever it is saved as being behind.
func (a *app) connectSaved(name string) error {
	h, ok := a.book.Lookup(name)
	if !ok {
		return fmt.Errorf("there is no saved server called %q", name)
	}
	return a.openOn(h.Name, nil)
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
	target := f.AddField("Server", a.newField("[user@]host[:port]", 0))
	key := f.AddField("Key file", a.newField("optional", 0))
	via := f.AddField("Through", a.newField("another saved server, optional", 0))

	name.SetText(was.Name)
	target.SetText(was.Target())
	if len(was.Identities) > 0 {
		key.SetText(was.Identities[0])
	}
	via.SetText(was.Via)

	f.AddButton(ui.Button{Title: "Save", Do: func() error {
		h, err := remote.HostFromTarget(name.Text(), target.Text())
		if err != nil {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return err
		}
		h.Via = strings.TrimSpace(via.Text())
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
		return a.book.Put(h, under)
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
