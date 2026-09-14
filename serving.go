package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/serve"
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
	port := f.AddField("Port", a.newField(strconv.Itoa(servePort), 0))
	reach := f.AddField("Reachable from", a.newField("", 0))
	reach.Options = []string{whereHere, whereAnywhere}
	reach.SetText(whereHere)
	f.Lines = append(f.Lines, "",
		"Reachable from: ctrl+down and ctrl+up choose. \""+whereAnywhere+"\" is"+
			" what a Tailscale address needs, and is also what every other"+
			" network can reach.")

	f.AddButton(ui.Button{Title: "Serve", Do: func() error {
		return a.startServing(port.Text(), reach.Text())
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

// startServing opens the port.
func (a *app) startServing(port, where string) error {
	// Zero is allowed and means whichever port is free. The dialog says
	// which one that turned out to be, so it is discoverable rather
	// than lost.
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("%q is not a port number", strings.TrimSpace(port))
	}
	host := "127.0.0.1"
	if where == whereAnywhere {
		// Empty means every address, which is what reaching a machine
		// over Tailscale needs. Written out rather than left implied,
		// because it is the choice that matters.
		host = ""
	}

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
		// Both arrive on the goroutine serving a client, so they are
		// handed to the one that draws.
		OnClient: func(c *serve.Client, gone bool) {
			a.pump.post(func() { a.servingChanged(c, gone) })
		},
		OnError: func(err error) {
			a.pump.post(func() { a.logError(err) })
		},
	})
	if err != nil {
		return err
	}
	a.server = s
	a.showServing()
	return nil
}

// stopServing closes the port and hangs up on whoever is connected.
func (a *app) stopServing() error {
	if a.server == nil {
		return nil
	}
	s := a.server
	a.server = nil
	a.markDirty()
	return s.Close()
}

// servingChanged is told when a client arrives or goes.
func (a *app) servingChanged(c *serve.Client, gone bool) {
	if a.server == nil {
		return
	}
	// The screen comes back to this machine when the client goes, and
	// the port stays open: the user may well be moving from one machine
	// to another.
	a.markDirty()
	if gone {
		return
	}
	_ = c
}

// showServing says what the window is serving and offers to stop.
func (a *app) showServing() error {
	if a.server == nil {
		return nil
	}
	paths, err := a.servingPaths()
	if err != nil {
		return err
	}
	hostKey, err := serve.HostKey(paths.hostKey)
	if err != nil {
		return err
	}

	lines := []string{
		"This window is being served on " + a.server.Addr() + ".",
		"",
		"Check this machine by its fingerprint when you first connect:",
		"  " + serve.Fingerprint(hostKey.PublicKey()),
	}
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
