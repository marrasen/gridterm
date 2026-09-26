package main

import (
	"fmt"
	"slices"
	"sync"

	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// shellPick is the shells a pane on this machine can run, the commands
// registered for them, and which one the user last chose.
//
// mu guards the fields under it: the goroutine that draws writes what
// the scan found, while a window being served reads it from the
// server's goroutine, one per session a client opens.
type shellPick struct {
	// scan carries the shells from the goroutine that looked, and
	// registered names the commands the last scan registered. Both
	// belong to the goroutine that draws.
	scan       chan foundShells
	registered []string

	mu sync.Mutex

	// found are the shells the scan turned up, in the order a menu
	// offers them, and cmds the command that opens each. One whose
	// command would not register is left out, because nothing could
	// open it.
	found []shells.Shell
	cmds  []string

	// scanned says the looking is over, which is when found becomes the
	// authority on what this machine has.
	scanned bool

	// told records that the user has been told the shell they chose has
	// gone, which is said once a run.
	told bool

	// remembered is which shell a new pane runs, kept between runs. Nil
	// until the window is given its settings.
	remembered *settings.Settings

	// findShells looks for the shells, and namedShell resolves one id
	// before the looking is over. They are fields so a test can
	// describe a machine.
	findShells func() ([]shells.Shell, error)
	namedShell func(id string) (shells.Shell, bool)
}

// foundShells is what the goroutine looking for the shells sends back.
type foundShells struct {
	shells []shells.Shell
	err    error
}

// newShellPick builds a shellPick that asks this machine.
func newShellPick() *shellPick {
	return &shellPick{findShells: shells.Find, namedShell: shells.Named}
}

// remember gives the window the settings the chosen shell is kept in.
func (p *shellPick) remember(set *settings.Settings) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remembered = set
}

// chosen is the id of the shell a new pane runs, and whether one was
// ever chosen.
func (p *shellPick) chosen() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.remembered == nil {
		return "", false
	}
	return p.remembered.Shell()
}

// choose writes down which shell was picked, for the next run, and lets
// the next shell that goes be said again.
func (p *shellPick) choose(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.told = false
	if p.remembered == nil {
		return nil
	}
	return p.remembered.PutShell(id)
}

// forget takes the pick away, so a new pane runs whatever the machine's
// own default is.
func (p *shellPick) forget() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.pickedLocked() {
		return nil
	}
	if err := p.remembered.ForgetShell(); err != nil {
		return err
	}
	p.told = false
	return nil
}

// resolve returns the shell an id names, and whether this machine
// still has it. What the scan found answers once it has landed, and
// namedShell before then, which is when the first pane opens.
func (p *shellPick) resolve(id string) (shells.Shell, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.scanned {
		return shells.Lookup(p.found, id)
	}
	return p.namedShell(id)
}

// titleOf is what a menu calls the shell an id names, and the id itself
// when nothing the scan found goes by it.
func (p *shellPick) titleOf(id string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sh, ok := shells.Lookup(p.found, id); ok {
		return sh.Title
	}
	return id
}

// take keeps what a scan turned up: the shells, and the command that
// opens a pane on each.
func (p *shellPick) take(list []shells.Shell, cmds []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.found, p.cmds, p.scanned = list, cmds, true
}

// list is the shells that were found, and nil when there are fewer than
// two, the way lines is.
func (p *shellPick) list() []shells.Shell {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.found) < 2 {
		return nil
	}
	return slices.Clone(p.found)
}

// running returns the shell an argv runs, of the ones that were found.
func (p *shellPick) running(argv []string) (shells.Shell, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return shells.Running(p.found, argv)
}

// landed says whether the looking is over.
func (p *shellPick) landed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scanned
}

// finder is the function that looks for the shells.
func (p *shellPick) finder() func() ([]shells.Shell, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.findShells
}

