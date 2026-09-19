package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
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
	// Before anything reads the list: this is the one place called
	// whenever it may have changed, and the name a window taken over is
	// held under comes from it.
	a.rekeyWindows()
	if a.root.Commands == nil {
		return
	}

	// Nothing more to do when the commands would come out the same as
	// the ones already registered. This runs whenever a connection is
	// made or lost, and rebuilding takes the open menu down with it: a
	// menu that vanished while the user was reading it, because a
	// connection they were not watching dropped, is the window getting
	// in their way.
	//
	// So the comparison carries whatever a title depends on, not only
	// the names. A saved window reads "connect to" until it is connected
	// over and "open a terminal on" after, and taking one over changes
	// no name at all.
	every, saved := a.everyHost(), a.savedHosts()
	want := make([]string, 0, len(every)+len(saved))
	for _, host := range every {
		if a.about(host).toTakeOver() {
			host += " (connect to)"
		}
		want = append(want, host)
	}
	want = append(want, saved...)
	// And whether a share is open, which is what the line that shares a
	// pane is worded from and what puts the line that shows the share on
	// the menu at all.
	if a.agents.sharing() {
		want = append(want, "(sharing)")
	}
	// And the folders saved on each machine, under the machine's own
	// name: a bare list of paths reads the same when two machines swap
	// one, and the commands would not be built again.
	for _, host := range every {
		if folders := a.foldersOn(host); len(folders) > 0 {
			want = append(want, host+" folders "+strconv.Itoa(len(folders)))
			want = append(want, folders...)
		}
	}
	if slices.Equal(want, a.builtFor) && !a.serversMenuMissing() {
		return
	}
	a.builtFor = want
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
	for _, name := range every {
		host := name
		title := "Open a terminal on " + groupName(host)
		var also []string
		if a.about(host).toTakeOver() {
			// A window is connected to before a terminal can be opened
			// on it, so the words somebody looks for lead here as well
			// as to the line that only connects.
			also = []string{"take over", "remote", "share panes"}
		}
		term := ui.Command{
			ID:       termPrefix + remote.CommandName(host),
			Title:    title,
			AlsoFind: also,
			Run:      func() error { return a.openTerminalOn(host, nil) },
		}
		a.registerServerCommands(a.reporting(term))
		browse := ui.Command{
			ID:    filesPrefix + remote.CommandName(host),
			Title: "Browse files on " + groupName(host),
			Run:   func() error { return a.openFilesOn(host) },
		}
		a.registerServerCommands(a.reporting(browse))
		// And one per folder saved on it, so the palette can be searched
		// by the folder rather than only by the machine.
		for i, folder := range a.foldersOn(host) {
			at := folder
			a.registerServerCommands(a.reporting(ui.Command{
				ID:    folderCommandID(host, i),
				Title: "Browse " + at + " on " + groupName(host),
				Run:   func() error { return a.openFilesAt(host, at) },
			}))
		}
	}

	var items []ui.MenuItem
	for _, host := range saved {
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

// savedWindowElsewhere is the saved window already serving at an
// address under a name other than the one being edited.
func (a *app) savedWindowElsewhere(under, addr string) (string, bool) {
	h, ok := a.windows.savedAt(addr)
	if !ok || strings.EqualFold(h.Name, under) {
		return "", false
	}
	return h.Name, true
}

// serversMenuMissing reports whether the menu bar is up and has no
// Servers menu on it yet, which is the one thing that can change while
// the commands do not.
func (a *app) serversMenuMissing() bool {
	if a.bar == nil {
		return false
	}
	for _, have := range a.bar.Menus {
		if have.Title == serversMenu {
			return false
		}
	}
	return true
}

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

// openTerminalOn opens a terminal on a machine: a pane here when it is
// this one, and a shell over the connection otherwise.
//
// about says what a name is; this says what to open on it, so every way
// in gets the same answer. at is the split the terminal should land in,
// or nil for a pane of its own.
func (a *app) openTerminalOn(host string, at *spot) error {
	f := a.about(host)
	switch {
	case f.kind == hostHere:
		// A shell on this machine, whatever new panes are opening on:
		// this row is the machine the user named.
		if at != nil {
			return a.splitNewTerminalHere(at.dir, at.beside)
		}
		return a.openPaneHere()
	case f.kind == hostWindow:
		return a.openOnWindow(f.name, at)
	case f.toTakeOver():
		// Nothing runs on a window until it is taken over, and there is
		// no shell on one to log in to: it serves gridterm's own
		// protocol and answers nothing else.
		h := f.record()
		return a.workOnWindow(h.ServeAddr(), h.KeyFile(), at)
	}
	// The server list's own spelling, because a connection made under
	// the name as typed would become a second heading beside the saved
	// one: the maps take the name as asked, and a new key takes the
	// list's.
	name := f.name
	if f.saved {
		name = f.spelling
	}
	return a.openOn(name, nil, "", at)
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
		ui.MenuItem{Command: "agent.hand", Title: a.shareItem()},
		ui.MenuItem{Command: "agent.take"},
		ui.MenuItem{Command: typedCommand})
	if a.agents.sharing() {
		// Only while there is one: a line that opens nothing is a line
		// the user reads and tries.
		items = append(items, ui.MenuItem{Command: "agent.share"})
	}

	def := ui.MenuDef{Title: serversMenu, Items: items}
	for i, have := range a.bar.Menus {
		if have.Title == def.Title {
			// Whatever is open holds the index of the title it hangs
			// under, and these lines are about to change.
			a.bar.Close()
			a.bar.Menus[i] = def
			return
		}
	}
	a.addMenu(def)
}

