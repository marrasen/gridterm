package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/sftp"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
)

// servePort is the port a window serves on unless the user says
// otherwise. Nothing standard, and nothing a scanner looks at first.
const servePort = remote.ServePort

// servePaths is where a serving window keeps its key and its list of
// who may connect.
//
// A field rather than a call to serve, so a test can point it at a
// directory of its own: the real one belongs to whoever is running
// gridterm, and a test has no business writing keys into it.
type servePaths struct{ hostKey, allowed string }

// serving is the listener letting another window take this one over,
// the rows for the windows that have, and what they were last told is
// open here.
//
// Only the goroutine that draws touches it, with one exception: opens
// is read from the goroutines serving clients, and the snapshot it
// gives back holds a lock of its own.
type serving struct {
	// server is nil when the window is not being served. Off unless the
	// user turns it on, and for this run only.
	server *serve.Server

	// paths points at the key and the list of who may connect, empty in
	// the program and set by a test to a directory of its own.
	paths servePaths

	// rows are the panel rows for the windows working in this one, by
	// the client each stands for.
	rows map[*serve.Client]*conns.Entry

	// last is what this window last told its clients it had open, so an
	// unchanged one is not sent again.
	last serve.Snapshot

	// openNow is the copy of that the goroutines serving clients read.
	openNow shared
}

// newServing builds a serving with nothing listening and no rows for
// clients.
func newServing() *serving {
	return &serving{rows: make(map[*serve.Client]*conns.Entry)}
}

// on reports whether the window is being served.
func (s *serving) on() bool { return s.server != nil }

// addr is the address the window is served on, empty when it is not.
func (s *serving) addr() string {
	if s.server == nil {
		return ""
	}
	return s.server.Addr()
}

// fingerprint is what a window connecting here checks this machine by,
// empty when nothing is listening.
func (s *serving) fingerprint() string {
	if s.server == nil {
		return ""
	}
	return serve.Fingerprint(s.server.HostKey())
}

// clients are the windows connected right now, none when nothing is
// listening.
func (s *serving) clients() []*serve.Client {
	if s.server == nil {
		return nil
	}
	return s.server.Clients()
}

// usePaths points the key and the allowed list at a directory of a
// test's own.
func (s *serving) usePaths(p servePaths) { s.paths = p }

