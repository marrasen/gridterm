package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// shellCommandPrefix is what a shell's command id starts with, so the
// commands registered for one scan can be told from the rest.
const shellCommandPrefix = "shell.open."

// shellPick is the shells a pane on this machine can run, the commands
// registered for them, and which one the user last chose.
//
// The goroutine that draws is the only one that touches it, apart from
// told: a window being served starts a shell from the server's
// goroutine.
type shellPick struct {
	// found are the shells the scan turned up, in the order a menu
	// offers them, and scan carries them from the goroutine that looked.
	// One whose command would not register is left out, because nothing
	// could open it.
	found []shells.Shell
	scan  chan foundShells

	// told records that the user has been told the shell they chose has
	// gone, which is said once a run.
	told atomic.Bool

	// remembered is which shell a new pane runs, kept between runs. Nil
	// until the window is given its settings.
	remembered *settings.Settings

	// findShells looks for the shells, and namedShell resolves one id.
	// They are fields so a test can describe a machine.
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
func (p *shellPick) remember(set *settings.Settings) { p.remembered = set }

// chosen is the id of the shell a new pane runs, and whether one was
// ever chosen.
func (p *shellPick) chosen() (string, bool) {
	if p.remembered == nil {
		return "", false
	}
	return p.remembered.Shell()
}

// choose writes down which shell was picked, for the next run.
func (p *shellPick) choose(id string) error {
	if p.remembered == nil {
		return nil
	}
	return p.remembered.PutShell(id)
}

// lines are the menu items that open a pane on each shell. titled puts
// the shell's own name on the line, for a menu whose heading already
// says a terminal is what opens.
//
// A machine with one shell gets none: there is nothing to choose
// between, and "Terminal" is still the way in.
func (p *shellPick) lines(titled bool) []ui.MenuItem {
	if len(p.found) < 2 {
		return nil
	}
	items := make([]ui.MenuItem, 0, len(p.found))
	for _, sh := range p.found {
		item := ui.MenuItem{Command: shellCommandID(sh.ID)}
		if titled {
			item.Title = sh.Title
		}
		items = append(items, item)
	}
	return items
}

// newSession starts the shell a new pane on this machine runs.
func (a *app) newSession(cols, rows int) (session.Session, error) {
	return a.newShell(a.localShell(), cols, rows)
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
	sh, ok := a.shellPick.namedShell(id)
	if !ok {
		a.sayShellHasGone(id)
		return nil
	}
	// No directory: nothing in the window tracks a pane's yet, and
	// Command leaves --cd off for an empty one.
	return sh.Command("")
}

// sayShellHasGone tells the user the shell they chose is no longer on
// this machine, once a run.
//
// Posted rather than shown, because this runs while the first pane is
// opening and there is no widget tree yet to show a notice in.
func (a *app) sayShellHasGone(id string) {
	if a.shellPick.told.Swap(true) {
		return
	}
	a.pump.post(func() {
		a.showNotice("The shell you chose is not on this machine",
			"A pane was to open on "+id+", which is no longer here, so the "+
				"default shell opened instead.", true)
	})
}

// openTabOn opens a tab here on a shell, and writes the pick down once
// it has started: a shell that would not start is not worth keeping.
//
// Here whatever new panes are opening on, because the row the line was
// chosen from is this machine.
func (a *app) openTabOn(sh shells.Shell) error {
	err := a.openTabWith(func() (*term.Terminal, error) {
		return a.localTerminalOn(sh.Command(""))
	})
	if err != nil {
		return err
	}
	if err := a.shellPick.choose(sh.ID); err != nil {
		a.reportError("Could not remember which shell to open", err)
	}
	return nil
}

// startShellScan looks for the shells a pane here can run, on a
// goroutine of its own, because wsl.exe takes a moment to answer.
func (a *app) startShellScan() {
	// The goroutine holds the channel rather than reading the field, so
	// that starting a second scan cannot race with the first one
	// finishing.
	found := make(chan foundShells, 1)
	a.shellPick.scan = found
	find := a.shellPick.findShells
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
	a.refreshFileMenu(a.shellPick.lines(false))
	a.reportShellScan(got.err)
}

// registerShells registers the command that opens a pane on each shell,
// and keeps the shells whose command took.
func (a *app) registerShells(list []shells.Shell) {
	a.shellPick.found = make([]shells.Shell, 0, len(list))
	for _, sh := range list {
		cmd := ui.Command{
			ID:    shellCommandID(sh.ID),
			Title: "New tab on " + sh.Title,
			Run:   func() error { return a.openTabOn(sh) },
		}
		if err := a.root.Commands.Register(a.reporting(cmd)); err != nil {
			// Two shells whose ids reduce to the same command id, or one
			// that collides with a command already registered. Neither is
			// worth losing the rest of the list over.
			a.logError(err)
			continue
		}
		a.shellPick.found = append(a.shellPick.found, sh)
	}
}

// reportShellScan says why the WSL distributions could not be listed,
// once the looking is over. The shells that were found are offered
// either way.
func (a *app) reportShellScan(err error) {
	if err == nil {
		return
	}
	a.logError(fmt.Errorf("listing the WSL distributions: %w", err))
	a.showNotice("Some shells could not be found",
		"The WSL distributions could not be listed, so none of them are offered:\n\n"+
			err.Error(), true)
}

// shellCommandID names the command that opens a pane on a shell. A
// shell's id holds a colon, spaces and stops; a command id has none of
// them, so that a menu or a key binding naming one is readable.
func shellCommandID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return shellCommandPrefix + b.String()
}
