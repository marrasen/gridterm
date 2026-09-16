package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui/term"
)

// machines are the connections this window holds, the ones it is making,
// and the panes running on them.
//
// A name is held by at most one of connected and connecting, because a
// name stands for one machine. A dial holds each name it is going to
// dial, which is the part of a route nothing is connected to yet. A
// machine that answers crosses from connecting to connected in one
// step. A window being taken over holds its name here and lands in
// windows rather than here, so that name never crosses.
//
// Only the goroutine that draws touches any of it. Its methods are in
// this file, apart from rename, which is in rename.go with the rest of
// what a rename has to follow.
type machines struct {
	// held is every connection, by the name the panel calls it. A
	// second terminal on a machine rides on the connection already here
	// rather than logging in again.
	held map[string]*machine

	// opening names every machine being connected to and every window
	// being taken over right now. Every name of a route points at the
	// same dialling, so giving up on one gives up on the route.
	//
	// A connection still being made is the one most likely to be given
	// up on, because it is the one that is taking too long.
	opening map[string]*dialling

	// on says which connection a pane runs on. Panes are grouped on the
	// panel by a name, and a name can hold more panes than one
	// connection carries -- a pane left behind by a connection that
	// failed keeps the name -- so closing one connection must find its
	// own panes rather than everything under that name.
	on map[*term.Terminal]*machine

	// making counts the connections being made and the windows being
	// taken over. The panel shows a row for each, so several can be on
	// their way at once; this is only so a test can tell when they have
	// all landed.
	making int
}

// newMachines builds the record of connections, holding none.
func newMachines() *machines {
	return &machines{
		held:    make(map[string]*machine),
		opening: make(map[string]*dialling),
		on:      make(map[*term.Terminal]*machine),
	}
}

// named is the connection held under a name, or nil.
//
// Exactly, not ignoring case: the key is the name the connection was
// made under, and the server list's own spelling is what about asks
// with.
func (ms *machines) named(name string) *machine { return ms.held[name] }

// connecting is the dial on its way to a name, or nil.
func (ms *machines) connecting(name string) *dialling { return ms.opening[name] }

// count is how many connections are held, for a test.
func (ms *machines) count() int { return len(ms.held) }

// names are the names the connections are held under, in order, for a
// test saying what it found instead.
func (ms *machines) names() []string { return slices.Sorted(maps.Keys(ms.held)) }

// reaching are the names being connected to, in order, for a test
// saying what it found instead.
func (ms *machines) reaching() []string { return slices.Sorted(maps.Keys(ms.opening)) }

// take records a connection under its name, with no row and nothing
// watching for it dropping, for a test that needs a name held and
// nothing behind it.
func (ms *machines) take(m *machine) { ms.held[m.at.name] = m }

// answered holds a connection a dial has just made and stops its name
// counting as connecting, which is where a name crosses from one side of
// the invariant to the other.
//
// It refuses when the name has gone to something else while the last of
// the handshake was finishing.
func (ms *machines) answered(d *dialling, m *machine) error {
	name := m.at.name
	if ms.held[name] != nil {
		return fmt.Errorf("%s is connected already, so this one was let go of", name)
	}
	if taken := ms.opening[name]; taken != nil && taken != d {
		return fmt.Errorf("something else is now connecting to %s, so this one was let go of", name)
	}
	ms.held[name] = m
	ms.releaseName(d, name)
	return nil
}

// drop lets go of a connection, by identity: the name it is held under
// is not always the name the caller asked about.
func (ms *machines) drop(m *machine) {
	for name, have := range ms.held {
		if have == m {
			delete(ms.held, name)
			return
		}
	}
}

// riding names the connections reached through one.
func (ms *machines) riding(m *machine) []string {
	var names []string
	for name, other := range ms.held {
		if other.conn.Via() == m.conn {
			names = append(names, name)
		}
	}
	return names
}

