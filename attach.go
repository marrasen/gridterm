package main

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui/term"
)

// attachTo gives a client what is already running in one of this
// window's panes.
//
// Called from a goroutine serving that client, so the work of finding
// the pane is handed to the one that draws and waited for here. Nothing
// in the widget tree may be touched from anywhere else.
func (a *app) attachTo(id string, cols, rows int) (session.Session, error) {
	type found struct {
		sess session.Session
		err  error
	}
	back := make(chan found, 1)
	a.pump.post(func() {
		sess, err := a.watchPane(id, cols, rows)
		back <- found{sess: sess, err: err}
	})
	got := <-back
	return got.sess, got.err
}

// watchPane finds the pane a client asked for and starts watching it.
//
// On the goroutine that draws, which is the only one that may look at
// what the window has open.
func (a *app) watchPane(id string, cols, rows int) (session.Session, error) {
	e := a.entryByID(id)
	if e == nil {
		return nil, fmt.Errorf("there is nothing called %q open here any more", id)
	}
	pane := a.paneFor(e)
	if pane == nil {
		return nil, errors.New("that is not something with a screen to watch")
	}
	// Not resized to suit the watcher. The pane is drawn on this
	// machine too, and a window that shrank somebody's shell to fit a
	// laptop they are not looking at would be a window that reaches
	// further than it was asked to.
	_ = cols
	_ = rows
	return newWatched(pane), nil
}

// entryByID finds what a snapshot called something.
//
// Worked out the same way the snapshot was, so the two agree: a machine
// and a place in its list. Nothing here has an identity of its own to
// send, and one made up would have to be kept in step with a list that
// is built afresh every frame.
func (a *app) entryByID(id string) *conns.Entry {
	for _, g := range a.registry.Groups(time.Now()) {
		for i, row := range g.Rows {
			if openID(g.Host, i) == id {
				return row.Entry
			}
		}
	}
	return nil
}

// paneFor is the terminal showing an entry, or nil when the entry is
// not something with a screen.
func (a *app) paneFor(e *conns.Entry) *term.Terminal {
	for pane, have := range a.panes {
		if have == e {
			return pane
		}
	}
	return nil
}

// watched is a pane on this machine, as a session somebody else can
// read and type into.
//
// The pane keeps running here and keeps being drawn here. What crosses
// is a copy of what it shows, so the screen on this machine stays right
// and is still right when the watcher goes. Two people are then looking
// at one terminal, which is what taking over a window means.
type watched struct {
	pane *term.Terminal
	stop func()

	// out carries what the pane says to whoever is reading this. It is
	// buffered: the pane's own reader writes into it, and a watcher on
	// a slow link must not hold up the screen on this machine.
	out chan []byte

	closeOnce sync.Once
	done      chan struct{}

	// left is what a read has taken from the front of the queue and not
	// yet given back.
	left []byte
}

// watchQueue is how far a watcher may fall behind before what it has
// not read is dropped.
//
// Dropped rather than waited for. The pane is being drawn on this
// machine as well, and a watcher on a bad link must not be able to stop
// it: what they lose is a moment of a screen that is about to be
// written over anyway.
const watchQueue = 256

func newWatched(pane *term.Terminal) *watched {
	w := &watched{pane: pane, out: make(chan []byte, watchQueue), done: make(chan struct{})}
	w.stop = pane.Watch(feed{w: w})
	return w
}

// feed is the watcher half of a watched pane.
//
// Its own type because Write means the opposite thing on each side: to
// a terminal's watcher it is what the program said, and to a session it
// is what somebody typed. One method cannot honestly be both.
type feed struct{ w *watched }

// Write is called by the pane's reader with what the program said.
func (f feed) Write(p []byte) (int, error) {
	select {
	case <-f.w.done:
		return 0, io.EOF
	default:
	}
	b := append([]byte(nil), p...)
	select {
	case f.w.out <- b:
	default:
		// Too far behind. What is dropped is a moment of a screen that
		// is about to be written over, and the alternative is holding
		// up the pane on this machine for somebody who is not here.
	}
	return len(p), nil
}

// Read gives what the pane has said.
func (w *watched) Read(p []byte) (int, error) {
	if len(w.left) == 0 {
		select {
		case b := <-w.out:
			w.left = b
		case <-w.done:
			return 0, io.EOF
		}
	}
	n := copy(p, w.left)
	w.left = w.left[n:]
	return n, nil
}

// Write puts what the watcher typed into the pane, as though it had
// been typed on this machine.
func (w *watched) Write(p []byte) (int, error) {
	select {
	case <-w.done:
		return 0, io.EOF
	default:
	}
	w.pane.Send(p)
	return len(p), nil
}

// Resize does nothing. The pane is drawn on this machine too, and its
// size is this machine's business.
func (w *watched) Resize(int, int) error { return nil }

// Wait blocks until the watcher stops watching.
func (w *watched) Wait() error {
	<-w.done
	return nil
}

// Close stops watching. The pane goes on running.
func (w *watched) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		if w.stop != nil {
			w.stop()
		}
	})
	return nil
}
