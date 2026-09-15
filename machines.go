package main

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

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
	// A machine the window has reached is worth a command of its own,
	// whether or not it was ever saved.
	a.refreshServers()

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
	// By identity: the name may hold another connection by now.
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
		a.refreshServers()
		return nil
	}
	// Whatever was on it has gone with it, so it is no longer a machine
	// worth a command of its own.
	a.refreshServers()
	a.markDirty()
}

// dropMachine closes a machine's connection and everything riding on it.
func (a *app) dropMachine(name string) error {
	on := a.about(name)
	if d := on.dialling; d != nil {
		// Still on its way. Cancelling closes the connection under the
		// handshake, wherever it is waiting -- including on a browser
		// window the user has thought better of -- and the goroutine
		// that was dialling takes the row away. The names are let go of
		// here rather than there, because a handshake carried inside
		// another connection need not come back at all.
		a.giveUp(d)
		return nil
	}
	m := on.machine
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
	// Last, once everything that was on it has gone: a machine still
	// holding a pane is still a machine worth a command.
	a.refreshServers()
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
		m := a.about(route[at].name).machine
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

// dialling is a connection being made, and the names the window holds
// while it is.
type dialling struct {
	cancel context.CancelFunc

	// names is every machine of the route and the name the pane goes by.
	names []string

	// made names the machines of the route that answered, in the order
	// they did.
	made []string

	// waiting are requests to run once this connection is made or
	// fails, for a user who asked for the same machine while it was on
	// its way and chose to wait.
	waiting []func()

	// settled says the dial has come back, so anything waiting on it has
	// already run and a new request must run now rather than queue.
	settled bool

	// say writes into the pane watching this connection, so a request
	// that was thrown away says so where the user is looking.
	say func(string)

	// renamed maps what a machine was called when the dial started to
	// what it is called now, for one renamed while it was on its way.
	// The dial goroutine holds the old names and cannot be told.
	renamed map[string]string
}

// nameNow gives what a machine is called now, which is what it was
// called when the dial started unless it has been renamed since.
func (d *dialling) nameNow(was string) string {
	if now, ok := d.renamed[was]; ok {
		return now
	}
	return was
}

// holdNames holds every name a connection needs while it is being made.
func (a *app) holdNames(d *dialling) {
	for _, n := range d.names {
		a.opening[n] = d
	}
}

// releaseName stops holding one name of a route, leaving the rest held.
//
// It is what a machine that has just connected does. The connection is
// there now, so the window is no longer connecting to it and a terminal
// can be opened on it while the machine beyond it is still being
// reached. A name another attempt has since taken is left alone.
func (a *app) releaseName(d *dialling, name string) {
	if a.opening[name] == d {
		delete(a.opening, name)
	}
}

// release stops holding a route's names, so another attempt can have
// them.
func (a *app) release(d *dialling) {
	for _, n := range d.names {
		a.releaseName(d, n)
	}
}

// giveUp cancels a connection being made and lets go of its names at
// once.
//
// The names go now rather than when the dial goroutine comes back. A
// handshake carried inside another connection can wait as long as the
// machine carrying it takes, and the window would otherwise answer the
// next attempt with "already connecting" about a machine nobody is
// connecting to any more.
func (a *app) giveUp(d *dialling) {
	d.cancel()
	a.release(d)
	// What was queued behind it goes with it. Giving up on a connection
	// and having the window make it anyway a moment later is not what
	// giving up means.
	a.dropWaiting(d)
}

// settle lets go of a connection that has been made, and runs whatever
// was waiting for it.
//
// made says the machine answered. What was waiting was waiting for that
// machine, so a connection that was not made leaves it nothing to do:
// starting a fresh connection instead is not what "wait for it" says,
// and on a machine that never answers it would ask again and again.
func (a *app) settle(d *dialling, made bool) {
	a.release(d)
	if !made {
		a.dropWaiting(d)
		return
	}
	waiting := d.waiting
	d.settled, d.waiting = true, nil
	for _, run := range waiting {
		run()
	}
}

// dropWaiting throws away what was queued behind a connection that was
// not made, saying so where the user is looking.
func (a *app) dropWaiting(d *dialling) {
	if len(d.waiting) > 0 && d.say != nil {
		d.say("what was waiting for this was not started")
	}
	d.settled, d.waiting = true, nil
}

// askAboutTheOneOnItsWay asks what to do about a machine that is
// already being connected to.
//
// Two connections to one machine at once would leave the window holding
// the second and closing neither, so one of them has to go. Which one is
// the user's to say: the one on its way may be a second from done, or
// may be stuck on a machine that will never answer.
func (a *app) askAboutTheOneOnItsWay(d *dialling, name string, again func()) {
	// Posted, not shown from here. This can be reached from a button of
	// another dialog, and that dialog closes as soon as the button
	// returns, taking anything stacked on top of it.
	a.pump.post(func() { a.showTheOneOnItsWay(d, name, again) })
}

// showTheOneOnItsWay is the dialog itself, on the goroutine that draws.
func (a *app) showTheOneOnItsWay(d *dialling, name string, again func()) {
	f := a.newConfirm("Already connecting to "+name, []string{
		"gridterm is still connecting to " + name + ".",
		"",
		"Two connections to one machine at once would leave one of them" +
			" open with nothing holding it. So either this waits for that" +
			" one, or that one goes.",
	})
	f.AddButton(ui.Button{Title: "Wait for it", Do: func() error {
		if d.settled {
			// It came back while the dialog was open, so there is
			// nothing left to wait for.
			a.pump.post(again)
			return nil
		}
		d.waiting = append(d.waiting, again)
		return nil
	}})
	f.AddButton(ui.Button{Title: "Give up on that one", Do: func() error {
		a.giveUp(d)
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(again)
		return nil
	}})
	f.AddButton(ui.Button{Title: "Leave it"})
	a.showForm(f, nil)
}

// openRoute connects to whatever of a route is not connected to yet and
// then opens a terminal or a command on the far end.
//
// The dial runs on its own goroutine because it can stop to ask the user
// something, and the dialog it asks with is drawn by this one. A pane
// holds the place until it is done, and closing it gives up.
func (a *app) openRoute(name string, route []step, command []string, at *spot) {
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
		if err := a.startOn(name, command, at); err != nil {
			a.reportError("Could not open it on "+name, err)
		}
		return
	}
	for _, s := range missing {
		// Already on its way. Asked about rather than refused: waiting
		// for it is usually what the user wants, and refusing left them
		// with a machine they could not reach and no way to say so.
		if d := a.about(s.name).dialling; d != nil {
			a.askAboutTheOneOnItsWay(d, s.name, func() { a.openRoute(name, route, command, at) })
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
	names := make([]string, 0, len(missing)+1)
	for _, s := range missing {
		names = append(names, s.name)
	}
	names = append(names, name)
	held := &dialling{cancel: cancel, names: names}

	// A pane rather than a row that only says "opening". It is somewhere
	// to watch from: every machine on the way as it is reached, and
	// whatever a server says, in full and there to copy. The same pane
	// carries the shell when there is one, so the account of how it was
	// reached stays in the scrollback above it.
	//
	// Letting go of the names is done here rather than by the closure
	// that finishes the dial: a dial that has not come back yet still
	// has to stop holding them, or nothing can try again.
	log := newConnLog(func() { a.pump.post(func() { a.giveUp(held) }) })
	held.say = log.Say
	pane, err := a.openSessionTab(log, name, kindOf(command), "connecting", at)
	if err != nil {
		cancel()
		a.reportError("Could not connect to "+name, err)
		return
	}
	a.connecting++
	a.holdNames(held)

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
		missing[i].cfg.Ask = &askUser{app: a, log: log, stop: func() { a.giveUp(held) }}
		// What the dial is doing, as it does it. A connection that stops
		// says where it stopped, which is the whole of what anybody has
		// to go on.
		missing[i].cfg.Saying = log.Say
	}
	go func() {
		var handed []*remote.Conn
		err := dialRoute(ctx, carrier, missing, func(at int, conn *remote.Conn) {
			handed = append(handed, conn)
			a.pump.post(func() { a.reached(held, log, missing, at, conn, first) })
		})
		if a.ctx.Err() != nil {
			closeOnTheWayOut(handed)
		}
		a.pump.post(func() {
			a.connecting--
			// Read before the context is let go of on the next line,
			// which would otherwise make every connection look like one
			// the user gave up on.
			gaveUp := ctx.Err()
			cancel()
			if err != nil {
				a.sayStillConnected(log, held)
				a.settle(held, false)
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
				a.settle(held, false)
				a.endedAs(pane, "not connected")
				log.Failed(why)
				return
			}
			// The shell first, so the pane the user is watching is the
			// one in front of them, and then whatever was waiting.
			a.becomeShellPane(held.nameNow(name), command, pane, log)
			a.settle(held, true)
		})
	}()
}

// savedWindowInRoute is the saved window a request names, by the name
// it was asked for or by the address its last step would be dialled at.
//
// By address as well as by name, because a connection made from a typed
// target knows nothing about the list. Only the last step: a window
// serves gridterm's own protocol and cannot be a machine on the way to
// another one.
func (a *app) savedWindowInRoute(name string, route []step) (remote.Host, bool) {
	if f := a.about(name); f.serves {
		return f.record(), true
	}
	if len(route) == 0 {
		return remote.Host{}, false
	}
	last := route[len(route)-1]
	if f := a.about(last.name); f.serves {
		return f.record(), true
	}
	addr := last.cfg.Host
	if addr == "" {
		return remote.Host{}, false
	}
	if last.cfg.Port != 0 {
		addr = net.JoinHostPort(last.cfg.Host, strconv.Itoa(last.cfg.Port))
	}
	if h, ok := a.savedWindowAt(addr); ok {
		return h, true
	}
	if last.cfg.Port == 0 {
		// A target typed with no port, so the address on its own is
		// what the list has to match.
		for _, h := range a.book.Hosts() {
			if h.Window && strings.EqualFold(h.Address, addr) {
				return h, true
			}
		}
	}
	return remote.Host{}, false
}

// reached takes a machine of a route that has just connected, while the
// rest of the route is still being made.
//
// The machine is held and its name let go of straight away rather than
// when the whole route is done. That is what lets a terminal open on a
// machine that answered while the machine beyond it is still being
// reached, instead of the window saying it is already connecting to one
// it is connected to.
func (a *app) reached(d *dialling, log *connLog, route []step, at int, conn *remote.Conn, first string) {
	s := route[at]
	// Renamed while it was being reached, in which case it goes under
	// what it is called now.
	s.name = d.nameNow(s.name)
	// Held even when the route was given up on in the meantime: the
	// machine answered, and whether this landed a frame before the user
	// pressed give up or a frame after is not something they can see.
	// Only another attempt owning the machine takes it away.
	if taken := a.opening[s.name]; a.machines[s.name] != nil || (taken != nil && taken != d) {
		a.letGoOfConn(s.name, conn)
		return
	}
	via := d.nameNow(first)
	if at > 0 {
		via = d.nameNow(route[at-1].name)
	}
	a.hold(s, conn, via)
	d.made = append(d.made, s.name)
	a.releaseName(d, s.name)
	log.Say("connected to " + s.name)
}

// renamedMachine follows a rename through everything the window keys by
// a machine's name.
//
// The machine has not changed, only what it is called. What was open
// under the old name would otherwise show as a second machine in the
// sidebar, and closing it would look for one that is not there any more.
func (a *app) renamedMachine(was string, to remote.Host) {
	now := to.Name
	if m := a.machines[was]; m != nil && m.at.cfg.SameMachine(to.Config()) {
		// Only when it is still the same machine. A rename that also
		// changed the address left this saying the new name was
		// connected, to something that was not it.
		delete(a.machines, was)
		m.at.name = now
		a.machines[now] = m
		// The closure knew the old name, and the row is how the user
		// closes the connection.
		m.entry.Close = func() error { return a.dropMachine(now) }
		a.renamedFiles(was, now)
	}
	if d := a.opening[was]; d != nil {
		delete(a.opening, was)
		a.opening[now] = d
		for i := range d.names {
			if d.names[i] == was {
				d.names[i] = now
			}
		}
		for i := range d.made {
			if d.made[i] == was {
				d.made[i] = now
			}
		}
		// The dial goroutine holds the name it started with, so it is
		// told this way rather than by writing into what it is reading.
		if d.renamed == nil {
			d.renamed = map[string]string{}
		}
		d.renamed[was] = now
	}
	a.renamedWindow(was, now)
	// Every row under the old name: the panes, the tunnels, and the
	// connection itself. They are the same entries the panel groups by.
	for _, group := range a.registry.Groups(time.Now()) {
		if group.Host != was {
			continue
		}
		for _, row := range group.Rows {
			row.Entry.Host = now
		}
	}
	a.refreshServers()
	a.markDirty()
}

// endedAs says on the panel what became of a connection that was being
// made.
//
// The row said "connecting" and nothing took that back, so a connection
// that failed an hour ago still read as one on its way, greyed out.
func (a *app) endedAs(pane *term.Terminal, what string) {
	a.kept[pane] = true
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

// becomeShellPane hands the pane that was watching a connection being
// made to a shell on the machine it reached.
func (a *app) becomeShellPane(name string, command []string, pane *term.Terminal, log *connLog) {
	m := a.about(name).machine
	if m == nil {
		a.endedAs(pane, "not connected")
		log.Failed(fmt.Errorf("nothing is connected to %s", name))
		return
	}
	size := pane.Size()
	sh, err := m.conn.Shell(a.ctx, remote.ShellConfig{
		Command: command,
		Cols:    size.Cols,
		Rows:    size.Rows,
		Term:    m.at.term,
	})
	if err != nil {
		a.endedAs(pane, "no terminal")
		log.Failed(err)
		return
	}
	// Which connection the pane rides on, rather than which machine it
	// is named after: two things can share a name -- the machine -ssh
	// put every pane on, and a connection made from the window -- and
	// closing one must not take the other's panes.
	a.paneOn[pane] = m
	if label := labelFor(command); label != "" {
		if e := a.panes[pane]; e != nil {
			e.Label = label
		}
	}
	log.Became(sh)
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
	if through != nil && a.machines[through.at.name] != through {
		return fmt.Errorf("%s closed while this was being connected through it", through.at.name)
	}
	return nil
}

// dialRoute opens each machine of a route in turn, each through the one
// before it, and reports each as soon as it answers.
//
// made is called with every connection the moment it is up, so the
// window can hold it: a machine that answered stays connected even when
// the machine beyond it does not. It is called from this goroutine.
func dialRoute(ctx context.Context, through *remote.Conn, route []step, made func(int, *remote.Conn)) error {
	for i, s := range route {
		// Checked between hops as well as inside each one, so giving up
		// does not start a login on the next machine along.
		if err := ctx.Err(); err != nil {
			return err
		}
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
				return fmt.Errorf("%s: %w", s.name, err)
			}
			return err
		}
		made(i, conn)
		through = conn
	}
	return nil
}

