package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/settings"
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

	// remembered is what the serve dialog was last set to, kept between
	// runs. Nil until the window is given its settings.
	remembered *settings.Settings

	// at is the address the listener was opened on, empty when nothing is
	// listening. Kept here so a frame can ask without taking the
	// server's own lock.
	at string

	// came counts the windows that have joined or left, so a frame can
	// tell one client from another of the same name without holding on
	// to either.
	came uint64

	// abandoned counts the relayed file sessions parked on each machine
	// right now, by the connection they are parked on rather than by the
	// name it is held under: a rename moves the name and leaves the
	// relays where they are. Each one holds a goroutine and an SSH
	// channel until the machine answers the close or the connection to it
	// goes, so a machine that stopped answering is not asked for any more
	// of them.
	abandoned map[*remote.Conn]int
}

// newServing builds a serving with nothing listening and no rows for
// clients.
func newServing() *serving {
	return &serving{
		rows:      make(map[*serve.Client]*conns.Entry),
		abandoned: make(map[*remote.Conn]int),
	}
}

// mostAbandonedRelays is how many file sessions may be parked on one
// machine at once before this window turns the next one away.
//
// Low, because each one is a goroutine and a channel on a machine that is
// already not answering, and because a browser opens one per pane: a
// client whose panes keep landing on a dead machine would otherwise pile
// them up until that connection is closed.
const mostAbandonedRelays = 4

// relayAbandoned counts one more file session parked on a connection.
func (s *serving) relayAbandoned(conn *remote.Conn) { s.abandoned[conn]++ }

// relayStopped counts one fewer, for a parked file session that has ended
// after all. A connection at nothing is forgotten rather than kept at
// zero.
func (s *serving) relayStopped(conn *remote.Conn) {
	if n := s.abandoned[conn]; n > 1 {
		s.abandoned[conn] = n - 1
		return
	}
	delete(s.abandoned, conn)
}

// abandonedRelays is how many file sessions are parked on a connection
// now.
func (s *serving) abandonedRelays(conn *remote.Conn) int { return s.abandoned[conn] }

// relaysEnded forgets what was parked on a connection that has closed:
// closing it ends every one of them.
func (s *serving) relaysEnded(conn *remote.Conn) { delete(s.abandoned, conn) }

// on reports whether the window is being served.
func (s *serving) on() bool { return s.server != nil }

// addr is the address the window is served on, empty when it is not.
func (s *serving) addr() string { return s.at }

// joined is how many windows are working in this one, which is one row
// each.
func (s *serving) joined() int { return len(s.rows) }

// changes counts the windows that have come and gone, for a caller
// telling one client from another under the same name.
func (s *serving) changes() uint64 { return s.came }

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

// remember gives the window the settings the serve dialog opens on and
// writes itself back to.
func (s *serving) remember(set *settings.Settings) { s.remembered = set }

// startPort is the port the serve dialog opens on: the one last served
// with, or the default.
func (s *serving) startPort() int {
	if s.remembered == nil {
		return servePort
	}
	if port, saved := s.remembered.ServePort(); saved {
		return port
	}
	return servePort
}

// startReach is how far the serve dialog opens on being reachable from,
// in the words the dialog uses.
func (s *serving) startReach() string {
	if s.remembered == nil {
		return whereHere
	}
	saved, have := s.remembered.ServeReach()
	if !have {
		return whereHere
	}
	return reachWords(saved)
}

// rememberServe writes down what the dialog was set to, for the next run.
func (s *serving) rememberServe(port int, where string) error {
	if s.remembered == nil {
		return nil
	}
	return s.remembered.PutServe(port, reachSaved(where))
}

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
	s.server, s.at = srv, srv.Addr()
	return nil
}

// close stops serving and hands back the server, for the caller to hang
// up on whoever is connected. It is nil when nothing was listening.
func (s *serving) close() *serve.Server {
	srv := s.server
	s.server, s.at = nil, ""
	return srv
}

// lost records that the listener has failed, which is the end of it: no
// further client can connect.
func (s *serving) lost() { s.server, s.at = nil, "" }

