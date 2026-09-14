package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
)

// machine is one connection to one server.
//
// One per server, not one per terminal: a second terminal on a machine
// is another channel on the connection already open, not another login.
// The window keeps these so the second one costs nothing and so that
// closing the connection closes everything riding on it.
type machine struct {
	// at is what the machine was opened as: the name the panel calls it
	// and the config it was reached with. The config is kept so a second
	// connection under the same name can be checked against it.
	at step

	conn *remote.Conn

	// entry is the panel row for the connection itself, so a machine
	// with nothing open on it is still visible and still closeable.
	entry *conns.Entry
}

// step is one machine on the way to another: what to connect to, and
// what the window calls it.
type step struct {
	name string
	cfg  remote.Config
	term string
}

// hostStep turns a saved machine into a step of a route.
func hostStep(h remote.Host) step {
	return step{name: h.Name, cfg: h.Config(), term: h.Term}
}

// hold remembers a connection under a name and puts a row on the panel
// for the machine itself. via names what carries it, or is empty.
func (a *app) hold(s step, conn *remote.Conn, via string) *machine {
	m := &machine{at: s, conn: conn}
	// Only what the dot cannot say. That it is connected is the dot's
	// business; which machine it was reached through is not.
	note := ""
	if via != "" {
		note = "via " + via
	}
	m.entry = &conns.Entry{
		Host:   s.name,
		Kind:   conns.Server,
		Label:  conn.String(),
		Note:   note,
		Reveal: func() { a.revealMachine(m) },
		Close:  func() error { return a.dropMachine(s.name) },
	}
	a.machines[s.name] = m
	a.registry.Add(m.entry)

	// A connection the far end drops is still held here, saying it is
	// connected, until something notices. Nothing else here would.
	go func() {
		_ = conn.Wait()
		a.pump.post(func() { a.machineDied(m) })
	}()
	return m
}

// revealMachine puts one of a machine's panes in front of the user.
//
// A connection with nothing open on it has nothing to show, so nothing
// happens: there is no window for a connection itself.
func (a *app) revealMachine(m *machine) {
	for t, on := range a.paneOn {
		if on == m {
			a.focus(t)
			return
		}
	}
}

// machineDied takes away a machine whose connection has gone on its own.
//
// The row stays, greyed and closed, the way a shell that exited leaves
// its row behind: what a connection did before it dropped is worth
// reading. The panes on it end by themselves, because their shells stop
// reading.
func (a *app) machineDied(m *machine) {
	if a.machines[m.at.name] != m {
		// Already closed from the window, and its row with it.
		return
	}
	delete(a.machines, m.at.name)
	// A pane of the file manager on this machine is reading through a
	// session that has gone. It is taken away here, because nothing else
	// would: a pane does not end by itself the way a shell does.
	if err := a.closeFilesOn(m.at.name); err != nil {
		a.reportError("Trouble closing the file panes on "+m.at.name, err)
	}
	// The tunnels went with it, but a local forward listens on a socket
	// of this machine, which the far end dropping does nothing to.
	if err := a.tunnelsDiedOn(m); err != nil {
		a.reportError("Trouble closing the tunnels on "+m.at.name, err)
	}

	dead := meter.New()
	dead.Close()
	m.entry.Meter = dead
	// The state says closed now, and the note said "connected".
	m.entry.Note = ""
	m.entry.Reveal = nil
	m.entry.Close = func() error {
		a.registry.Drop(m.entry)
		return nil
	}
	a.markDirty()
}

// dropMachine closes a machine's connection and everything riding on it.
func (a *app) dropMachine(name string) error {
	m := a.machines[name]
	if m == nil {
		return nil
	}
	delete(a.machines, name)
	a.registry.Drop(m.entry)

	// The file panes on this machine go first, which cancels the jobs
	// reading through them and lets go of their sessions once those have
	// stopped. A cancelled job stops when whatever it is waiting on
	// gives up, which can be after this returns: one still unwinding
	// then finds the connection gone underneath it, reports that like
	// any other failure, and leaves what it half wrote where it is.
	errs := []error{a.closeFilesOn(name)}
	// Then the connection, which closes everything riding on it in
	// parallel, each waiting out its own drain period; closing the panes
	// first would wait out one drain period per pane instead.
	errs = append(errs, m.conn.Close())
	// The rows of its tunnels, which the connection has just closed as
	// riders of its own.
	a.dropTunnelsOn(m)
	// A machine reached through this one has been closed with it, but
	// the window is still holding a record of it.
	for _, rider := range a.ridingOn(m) {
		errs = append(errs, a.dropMachine(rider))
	}
	for t, on := range a.paneOn {
		if on == m {
			errs = append(errs, a.closePane(t))
		}
	}
	return errors.Join(errs...)
}

// ridingOn names the machines reached through one.
func (a *app) ridingOn(m *machine) []string {
	var names []string
	for name, other := range a.machines {
		if other.conn.Via() == m.conn {
			names = append(names, name)
		}
	}
	return names
}

