package main

import (
	"context"
	"fmt"
	stdlog "log"
	"slices"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui/term"
)

// opening is what a route is opened for: a shell to type into, one
// command to run, or a pane of the file manager.
type opening struct {
	// command is what the shell runs, and empty for a shell to type
	// into. It is read only when files is false.
	command []string

	// files opens a pane of the file manager on the machine and no shell
	// on it, which is what "Files" asks for.
	files bool
}

// kind says what sort of connection an opening is, for the row the
// sidebar draws.
func (o opening) kind() conns.Kind {
	switch {
	case o.files:
		return conns.Files
	case len(o.command) == 0:
		return conns.Terminal
	}
	return conns.Command
}

// openRoute connects to whatever of a route is not connected to yet and
// then opens what was asked for on the far end.
//
// The dial runs on its own goroutine because it can stop to ask the user
// something, and the dialog it asks with is drawn by this one. A pane
// holds the place until it is done, and closing it gives up.
func (a *app) openRoute(name string, route []step, open opening, at *spot) {
	// A window the server list holds is taken over, not logged in to.
	//
	// Here because this is the one place every connection really goes
	// through. Guarding the ways in instead left one of them -- opening
	// a terminal on a saved machine, which builds its own route -- still
	// logging in to the serve port, and the far end refusing a session
	// was the first anything heard of it.
	if h, ok := a.savedWindowInRoute(name, route); ok {
		a.workOnWindowOrSay(h.ServeAddr(), h.KeyFile(), at)
		return
	}
	through, missing, err := a.plan(route)
	if err != nil {
		a.reportError("Could not connect to "+name, err)
		return
	}
	if len(missing) == 0 {
		if err := a.startOn(name, open, at); err != nil {
			a.reportError("Could not open it on "+name, err)
		}
		return
	}
	for _, s := range missing {
		// Already on its way. Asked about rather than refused: waiting
		// for it is usually what the user wants, and refusing left them
		// with a machine they could not reach and no way to say so.
		if d := a.about(s.name).dialling; d != nil {
			a.askAboutTheOneOnItsWay(d, s.name, func() { a.openRoute(name, route, open, at) })
			return
		}
	}

	// A copy, because plan hands back a view on the route the caller
	// passed in and this fills each step in. A request that runs again
	// would otherwise be preparing configs that were prepared already.
	missing = slices.Clone(missing)

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

	// Every machine still to reach, and the name the pane goes by. That
	// last one is the end of the route rather than a step being dialled:
	// it is the name the user sees and the one they will ask to close.
	// Usually it is the last step spelled the same way, and held once.
	names := make([]string, 0, len(missing)+1)
	for _, s := range missing {
		names = append(names, s.name)
	}
	if !slices.Contains(names, name) {
		names = append(names, name)
	}
	held := &dialling{cancel: cancel, names: names}
	// Before the pane opens, so a route that cannot have its names says
	// so rather than leaving a pane behind saying it is connecting.
	if err := a.machines.holdNames(held); err != nil {
		cancel()
		// The message names the machine, so the title does not.
		a.reportError("Could not connect", err)
		return
	}

	// A pane rather than a row that only says "opening". It is somewhere
	// to watch from: every machine on the way as it is reached, and
	// whatever a server says, in full and there to copy. The same pane
	// carries the shell when there is one, and the account folds away to
	// one line that says where to read the rest of it.
	//
	// Letting go of the names is done here rather than by the closure
	// that finishes the dial: a dial that has not come back yet still
	// has to stop holding them, or nothing can try again.
	log := newConnLog(func() { a.pump.post(func() { a.machines.giveUp(held) }) })
	held.log = log
	pane, err := a.openSessionTab(log, name, open.kind(), "connecting", at)
	if err != nil {
		cancel()
		a.machines.release(held)
		a.reportError("Could not connect to "+name, err)
		return
	}
	a.machines.dialStarted()

	var carrier *remote.Conn
	first := ""
	if through != nil {
		carrier = through.conn
		first = through.at.name
		log.Say("going through " + first + ", which is already connected")
	}
	for _, s := range missing {
		log.Say("connecting to " + s.cfg.Target())
	}
	for i := range missing {
		missing[i].cfg.Ask = &askUser{app: a, log: log, stop: func() { a.machines.giveUp(held) }}
		// What the dial is doing, as it does it. A connection that stops
		// says where it stopped, which is the whole of what anybody has
		// to go on.
		missing[i].cfg.Saying = log.Say
	}
	go func() {
		var handed []*remote.Conn
		err := dialRoute(ctx, carrier, missing, func(at int, conn *remote.Conn) {
			handed = append(handed, conn)
			a.pump.post(func() { a.reached(held, log, pane, missing, at, conn, first) })
		})
		if a.ctx.Err() != nil {
			closeOnTheWayOut(handed)
		}
		a.pump.post(func() {
			a.machines.dialEnded()
			// Read before the context is let go of on the next line,
			// which would otherwise make every connection look like one
			// the user gave up on.
			gaveUp := ctx.Err()
			cancel()
			if err != nil {
				a.sayStillConnected(log, held)
				a.machines.settle(held, false)
				if gaveUp != nil {
					a.endedAs(pane, "given up on")
					log.GaveUp()
					return
				}
				a.endedAs(pane, "not connected")
				log.Failed(err)
				return
			}
			// Given up on, or the machine it was reached through closed,
			// while the last handshake was finishing.
			if why := a.stillWanted(gaveUp, through); why != nil {
				a.sayStillConnected(log, held)
				a.machines.settle(held, false)
				a.endedAs(pane, "not connected")
				log.Failed(why)
				return
			}
			// What was asked for first, so the pane the user is watching
			// is the one in front of them, and then whatever was waiting.
			a.becamePane(held.nameNow(name), open, pane, log)
			a.machines.settle(held, true)
		})
	}()
}