// drain takes every connection out and hands them back, for a window
// that is shutting down.
func (ms *machines) drain() []*machine {
	out := make([]*machine, 0, len(ms.held))
	for name, m := range ms.held {
		delete(ms.held, name)
		out = append(out, m)
	}
	return out
}

// runs records that a pane is running on a connection.
func (ms *machines) runs(pane *term.Terminal, m *machine) { ms.on[pane] = m }

// runningOn is the connection a pane runs on, or nil when it runs on
// the machine gridterm itself is on.
func (ms *machines) runningOn(pane *term.Terminal) *machine { return ms.on[pane] }

// panesOn are the panes running on one connection.
func (ms *machines) panesOn(m *machine) []*term.Terminal {
	var out []*term.Terminal
	for pane, on := range ms.on {
		if on == m {
			out = append(out, pane)
		}
	}
	return out
}

// forget takes a pane off the record, for one that has been closed.
func (ms *machines) forget(pane *term.Terminal) { delete(ms.on, pane) }

// beingMade is how many connections and take-overs are on their way,
// for a test.
func (ms *machines) beingMade() int { return ms.making }

// dialStarted counts a connection or a take-over out.
func (ms *machines) dialStarted() { ms.making++ }

// dialEnded counts one back.
func (ms *machines) dialEnded() { ms.making-- }

// holdNames holds every name a connection needs while it is being made.
//
// A name something is already connected under is refused, and then
// nothing is held at all.
func (ms *machines) holdNames(d *dialling) error {
	for _, n := range d.names {
		if ms.held[n] != nil {
			return fmt.Errorf("%s is already connected; close it first", n)
		}
	}
	for _, n := range d.names {
		ms.opening[n] = d
	}
	return nil
}

// releaseName stops holding one name of a route, leaving the rest held.
//
// It is what a machine that has just connected does. The connection is
// there now, so the window is no longer connecting to it and a terminal
// can be opened on it while the machine beyond it is still being
// reached. A name another attempt has since taken is left alone.
func (ms *machines) releaseName(d *dialling, name string) {
	if ms.opening[name] == d {
		delete(ms.opening, name)
	}
}

