package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
)

// currentHost returns the machine the user is looking at: the one the
// panel has selected while the panel has the keys, and otherwise the one
// the focused pane is running on.
//
// The panel only counts while it is focused. Its selection outlives
// being looked at -- it is still there when the panel is hidden -- and a
// command that opened a terminal on a machine the user chose ten minutes
// ago would be opening it somewhere they are not looking.
func (a *app) currentHost() string {
	// A menu dropped from a machine's row beats everything else: the
	// user named the machine by clicking it. A menu dropped from a
	// machine of a window taken over is not a name at all, and machine
	// says so: a.current is what answers for those.
	if host, up := a.hostMenus.machine(); up {
		return host
	}
	if a.panel != nil && a.panel.Focused() {
		if e, ok := a.selectedConnection(); ok {
			return e.Host
		}
	}
	if t := a.focusedTerminal(); t != nil {
		if m := a.machines.runningOn(t); m != nil {
			return m.at.name
		}
		if e := a.panes[t]; e != nil {
			return e.Host
		}
	}
	// A file pane is not a terminal, and the one with the keys is on a
	// machine like anything else.
	if p, ok := ui.FocusedLeaf(a.root.Widget()).(*files.Pane); ok {
		return a.hostOf(p.FS())
	}
	// Nothing in front to read it off, so it is wherever a new pane
	// would open: the machine -ssh named, or this one.
	return a.newPaneHost()
}

// current is the machine the user is looking at, as facts rather than as
// a name.
//
// A menu dropped from a machine of a window taken over is the one case a
// name cannot carry: that machine is reached through the window, and this
// window holds nothing under any name for it. So it comes back as
// hostFar, which every command but Files refuses -- structurally, rather
// than by a name that a saved server could be called as well.
func (a *app) current() hostFacts {
	if key, on := a.hostMenus.farMachine(); on {
		return hostFacts{a: a, name: key.host, kind: hostFar, far: key}
	}
	return a.about(a.currentHost())
}

// here is what the commands that act on the machine in front of the user
// work from: the facts, or the refusal for a machine reached through a
// window taken over.
func (a *app) here() (hostFacts, error) {
	h := a.current()
	if h.kind == hostFar {
		return h, farRefusal(h.far)
	}
	return h, nil
}

// farRefusal says a command cannot act on a machine of a window taken
// over, naming the machine and the window.
//
// One sentence for every command, because there is one reason: the shells
// and the tunnels on that machine belong to the window, and a pane
// reading its files is all this window can open over there.
func farRefusal(key remoteHostKey) error {
	return fmt.Errorf("%s is reached through %s: only its files can be opened from here",
		key.host, key.window.name)
}

// disconnectHere closes the connection to the machine the user is
// looking at, and everything riding on it.
func (a *app) disconnectHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	switch h.kind {
	case hostHere:
		return errors.New("this is the machine gridterm is running on, not one it connected to")
	case hostWindow:
		return a.dropWindow(h.name)
	case hostMachine, hostConnecting:
		return a.dropMachine(h.name)
	}
	// Said rather than done quietly. A command that reports success and
	// changes nothing is how a connection that would not close looked
	// like a window that had stopped listening.
	return fmt.Errorf("nothing is connected to %s", h.name)
}

// openTerminalHere opens another terminal on the machine the user is
// looking at.
func (a *app) openTerminalHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	return a.openTerminalOn(h.name, nil)
}

// openCommandHere asks for a command to run on the machine the user is
// looking at, connecting to it if the connection has since been closed.
func (a *app) openCommandHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	if h.kind == hostWindow || h.kind == hostSavedWindow {
		return fmt.Errorf(
			"%s is a gridterm window: it has no shell, so there is nothing to run a command in. "+
				"Open a terminal on it instead.",
			h.name)
	}
	a.askCommandOn(h.name, nil)
	return nil
}