// reached takes a machine of a route that has just connected, while the
// rest of the route is still being made.
//
// The machine is held and its name let go of straight away rather than
// when the whole route is done. That is what lets a terminal open on a
// machine that answered while the machine beyond it is still being
// reached, instead of the window saying it is already connecting to one
// it is connected to.
func (a *app) reached(d *dialling, log *connLog, pane *term.Terminal, route []step,
	at int, conn *remote.Conn, first string) {
	s := route[at]
	// Renamed while it was being reached, in which case it goes under
	// what it is called now.
	s.name = d.nameNow(s.name)
	m := &machine{at: s, conn: conn}
	// Held even when the route was given up on in the meantime: the
	// machine answered, and whether this landed a frame before the user
	// pressed give up or a frame after is not something they can see.
	// Only another attempt owning the machine takes it away.
	if err := a.machines.answered(d, m); err != nil {
		// Said in the pane the user is watching this connection in,
		// which is the only place they would look for it.
		log.Say(err.Error())
		a.endedAs(pane, "not connected")
		a.letGoOfConn(s.name, conn)
		return
	}
	via := d.nameNow(first)
	if at > 0 {
		via = d.nameNow(route[at-1].name)
	}
	a.hold(m, via)
	d.made = append(d.made, s.name)
	log.Say("connected to " + s.name)
}

// endedAs says on the panel what became of a connection that was being
// made.
//
// The row said "connecting" and nothing took that back, so a connection
// that failed an hour ago still read as one on its way, greyed out.
func (a *app) endedAs(pane *term.Terminal, what string) {
	if e := a.panes[pane]; e != nil {
		e.Label = what
	}
}

// closeOnTheWayOut closes connections the window is never going to
// take, because it has already stopped.
//
// Work posted to the pump does not run once the window has gone, so
// these would be left open and the far end would see the socket break
// rather than a hangup. Closing is idempotent, so one the window did
// take is closed once either way. There is no window left to report a
// failure to, so it goes to the log the process was started with.
func closeOnTheWayOut(conns []*remote.Conn) {
	for _, c := range conns {
		if err := c.Close(); err != nil {
			stdlog.Printf("closing a connection the window never took: %v", err)
		}
	}
}

// letGoOfConn closes a connection nothing wants, away from the
// goroutine that draws.
//
// Closing one can take as long as the machine carrying it: it is a
// polite hangup on a connection that may itself be wedged. Doing that
// here would stop the window drawing and stop it taking keys.
func (a *app) letGoOfConn(name string, conn *remote.Conn) {
	go func() {
		err := conn.Close()
		a.pump.post(func() {
			if err != nil {
				a.reportError("Could not let go of "+name, err)
			}
		})
	}()
}

// sayStillConnected names the machines of a route that are connected
// even though the route as a whole was not made.
//
// A machine that answered is kept: it is a machine like any other now,
// and closing it because the one beyond it did not answer would throw
// away a connection the user can work on and would have to make again.
func (a *app) sayStillConnected(log *connLog, d *dialling) {
	var still []string
	for _, name := range d.made {
		if a.about(name).machine != nil {
			still = append(still, name)
		}
	}
	if len(still) > 0 {
		log.Say(strings.Join(still, ", ") + " answered and stays connected;" +
			" closing this pane does not close it")
	}
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
	// By identity: the name may hold another connection by now.
	if through != nil && a.machines.named(through.at.name) != through {
		return fmt.Errorf("%s closed while this was being connected through it", through.at.name)
	}
	return nil
}