// release stops holding a route's names, so another attempt can have
// them.
func (ms *machines) release(d *dialling) {
	for _, n := range d.names {
		ms.releaseName(d, n)
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
func (ms *machines) giveUp(d *dialling) {
	d.cancel()
	ms.release(d)
	// What was queued behind it goes with it. Giving up on a connection
	// and having the window make it anyway a moment later is not what
	// giving up means.
	ms.dropWaiting(d)
}

// settle lets go of a connection that has been made, and runs whatever
// was waiting for it.
//
// made says the machine answered. What was waiting was waiting for that
// machine, so a connection that was not made leaves it nothing to do:
// starting a fresh connection instead is not what "wait for it" says,
// and on a machine that never answers it would ask again and again.
func (ms *machines) settle(d *dialling, made bool) {
	ms.release(d)
	if !made {
		ms.dropWaiting(d)
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
func (ms *machines) dropWaiting(d *dialling) {
	if len(d.waiting) > 0 && d.log != nil {
		d.log.Say("what was waiting for this was not started")
	}
	d.settled, d.waiting = true, nil
}

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

	// log is the account of how the machine was reached, kept after the
	// pane folded it away so "How it was reached" can show it.
	log *connLog
}

// hold puts a row on the panel for a connection the window has just
// taken and watches for the far end dropping it. via names what carries
// it, or is empty.
func (a *app) hold(m *machine, via string) {
	// Only what the dot cannot say. That it is connected is the dot's
	// business; which machine it was reached through is not.
	note := ""
	if via != "" {
		note = "via " + via
	}
	m.entry = &conns.Entry{
		Host:   m.at.name,
		Kind:   conns.Server,
		Label:  m.conn.String(),
		Note:   note,
		Reveal: func() { a.revealMachine(m) },
		// Closed by the connection itself rather than by the name it is
		// held under now: a rename can give it another at any time.
		Close: func() error { return a.dropMachine(m.at.name) },
	}
	a.registry.Add(m.entry)
	// Nothing is parked on a connection this window has only just made, so
	// what an earlier one under this name was counted for goes.
	a.serving.relaysEnded(m.at.name)
	// A machine the window has reached is worth a command of its own,
	// whether or not it was ever saved.
	a.refreshServers()

	// A connection the far end drops is still held here, saying it is
	// connected, until something notices. Nothing else here would.
	go func() {
		// Why it ended, not only that it did: it goes on the greyed row,
		// which is all the user has to work from afterwards.
		why := m.conn.Wait()
		a.pump.post(func() { a.machineDied(m, why) })
	}()
}

// revealMachine puts one of a machine's panes in front of the user.
//
// A connection with nothing open on it has nothing to show, so nothing
// happens: there is no window for a connection itself.
func (a *app) revealMachine(m *machine) {
	for _, pane := range a.machines.panesOn(m) {
		a.focus(pane)
		return
	}
}

// machineDied takes away a machine whose connection has gone on its own.
//
// The panes on it end by themselves, because their shells stop reading.
// The row stays, greyed, the way greyRow says a dropped connection's row
// does.
func (a *app) machineDied(m *machine, why error) {
	// By identity: the name may hold another connection by now.
	if a.machines.named(m.at.name) != m {
		// Already closed from the window, and its row with it.
		return
	}
	a.machines.drop(m)
	// The file sessions relayed to it and left parked have ended with the
	// transport, so the window stops counting them against it.
	a.serving.relaysEnded(m.at.name)
	// A pane of the file manager on this machine is reading through a
	// session that has gone. It is taken away here, because nothing else
	// would: a pane does not end by itself the way a shell does.
	if err := a.graceLogged(a.closeFilesOn(m.at.name)); err != nil {
		a.reportError("Trouble closing the file panes on "+m.at.name, err)
	}
	// The tunnels went with it, but a local forward listens on a socket
	// of this machine, which the far end dropping does nothing to.
	if err := a.tunnelsDiedOn(m); err != nil {
		a.reportError("Trouble closing the tunnels on "+m.at.name, err)
	}
	// The note said what carried it, and now it says why it went.
	m.entry.Note = ""
	a.greyRow(m.entry, why)
	// What the window is holding has changed, so the commands and their
	// titles are worked out again.
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
		a.machines.giveUp(d)
		return nil
	}
	m := on.machine
	if m == nil {
		return nil
	}
	a.machines.drop(m)
	a.registry.Drop(m.entry)
	// Closing the connection ends every file session left parked on it,
	// so what was counted against the machine goes too.
	a.serving.relaysEnded(m.at.name)

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
	for _, rider := range a.machines.riding(m) {
		errs = append(errs, a.dropMachine(rider))
	}
	for _, pane := range a.machines.panesOn(m) {
		errs = append(errs, a.closePane(pane))
	}
	// Last, once everything that was on it has gone: a machine still
	// holding a pane is still a machine worth a command.
	a.refreshServers()
	// A file session that closed on a bound of its own is logged rather
	// than shown: the machine has gone either way.
	return a.graceLogged(errors.Join(errs...))
}

// forgetPane takes a pane off the record of what runs where, for one
// that has been closed.
func (a *app) forgetPane(t *term.Terminal) {
	a.machines.forget(t)
	// And the window it was drawn from, if it was drawn from one. Left
	// behind, the record keeps a terminal that has gone, and letting go
	// of that window later reports a failure to close a pane that was
	// closed long before.
	a.windows.forget(t)
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
	held := a.machines.drain()
	// A machine reached through another is closed by that one, so it is
	// not closed again here: the second close would report the first
	// one's failure a second time.
	carried := make(map[*machine]bool, len(held))
	for _, m := range held {
		for _, other := range held {
			if m.conn.Via() == other.conn {
				carried[m] = true
			}
		}
	}
	var errs []error
	for _, m := range held {
		if carried[m] {
			continue
		}
		errs = append(errs, m.conn.Close())
	}
	return errors.Join(errs...)
}
