package main

import (
	"errors"

	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/session"
)

// servedLabel says a pane here was opened by a window that has taken
// this one over, on the row for it on this window's own sidebar.
const servedLabel = "opened from another window"

// newSession opens a pane on this machine for a window that has taken
// this one over, and hands back a watch on it and what this window calls
// it.
//
// A pane of this window's own, rather than a shell with nowhere to
// live. The shell runs on this machine, so it belongs to this window:
// somebody sitting here sees it and can work in it, and it is still
// here when the window that asked for it goes.
//
// Called on a goroutine of the server's, so the work is handed to the
// goroutine that draws.
func (a *app) newSession(cols, rows int) (session.Session, serve.Attached, error) {
	type made struct {
		sess  session.Session
		named serve.Attached
		err   error
	}
	back := make(chan made, 1)
	a.pump.post(func() {
		sess, named, err := a.paneForClient(cols, rows)
		back <- made{sess: sess, named: named, err: err}
	})
	select {
	case got := <-back:
		return got.sess, got.named, got.err
	case <-a.ctx.Done():
		// The window is closing and nothing will run what was posted.
		return nil, serve.Attached{}, errors.New("this window is closing")
	}
}

// paneForClient opens a pane here and starts watching it, for a client
// that asked for something to work in. On the goroutine that draws.
func (a *app) paneForClient(cols, rows int) (session.Session, serve.Attached, error) {
	pane, err := a.openAPaneWith(a.localTerminal)
	if err != nil {
		return nil, serve.Attached{}, err
	}
	// Said on the row, so somebody sitting here knows where a shell on
	// their own machine came from.
	var named serve.Attached
	if e := a.panes[pane]; e != nil {
		e.Note = servedLabel
		// The same three parts a client would use to ask for it again,
		// which is what lets the client draw one row for it rather than
		// one of its own and one for this.
		named = serve.Attached{ID: e.ID(), Host: e.Host, Kind: e.Kind.String()}
	}
	a.markDirty()
	sess, err := a.watchOpenPane(pane, cols, rows)
	if err != nil {
		return nil, serve.Attached{}, err
	}
	return sess, named, nil
}
