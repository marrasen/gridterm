package main

import (
	"context"
	"fmt"
	stdlog "log"
	"slices"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// opening is what a route is opened for: a shell to type into, one
// command to run, or a pane of the file manager.
type opening struct {
	// command is what the shell runs, and empty for a shell to type
	// into. It is read only when files is false.
	command []string

	// dir is where the command runs, or where a file pane opens. Empty
	// is wherever the login lands, and for files whatever the machine
	// calls home.
	dir string

	// files opens a pane of the file manager on the machine and no shell
	// on it, which is what "Files" asks for. command is not read for one.
	files bool

	// into is a pane whose program has ended and which takes what is
	// opened, rather than a pane of its own being made for it. It is the
	// pane the user answered the question on, and it keeps everything
	// the last program printed.
	into *term.Terminal

	// only says to make the connection and open nothing on it.
	//
	// It is how a file pane whose machine dropped asks for it back: the
	// pane is on screen already and wants the connection, not a second
	// pane beside it.
	only bool
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
	if open.into != nil && !a.live(open.into) {
		// The pane that asked has been closed since, which a dial queued
		// behind another one and settling minutes later finds.
		return
	}
	// A window the server list holds is taken over, not logged in to.
	//
	// Here because this is the one place every connection really goes
	// through. Guarding the ways in instead left one of them -- opening
	// a terminal on a saved machine, which builds its own route -- still
	// logging in to the serve port, and the far end refusing a session
	// was the first anything heard of it.
	if h, ok := a.savedWindowInRoute(name, route); ok {
		if open.into != nil {
			// Taking a window over draws its panes as panes of its own,
			// so there is nothing to put in the pane that asked.
			a.reportError("Could not restart the command", fmt.Errorf(
				"%s is a gridterm window now, which is connected to rather than logged in to", name))
			return
		}
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
			a.reportError("Could not open on "+name, err)
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
	held := &dialling{cancel: cancel, names: names, route: slices.Clone(route)}
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
	// one line that says where to read the rest of it. A pane answering
	// the reconnect question takes all of that on rather than a second
	// pane opening beside it.
	//
	// Letting go of the names is done here rather than by the closure
	// that finishes the dial: a dial that has not come back yet still
	// has to stop holding them, or nothing can try again.
	log := newConnLog(func() { a.pump.post(func() { a.machines.giveUp(held) }) })
	// A pane taking this on already holds a transcript the user asked to
	// keep, so the account is left where it is rather than the pane
	// being cleared when the connection is made.
	log.keep = open.into != nil
	held.log = log
	pane, err := a.openFor(open, log, name, "connecting", at)
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
		// And the steps that went wrong, stamped in red. An account
		// where every row reads the same says a connection went well
		// when it did not.
		missing[i].cfg.Wrong = log.sayBadly
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
					a.endedAs(pane, stateCancelled)
					log.GaveUp()
					return
				}
				a.endedAs(pane, stateNotConnected)
				log.Failed(err)
				return
			}
			// Given up on, or the machine it was reached through closed,
			// while the last handshake was finishing.
			if why := a.stillWanted(gaveUp, through); why != nil {
				a.sayStillConnected(log, held)
				a.machines.settle(held, false)
				a.endedAs(pane, stateNotConnected)
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
		a.endedAs(pane, stateNotConnected)
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

// What a row says about a connection that is not running any more.
//
// A state each, in the register a row is read in: a short phrase, lower
// case and no full stop, because a row is scanned in a narrow column
// beside a dozen others rather than read. See WORDING.md.
//
// Constants because more than one surface says them. A connection that
// dropped reads the same whether it was carrying a pane or a whole
// window, and two copies of a state drift the first time one is
// reworded.
const (
	// transportLost is the connection carrying it going, rather than the
	// program in it finishing. Both leave a grey row, and the reason
	// lives on the machine's row, which the user can clear. This is what
	// is left on the pane's own.
	transportLost = "connection lost"

	// stateCancelled is the user walking away from it while it was being
	// made.
	stateCancelled = "cancelled"

	// stateNotConnected is a connection that was never made, and
	// stateNotTakenOver the same for a window.
	stateNotConnected = "not connected"
	stateNotTakenOver = "not taken over"

	// stateNoTerminal is a window reached with nothing on it to show.
	stateNoTerminal = "no terminal"

	// stateTakenOver is a window this one is working in.
	stateTakenOver = "taken over"

	// stateStoppedSharing is that window saying it has stopped, and
	// stateClosedByWindow is it closing this connection in particular.
	// Neither names the window: the row is already under its name.
	stateStoppedSharing = "stopped sharing"
	stateClosedByWindow = "closed by that window"
)

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
				a.reportError("Could not disconnect from "+name, err)
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
		a.endedAs(pane, stateNotConnected)
		log.Failed(fmt.Errorf("nothing is connected to %s", name))
		return
	}
	// The machine the route was for, and only that one: every machine on
	// the way shares this account, and a line on each of their rows would
	// open an account about somewhere else.
	m.log = log
	if open.only {
		// Nothing to open: what asked for this is already on screen.
		// The pane that watched the dial goes the way it does for
		// files, because nothing rides in it either.
		log.Connected()
		if err := a.closePane(pane); err != nil {
			a.reportError("Could not close the pane", err)
		}
		return
	}
	if open.files {
		a.becomeFilesPane(name, pane, log, open.dir)
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
func (a *app) becomeFilesPane(name string, pane *term.Terminal, log *connLog, at string) {
	if err := a.browseOn(name, at); err != nil {
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
		a.reportError("Could not close the pane", err)
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
		// On the machine even with nothing running in it, or closing the
		// machine's row would leave this pane behind offering to reconnect
		// to a machine the window no longer holds.
		a.machines.endedOn(pane, m)
		log.Refused(name, "a terminal", err)
		return
	}
	// Which connection the pane rides on rather than which machine it is
	// named after, for the reason the on field of machines gives.
	a.machines.runs(pane, m)
	if len(command) == 0 {
		if err := a.teachPaneShell(sh, name, conns.Terminal, nil); err != nil {
			log.Refused(name, "the shell setup", err)
		}
	}
	a.startsAgain(pane, command, "", m)
	if label := labelFor(command); label != "" {
		if e := a.panes[pane]; e != nil {
			e.Label = label
		}
	}
	log.Became(name, sh)
}

// openFor puts a session in the pane a restart named, and in a pane of
// its own otherwise.
func (a *app) openFor(open opening, sess session.Session, host, label string,
	at *spot) (*term.Terminal, error) {

	if open.into != nil {
		return open.into, a.restartPane(open.into, sess, label)
	}
	return a.openSessionPane(sess, host, open.kind(), label, at)
}

// startOn opens what was asked for on a machine that is already
// connected to.
func (a *app) startOn(name string, open opening, at *spot) error {
	if open.only {
		// Already connected, which is the whole of what was asked for.
		return nil
	}
	if open.files {
		// at is not used: a file pane goes in the file manager itself.
		return a.browseOn(name, open.dir)
	}
	m := a.about(name).machine
	if m == nil {
		return fmt.Errorf("nothing is connected to %s", name)
	}
	sh, err := m.conn.Shell(a.ctx, remote.ShellConfig{
		Command: open.command,
		Dir:     open.dir,
		Cols:    a.lastSize[0],
		Rows:    a.lastSize[1],
		Term:    m.at.term,
	})
	if err != nil {
		return err
	}
	t, err := a.openFor(open, sh, name, labelFor(open.command), at)
	if err != nil {
		// The shell is ours and nothing else knows about it.
		_ = sh.Close()
		return err
	}
	if len(open.command) == 0 {
		if err := a.teachPaneShell(sh, name, conns.Terminal, nil); err != nil {
			return err
		}
	}
	// Which connection the pane rides on rather than which machine it is
	// named after, for the reason the on field of machines gives.
	a.machines.runs(t, m)
	a.startsAgain(t, open.command, open.dir, m)
	return nil
}

// openOn puts a terminal or a command on a machine, connecting to it
// first when nothing is connected to it yet.
func (a *app) openOn(name string, command []string, dir string, at *spot) error {
	route, err := a.route(name)
	if err != nil {
		return err
	}
	a.openRoute(name, route, opening{command: command, dir: dir}, at)
	return nil
}

// dialAgainFor connects to a pane's machine once more and puts what the
// pane was running back in the same pane.
//
// The saved route when the list has one, and the step the machine was
// reached by otherwise: a machine connected to from a typed target is on
// no list and still has to be reachable again.
func (a *app) dialAgainFor(t *term.Terminal, host string, s *startedAs) error {
	route, err := a.route(host)
	if err != nil {
		if a.about(host).saved || s.at.cfg.Host == "" {
			return err
		}
		route = []step{{name: host, cfg: s.at.cfg, term: s.at.term}}
	}
	a.sayIfTheTargetMoved(t, host, route, s)
	a.openRoute(host, route, opening{command: s.argv, into: t}, nil)
	if a.ended[t] {
		// The dial never reached the pane, and openRoute has said why.
		// The question goes back up so it can be answered again: a pane
		// left dead with nothing to answer could not be tried at all.
		a.askWhatNext(t)
	}
	return nil
}

// sayIfTheTargetMoved writes a line into a pane whose machine is not at
// the address the pane was reached at, which a saved entry edited since
// the pane opened leaves it.
//
// Reconnecting dials the new address and puts it under the old
// transcript, so the two runs would otherwise read as one machine.
func (a *app) sayIfTheTargetMoved(t *term.Terminal, host string, route []step, s *startedAs) {
	if len(route) == 0 || s.at.cfg.Host == "" {
		return
	}
	was, now := s.at.cfg.Target(), route[len(route)-1].cfg.Target()
	if was == now {
		return
	}
	t.Say("-- gridterm: " + host + " is " + now + " now. This pane was on " + was + " --")
}

// connectAndBrowse connects to a machine and opens a pane of the file
// manager on it when it answers.
//
// The route is built here rather than in browse.go because openRoute is
// the one place that dials.
func (a *app) connectAndBrowse(name, at string) error {
	route, err := a.route(name)
	if err != nil {
		return err
	}
	a.openRoute(name, route, opening{files: true, dir: at}, nil)
	return nil
}

// filesystemAgain hands back a filesystem on a machine, connecting to
// it first when nothing is.
//
// On the goroutine that draws. then is called when the connection is
// made or has failed, and for one already on its way that means when
// that one settles rather than starting a second: four panes on one
// machine that dropped make one connection between them.
// calledNow is asked what a machine goes by now, for a read that has
// been waiting while it was renamed. was is the name the dial it waited
// on knows it by; the answer is the name to ask for, and empty when
// nothing wants the machine any more. A nil one means the dial's answer
// is the whole of it.
//
// It is asked rather than worked out here because a filesystem also
// files its pane under the name it finds, and finished work has no pane
// to file. Both ask the list by the id of the saved server, so what
// comes back is that server's name and never another machine's.
func (a *app) filesystemAgain(host string, at step, calledNow func(was string) string,
	then func(vfs.FS, step, error)) {
	// answerOn hands back a filesystem and the machine it was opened
	// on. The machine goes with it because the caller keeps it, and a
	// caller that looked it up again by the name it asked with could
	// find a different machine: a name given up in a rename can be
	// saved for somewhere else while the dial is still running.
	answerOn := func(name string) {
		if m := a.about(name).machine; m != nil && anotherServer(m.at, at) {
			then(nil, step{}, connectedElsewhere(name))
			return
		}
		f, err := a.machineFilesWithArchives(name)
		var on step
		if m := a.about(name).machine; m != nil {
			on = m.at
		}
		then(f, on, err)
	}
	answer := func() { answerOn(host) }
	// Why it was not made is in the account of the connection, which is
	// where a reason belongs: this is read in a file pane, which has no
	// room for one and nothing to do with it.
	notMade := func() {
		then(nil, step{}, fmt.Errorf("the connection to %s was not made", groupName(host)))
	}
	// reachable says a machine on the way to this one is connected, so
	// this one is worth asking for again.
	//
	// Asked of the window rather than of the dial that settled, because
	// what a dial reports is whether its own far end answered -- which
	// is not this machine and need not even be the one on the way. A
	// machine of a route that answered is held, stays held when the
	// dial beyond it fails, and is no longer a name that dial
	// remembers.
	reachable := func(name string) bool {
		route, err := a.route(name)
		if err != nil {
			return false
		}
		through, _, err := a.plan(route)
		return err == nil && through != nil
	}
	// Said on the bottom row for as long as the wait lasts, because a
	// folder click that waits the length of a login with nothing on
	// screen reads as a window that has stopped. Every way of waiting
	// says it, not only the one that starts the connection: a second
	// pane queueing behind the first waits just as long.
	line := "Reconnecting to " + groupName(host) + "…"
	// waitFor queues this read behind a connection being made.
	//
	// ours says that connection is this machine's own attempt. When one
	// of those settles the read has its answer either way: the machine
	// is there or it is not, and asking again would dial it a second
	// time the moment the first attempt failed -- and a third, and a
	// fourth, for as long as the way there looked open. It would also
	// dial it again the moment the user gave up on it, which is the
	// opposite of what giving up means.
	//
	// A dial that was on its way somewhere else is the other case. That
	// one says nothing about this machine, which may never have been
	// tried, so this asks again.
	//
	// Either way it asks for the machine as it is called then, because
	// one renamed while it was being dialled is held under the name the
	// window uses now.
	//
	// Queued on answering rather than waiting: a pane is blocked on
	// this, so a connection that was not made has to come back as a
	// failure. What waits is thrown away when the dial fails, which
	// here would leave the pane reading for ever.
	waitFor := func(d *dialling, ours bool) {
		a.sayWhile(line)
		d.answering = append(d.answering, func(bool) {
			a.doneSaying(line)
			// What the machine is called now: the dial's own answer,
			// and then whatever the caller knows on top of it.
			now := d.nameNow(host)
			if calledNow != nil {
				now = calledNow(now)
				if now == "" {
					// Nothing is using this any more. The pane it
					// belonged to was closed while this waited.
					notMade()
					return
				}
			}
			if a.about(now).machine != nil {
				answerOn(now)
				return
			}
			if ours || !reachable(now) {
				notMade()
				return
			}
			// The step goes by that name too, or a machine on no list
			// would be dialled under the name it has stopped using.
			on := at
			on.name = now
			a.filesystemAgain(now, on, calledNow, then)
		})
	}
	if a.about(host).machine != nil {
		answer()
		return
	}
	if d := a.machines.connecting(host); d != nil {
		if d.settled {
			answer()
			return
		}
		waitFor(d, true)
		return
	}
	route, err := a.route(host)
	if err != nil {
		// A saved server is reached by the route the list gives it and
		// no other way. The step kept is where it was, and the list has
		// had the last word on where it is since.
		if a.about(host).saved || at.cfg.Host == "" || at.id != "" {
			then(nil, step{}, err)
			return
		}
		// No route on any list, and one is still owed: a machine
		// connected to from a typed target is on no list and was
		// reached all the same. The step it was reached by is what
		// this filesystem kept, the way a pane keeps the one its
		// shell was started on.
		route = []step{at}
	}
	// A machine on the way already being connected to: wait for that one
	// and ask again. openRoute would put up the dialog about it, which
	// asks the user about a connection they did not ask for -- a pane
	// read through a filesystem is what is happening here -- and answer
	// this read with "nothing is connected" while they read it.
	for _, s := range route {
		if d := a.about(s.name).dialling; d != nil && !d.settled {
			waitFor(d, false)
			return
		}
	}

	a.sayWhile(line)
	a.openRoute(host, route, opening{only: true}, nil)
	if d := a.machines.connecting(host); d != nil && !d.settled {
		waitFor(d, true)
		return
	}
	a.doneSaying(line)
	answer()
}

// anotherServer reports whether a connection is to a saved server other
// than the one a filesystem or a piece of work is on.
//
// The name cannot say: a connection left under a name when its entry was
// renamed and pointed somewhere else keeps that name, and a server saved
// since can be given it too. A connection to a machine on no list says
// nothing either way, and is taken at its name the way it always was.
func anotherServer(held, want step) bool {
	return want.id != "" && held.id != "" && held.id != want.id
}

// connectedElsewhere is what a call is answered with when the name its
// machine goes by is held by a connection to a different one.
func connectedElsewhere(name string) error {
	return fmt.Errorf("%s is connected to another machine", groupName(name))
}

// labelFor names a connection by what it is running.
func labelFor(command []string) string {
	return strings.Join(command, " ")
}
