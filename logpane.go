package main

import (
	"errors"
	"log"
	"os"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/logs"
	"github.com/marrasen/gridterm/ui/term"
)

// windowLog is what this process has logged.
//
// One per process, because log is a package-level thing and pointing it
// anywhere else would take the lines away from wherever they go now.
// The lines still reach stderr; this keeps a copy so a pane can show
// them, which is the only way to read them in a window started from
// Explorer.
var windowLog = logs.New(0, os.Stderr)

// keepLog sends what the window logs to windowLog as well as to stderr.
func keepLog() { log.SetOutput(windowLog) }

// logCommand shows the pane with the log in it, and logTitle is what
// the sidebar and the menu call it.
const (
	logCommand = "view.log"
	logTitle   = "Show what the window has logged"
	logLabel   = "Log"
)

// showLog opens a pane on the window's own log, or goes to the one
// already open.
//
// It is a terminal like any other, so the scrollback, the selection and
// the copy key are the ones the user already knows. Nothing can be
// typed into it: there is no program at the far end.
func (a *app) showLog() error {
	if t := a.logPane(); t != nil {
		a.focus(t)
		return nil
	}
	t, err := a.newTerminalOn(windowLog.Open(), conns.Local, conns.Log, logLabel)
	if err != nil {
		return err
	}
	if err := a.placePane(t); err != nil {
		// The terminal is reading the log on a goroutine of its own
		// already. Left here it would read it for ever, into a pane
		// nowhere on the screen.
		delete(a.panes, t)
		delete(a.started, t)
		return errors.Join(err, t.Close())
	}
	a.showPane(t)
	return nil
}

// logPane is the pane showing the log, or nil when none is open. There
// is at most one: a second would show the same lines twice.
func (a *app) logPane() *term.Terminal {
	for t, e := range a.panes {
		if e != nil && e.Kind == conns.Log {
			return t
		}
	}
	return nil
}