// plan splits a route into the connection it can start from and the
// machines still to be reached.
//
// It runs on the drawing goroutine, which is the only one that may read
// what the window is holding. The dialling happens elsewhere.
//
// The search runs from the far end back, so a machine already connected
// to is used however it was reached. Walking forwards instead would
// connect to something already open a second time, and the window would
// hold the second connection and close neither.
func (a *app) plan(route []step) (through *machine, missing []step, err error) {
	if len(route) == 0 {
		return nil, nil, errors.New("there is no route to that machine")
	}
	for at := len(route) - 1; at >= 0; at-- {
		m := a.machines[route[at].name]
		if m == nil {
			continue
		}
		if !m.at.cfg.SameMachine(route[at].cfg) {
			return nil, nil, fmt.Errorf(
				"%q is already connected to %s, which is not %s; close it first",
				m.at.name, m.at.cfg.Target(), route[at].cfg.Target())
		}
		return m, route[at+1:], nil
	}
	return nil, route, nil
}

// openRoute connects to whatever of a route is not connected to yet and
// then opens a terminal or a command on the far end.
//
// The dial runs on its own goroutine because it can stop to ask the user
// something, and the dialog it asks with is drawn by this one. A row on
// the panel holds the place until it is done, and cancelling that gives
// up.
func (a *app) openRoute(name string, route []step, command []string, at *spot) {
	through, missing, err := a.plan(route)
	if err != nil {
		a.reportError("Could not connect to "+name, err)
		return
	}
	if len(missing) == 0 {
		if err := a.startOn(name, command, at); err != nil {
			a.reportError("Could not open it on "+name, err)
		}
		return
	}
	for _, s := range missing {
		// Two connections to one machine at once would leave the window
		// holding the second and closing neither.
		if a.opening[s.name] {
			a.reportError("Could not connect to "+name,
				fmt.Errorf("gridterm is already connecting to %s", s.name))
			return
		}
	}

	// Filled in here rather than by the caller, so nothing connects
	// without a way to reach the user and without the keys already
	// unlocked.
	for i := range missing {
		if a.prepare != nil {
			missing[i].cfg = a.prepare(missing[i].cfg)
		}
		missing[i].cfg.Ask = &askUser{app: a}
		missing[i].cfg.Ring = a.keys
	}

	ctx, cancel := context.WithCancel(a.ctx)
	// A row on the panel rather than a dialog to wait in. Several
	// connections can be on their way at once, and a dialog each would
	// stack: closing one takes everything above it, so the one that
	// finished first would tear down the one still waiting.
	waiting := &conns.Entry{
		Host:  name,
		Kind:  kindOf(command),
		Label: "connecting",
		Note:  "opening",
		Close: func() error {
			cancel()
			return nil
		},
	}
	a.registry.Add(waiting)
	a.connecting++
	for _, s := range missing {
		a.opening[s.name] = true
	}

	var carrier *remote.Conn
	if through != nil {
		carrier = through.conn
	}
	go func() {
		opened, err := dialRoute(ctx, carrier, missing)
		a.pump.post(func() {
			a.connecting--
			a.registry.Drop(waiting)
			for _, s := range missing {
				delete(a.opening, s.name)
			}
			// Read before the context is let go of on the next line,
			// which would otherwise make every connection look like one
			// the user gave up on.
			gaveUp := ctx.Err()
			cancel()
			if err != nil {
				a.reportError("Could not connect to "+name, err)
				return
			}
			// Given up on, or the machine it was reached through closed,
			// while the last handshake was finishing. Either way what
			// was opened is no use and nothing else knows about it.
			if why := a.stillWanted(gaveUp, through); why != nil {
				a.reportError("Could not connect to "+name,
					errors.Join(append([]error{why}, closeAll(opened)...)...))
				return
			}
			via := ""
			if through != nil {
				via = through.at.name
			}
			for i, conn := range opened {
				a.hold(missing[i], conn, via)
				via = missing[i].name
			}
			if err := a.startOn(name, command, at); err != nil {
				a.reportError("Could not open it on "+name, err)
			}
		})
	}()
}

// stillWanted says why a connection that has just been made is no use,
// or nil when it is still the connection that was asked for.
//
// gaveUp is what the connection's own context said before it was let go
// of: the user cancelling the row that was waiting for it.
func (a *app) stillWanted(gaveUp error, through *machine) error {
	if gaveUp != nil {
		return gaveUp
	}
	if through != nil && a.machines[through.at.name] != through {
		return fmt.Errorf("%s closed while this was being connected through it", through.at.name)
	}
	return nil
}