// serversMenu is the menu bar title the saved machines hang under.
const serversMenu = "Servers"

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

// connectSaved connects to a saved machine.
//
// A server is connected to by opening a shell on it, because a shell is
// all there is to connect to. A gridterm window is connected to and
// nothing is opened on it: what it has open lands on the sidebar, and
// the plus on its heading opens a pane there.
func (a *app) connectSaved(name string) error {
	f := a.about(name)
	if !f.saved {
		return fmt.Errorf("there is no saved server called %q", name)
	}
	if f.kind == hostWindow {
		return fmt.Errorf("this window is already connected to %s", groupName(f.name))
	}
	if f.toTakeOver() {
		h := f.record()
		return a.takeOver(h.ServeAddr(), h.KeyFile(), nil, false)
	}
	return a.openTerminalOn(f.name, nil)
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
	// The keys this window keeps, so one is a key away rather than a
	// path to remember. Blank first: leaving it empty is the usual
	// answer, and it is what cycling comes back round to.
	key.Options = append([]string{""}, a.keyFiles.all()...)
	via := f.AddField("Through", a.newField("another saved server, optional", 0))
	folders := f.AddField("Folders", a.newField("where to open files, separated by commas", 0))
	// The machines already saved, so the field can be cycled rather than
	// typed from memory. Blank first: leaving it empty is the usual
	// answer, and it is what cycling comes back round to.
	via.Options = append([]string{""}, a.serverNames(under)...)
	f.Lines = append(f.Lines,
		"Kind steps with ctrl+down and ctrl+up. A gridterm window is one",
		"serving on another machine, connected to rather than logged in to.",
		viaHint(via.Options),
		"Folders are where the file browser opens on this server. One and",
		"it opens there; several and the plus offers a line for each.",
		"Key file steps through the keys this window keeps.")

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
	folders.SetText(was.FoldersJoined())

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
		// The folders are one line, which a path holding a comma or a
		// space at either end cannot come back from. One that cannot is
		// left as it was when the field was not touched, and refused
		// when it was.
		h.Folders = remote.FoldersFrom(folders.Text())
		if !remote.FoldersRoundTrip(was.Folders) {
			if folders.Text() != was.FoldersJoined() {
				return fmt.Errorf(
					"a folder on %s holds a comma or a space at one end, which this field"+
						" cannot show. Edit the folders in the server list file instead",
					was.Name)
			}
			h.Folders = was.Folders
		}
		// The dialog edits the first key file. Any others the machine
		// had stay: a field that cannot show them must not delete them.
		rest := was.Identities
		if len(rest) > 0 {
			rest = rest[1:]
		}
		path := strings.TrimSpace(key.Text())
		if path != "" {
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
			if a.about(h.Name).held() {
				return fmt.Errorf("something is already connected as %q; close it first", h.Name)
			}
		}
		if window {
			if at, ok := a.savedWindowElsewhere(under, h.ServeAddr()); ok {
				return fmt.Errorf(
					"%s is already saved as the window at %s; a window has one entry in the list",
					at, h.ServeAddr())
			}
		}
		if t := a.about(under).window; t != nil && (!window || t.addr != h.ServeAddr()) {
			// The connection is to the machine it was made to, and
			// saying it is somewhere else does not move it.
			return fmt.Errorf("%s is connected at %s; let go of it before changing where it is",
				under, t.addr)
		}
		if err := a.book.Put(h, under); err != nil {
			return err
		}
		// Exactly, not ignoring case: the window's own record is kept by
		// name and a change of capitals is a change of name to it.
		if path != "" && path != firstIdentity(was) {
			// A key the user typed or picked, not one the dialog had
			// already filled in. Said rather than returned: the server
			// is saved, and failing to remember the key is not a reason
			// to report that it was not.
			if err := a.keyFiles.keep(path); err != nil {
				a.reportError("The server was saved and its key was not added to the list", err)
			}
		}
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

// confirmRemoveServer asks before forgetting a machine, and says what
// forgetting it closes.
func (a *app) confirmRemoveServer(name string) {
	f := a.newConfirm("Remove "+name+"?", a.removeLines(a.about(name)))
	f.AddButton(ui.Button{Title: "Remove", Do: func() error {
		// Read again: the body above was written when the dialog
		// opened, and the dial may have landed or the far end gone
		// since.
		on := a.about(name)
		// The list first: it refuses a machine another saved one is
		// reached through.
		if err := a.book.Remove(name); err != nil {
			return err
		}
		if err := a.closeWhatIsHeld(on); err != nil {
			// Reported on its own rather than on this dialog, which has
			// nothing left to do: the machine has gone from the list,
			// so pressing Remove again would only say there is no such
			// server. Posted, because a dialog cannot open another.
			a.pump.post(func() { a.reportError("Trouble closing "+groupName(name), err) })
		}
		return nil
	}})
	f.AddButton(ui.Button{Title: "Keep it"})
	// Opens on the button that changes nothing.
	f.FocusButton(1)
	a.showForm(f, a.refreshServers)
}

// removeLines is the body of the forget dialog: what forgetting the
// machine does, and what goes with it when the window is holding it.
func (a *app) removeLines(on hostFacts) []string {
	nothing := "Nothing on the machine changes."
	name := groupName(on.name)
	switch {
	case on.window != nil:
		return []string{
			"This window is connected to " + name + ".",
			"Forgetting it lets go of " + name + " and closes its panes.",
			nothing,
		}
	case on.machine != nil:
		closes := "Forgetting it closes that connection"
		if with := a.whatRidesOn(on.machine); with != "" {
			closes += ", and everything reached through it: " + with
		}
		return []string{name + " is connected.", closes + ".", nothing}
	case on.dialling != nil:
		return []string{
			"The window is still connecting to " + name + ".",
			"Forgetting it gives up on that connection.",
			nothing,
		}
	}
	return []string{"It is only forgotten here.", nothing}
}

// whatRidesOn counts what else a machine's connection carries -- the
// machines reached through it and the panes on them -- and is empty when
// it carries nothing.
func (a *app) whatRidesOn(m *machine) string {
	riders := a.ridingOn(m)
	if len(riders) == 0 {
		return ""
	}
	panes := 0
	for _, name := range riders {
		if r := a.machines.named(name); r != nil {
			panes += len(a.machines.panesOn(r))
		}
		panes += a.filePanesOn(name)
	}
	what := howMany(len(riders), "machine")
	if panes > 0 {
		what += ", " + howMany(panes, "pane")
	}
	return what
}

// ridingOn names every machine reached through one, however many hops
// away, because closing it closes all of them.
func (a *app) ridingOn(m *machine) []string {
	var names []string
	for _, name := range a.machines.riding(m) {
		names = append(names, name)
		if rider := a.machines.named(name); rider != nil {
			names = append(names, a.ridingOn(rider)...)
		}
	}
	return names
}

// filePanesOn counts the panes of the file manager filed under a
// machine, a pane reading through a window of that name among them.
func (a *app) filePanesOn(host string) int {
	if a.files == nil {
		return 0
	}
	n := 0
	for _, p := range a.files.view.Panes() {
		if a.filedUnder(p, host) {
			n++
		}
	}
	return n
}

// howMany counts things, with the plural.
func howMany(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// closeWhatIsHeld closes whatever the window is holding under a name --
// another gridterm taken over, a connection, or a dial on its way --
// taking a window under the name the server list gave it rather than the
// one that was asked about.
func (a *app) closeWhatIsHeld(on hostFacts) error {
	switch {
	case on.window != nil:
		return a.dropWindow(on.window.name)
	case on.machine != nil:
		return a.dropMachine(on.name)
	case on.dialling != nil:
		a.machines.giveUp(on.dialling)
	}
	return nil
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
	f, err := a.here()
	if err != nil {
		return err
	}
	if !f.saved {
		return fmt.Errorf("%s is not in the server list", groupName(f.name))
	}
	return a.openEditServer(f.name)
}

// forgetThisServer takes the machine whose row was clicked out of the
// server list, once the user has said so.
//
// Asked from here rather than done: a server is a few minutes of typing
// and the list is the only record of it.
func (a *app) forgetThisServer() error {
	f, err := a.here()
	if err != nil {
		return err
	}
	if !f.saved {
		return fmt.Errorf("%s is not in the server list", groupName(f.name))
	}
	a.confirmRemoveServer(f.name)
	return nil
}

// folderCommandID names the command that opens a file browser at one of
// a machine's saved folders. By place in the list rather than by path: a
// path is not a command name and two of them may reduce to one.
func folderCommandID(host string, at int) string {
	return filesPrefix + remote.CommandName(host) + "." + strconv.Itoa(at+1)
}

// folderItems are the plus menu's lines for a machine with more than one
// folder saved, and none for a machine with one or none: one folder is
// where "Files" already opens.
func (a *app) folderItems(host string) []ui.MenuItem {
	folders := a.foldersOn(host)
	if len(folders) < 2 {
		return nil
	}
	items := make([]ui.MenuItem, 0, len(folders))
	for i, folder := range folders {
		id := folderCommandID(host, i)
		if _, ok := a.root.Commands.Lookup(id); !ok {
			// A line naming a command that is not there is drawn greyed
			// out and cannot be chosen, which reads as a fault.
			continue
		}
		items = append(items, ui.MenuItem{Command: id, Title: "Files in " + folder})
	}
	return items
}

// firstIdentity is the key file a saved server's dialog shows, and empty
// for one with none.
func firstIdentity(h remote.Host) string {
	if len(h.Identities) == 0 {
		return ""
	}
	return h.Identities[0]
}
