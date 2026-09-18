package main

import (
	"errors"

	"github.com/marrasen/gridterm/session"
)

// servedLabel says a pane here was opened by a window that has taken
// this one over, on the row for it on this window's own sidebar.
const servedLabel = "opened from another window"

// newSession opens a pane on this machine for a window that has taken
// this one over, and hands back a watch on it.
//
// A pane of this window's own, rather than a shell with nowhere to
// live. The shell runs on this machine, so it belongs to this window:
// somebody sitting here sees it and can work in it, and it is still
// here when the window that asked for it goes.
//
// Called on a goroutine of the server's, so the work is handed to the
// goroutine that draws.
func (a *app) newSession(cols, rows int) (session.Session, error) {
	type made struct {
		sess session.Session
		err  error
	}
	back := make(chan made, 1)
	a.pump.post(func() {
		sess, err := a.paneForClient(cols, rows)
		back <- made{sess: sess, err: err}
	})
	select {
	case got := <-back:
		return got.sess, got.err
	case <-a.ctx.Done():
		// The window is closing and nothing will run what was posted.
		return nil, errors.New("this window is closing")
	}
}

// paneForClient opens a pane here and starts watching it, for a client
// that asked for something to work in. On the goroutine that draws.
func (a *app) paneForClient(cols, rows int) (session.Session, error) {
	pane, err := a.openAPaneWith(a.localTerminal)
	if err != nil {
		return nil, err
	}
	// Said on the row, so somebody sitting here knows where a shell on
	// their own machine came from.
	if e := a.panes[pane]; e != nil {
		e.Note = servedLabel
	}
	a.markDirty()
	return a.watchOpenPane(pane, cols, rows)
}