// closeAll shuts a set of connections and returns what went wrong.
func closeAll(conns []*remote.Conn) []error {
	errs := make([]error, 0, len(conns))
	for i := len(conns) - 1; i >= 0; i-- {
		if err := conns[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// dialRoute opens each machine of a route in turn, each through the one
// before it, and hands back what it opened.
//
// A failure part way closes what it had already opened. Half a route is
// a set of connections nothing knows about and nothing would ever close,
// so a failure to close one of them is reported alongside the failure
// that caused it.
func dialRoute(ctx context.Context, through *remote.Conn, route []step) ([]*remote.Conn, error) {
	opened := make([]*remote.Conn, 0, len(route))
	for _, s := range route {
		var (
			conn *remote.Conn
			err  error
		)
		if through == nil {
			conn, err = remote.Connect(ctx, s.cfg)
		} else {
			conn, err = through.Through(ctx, s.cfg)
		}
		if err != nil {
			if len(route) > 1 {
				// Which machine of the route failed, which the caller
				// cannot work out from the error on its own.
				err = fmt.Errorf("%s: %w", s.name, err)
			}
			return nil, errors.Join(append([]error{err}, closeAll(opened)...)...)
		}
		opened = append(opened, conn)
		through = conn
	}
	return opened, nil
}

// startOn runs something on a machine that is already connected to.
func (a *app) startOn(name string, command []string, at *spot) error {
	m := a.machines[name]
	if m == nil {
		return fmt.Errorf("nothing is connected to %s", name)
	}
	sh, err := m.conn.Shell(remote.ShellConfig{
		Command: command,
		Cols:    a.lastSize[0],
		Rows:    a.lastSize[1],
		Term:    m.at.term,
	})
	if err != nil {
		return err
	}
	t, err := a.openSessionTab(sh, name, kindOf(command), labelFor(command), at)
	if err != nil {
		// The shell is ours and nothing else knows about it.
		_ = sh.Close()
		return err
	}
	// Which connection the pane rides on, rather than which machine it
	// is named after: two things can share a name -- the machine -ssh
	// put every pane on, and a connection made from the window -- and
	// closing one must not take the other's panes.
	a.paneOn[t] = m
	return nil
}

// openOn puts a terminal or a command on a machine, connecting to it
// first when nothing is connected to it yet.
func (a *app) openOn(name string, command []string, at *spot) error {
	route, err := a.route(name)
	if err != nil {
		return err
	}
	a.openRoute(name, route, command, at)
	return nil
}

// route returns the machines to connect to in order to reach one: the
// far end last, and whatever it is reached through before it.
//
// A machine already connected to is a route of one, whether or not it
// was ever saved: it is reachable, which is what a route is for.
func (a *app) route(name string) ([]step, error) {
	if m := a.machines[name]; m != nil {
		return []step{m.at}, nil
	}
	hosts, err := a.book.Route(name)
	if err != nil {
		return nil, err
	}
	route := make([]step, len(hosts))
	for i, h := range hosts {
		route[i] = hostStep(h)
	}
	return route, nil
}

// kindOf says what sort of connection a command is: one program run and
// finished with, or a shell to type into.
func kindOf(command []string) conns.Kind {
	if len(command) == 0 {
		return conns.Terminal
	}
	return conns.Command
}

// labelFor names a connection by what it is running.
func labelFor(command []string) string {
	return strings.Join(command, " ")
}

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
	if a.acting {
		return a.actOn
	}
	if a.panel != nil && a.panel.Focused() {
		if e, ok := a.selectedConnection(); ok {
			return e.Host
		}
	}
	if t := a.focusedTerminal(); t != nil {
		if m := a.paneOn[t]; m != nil {
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

// isHere reports whether the machine the user is looking at is one the
// window has no connection of its own to: this machine, or the one -ssh
// put every pane on.
func (a *app) isHere(host string) bool {
	return a.machines[host] == nil && (host == conns.Local || host == a.localHost)
}

// disconnectHere closes the connection to the machine the user is
// looking at, and everything riding on it.
func (a *app) disconnectHere() error {
	host := a.currentHost()
	if a.isHere(host) {
		return errors.New("this is the machine gridterm is running on, not one it connected to")
	}
	return a.dropMachine(host)
}

// openTerminalHere opens another terminal on the machine the user is
// looking at.
func (a *app) openTerminalHere() error {
	host := a.currentHost()
	if a.isHere(host) {
		// A new pane already runs there, which is what a new tab is.
		return a.openTab()
	}
	return a.openOn(host, nil, nil)
}

// openCommandHere asks for a command to run on the machine the user is
// looking at, connecting to it if the connection has since been closed.
func (a *app) openCommandHere() error {
	host := a.currentHost()
	if a.isHere(host) {
		return errors.New(
			"a command runs on a machine gridterm connected to, and this is the one it is running on")
	}
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

// forgetPane takes a pane off the record of what runs where, for one
// that has been closed.
func (a *app) forgetPane(t *term.Terminal) { delete(a.paneOn, t) }

// closeMachines ends every connection the window is holding, for a
// window that is closing.
func (a *app) closeMachines() error {
	// A machine reached through another is closed by that one, so it is
	// not closed again here: the second close would report the first
	// one's failure a second time.
	carried := make(map[*machine]bool, len(a.machines))
	for _, m := range a.machines {
		for _, other := range a.machines {
			if m.conn.Via() == other.conn {
				carried[m] = true
			}
		}
	}
	var errs []error
	for _, m := range a.machines {
		if carried[m] {
			continue
		}
		errs = append(errs, m.conn.Close())
	}
	clear(a.machines)
	return errors.Join(errs...)
}
