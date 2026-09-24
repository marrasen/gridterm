package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// startedAs is how a pane's program was started and what the pane is
// reading, so the same pane can start it again.
//
// The machine it is on and what its row calls it are not here: they live
// on the panel entry, which a rename moves and this would not.
type startedAs struct {
	// argv is the shell a pane here was started on, or the command a
	// pane on a machine was opened with. It is empty for a shell on a
	// machine, which is a shell to type into.
	argv []string

	// dir is where the command was run, so running it again runs it in
	// the same place. Empty for a shell, and for a command with no
	// directory asked for.
	dir string

	// at is the machine the pane's connection was made to, so one that
	// has gone can be dialled again even when the server list has never
	// heard of it.
	at step

	// on is the session the pane reads, wrapped in the meter its row is
	// drawn from, so a program that ends can be let go of with the pane
	// left open.
	on *metered

	// again says the window knows how to start the program once more.
	again bool

	// window is the window connected to that a shell on its own machine
	// was opened through, and nil for anything else. Starting it again
	// asks that window for another shell, into the same pane: the
	// program belongs to the other window, and this one cannot start it
	// by itself.
	window *taken

	// asking says the window has been asked to start it again and has
	// not answered, so a second press does not ask twice.
	asking bool
}

// startsAgain records what a pane's program was started on, so the
// question on it can offer to start it again. m is the connection it
// rides on, and nil for a pane on this machine.
func (a *app) startsAgain(t *term.Terminal, argv []string, dir string, m *machine) {
	s := a.started[t]
	if s == nil {
		return
	}
	s.argv, s.dir, s.again = argv, dir, true
	if m != nil {
		s.at = m.at
	}
}

// startsAgainOnWindow records that a pane's shell was opened on a window
// connected to, on that window's own machine, so the question on it can
// offer another one there.
//
// Only the window's own machine: a shell over there on a machine it is
// connected to in turn is opened by that window, and asking it for
// another opens one on the window's own machine instead.
func (a *app) startsAgainOnWindow(pane *term.Terminal, t *taken) {
	s := a.started[pane]
	if s == nil || t == nil {
		return
	}
	s.again, s.window = true, t
}

// askWhatNext puts the question on the last row of a pane whose program
// has ended: what happened, and the two things to do about it.
func (a *app) askWhatNext(t *term.Terminal) {
	s := a.started[t]
	if s == nil || !s.again {
		// Nothing here knows how to start it again, so there is nothing
		// to ask. The row and the transcript say it has gone.
		return
	}
	e := a.panes[t]
	question, start := whatHappened(e, s.argv, a.howItEnded(t))
	t.Ask(question,
		term.Choice{Label: start, Do: func() error { return a.startAgain(t) }},
		// Close by default. Enter at a terminal that has stopped
		// answering is a reflex, and the reflex must not start a shell
		// the user has finished with or run a deploy a second time.
		term.Choice{
			Label:   "Close",
			Default: true,
			Do:      func() error { return a.closePane(t) },
		})
}

// askAgainIfMoreIsKnown puts the question up again once the program's
// status has landed, and only when that changes the words: a question
// rebuilt on every frame would take back the choice the user moved to.
func (a *app) askAgainIfMoreIsKnown(t *term.Terminal) {
	s := a.started[t]
	if s == nil || !s.again {
		return
	}
	question, _ := whatHappened(a.panes[t], s.argv, a.howItEnded(t))
	if question == t.Asking() {
		return
	}
	a.askWhatNext(t)
}

// endedHow is the way a pane's program ended, which is what the question
// on the pane has to word.
type endedHow int

const (
	// ranAndStopped is a program that ran and stopped, which is the zero
	// value and so has to stay first: a pane that ended with nothing
	// else said about it means this one.
	ranAndStopped endedHow = iota

	// cutOff is a program whose connection went while it was running, so
	// it did not finish and has no status.
	cutOff

	// neverRan is a program whose connection was never made, so nothing
	// ran at all.
	neverRan
)