// askCommandOn asks for a command to run on a machine and puts the pane
// it opens at a spot, or on the stage when at is nil.
func (a *app) askCommandOn(host string, at *spot) {
	where := groupName(host)
	f := a.newForm("Run a command on " + where)
	what := f.AddField("Command", a.newField("the program and its arguments", 0))
	what.Options = a.saved.lines()
	in := f.AddField("Directory", a.newField("where to run it, or leave it empty", 0))
	keep := f.AddTick("Remember this command", false)
	f.Lines = []string{
		"Ctrl+down and Ctrl+up step through the commands you have kept.",
		"Clearing the box on one of those forgets it.",
	}
	// picked is the saved command taken off the list, and "" until one
	// is. Only that one can be forgotten here, so a command typed out by
	// hand is never thrown away by a box the user did not tick.
	picked := ""
	// On a pick and not on a keystroke, or a saved command that is the
	// start of a longer one would tick the box half way through typing
	// it.
	what.OnPick = func(text string) {
		saved, have := a.saved.find(commandLine(text))
		if !have {
			return
		}
		picked = saved.Line
		keep.SetOn(true)
		// Only from the machine it was saved on, and only over an empty
		// field: a path belongs to its machine, and what the user typed
		// is theirs.
		if saved.Dir != "" && saved.Host == host && strings.TrimSpace(in.Text()) == "" {
			in.SetText(saved.Dir)
		}
	}
	f.AddButton(ui.Button{Title: "Run", Do: func() error {
		command := strings.Fields(what.Text())
		if len(command) == 0 {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return errors.New("there is nothing to run")
		}
		dir := strings.TrimSpace(in.Text())
		line := commandLine(what.Text())
		switch {
		case keep.On():
			cmd := settings.SavedCommand{Line: line, Dir: dir, Host: host}
			if err := a.saved.keep(cmd); err != nil {
				return err
			}
		case picked == line:
			// Taken off the list and then unticked, which is how the
			// user says to forget it.
			if err := a.saved.forget(line); err != nil {
				return err
			}
		}
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(func() {
			if err := a.runCommandOn(host, command, dir, at); err != nil {
				a.reportError("Could not run it on "+where, err)
			}
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
}

// runCommandOn opens a pane running a command, on this machine or on one
// the window connects to. dir is where it runs, and empty is wherever
// the shell lands.
func (a *app) runCommandOn(host string, command []string, dir string, at *spot) error {
	if host == conns.Local {
		return a.runCommandHere(command, dir, at)
	}
	return a.openOn(host, command, dir, at)
}

// showConnLogHere opens the account of how the machine the user is
// looking at was reached, or is being reached.
func (a *app) showConnLogHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	name := groupName(h.name)
	log := h.log()
	if log == nil {
		return fmt.Errorf("there is no account of how %s was reached", name)
	}
	a.showNotice(connLogTitle(name, h.kind), strings.Join(log.Lines(), "\n"), false)
	return nil
}

// connLogTitle names the account dialog, in the tense the connection is
// in.
func connLogTitle(name string, kind hostKind) string {
	if kind == hostConnecting {
		return "How " + name + " is being reached"
	}
	return "How " + name + " was reached"
}

// openFilesHere puts another pane in the file manager, on the machine
// the user is looking at.
func (a *app) openFilesHere() error {
	h := a.current()
	// A machine of a window taken over: the pane reads it through the
	// window, which is the only way this one can reach it. The one
	// command hostFar does not refuse.
	if h.kind == hostFar {
		return a.openFilesFar(h.far)
	}
	return a.openFilesOn(h.name)
}

// tunnelHost returns the machine to run a tunnel over: the one the user
// is looking at, if it is one gridterm has a connection to.
func (a *app) tunnelHost() (string, error) {
	on, err := a.here()
	if err != nil {
		return "", err
	}
	if on.machine == nil {
		return "", fmt.Errorf(
			"a tunnel runs over a connection to another machine, and %s is not one",
			groupName(on.name))
	}
	return on.name, nil
}

// openTunnelHere asks for a port to forward over the connection to the
// machine the user is looking at.
func (a *app) openTunnelHere() error {
	host, err := a.tunnelHost()
	if err != nil {
		return err
	}

	f := a.newForm("Tunnel over " + host)
	f.Lines = wrapLines("A port on one machine that stands for a service "+
		"the other one can reach. Listen here to reach a service on "+
		host+", or there to give "+host+" one of ours.", errorLineWidth)
	listen := f.AddField("Listen on", a.newField("[address:]port", 0))
	target := f.AddField("Reach", a.newField("host:port", 0))

	// The direction is on the buttons rather than in a field: which
	// machine listens is the whole of what a tunnel is, and a word for it
	// in a box would be one more thing to get wrong.
	open := func(kind remote.TunnelKind) func() error {
		return func() error {
			t := remote.Tunnel{
				Kind:   kind,
				Listen: strings.TrimSpace(listen.Text()),
				Target: strings.TrimSpace(target.Text()),
			}
			if err := t.Validate(); err != nil {
				// Returned rather than shown here, so the dialog stays
				// open with what was typed still there to correct.
				return err
			}
			// Not from here: this dialog closes as soon as this returns,
			// and closing one takes anything stacked on top of it.
			a.pump.post(func() { a.confirmTunnel(host, t) })
			return nil
		}
	}
	// The names stay short whatever the machine is called: a button whose
	// title carried the host name would be too wide to draw on a dialog
	// this size, and a button that is not drawn cannot be pressed.
	f.AddButton(ui.Button{Title: "Listen here", Do: open(remote.LocalForward)})
	f.AddButton(ui.Button{Title: "Listen there", Do: open(remote.RemoteForward)})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// openSocksHere asks for a SOCKS5 proxy over the connection to the
// machine the user is looking at.
//
// A dialog of its own rather than a third button on the tunnel one: it
// takes no address to reach, because every stream through it says where
// it is going.
func (a *app) openSocksHere() error {
	host, err := a.tunnelHost()
	if err != nil {
		return err
	}

	f := a.newForm("SOCKS proxy over " + host)
	f.Lines = wrapLines("A proxy on this machine that reaches whatever it is "+
		"asked for, as "+host+" sees it.", errorLineWidth)
	listen := f.AddField("Listen on", a.newField("[address:]port", 0))
	f.AddButton(ui.Button{Title: "Open", Do: func() error {
		t := remote.Tunnel{
			Kind:   remote.DynamicForward,
			Listen: strings.TrimSpace(listen.Text()),
		}
		if err := t.Validate(); err != nil {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return err
		}
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(func() { a.confirmTunnel(host, t) })
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}