// arrived records the row for a window that has taken this one over.
func (s *serving) arrived(c *serve.Client, e *conns.Entry) {
	s.rows[c] = e
	s.came++
}

// left takes away one window's row and gives it back, or nil when that
// window had none.
func (s *serving) left(c *serve.Client) *conns.Entry {
	s.came++
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
// serving without anybody having decided to today. What the dialog was
// last set to is remembered; whether it was answered is not.
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
	port.SetText(strconv.Itoa(a.serving.startPort()))
	reach := f.AddField("Reachable from", a.newField("", 0))
	reach.Options = []string{whereHere, whereAnywhere}
	reach.SetText(a.serving.startReach())
	f.Lines = append(f.Lines, "",
		"Port: 0 asks for whichever port is free.",
		"",
		"Reachable from: ctrl+down and ctrl+up choose. \""+whereAnywhere+"\" is"+
			" what a Tailscale address needs, and is also what every other"+
			" network can reach.")

	f.AddButton(ui.Button{Title: "Serve", Do: func() error {
		asked, where := port.Text(), reach.Text()
		if err := a.startServing(asked, where); err != nil {
			return err
		}
		// Not from here: this form closes as soon as this returns, and
		// closing a dialog takes anything stacked on top of it.
		a.pump.post(func() {
			if err := a.showServing(); err != nil {
				a.reportError("Could not say what is being served", err)
			}
			// After that dialog, so a failure to write the settings
			// down lands on top of it rather than underneath. The window
			// goes on serving either way.
			if err := a.rememberServing(asked, where); err != nil {
				a.reportError("Could not remember what the serve dialog was set to", err)
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

// reachWords is the dialog's wording for a reach that was written down.
//
// Anything but the wider one, exactly, reads back as this machine only:
// the wider one has to be asked for.
func reachWords(saved string) string {
	if saved == settings.ReachAnywhere {
		return whereAnywhere
	}
	return whereHere
}

// reachSaved is how a choice from the dialog is written down.
func reachSaved(where string) string {
	if where == whereAnywhere {
		return settings.ReachAnywhere
	}
	return settings.ReachHere
}

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

// portAsked is the port number the dialog was set to.
//
// Zero is allowed and means whichever port is free. The dialog says
// which one that turned out to be, so it is discoverable rather than
// lost.
func portAsked(port string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || n < 0 || n > 65535 {
		return 0, fmt.Errorf("%q is not a port number", strings.TrimSpace(port))
	}
	return n, nil
}

// rememberServing writes down what the serve dialog was set to, for the
// next run. The port goes down as it was typed, so 0 goes on meaning
// whichever port is free.
func (a *app) rememberServing(port, where string) error {
	n, err := portAsked(port)
	if err != nil {
		return err
	}
	return a.serving.rememberServe(n, where)
}

// startServing opens the port.
func (a *app) startServing(port, where string) error {
	if a.serving.on() {
		// Two dialogs can be open at once, and a second listener would
		// take the place of the first in a window that then had no way
		// to reach it and no way to close it.
		return fmt.Errorf("this window is already being served on %s", a.serving.addr())
	}
	n, err := portAsked(port)
	if err != nil {
		return err
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

	cfg := serve.Config{
		Addr:    net.JoinHostPort(host, strconv.Itoa(n)),
		HostKey: hostKey,
		Allowed: allowed,
		// The files of this machine, and of the machines this window is
		// connected to, which the client cannot reach for itself.
		Files: a.serveFiles,
		// What this window has open, for a client that wants to see it.
		// Read from the goroutine serving that client, so it goes
		// through the same snapshot the panel was built from rather
		// than walking the registry from there.
		Opens: a.serving.opens,
		// What is already running here, so a window taken over shows
		// the shell that was left running rather than only new ones.
		Attach: a.attachTo,
		// A shell on this machine, sized for the pane the other window
		// will draw it in. The same shell a pane here starts, without a
		// pane: this is the machine being worked on, not the one doing
		// the drawing, and nothing here touches the widget tree.
		Open: a.newSession,
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
	}
	return a.serving.listen(cfg)
}

// useSettings gives the window what it remembers between runs, and says
// on its first frame when the settings could not be read.
func (a *app) useSettings(set *settings.Settings) {
	a.serving.remember(set)
	a.agents.remember(set)
	err := set.Err()
	if err == nil {
		return
	}
	a.pump.post(func() {
		a.reportError("The settings could not be read", errors.Join(err,
			errors.New("gridterm will not write over them until they are repaired")))
	})
}

// openSettings reads the settings file, and gives back settings that
// remember nothing and refuse to save when it could not be read.
func openSettings() *settings.Settings {
	path, err := settings.Path()
	if err != nil {
		return settings.Unusable(err)
	}
	set, err := settings.Load(path)
	if err != nil {
		// Kept on the settings and shown in the window rather than only
		// here: a window opened from an icon has no console to read.
		log.Print(err)
	}
	return set
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
	a.serving.arrived(c, e)
	a.registry.Add(e)
	a.markDirty()
}

// clientWent is told when it has gone, and why.
//
// The screen comes back to this machine and the port stays open: the
// user may well be moving from one machine to another. A client lost to
// a fault is reported, one that hung up is not -- the first is something
// the user did not ask for.
//
// A kick is not either: it closes that client's socket from here, so this
// window's own read of it fails with net.ErrClosed.
func (a *app) clientWent(c *serve.Client, why error) {
	if e := a.serving.left(c); e != nil {
		a.registry.Drop(e)
	}
	a.markDirty()
	if why != nil && !errors.Is(why, io.EOF) && !errors.Is(why, net.ErrClosed) {
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
	clients := a.serving.clients()
	if len(clients) > 0 {
		lines = append(lines, "", "Connected now:")
		for _, c := range clients {
			lines = append(lines, "  "+c.Name+" from "+c.Addr)
		}
	} else {
		lines = append(lines, "", "Nobody is connected.")
	}

	f := a.newConfirm("Serving this window", lines)
	f.AddButton(ui.Button{Title: "Keep serving"})
	if len(clients) > 0 {
		f.AddButton(ui.Button{
			Title: kickTitle(clients),
			// The windows the dialog named, not whoever is connected
			// when the button is pressed: the user answered the list
			// they were shown.
			Do: func() error { return a.kickOut(clients) },
		})
	}
	f.AddButton(ui.Button{Title: "Stop serving", Do: a.stopServing})
	a.showForm(f, nil)
	return nil
}

// kickTitle is what the button that hangs up on the connected windows
// says, naming the one client there is.
func kickTitle(clients []*serve.Client) string {
	if len(clients) == 1 {
		return "Kick " + clients[0].Name + " out"
	}
	return "Kick everyone out"
}

// kickOut hangs up on the windows given. The port stays open, so the
// same person can connect again.
//
// A window that hung up by itself between the dialog and the press is
// not a failed kick: closing its socket again says it has already gone,
// and it has gone, which is what was asked for. A press that reached
// none of them while somebody is still working here is said, because the
// dialog would otherwise close on a kick that kicked nobody.
func (a *app) kickOut(clients []*serve.Client) error {
	live := make(map[*serve.Client]bool, len(clients))
	for _, c := range a.serving.clients() {
		live[c] = true
	}
	var errs []error
	kicked := 0
	for _, c := range clients {
		if live[c] {
			kicked++
		}
		// An end of file is a window that has already gone too, which
		// some transports say that way rather than with net.ErrClosed.
		if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			errs = append(errs, err)
		}
	}
	if kicked == 0 {
		errs = append(errs, missedTheKick(clients, a.serving.clients()))
	}
	return errors.Join(errs...)
}

// missedTheKick says a kick reached none of the windows it named while
// somebody is still working in this one.
//
// Nil when nobody is connected: every window the dialog listed has gone,
// which is what the button was pressed for. A window that connected again
// under a name the dialog listed is the one worth naming, because the
// dialog and the panel both go on showing that name.
func missedTheKick(named, now []*serve.Client) error {
	if len(now) == 0 {
		return nil
	}
	for _, c := range named {
		for _, live := range now {
			if live.Name == c.Name {
				return fmt.Errorf("%s had already gone and has connected again since,"+
					" so nothing was kicked out. Open the dialog again to kick the window connected now",
					c.Name)
			}
		}
	}
	return fmt.Errorf("the windows named had already gone, and %s is connected now,"+
		" so nothing was kicked out", now[0].Name)
}

// dropServedRows takes away the rows for the windows that were being
// served. Nothing is being served any more, so nothing of theirs is
// left on the panel to close.
func (a *app) dropServedRows() {
	for _, e := range a.serving.dropRows() {
		a.registry.Drop(e)
	}
}

// serveFiles gives a client the files of a machine this window can
// reach, as SFTP on the channel it was handed.
//
// This machine's own name, which is empty, is served from here. Any
// other name is whatever the client asked for, and the bytes are
// relayed over this window's connection to it.
//
// It runs on a goroutine of the server's, so what it needs from the
// window is asked for on the goroutine that draws. client ends when that
// client's connection has finished.
func (a *app) serveFiles(client context.Context, host string, ch io.ReadWriteCloser) error {
	if host == conns.Local {
		return serveLocalFiles(ch)
	}
	return a.relayFiles(client, host, ch)
}

// relayFiles carries a client's file session to a machine this window is
// connected to, over the connection it already holds.
//
// Nothing is read here: this window only passes the bytes along, so the
// client speaks SFTP to the machine rather than to this window. client
// ends when that client's connection has finished, which is what says
// whether anybody is still waiting for the machine to answer.
func (a *app) relayFiles(client context.Context, host string, ch io.ReadWriteCloser) error {
	conn, err := a.connectionTo(host)
	if err != nil {
		return err
	}
	relay, err := conn.FileSubsystem(a.ctx)
	if err != nil {
		return fmt.Errorf("could not open a file session on %s: %w", host, err)
	}

	// What the client sends, on to the machine. The client going is an
	// end of file here.
	sent := make(chan error, 1)
	go func() {
		_, err := io.Copy(relay, ch)
		if errors.Is(err, io.EOF) {
			err = nil
		}
		sent <- err
	}()

	// And what the machine says, back to the client. It ends when the
	// machine's session does and when the connection to it closes.
	back := make(chan error, 1)
	go func() {
		_, err := io.Copy(ch, relay)
		if errors.Is(err, io.EOF) {
			err = nil
		}
		back <- err
	}()

	var fromClient, fromMachine error
	machineDone := false
	select {
	case fromClient = <-sent:
	case fromMachine = <-back:
		machineDone = true
		// Waited for rather than asked about on the spot: a client that
		// closed at the same moment as the machine is its own close, and
		// which of the two copies finishes first is a race.
		select {
		case fromClient = <-sent:
			// Both ended together, which the client going accounts for:
			// a session the client closed itself is not the machine
			// ending under it.
		case <-time.After(relayTogether):
			// The machine's end came first and the client is still there.
			// The copy from the client is parked on a read of the client's
			// channel, and closing that channel only sends the client a
			// close: the read ends when the client answers it or when the
			// connection to the client goes. So its account is waited for
			// off this goroutine and logged there, and the client is told
			// here.
			closed := relay.Close()
			go func() {
				if err := errors.Join(<-sent, closed); err != nil {
					a.pump.post(func() {
						a.logError(fmt.Errorf("carrying a file session to %s: %w", host, err))
					})
				}
			}()
			return errors.Join(fromMachine, relayEnded(conn, host))
		}
	}

	// The client is finished, which is how a file session usually ends.
	// Closing the subsystem sends the machine a channel close and nothing
	// more.
	errs := []error{fromClient, relay.Close()}
	if machineDone {
		return errors.Join(append(errs, fromMachine)...)
	}
	// What the machine has already said, before either wait below is
	// asked. A machine that answered is not one to walk away from, and a
	// select offered two ready channels picks either.
	select {
	case fromMachine = <-back:
		return errors.Join(append(errs, fromMachine)...)
	default:
	}
	expired := false
	select {
	case fromMachine = <-back:
		return errors.Join(append(errs, fromMachine)...)
	case <-client.Done():
		// The client's whole connection has gone, so the wait below would
		// buy nothing: nobody is left to read the machine's answer on the
		// error stream, and the row for that client is held until this
		// returns. An end of file on the channel does not say which of the
		// two happened, which is why the client's connection is asked.
	case <-time.After(relayGrace):
		expired = true
	}
	a.parkRelay(host, conn, back, expired)
	return errors.Join(errs...)
}

// parkRelay leaves the copy from a machine where it is and counts it
// against the connection it rode on until it ends.
//
// A machine that has stopped answering never answers the close, so the
// copy is left rather than holding open the row for a client that has
// gone. It ends when the machine answers after all or when the connection
// to it closes, and either one takes the count back off: what the count
// says is how many are parked right now.
//
// Counted against the connection rather than the name, because a rename
// moves the name and leaves the relay where it is. host is what to call
// the machine in the lines this logs.
//
// expired says the grace ran out with the client still there, which is
// the machine failing to answer rather than a client that walked away,
// and is what is worth saying in the window.
func (a *app) parkRelay(host string, conn *remote.Conn, back <-chan error, expired bool) {
	a.pump.post(func() {
		a.serving.relayAbandoned(conn)
		if expired {
			a.logError(fmt.Errorf("abandoned a file session on %s that did not answer the close;"+
				" it ends when the connection to it does", host))
		}
	})
	go func() {
		fromMachine := <-back
		a.pump.post(func() {
			a.serving.relayStopped(conn)
			if expired {
				a.logError(errors.Join(fmt.Errorf(
					"the file session left parked on %s has ended", host), fromMachine))
			}
		})
	}()
}

// relayEnded says what ended a relayed file session whose machine
// finished first.
//
// The machine's end alone does not mean the connection went: the file
// server over there can exit by itself, and so can a write back to the
// client fail. So the connection is asked.
func relayEnded(conn *remote.Conn, host string) error {
	if conn.Closed() {
		return fmt.Errorf("the connection to %s went while a file session was running over it", host)
	}
	return fmt.Errorf("the file session on %s ended while it was still in use", host)
}

// connectionTo is the connection this window holds to a machine, and
// refuses a machine already holding as many parked file sessions as it
// will.
//
// Called from a goroutine serving a client, so the look-up is handed to
// the one that draws and waited for here. Nothing the window holds may
// be read from anywhere else.
func (a *app) connectionTo(host string) (*remote.Conn, error) {
	type found struct {
		conn *remote.Conn
		err  error
	}
	back := make(chan found, 1)
	a.pump.post(func() {
		m := a.about(host).machine
		if m == nil {
			back <- found{err: fmt.Errorf(
				"the window you are reading through is not connected to %s", host)}
			return
		}
		if n := a.serving.abandonedRelays(m.conn); n >= mostAbandonedRelays {
			back <- found{err: fmt.Errorf(
				"%s has stopped answering; %s file sessions to it are still waiting to end",
				host, inWords(n))}
			return
		}
		back <- found{conn: m.conn}
	})
	select {
	case got := <-back:
		return got.conn, got.err
	case <-a.ctx.Done():
		// The window is closing and nothing will run what was posted.
		return nil, errors.New("the window you are reading through is closing")
	}
}

// inWords spells a small count, for a sentence where a bare digit reads
// like a code. Anything past the words it has is given as a number.
func inWords(n int) string {
	words := [...]string{"no", "one", "two", "three", "four", "five",
		"six", "seven", "eight", "nine", "ten"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}

// serveLocalFiles gives a client the files of this machine, as SFTP on
// the channel it was handed.
//
// It runs on a goroutine of the server's and touches nothing the window
// holds.
func serveLocalFiles(ch io.ReadWriteCloser) error {
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