// tell reports whether the user has still to be told that a shell has
// gone, and takes the telling: it is said once a run.
func (p *shellPick) tell() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.told {
		return false
	}
	p.told = true
	return true
}

// lines are the menu items that open a pane on each shell. titled puts
// the shell's own name on the line, for a menu whose heading already
// says a terminal is what opens. A machine with one shell gets none.
func (p *shellPick) lines(titled bool) []ui.MenuItem {
	p.mu.Lock()
	defer p.mu.Unlock()
	var items []ui.MenuItem
	// A machine with one shell has nothing to pick between, but it can
	// still be carrying a pick for a shell that has since gone.
	if len(p.found) > 1 {
		items = make([]ui.MenuItem, 0, len(p.found)+1)
		for i, sh := range p.found {
			item := ui.MenuItem{Command: p.cmds[i]}
			if titled {
				item.Title = sh.Title
			}
			items = append(items, item)
		}
	}
	// And the way back, while a shell is picked.
	if p.pickedLocked() {
		item := ui.MenuItem{Command: defaultShellCommand}
		if titled {
			item.Title = defaultShellTitle
		}
		items = append(items, item)
	}
	return items
}

// pickedLocked reports whether a shell is picked, with p.mu already
// held.
func (p *shellPick) pickedLocked() bool {
	if p.remembered == nil {
		return false
	}
	_, picked := p.remembered.Shell()
	return picked
}

// localShell is the argv a new pane on this machine runs, and is nil
// when session.StartLocal is to pick the default.
func (a *app) localShell() []string {
	// What -e named beats anything remembered: it is what the window was
	// started with.
	if len(a.command) > 0 {
		return a.command
	}
	id, chosen := a.shellPick.chosen()
	if !chosen {
		return nil
	}
	sh, ok := a.shellPick.resolve(id)
	if !ok {
		a.sayShellHasGone(id)
		return nil
	}
	// Where the pane the user is in says it is, so a WSL shell opens
	// there. Empty when no shell has said, and Command leaves --cd off
	// for an empty one.
	return sh.Command(a.dirOfThePaneHere())
}

// sayShellHasGone tells the user the shell they chose is no longer on
// this machine, once a run. It posts the notice, because the first pane
// opens before there is a widget tree to show one in.
func (a *app) sayShellHasGone(id string) {
	if !a.shellPick.tell() {
		return
	}
	name := a.shellPick.titleOf(id)
	a.pump.post(func() {
		n := a.newNotice("Shell not found",
			name+" is no longer installed. The default shell was opened."+
				" Choose another under File › "+newTerminalInHeader+".")
		n.Failure = true
		// Nothing to copy: it is a sentence about a shell that has gone.
		n.SetNoCopy()
		a.presentNotice(n)
	})
}

// openPaneOn opens a pane here on a shell, and writes the pick down once
// it has started: a shell that would not start is not worth keeping.
func (a *app) openPaneOn(sh shells.Shell) error {
	// Read before the pane opens, while the one the user is in is
	// still the focused one.
	at := a.dirOfThePaneHere()
	err := a.openPaneWith(func() (*term.Terminal, error) {
		return a.localTerminalIn(sh.Command(at), at)
	})
	if err != nil {
		return err
	}
	a.rememberShell(sh)
	return nil
}

// defaultShellCommand opens a pane on whatever this machine's own
// default shell is, and forgets the shell that was picked.
const defaultShellCommand = "shell.default"

// defaultShellTitle names that line on a menu whose heading already says
// a terminal is what opens.
const defaultShellTitle = "Default shell"

// newTerminalInHeader is the File menu's heading over the shells.
//
// A constant because it is said twice: on the menu, and in the notice
// that sends somebody there when the shell they picked has gone.
const newTerminalInHeader = "New Terminal In"

