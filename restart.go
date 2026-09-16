package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/session"
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

	// at is the machine the pane's connection was made to, so one that
	// has gone can be dialled again even when the server list has never
	// heard of it.
	at step

	// on is the session the pane reads, wrapped in the meter its row is
	// drawn from, so a program that ends can be let go of with the pane
	// left open.
	on *metered

	// again says the window knows how to start the program once more. A
	// pane drawn from a window taken over does not: that program belongs
	// to the other window.
	again bool
}

// startsAgain records what a pane's program was started on, so the
// question on it can offer to start it again. m is the connection it
// rides on, and nil for a pane on this machine.
func (a *app) startsAgain(t *term.Terminal, argv []string, m *machine) {
	s := a.started[t]
	if s == nil {
		return
	}
	s.argv, s.again = argv, true
	if m != nil {
		s.at = m.at
	}
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
	status, known := exitStatus(t.Ending())
	question, start := whatHappened(a.panes[t], s.argv, status, known)
	t.Ask(question,
		term.Choice{Label: start, Do: func() error { return a.startAgain(t) }},
		term.Choice{Label: "Close", Do: func() error { return a.closePane(t) }})
}

// whatHappened words the question on a pane whose program has ended, and
// names the choice that starts it again.
//
// A command is named and its choice says it will run, because picking it
// runs that command a second time with whatever it does to the machine;
// picking it on a shell only opens a prompt.
//
// status is what the program exited with, and known says there is one: a
// transport that broke leaves none, and a question that called that an
// exit of zero would say the program ended cleanly, which is the one
// thing a dropped connection did not do.
func whatHappened(e *conns.Entry, argv []string, status int, known bool) (question, start string) {
	said := ""
	if known {
		said = ", exit " + strconv.Itoa(status)
	}
	if e != nil && e.Kind == conns.Command {
		what := labelFor(argv)
		if what == "" {
			what = "The command"
		}
		// Whatever it was, because which run this was is the whole of
		// what the user is deciding on.
		return what + " finished" + said + ". Run it again?", "Run again"
	}
	// A shell that ended cleanly is the ordinary way out, and saying so
	// is noise; one that died is worth knowing about.
	if status == 0 {
		said = ""
	}
	// The words ssh itself prints after a shell is exited, and the same
	// words fit a transport that went: either way the pane's connection
	// has closed and reconnecting is what brings it back.
	return "Connection closed" + said + ". Reconnect?", "Yes"
}

// exitStatus is the status a program ended with, taken from what its
// session reported, and whether one is known at all.
//
// over says the program has stopped. One still running has no status,
// and neither has one whose transport broke before it could send one.
func exitStatus(why error, over bool) (int, bool) {
	switch {
	case !over:
		return 0, false
	case why == nil:
		return 0, true
	}
	var far *ssh.ExitError
	if errors.As(why, &far) {
		return far.ExitStatus(), true
	}
	var here *exec.ExitError
	if errors.As(why, &here) {
		return here.ExitCode(), true
	}
	return 0, false
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
	sess, err := a.newShell(s.argv, a.lastSize[0], a.lastSize[1])
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
	// The row said what became of the program. It is running again, so
	// the row says what it is rather than how it ended.
	e.Label, e.Note = label, ""
	a.machines.startedAgain(t)
	delete(a.ended, t)
	a.markDirty()
	return nil
}
