package main

import (
	"errors"
	"fmt"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/shellsetup"
)

// shellSetup is whether a shell on this machine is taught to say what
// it is doing, kept between runs.
type shellSetup struct {
	// remembered is where the answer is kept. Nothing is saved while it
	// is nil, and the answer is the one a fresh gridterm gives.
	remembered *settings.Settings
}

// newShellSetup builds a switch that remembers nothing until it is
// given the settings.
func newShellSetup() *shellSetup { return &shellSetup{} }

// remember gives the switch the settings it reads and writes.
func (p *shellSetup) remember(set *settings.Settings) { p.remembered = set }

// on reports whether a shell on this machine is taught.
//
// On until somebody turns it off. gridterm types the line in itself, so
// there is nothing to install, and without it a path a compiler printed
// is not clickable.
func (p *shellSetup) on() bool {
	return p.remembered == nil || p.remembered.ShellSetup()
}

// set turns it on or off and writes the answer down.
func (p *shellSetup) set(on bool) error {
	if p.remembered == nil {
		return errNoSettingsForSetup
	}
	return p.remembered.PutShellSetup(on)
}

// errNoSettingsForSetup is what a switch with no settings behind it
// answers.
var errNoSettingsForSetup = errors.New(
	"this window has no settings to keep the shell setup in")

// toggleShellSetup turns the shell setup on this machine on or off.
func (a *app) toggleShellSetup() error {
	on := !a.shellSetup.on()
	if err := a.shellSetup.set(on); err != nil {
		return err
	}
	what := "off"
	if on {
		what = "on"
	}
	// A line along the bottom: the switch is already over, and a dialog
	// saying so would be one to dismiss before the next pane can be
	// opened.
	a.say("Shell setup " + what + " — applies to new panes")
	return nil
}

// setupOn reports whether a shell on a machine is taught to say what it
// is doing.
//
// This machine answers from the settings. A saved server answers from
// its own row, because the line goes into whatever login shell that
// account has and only the user knows what that is. A machine the
// window reached by a typed target was never saved, so it has no row
// and no setup.
func (a *app) setupOn(host string) bool {
	if host == conns.Local {
		return a.shellSetup.on()
	}
	h, ok := a.book.Lookup(host)
	// Not another gridterm: the window over there starts the shell and
	// applies its own answer to this question.
	return ok && !h.Window && h.Setup
}

// teachPaneShell types the setup into the shell a pane has just
// started on, for a pane that runs a shell on a machine set to have it.
//
// Only a pane that runs a shell. A pane running one command has no
// prompt to hook and nothing would read the marks anyway, and the line
// would land in that command's input.
func (a *app) teachPaneShell(sess session.Session, host string, kind conns.Kind, argv []string) error {
	if kind != conns.Terminal || !a.setupOn(host) {
		return nil
	}
	if host == conns.Local && len(argv) == 0 {
		// The shell StartLocal picked, which is what says which kind of
		// shell this is. It started, so asking again answers.
		got, err := session.DefaultShell()
		if err != nil {
			return fmt.Errorf("find which shell this pane runs: %w", err)
		}
		argv = got
	}
	return a.teachShell(sess, host, argv)
}

// teachShell types the setup into a shell that has just started, for a
// machine set to have it.
//
// argv is what the pane runs, which says what kind of shell it is. An
// empty one is a login shell on a machine at the far end.
//
// Written into the shell's input rather than put on its command line:
// on a server the login shell is started by sshd, and gridterm never
// sees a command line to add to. The shell echoes it, so the last line
// typed clears the pane.
func (a *app) teachShell(sess session.Session, host string, argv []string) error {
	if !a.setupOn(host) {
		return nil
	}
	typed := shellsetup.Typed(shellsetup.RouteFor(argv))
	if len(typed) == 0 {
		return nil
	}
	if _, err := sess.Write(typed); err != nil {
		return fmt.Errorf("teach the shell on %s to say what it is doing: %w", groupName(host), err)
	}
	return nil
}
