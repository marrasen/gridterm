package main

import (
	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/session"
)

// servedLabel names a shell another window started on this machine, on
// this window's own sidebar.
const servedLabel = "started from another window"

// newSession starts a shell for a window that has taken this one over,
// with a row on this window's sidebar and no pane of its own. Whoever is
// sitting here can then see a shell somebody else started on their
// machine, and end it.
func (a *app) newSession(cols, rows int) (session.Session, error) {
	sess, err := a.newShell(a.localShell(), "", cols, rows)
	if err != nil {
		return nil, err
	}
	return a.rowForServedShell(sess), nil
}

// rowForServedShell puts a row on the sidebar for a session and takes it
// off again when the session closes.
//
// Called on a goroutine of the server's, so the registry is reached
// through the pump: the panel is the drawing goroutine's.
func (a *app) rowForServedShell(sess session.Session) session.Session {
	counted := &metered{Session: sess, m: meter.New()}
	s := &servedShell{Session: counted, a: a}
	s.entry = &conns.Entry{
		Host:  conns.Local,
		Kind:  conns.Terminal,
		Label: servedLabel,
		Meter: counted.m,
		// No Reveal: the pane it is drawn in belongs to the window that
		// opened it, and there is nothing here to go to.
		Close: func() error { return s.Close() },
	}
	a.pump.post(func() {
		if a.servedRows == nil {
			a.servedRows = make(map[*conns.Entry]bool)
		}
		a.servedRows[s.entry] = true
		a.registry.Add(s.entry)
	})
	return s
}

// servedShell is a shell another window started here, with a row on this
// window's sidebar for as long as it runs.
type servedShell struct {
	session.Session

	a     *app
	entry *conns.Entry
}

// Close ends the shell and takes its row off the sidebar.
func (s *servedShell) Close() error {
	s.a.pump.post(func() {
		delete(s.a.servedRows, s.entry)
		s.a.registry.Drop(s.entry)
	})
	return s.Session.Close()
}
