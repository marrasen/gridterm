package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// machine is one connection to one server.
//
// One per server, not one per terminal: a second terminal on a machine
// is another channel on the connection already open, not another login.
// The window keeps these so the second one costs nothing and so that
// closing the connection closes everything riding on it.
type machine struct {
	name string
	conn *remote.Conn

	// term is the TERM value shells on this machine are started with.
	term string

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
	m := &machine{name: s.name, conn: conn, term: s.term}
	note := "connected"
	if via != "" {
		note = "via " + via
	}
	m.entry = &conns.Entry{
		Host:  s.name,
		Kind:  conns.Server,
		Label: conn.String(),
		Note:  note,
		Close: func() error { return a.dropMachine(s.name) },
	}
	a.machines[s.name] = m
	a.registry.Add(m.entry)
	return m
}

// dropMachine closes a machine's connection and everything riding on it.
func (a *app) dropMachine(name string) error {
	m := a.machines[name]
	if m == nil {
		return nil
	}
	delete(a.machines, name)
	a.registry.Drop(m.entry)

	var errs []error
	// A machine reached through this one cannot outlive it, and neither
	// can what is open on that.
	for _, rider := range a.ridingOn(m) {
		errs = append(errs, a.dropMachine(rider))
	}
	// The panes before the connection, so each is taken out of the tree
	// rather than left showing a shell whose transport has gone.
	for t, e := range a.panes {
		if e.Host == name {
			errs = append(errs, a.closePane(t))
		}
	}
	errs = append(errs, m.conn.Close())
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
func (a *app) plan(route []step) (through *machine, missing []step, err error) {
	if len(route) == 0 {
		return nil, nil, errors.New("there is no route to that machine")
	}
	at := 0
	for i, s := range route {
		m := a.machines[s.name]
		if m == nil {
			break
		}
		through, at = m, i+1
	}
	return through, route[at:], nil
}

// openRoute connects to whatever of a route is not connected to yet and
// then opens a terminal or a command on the far end.
//
// The dial runs on its own goroutine because it can stop to ask the user
// something, and the dialog it asks with is drawn by this one. A row on
// the panel holds the place until it is done, and cancelling that gives
// up.
func (a *app) openRoute(name string, route []step, command []string) {
	through, missing, err := a.plan(route)
	if err != nil {
		a.reportError("Could not connect to "+name, err)
		return
	}
	if len(missing) == 0 {
		if err := a.startOn(name, command); err != nil {
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
			cancel()
			if err != nil {
				a.reportError("Could not connect to "+name, err)
				return
			}
			via := ""
			if through != nil {
				via = through.name
			}
			for i, conn := range opened {
				a.hold(missing[i], conn, via)
				via = missing[i].name
			}
			if err := a.startOn(name, command); err != nil {
				a.reportError("Could not open it on "+name, err)
			}
		})
	}()
}

// dialRoute opens each machine of a route in turn, each through the one
// before it, and hands back what it opened.
//
// A failure part way closes what it had already opened. Half a route is
// a set of connections nothing knows about and nothing would ever close.
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
			for i := len(opened) - 1; i >= 0; i-- {
				_ = opened[i].Close()
			}
			if len(route) > 1 {
				// Which machine of the route failed, which the caller
				// cannot work out from the error on its own.
				return nil, fmt.Errorf("%s: %w", s.name, err)
			}
			return nil, err
		}
		opened = append(opened, conn)
		through = conn
	}
	return opened, nil
}

// startOn runs something on a machine that is already connected to.
func (a *app) startOn(name string, command []string) error {
	m := a.machines[name]
	if m == nil {
		return fmt.Errorf("nothing is connected to %s", name)
	}
	sh, err := m.conn.Shell(remote.ShellConfig{
		Command: command,
		Cols:    a.lastSize[0],
		Rows:    a.lastSize[1],
		Term:    m.term,
	})
	if err != nil {
		return err
	}
	if err := a.openSessionTab(sh, name, kindOf(command), labelFor(command)); err != nil {
		// The shell is ours and nothing else knows about it.
		_ = sh.Close()
		return err
	}
	return nil
}

// openOn puts a terminal or a command on a saved machine, connecting to
// it first when nothing is connected to it yet.
func (a *app) openOn(name string, command []string) error {
	if a.machines[name] != nil {
		return a.startOn(name, command)
	}
	route, err := a.route(name)
	if err != nil {
		return err
	}
	a.openRoute(name, route, command)
	return nil
}

// route returns the machines to connect to in order to reach a saved
// one: the far end last, and whatever it is reached through before it.
func (a *app) route(name string) ([]step, error) {
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

// currentHost returns the machine the user is looking at: whatever the
// panel has selected, or the machine the focused pane runs on.
func (a *app) currentHost() string {
	if e, ok := a.selectedConnection(); ok {
		return e.Host
	}
	if t := a.focusedTerminal(); t != nil {
		if e := a.panes[t]; e != nil {
			return e.Host
		}
	}
	return a.localHost
}

// isHere reports whether the machine the user is looking at is the one
// gridterm itself is running on, or the one -ssh put every pane on.
// Neither has a connection of its own to open anything else on.
func (a *app) isHere(host string) bool {
	return host == conns.Local || (a.machines[host] == nil && host == a.localHost)
}

// openTerminalHere opens another terminal on the machine the user is
// looking at.
func (a *app) openTerminalHere() error {
	host := a.currentHost()
	if a.isHere(host) {
		// A new pane already runs there, which is what a new tab is.
		return a.openTab()
	}
	return a.openOn(host, nil)
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
			if err := a.openOn(host, command); err != nil {
				a.reportError("Could not run it on "+host, err)
			}
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// closeMachines ends every connection the window is holding, for a
// window that is closing.
func (a *app) closeMachines() error {
	var errs []error
	for name, m := range a.machines {
		delete(a.machines, name)
		errs = append(errs, m.conn.Close())
	}
	return errors.Join(errs...)
}
