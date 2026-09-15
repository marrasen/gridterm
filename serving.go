package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
)

// servePort is the port a window serves on unless the user says
// otherwise. Nothing standard, and nothing a scanner looks at first.
const servePort = 2222

// servePaths is where a serving window keeps its key and its list of
// who may connect.
//
// A field rather than a call to serve, so a test can point it at a
// directory of its own: the real one belongs to whoever is running
// gridterm, and a test has no business writing keys into it.
type servePaths struct{ hostKey, allowed string }

// servingPaths returns where this window keeps its serving files.
func (a *app) servingPaths() (servePaths, error) {
	if a.servePaths.hostKey != "" {
		return a.servePaths, nil
	}
	key, err := serve.HostKeyPath()
	if err != nil {
		return servePaths{}, err
	}
	at, err := serve.AuthorizedKeysPath()
	if err != nil {
		return servePaths{}, err
	}
	return servePaths{hostKey: key, allowed: at}, nil
}

// openServing asks whether to let another window take this one over.
//
// Off unless it is turned on, and turned on for this run only: a window
// that started serving because it did last time would be one that is
// serving without anybody having decided to today.
func (a *app) openServing() error {
	if a.server != nil {
		a.showServing()
		return nil
	}
	paths, err := a.servingPaths()
	if err != nil {
		return err
	}
	at := paths.allowed
	allowed, err := serve.LoadAllowed(at)
	if err != nil {
		return err
	}
	if allowed.Len() == 0 {
		// Said rather than offered. There is nothing to turn on: a port
		// that turns every caller away is worse than no port at all.
		return fmt.Errorf(
			"no keys are allowed to connect, so there is nobody to serve."+
				" Put the public key of the machine you will connect from in %s", at)
	}

	lines := []string{
		"Another gridterm can take this window over and work in it.",
		"",
		"These keys may connect:",
	}
	for _, name := range allowed.Names() {
		lines = append(lines, "  "+name)
	}
	lines = append(lines, "", "They are read from "+at+".")

	f := a.newForm("Serve this window")
	f.Lines = lines
	port := f.AddField("Port", a.newField("", 0))
	port.SetText(strconv.Itoa(servePort))
	reach := f.AddField("Reachable from", a.newField("", 0))
	reach.Options = []string{whereHere, whereAnywhere}
	reach.SetText(whereHere)
	f.Lines = append(f.Lines, "",
		"Reachable from: ctrl+down and ctrl+up choose. \""+whereAnywhere+"\" is"+
			" what a Tailscale address needs, and is also what every other"+
			" network can reach.")

	f.AddButton(ui.Button{Title: "Serve", Do: func() error {
		if err := a.startServing(port.Text(), reach.Text()); err != nil {
			return err
		}
		// Not from here: this form closes as soon as this returns, and
		// closing a dialog takes anything stacked on top of it.
		a.pump.post(func() {
			if err := a.showServing(); err != nil {
				a.reportError("Could not say what is being served", err)
			}
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// Where a window may be reached from, in the words the dialog uses.
const (
	whereHere     = "This machine only"
	whereAnywhere = "Anywhere this machine can be reached"
)

// listenHost is the address a choice from the dialog means.
//
// Empty means every address the machine has, which is what reaching it
// over Tailscale needs and is also what every other network can reach.
// Anything that is not that choice, exactly, is this machine only: the
// wider one has to be asked for.
func listenHost(where string) string {
	if where == whereAnywhere {
		return ""
	}
	return "127.0.0.1"
}

// startServing opens the port.
func (a *app) startServing(port, where string) error {
	if a.server != nil {
		// Two dialogs can be open at once, and a second listener would
		// take the place of the first in a window that then had no way
		// to reach it and no way to close it.
		return fmt.Errorf("this window is already being served on %s", a.server.Addr())
	}
	// Zero is allowed and means whichever port is free. The dialog says
	// which one that turned out to be, so it is discoverable rather
	// than lost.
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("%q is not a port number", strings.TrimSpace(port))
	}
	host := listenHost(where)

	paths, err := a.servingPaths()
	if err != nil {
		return err
	}
	hostKey, err := serve.HostKey(paths.hostKey)
	if err != nil {
		return err
	}
	allowed, err := serve.LoadAllowed(paths.allowed)
	if err != nil {
		return err
	}

	s, err := serve.Listen(serve.Config{
		Addr:    net.JoinHostPort(host, strconv.Itoa(n)),
		HostKey: hostKey,
		Allowed: allowed,
		// A shell on this machine, sized for the pane the other window
		// will draw it in. Started straight from session rather than
		// through the window's own panes: this is the machine being
		// worked on, not the one doing the drawing, and nothing here
		// touches the widget tree.
		Open: func(cols, rows int) (session.Session, error) {
			return session.StartLocal(session.LocalConfig{Cols: cols, Rows: rows})
		},
		// Every one of these arrives on a goroutine of the server's, so
		// they are handed to the one that draws.
		OnJoin: func(c *serve.Client) {
			a.pump.post(func() { a.clientArrived(c) })
		},
		OnGone: func(c *serve.Client, why error) {
			a.pump.post(func() { a.clientWent(c, why) })
		},
		OnStopped: func(err error) {
			a.pump.post(func() { a.servingStopped(err) })
		},
		OnError: func(err error) {
			a.pump.post(func() { a.logError(err) })
		},
	})
	if err != nil {
		return err
	}
	a.server = s
	return nil
}

// stopServing closes the port and hangs up on whoever is connected.
func (a *app) stopServing() error {
	if a.server == nil {
		return nil
	}
	s := a.server
	a.server = nil
	a.dropServedRows()
	a.markDirty()
	return s.Close()
}

// clientArrived is told when a window has taken this one over.
func (a *app) clientArrived(c *serve.Client) {
	// A row of its own on the panel. This machine is being worked in
	// from somewhere else, which is worth seeing at a glance and is
	// worth being able to end from here: whoever is sitting at this
	// screen owns it, however far away the person using it is.
	e := &conns.Entry{
		Host:  conns.Local,
		Kind:  conns.Terminal,
		Label: "serving " + c.Name,
		Note:  "from " + c.Addr,
		Close: func() error { return c.Close() },
	}
	a.served[c] = e
	a.registry.Add(e)
	a.markDirty()
}

// clientWent is told when it has gone, and why.
//
// The screen comes back to this machine and the port stays open: the
// user may well be moving from one machine to another. A client lost to
// a fault is reported, one that hung up is not -- the first is
// something the user did not ask for.
func (a *app) clientWent(c *serve.Client, why error) {
	if e := a.served[c]; e != nil {
		delete(a.served, c)
		a.registry.Drop(e)
	}
	a.markDirty()
	if why != nil && !errors.Is(why, io.EOF) {
		a.reportError("The window serving "+c.Name+" was lost", why)
	}
}

// servingStopped is told when the listener has failed, which is the end
// of it: no further client can connect.
//
// Said to the user rather than logged. A window that went on offering
// to stop serving, and on saying it was being served, would be lying
// about the one thing the user turned on deliberately.
func (a *app) servingStopped(err error) {
	a.server = nil
	a.dropServedRows()
	a.markDirty()
	a.reportError("This window is no longer being served", err)
}

// showServing says what the window is serving and offers to stop.
func (a *app) showServing() error {
	if a.server == nil {
		return nil
	}
	lines := []string{
		"This window is being served on " + a.server.Addr() + ".",
		"",
		"Check this machine by its fingerprint when you first connect:",
		"  " + serve.Fingerprint(a.server.HostKey()),
	}
	// What is connected as the dialog opens. It says so once rather
	// than following: a dialog is read and answered, and one that
	// rewrote itself under the reader would be harder to trust, not
	// easier.
	if clients := a.server.Clients(); len(clients) > 0 {
		lines = append(lines, "", "Connected now:")
		for _, c := range clients {
			lines = append(lines, "  "+c.Name+" from "+c.Addr)
		}
	} else {
		lines = append(lines, "", "Nobody is connected.")
	}

	f := a.newConfirm("Serving this window", lines)
	f.AddButton(ui.Button{Title: "Keep serving"})
	f.AddButton(ui.Button{Title: "Stop serving", Do: a.stopServing})
	a.showForm(f, nil)
	return nil
}

// dropServedRows takes away the rows for the windows that were being
// served. Nothing is being served any more, so nothing of theirs is
// left on the panel to close.
func (a *app) dropServedRows() {
	for c, e := range a.served {
		delete(a.served, c)
		a.registry.Drop(e)
	}
}