// startOn runs something on a machine that is already connected to.
func (a *app) startOn(name string, command []string, at *spot) error {
	m := a.about(name).machine
	if m == nil {
		return fmt.Errorf("nothing is connected to %s", name)
	}
	sh, err := m.conn.Shell(a.ctx, remote.ShellConfig{
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
	f := a.about(name)
	if f.machine != nil {
		return []step{f.machine.at}, nil
	}
	hosts, err := a.book.Route(name)
	if err != nil {
		return nil, err
	}
	route := make([]step, len(hosts))
	for i, h := range hosts {
		if h.Window {
			// A window serves gridterm's own protocol and has no shell
			// to log in to. Refused where a route is built rather than
			// only where one is opened: a caller that forgot to ask
			// would otherwise log in to the serve port and find out from
			// the far end refusing a session.
			return nil, fmt.Errorf(
				"%s is a gridterm window, which is taken over rather than logged in to", h.Name)
		}
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
	if host := a.hostMenus.machine(); host != "" {
		return host
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
	if h.heldAt != nil {
		// Saved as a window and already taken over under another name.
		return a.dropWindow(h.heldAt.name)
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
	if h.kind == hostWindow || h.kind == hostSavedWindow || h.heldAt != nil {
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

// forgetPane takes a pane off the record of what runs where, for one
// that has been closed.
func (a *app) forgetPane(t *term.Terminal) {
	delete(a.paneOn, t)
	// And the window it was drawn from, if it was drawn from one. Left
	// behind, the record keeps a terminal that has gone, and letting go
	// of that window later reports a failure to close a pane that was
	// closed long before.
	delete(a.paneOnWindow, t)
	delete(a.watching, t)
	delete(a.kept, t)
	// And the agent the user handed it to, which has nothing left to
	// work in. Its code stops naming anything the moment this is gone.
	if err := a.forgetHandover(t); err != nil {
		a.reportError("Trouble letting go of the agent on that pane", err)
	}
}

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