// outcome is what became of a pane's program: the way it ended, and the
// status it ended with when there is one.
type outcome struct {
	how    endedHow
	status int
	known  bool
}

// argvRoom is how much of a command the question names. Long enough to
// tell one run from another, and short enough that what the question
// asks still fits on the row after it.
const argvRoom = 40

// howItEnded is what became of the program in a pane, as far as the
// window can tell.
func (a *app) howItEnded(t *term.Terminal) outcome {
	why, over := t.Ending()
	status, known := exitStatus(why, over)
	end := outcome{status: status, known: known}
	// A status means the program ran and said how it went, whatever
	// became of the connection afterwards.
	if known {
		return end
	}
	switch {
	case over && errors.Is(why, errNeverConnected):
		end.how = neverRan
	case a.transportWent(t, why, over):
		end.how = cutOff
	}
	return end
}

// transportWent reports whether the connection carrying a pane's program
// went while the program was running, which leaves the program no status
// and no end.
func (a *app) transportWent(t *term.Terminal, why error, over bool) bool {
	if m := a.machines.ranOn(t); m != nil && m.died {
		return true
	}
	var missing *ssh.ExitMissingError
	return over && errors.As(why, &missing)
}

// whatHappened words the question on a pane whose program has ended, and
// names the choice that starts it again.
//
// A command is named and its choice says it will run, because picking it
// runs that command a second time with whatever it does to the machine;
// picking it on a shell only opens a prompt.
func whatHappened(e *conns.Entry, argv []string, end outcome) (question, start string) {
	if e != nil && e.Kind == conns.Command {
		return commandQuestion(argv, end), "Run again"
	}
	// A shell that ended cleanly is the ordinary way out, and saying so
	// is noise; one that died is worth knowing about.
	said := ""
	if end.known && end.status != 0 {
		said = " Exit " + strconv.Itoa(end.status) + "."
	}
	// The words ssh itself prints after a shell is exited, and the same
	// words fit a transport that went. What to do about it is on the
	// buttons rather than in the sentence: "Yes" only means something
	// to somebody who read the question, and a pane that has just
	// stopped answering is read at a glance.
	return "Connection closed." + said, "Reconnect"
}

// commandQuestion words the question on a pane that ran one command,
// naming it because which run this was is the whole of what the user is
// deciding on.
//
// How it ended is worded rather than assumed: a command that was cut off
// did not finish, and saying it had is as wrong as reporting an exit of
// zero.
func commandQuestion(argv []string, end outcome) string {
	what := grid.TrimTail(labelFor(argv), argvRoom)
	if what == "" {
		what = "The command"
	}
	switch end.how {
	case cutOff:
		return "The connection went while " + what + " was running. Run it again?"
	case neverRan:
		return "The connection was not made, so " + what + " did not run. Run it again?"
	}
	if !end.known {
		return what + " has stopped. Run it again?"
	}
	return what + " finished. Exit " + strconv.Itoa(end.status) + ". Run it again?"
}

// exitStatus is the status a program ended with, taken from what its
// session reported, and whether one is known at all.
//
// over says the session has said how the program ended. One still
// running has no status, and neither has one whose transport broke
// before it could send one.
func exitStatus(why error, over bool) (int, bool) {
	switch {
	case !over:
		return 0, false
	case why == nil:
		return 0, true
	}
	if far, ok := errors.AsType[*ssh.ExitError](why); ok {
		return far.ExitStatus(), true
	}
	if here, ok := errors.AsType[*exec.ExitError](why); ok {
		return localStatus(here.ExitCode(), here.Sys())
	}
	return 0, false
}

// signalled is a process state that says the program was killed by a
// signal rather than exiting, which is what exec reports through Sys.
type signalled interface {
	Signaled() bool
	Signal() syscall.Signal
}