// where is the key and the list of who may connect this window serves
// with.
func (s *serving) where() (servePaths, error) {
	if s.paths.hostKey != "" {
		return s.paths, nil
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

// listen opens the port, and fails when one is already open.
func (s *serving) listen(cfg serve.Config) error {
	if s.server != nil {
		return fmt.Errorf("this window is already being served on %s", s.addr())
	}
	srv, err := serve.Listen(cfg)
	if err != nil {
		return err
	}
	s.server = srv
	return nil
}

// close stops serving and hands back the server, for the caller to hang
// up on whoever is connected. It is nil when nothing was listening.
func (s *serving) close() *serve.Server {
	srv := s.server
	s.server = nil
	return srv
}

// lost records that the listener has failed, which is the end of it: no
// further client can connect.
func (s *serving) lost() { s.server = nil }

// joined records the row for a window that has taken this one over.
func (s *serving) joined(c *serve.Client, e *conns.Entry) { s.rows[c] = e }

// left takes away one window's row and gives it back, or nil when that
// window had none.
func (s *serving) left(c *serve.Client) *conns.Entry {
	e := s.rows[c]
	if e != nil {
		delete(s.rows, c)
	}
	return e
}

// dropRows takes away the rows for every window being served and gives
// them back, for the caller to take off the panel.
func (s *serving) dropRows() []*conns.Entry {
	out := make([]*conns.Entry, 0, len(s.rows))
	for c, e := range s.rows {
		delete(s.rows, c)
		out = append(out, e)
	}
	return out
}

// publish tells the clients what this window has open, when that has
// changed since the last time they were told.
func (s *serving) publish(snap serve.Snapshot) {
	if s.server == nil || sameSnapshot(s.last, snap) {
		return
	}
	s.last = snap
	s.openNow.set(snap)
	s.server.Publish(snap)
}

// opens is the last snapshot the clients were sent, read from the
// goroutines serving them.
func (s *serving) opens() serve.Snapshot { return s.openNow.get() }

// openServing asks whether to let another window take this one over.
//
// Off unless it is turned on, and turned on for this run only: a window
// that started serving because it did last time would be one that is
// serving without anybody having decided to today.
func (a *app) openServing() error {
	if a.serving.on() {
		a.showServing()
		return nil
	}
	paths, err := a.serving.where()
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
		// Said plainly. A shell on this machine already reaches every
		// file this user can reach, so the files are no more than the
		// shell was; but somebody deciding whether to open a port has
		// to be told what goes through it.
		"It can open shells here, work in the ones already running,",
		"and read and write this machine's files as you.",
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
	if a.serving.on() {
		// Two dialogs can be open at once, and a second listener would
		// take the place of the first in a window that then had no way
		// to reach it and no way to close it.
		return fmt.Errorf("this window is already being served on %s", a.serving.addr())
	}
	// Zero is allowed and means whichever port is free. The dialog says
	// which one that turned out to be, so it is discoverable rather
	// than lost.
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("%q is not a port number", strings.TrimSpace(port))
	}
	host := listenHost(where)

	paths, err := a.serving.where()
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

	return a.serving.listen(serve.Config{
		Addr:    net.JoinHostPort(host, strconv.Itoa(n)),
		HostKey: hostKey,
		Allowed: allowed,
		// The files of this machine, which the connection already
		// reaches through the shell below.
		Files: serveFiles,
		// What this window has open, for a client that wants to see it.
		// Read from the goroutine serving that client, so it goes
		// through the same snapshot the panel was built from rather
		// than walking the registry from there.
		Opens: a.serving.opens,
		// What is already running here, so a window taken over shows
		// the shell that was left running rather than only new ones.
		Attach: a.attachTo,
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
}

// stopServing closes the port and hangs up on whoever is connected.
func (a *app) stopServing() error {
	if !a.serving.on() {
		return nil
	}
	srv := a.serving.close()
	a.dropServedRows()
	a.markDirty()
	return srv.Close()
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
	a.serving.joined(c, e)
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
	if e := a.serving.left(c); e != nil {
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
	a.serving.lost()
	a.dropServedRows()
	a.markDirty()
	a.reportError("This window is no longer being served", err)
}

// showServing says what the window is serving and offers to stop.
func (a *app) showServing() error {
	if !a.serving.on() {
		return nil
	}
	lines := []string{
		"This window is being served on " + a.serving.addr() + ".",
		"",
		"Check this machine by its fingerprint when you first connect:",
		"  " + a.serving.fingerprint(),
	}
	// What is connected as the dialog opens. It says so once rather
	// than following: a dialog is read and answered, and one that
	// rewrote itself under the reader would be harder to trust, not
	// easier.
	if clients := a.serving.clients(); len(clients) > 0 {
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
	for _, e := range a.serving.dropRows() {
		a.registry.Drop(e)
	}
}

// serveFiles gives a client the files of this machine, as SFTP on the
// channel it was handed.
//
// It runs on a goroutine of the server's and touches nothing the window
// holds.
func serveFiles(ch io.ReadWriteCloser) error {
	// The channel is not this function's to close: whoever handed it
	// over closes it once, and an SFTP server closes what it was given
	// as it goes. Left alone, the two would close it twice and the
	// second would have to be told not to mind.
	// Where the user lives, so a pane on this machine opens there. A
	// file session starts in the serving process's own directory
	// otherwise, which is wherever the window happened to be launched
	// from.
	options := []sftp.ServerOption{
		// A Windows machine has drives rather than one root. Without
		// this, "/" is whatever drive this process happens to be on and
		// the others cannot be reached by going up.
		sftp.WindowsRootEnumeratesDrives(),
	}
	if home, err := os.UserHomeDir(); err == nil {
		options = append(options, sftp.WithServerWorkingDirectory(home))
	}
	srv, err := sftp.NewServer(keptOpen{ch}, options...)
	if err != nil {
		return fmt.Errorf("could not serve the files of this machine: %w", err)
	}
	served := srv.Serve()
	if errors.Is(served, io.EOF) {
		served = nil
	}
	// Closed after serving, and its failure said: it lets go of every
	// file the session left open.
	if err := errors.Join(served, srv.Close()); err != nil {
		return fmt.Errorf("serving the files of this machine: %w", err)
	}
	return nil
}

// keptOpen is a stream whose close does nothing, for handing to
// something that closes what it is given when the caller needs it after.
type keptOpen struct{ io.ReadWriteCloser }

func (keptOpen) Close() error { return nil }