// openPaneOnDefault opens a pane on this machine's own default shell and
// forgets the pick, so every pane after it opens on the default too.
func (a *app) openPaneOnDefault() error {
	if m := a.homeMachine(); m != nil {
		return fmt.Errorf("this window's panes open on %s, which picks its own shell", m.at.name)
	}
	err := a.openPaneWith(func() (*term.Terminal, error) {
		return a.localTerminalOn(nil)
	})
	if err != nil {
		return err
	}
	// Once it has started, the way rememberShell writes a pick down
	// once: a shell that would not start is not worth acting on. Said
	// rather than returned, so a settings file that cannot be written
	// does not cost the user the pane they asked for.
	if err := a.shellPick.forget(); err != nil {
		a.reportError("Could not save the settings", err)
	}
	// The File menu holds a copy of its lines, so the way back has to be
	// taken off it here rather than at the next scan.
	a.refreshFileMenu(a.fileMenuShells())
	return nil
}

// splitOnShell divides a pane with a new one running a named shell.
func (a *app) splitOnShell(dir ui.Dir, current ui.Widget, sh shells.Shell) error {
	at := a.dirOfThePaneHere()
	err := a.splitWithNew(dir, current, func() (*term.Terminal, error) {
		return a.localTerminalIn(sh.Command(at), at)
	})
	if err != nil {
		return err
	}
	a.rememberShell(sh)
	return nil
}

// rememberShell writes down which shell a pane was opened on, so the
// next one opens on it too.
func (a *app) rememberShell(sh shells.Shell) {
	if err := a.shellPick.choose(sh.ID); err != nil {
		a.reportError("Could not save the settings", err)
	}
	a.refreshFileMenu(a.fileMenuShells())
}

// startShellScan looks for the shells a pane here can run, on a
// goroutine of its own, because wsl.exe takes a moment to answer.
func (a *app) startShellScan() {
	// The goroutine holds the channel, so a second scan cannot race with
	// the first one finishing.
	found := make(chan foundShells, 1)
	a.shellPick.scan = found
	find := a.shellPick.finder()
	go func() {
		list, err := find()
		found <- foundShells{shells: list, err: err}
	}()
}

// reapShellScan registers a command per shell and puts the shells on the
// menus, once the scan has finished. It is called from the draw loop.
func (a *app) reapShellScan() {
	var got foundShells
	select {
	case got = <-a.shellPick.scan:
	default:
		return
	}
	a.registerShells(got.shells)
	a.refreshFileMenu(a.fileMenuShells())
	a.reportShellScan(got.err)
}

// registerShells registers the command that opens a pane on each shell,
// and keeps the shells whose command took.
func (a *app) registerShells(list []shells.Shell) {
	// Whatever the last scan registered goes first, or a shell that has
	// since gone would still answer to its command.
	for _, id := range a.shellPick.registered {
		a.root.Commands.Unregister(id)
	}
	a.shellPick.registered = nil

	ids := shells.CommandIDs(list)
	found := make([]shells.Shell, 0, len(list))
	cmds := make([]string, 0, len(list))
	for i, sh := range list {
		cmd := ui.Command{
			ID:    ids[i],
			Title: "New Terminal: " + sh.Title,
			Run:   func() error { return a.openPaneOn(sh) },
		}
		if err := a.root.Commands.Register(a.reporting(cmd)); err != nil {
			// An id something else is already registered under.
			a.logError(err)
			continue
		}
		a.shellPick.registered = append(a.shellPick.registered, ids[i])
		found = append(found, sh)
		cmds = append(cmds, ids[i])
	}
	a.shellPick.take(found, cmds)
}

// reportShellScan says why the WSL distributions could not be listed,
// once the looking is over. The shells that were found are offered
// either way.
func (a *app) reportShellScan(err error) {
	if err == nil {
		return
	}
	a.logError(fmt.Errorf("listing the WSL distributions: %w", err))
	a.showNotice("WSL shells unavailable",
		"Could not list the WSL distributions:\n\n"+err.Error(), true)
}