// localStatus is the status a program on this machine ended with, from
// the exit code and the process state exec reports.
//
// A program killed by a signal has an exit code of -1, which is not a
// status any shell reports, so it is counted the way the shells and the
// remote side count one: 128 plus the signal. A platform with no signal
// to name leaves the status unknown rather than passing -1 on.
func localStatus(code int, sys any) (int, bool) {
	if code >= 0 {
		return code, true
	}
	s, ok := sys.(signalled)
	if !ok || !s.Signaled() {
		return 0, false
	}
	return 128 + int(s.Signal()), true
}

// startAgain runs a pane's program once more in the same pane.
//
// Where the pane is decides what that means: a shell on this machine, a
// shell on a machine that is still connected, or a machine that has to
// be dialled again first.
func (a *app) startAgain(t *term.Terminal) error {
	s, e := a.started[t], a.panes[t]
	if s == nil || e == nil || !s.again {
		return errors.New("nothing here says what this pane was started on")
	}
	if s.window != nil {
		return a.startAgainOnWindow(t, s.window)
	}
	if e.Host == conns.Local {
		return a.startAgainHere(t, s)
	}
	if m := a.about(e.Host).machine; m != nil {
		return a.startAgainOn(t, m, s)
	}
	return a.dialAgainFor(t, e.Host, s)
}

// startAgainHere starts the shell a pane on this machine ran, on the
// argv it ran before.
func (a *app) startAgainHere(t *term.Terminal, s *startedAs) error {
	size := t.Size()
	sess, err := a.newShell(s.argv, s.dir, size.Cols, size.Rows)
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	if err := a.restartPane(t, sess, a.startingLabel(t, s)); err != nil {
		// The session is ours now and nothing else will close it.
		_ = sess.Close()
		return err
	}
	return nil
}

// startAgainOn opens another shell on a machine the window is still
// connected to, riding on the connection it already has.
func (a *app) startAgainOn(t *term.Terminal, m *machine, s *startedAs) error {
	size := t.Size()
	sh, err := m.conn.Shell(a.ctx, remote.ShellConfig{
		Command: s.argv,
		Dir:     s.dir,
		Cols:    size.Cols,
		Rows:    size.Rows,
		Term:    m.at.term,
	})
	if err != nil {
		return err
	}
	if err := a.restartPane(t, sh, a.startingLabel(t, s)); err != nil {
		// The shell is ours now and nothing else will close it.
		_ = sh.Close()
		return err
	}
	// Which connection the pane rides on rather than which machine it is
	// named after, for the reason the on field of machines gives.
	a.machines.runs(t, m)
	return nil
}

// startAgainOnWindow starts again the shell a pane on a window connected
// to was watching, and puts it back in the pane.
//
// The other window is asked to start its own pane's shell again and the
// pane watches that, so the pane over there is the one that comes back
// rather than a new one opening beside a finished one on every
// reconnect. A window of a build that cannot is asked for a new shell
// instead, the way a terminal is opened on it from its plus.
func (a *app) startAgainOnWindow(pane *term.Terminal, t *taken) error {
	if !a.windows.holds(t) {
		return fmt.Errorf("this window is no longer connected to %s", t.name)
	}
	size := pane.Size()
	if what, ok := a.windows.watching(pane); ok {
		if open, still := a.openOver(what); still {
			if s := a.started[pane]; s != nil {
				if s.asking {
					return nil
				}
				s.asking = true
			}
			a.startAgainOver(pane, t, open, size)
			return nil
		}
	}
	return a.openAgainOnWindow(pane, t, size)
}