// becamePane hands the pane that was watching a connection being made
// to what the route was opened for: a shell in the same pane, or a file
// pane with this one closed behind it.
func (a *app) becamePane(name string, open opening, pane *term.Terminal, log *connLog) {
	m := a.about(name).machine
	if m == nil {
		a.endedAs(pane, "not connected")
		log.Failed(fmt.Errorf("nothing is connected to %s", name))
		return
	}
	// The machine the route was for, and only that one: every machine on
	// the way shares this account, and a line on each of their rows would
	// open an account about somewhere else.
	m.log = log
	if open.files {
		a.becomeFilesPane(name, pane, log)
		return
	}
	a.becomeShellPane(m, name, open.command, pane, log)
}

// becomeFilesPane opens a pane of the file manager on a machine that has
// just answered, and closes the pane that watched it being reached.
//
// That pane goes because nothing rides in it: the connection was made
// for files and there is no shell on it. Its account is kept on the
// machine's row, under "How it was reached". A dial that failed keeps
// its pane, with the account still in it.
func (a *app) becomeFilesPane(name string, pane *term.Terminal, log *connLog) {
	if err := a.browseOn(name); err != nil {
		a.endedAs(pane, "no files")
		log.Refused(name, "the files", err)
		return
	}
	// Said before the pane goes, so closing it lets go of the account
	// rather than giving up on the connection under it.
	log.Connected()
	// Closed after the file pane is open, because the window quits with
	// its last pane.
	if err := a.closePane(pane); err != nil {
		a.reportError("Could not close the pane that was connecting", err)
	}
}

// becomeShellPane hands the pane that was watching a connection being
// made to a shell on the machine it reached.
func (a *app) becomeShellPane(m *machine, name string, command []string,
	pane *term.Terminal, log *connLog) {

	size := pane.Size()
	sh, err := m.conn.Shell(a.ctx, remote.ShellConfig{
		Command: command,
		Cols:    size.Cols,
		Rows:    size.Rows,
		Term:    m.at.term,
	})
	if err != nil {
		a.endedAs(pane, "no terminal")
		log.Refused(name, "a terminal", err)
		return
	}
	// Which connection the pane rides on rather than which machine it is
	// named after, for the reason the on field of machines gives.
	a.machines.runs(pane, m)
	if label := labelFor(command); label != "" {
		if e := a.panes[pane]; e != nil {
			e.Label = label
		}
	}
	log.Became(name, sh)
}

// startOn opens what was asked for on a machine that is already
// connected to.
func (a *app) startOn(name string, open opening, at *spot) error {
	if open.files {
		// at is not used: a file pane goes in the file manager's own tab.
		return a.browseOn(name)
	}
	m := a.about(name).machine
	if m == nil {
		return fmt.Errorf("nothing is connected to %s", name)
	}
	sh, err := m.conn.Shell(a.ctx, remote.ShellConfig{
		Command: open.command,
		Cols:    a.lastSize[0],
		Rows:    a.lastSize[1],
		Term:    m.at.term,
	})
	if err != nil {
		return err
	}
	t, err := a.openSessionTab(sh, name, open.kind(), labelFor(open.command), at)
	if err != nil {
		// The shell is ours and nothing else knows about it.
		_ = sh.Close()
		return err
	}
	// Which connection the pane rides on rather than which machine it is
	// named after, for the reason the on field of machines gives.
	a.machines.runs(t, m)
	return nil
}

// openOn puts a terminal or a command on a machine, connecting to it
// first when nothing is connected to it yet.
func (a *app) openOn(name string, command []string, at *spot) error {
	route, err := a.route(name)
	if err != nil {
		return err
	}
	a.openRoute(name, route, opening{command: command}, at)
	return nil
}

// connectAndBrowse connects to a machine and opens a pane of the file
// manager on it when it answers.
//
// The route is built here rather than in browse.go because openRoute is
// the one place that dials.
func (a *app) connectAndBrowse(name string) error {
	route, err := a.route(name)
	if err != nil {
		return err
	}
	a.openRoute(name, route, opening{files: true}, nil)
	return nil
}

// labelFor names a connection by what it is running.
func labelFor(command []string) string {
	return strings.Join(command, " ")
}
