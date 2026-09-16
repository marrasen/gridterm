package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/remote"
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
	// user named the machine by clicking it.
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
	return a.localHost
}

// disconnectHere closes the connection to the machine the user is
// looking at, and everything riding on it.
func (a *app) disconnectHere() error {
	h := a.about(a.currentHost())
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
func (a *app) openTerminalHere() error { return a.openTerminalOn(a.currentHost(), nil) }

// openCommandHere asks for a command to run on the machine the user is
// looking at, connecting to it if the connection has since been closed.
func (a *app) openCommandHere() error {
	h := a.about(a.currentHost())
	if h.kind == hostHere {
		return errors.New(
			"a command runs on a machine gridterm connected to, and this is the one it is running on")
	}
	if h.kind == hostWindow || h.kind == hostSavedWindow {
		return fmt.Errorf(
			"%s is a gridterm window: it has no shell, so there is nothing to run a command in. "+
				"Open a terminal on it instead.",
			h.name)
	}
	host := h.name
	f := a.newForm("Run a command on " + host)
	what := f.AddField("Command", a.newField("the program and its arguments", 0))
	f.AddButton(ui.Button{Title: "Run", Do: func() error {
		command := strings.Fields(what.Text())
		if len(command) == 0 {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return errors.New("there is nothing to run")
		}
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(func() {
			if err := a.openOn(host, command, nil); err != nil {
				a.reportError("Could not run it on "+host, err)
			}
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// showConnLogHere opens the account of how the machine the user is
// looking at was reached.
func (a *app) showConnLogHere() error {
	h := a.about(a.currentHost())
	log := h.log()
	if log == nil {
		return fmt.Errorf("there is no account of how %s was reached", groupName(h.name))
	}
	a.showNotice("How "+h.name+" was reached", strings.Join(log.Lines(), "\n"), false)
	return nil
}

// openFilesHere puts another pane in the file manager, on the machine
// the user is looking at.
func (a *app) openFilesHere() error { return a.openFilesOn(a.currentHost()) }

// tunnelHost returns the machine to run a tunnel over: the one the user
// is looking at, if it is one gridterm has a connection to.
func (a *app) tunnelHost() (string, error) {
	on := a.about(a.currentHost())
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