// startAgainOver asks the other window to start its pane's program again
// and has the pane watch it, off the goroutine that draws: the answer is
// a round trip away and waits on that window's own frame. A window of a
// build that cannot is asked for a new shell instead, the same way.
//
// A failure puts the question back on the pane, so the user is not left
// looking at a finished pane with nothing to press.
func (a *app) startAgainOver(pane *term.Terminal, t *taken, open serve.Open, size ui.Size) {
	// What the pane watched before, so a fresh shell's binding, which can
	// land before the answer below does, is not the one taken away.
	was, _ := a.windows.watching(pane)
	a.closes.inBackground(func() error {
		var (
			sess  session.Session
			fresh bool
		)
		err := t.win.StartAgain(serve.Attached{ID: open.ID, Host: open.Host, Kind: open.Kind})
		switch {
		case err == nil:
			sess, err = t.win.Attach(open, size.Cols, size.Rows)
		case errors.Is(err, serve.ErrCannotStartAgain):
			fresh = true
			sess, err = t.win.Open(size.Cols, size.Rows, func(named serve.Attached) {
				a.pump.post(func() { a.bindWatched(pane, t, named) })
			})
		}
		a.pump.post(func() {
			if s := a.started[pane]; s != nil {
				s.asking = false
			}
			if !a.live(pane) {
				// Closed while the answer was on its way. What runs over
				// there runs on in its own pane there.
				if sess != nil {
					_ = sess.Close()
				}
				return
			}
			if err == nil {
				err = a.restartPane(pane, sess, "")
				if err != nil {
					err = errors.Join(err, sess.Close())
				}
			}
			if err != nil {
				a.reportError("Could not reconnect to "+t.name, err)
				a.askWhatNext(pane)
				return
			}
			if fresh {
				// What it was watching has ended over there. The new
				// shell is bound once the window says what it calls it,
				// which may have happened already.
				if now, _ := a.windows.watching(pane); now == was {
					delete(a.windows.seen, pane)
				}
				a.windows.draws(pane, t)
			}
		})
		return nil
	})
}

// openAgainOnWindow asks a window for a new shell and puts it in a pane
// whose shell there ended, for a window that cannot start the old one
// again.
func (a *app) openAgainOnWindow(pane *term.Terminal, t *taken, size ui.Size) error {
	sess, err := t.win.Open(size.Cols, size.Rows, func(named serve.Attached) {
		// Said on a goroutine of the session's, and the record of what a
		// pane is watching belongs to the one that draws.
		a.pump.post(func() { a.bindWatched(pane, t, named) })
	})
	if err != nil {
		return err
	}
	if err := a.restartPane(pane, sess, ""); err != nil {
		// The shell over there is ours now and nothing else will close it.
		return errors.Join(err, sess.Close())
	}
	// What it was watching has ended over there. The new shell is bound
	// once the window says what it calls it.
	delete(a.windows.seen, pane)
	a.windows.draws(pane, t)
	return nil
}

// startingLabel is what a pane's row says the moment its program starts:
// what a command runs, and nothing for a shell, which names itself once
// it has a title.
func (a *app) startingLabel(t *term.Terminal, s *startedAs) string {
	if e := a.panes[t]; e != nil && e.Kind == conns.Command {
		return labelFor(s.argv)
	}
	return ""
}

// restartPane puts a new session under a pane whose program has ended
// and puts back everything that was let go of when it ended: the meter
// its row is drawn from, the rate that meter is sampled against, what
// the row says, and the pane's place among the ones still running.
//
// The pane keeps what the last program printed, which is why the program
// starts here rather than in a pane of its own.
func (a *app) restartPane(t *term.Terminal, sess session.Session, label string) error {
	s, e := a.started[t], a.panes[t]
	if s == nil || e == nil {
		return errors.New("this pane is not one the window is holding")
	}
	// A meter of its own, because the one the row was drawn from closed
	// with the program that ended.
	counted := &metered{Session: sess, m: meter.New()}
	// First, because it closes the session that ended and refuses a pane
	// that cannot take another program.
	if err := t.Restart(counted); err != nil {
		return err
	}
	s.on = counted
	e.Meter = counted.m
	// The rate remembers the totals of the meter that has gone, so a
	// sample against a fresh one would wrap and read as an absurd speed.
	delete(a.rates, e)
	if err := a.teachPaneShell(sess, e.Host, e.Kind, s.argv); err != nil {
		return err
	}
	// The row said what became of the program. It is running again, so
	// the row says what it is rather than how it ended.
	e.Label, e.Note = label, ""
	a.machines.startedAgain(t)
	delete(a.ended, t)
	a.markDirty()
	return nil
}
