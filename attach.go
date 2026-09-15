package main

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
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
func (a *app) attachTo(id, kind, label string) (session.Session, error) {
	type found struct {
		sess session.Session
		err  error
	}
	back := make(chan found, 1)
	a.pump.post(func() {
		sess, err := a.watchPane(id, kind, label)
		back <- found{sess: sess, err: err}
	})
	select {
	case got := <-back:
		return got.sess, got.err
	case <-a.ctx.Done():
		// The window is closing and nothing will run what was posted.
		// Waiting for it would park this goroutine for the rest of the
		// process, and the connection it belongs to with it.
		return nil, errors.New("this window is closing")
	}
}

// watchPane finds the pane a client asked for and starts watching it.
//
// On the goroutine that draws, which is the only one that may look at
// what the window has open.
func (a *app) watchPane(id, kind, label string) (session.Session, error) {
	e := a.entryByID(id)
	if e == nil {
		return nil, fmt.Errorf("there is nothing called %q open here any more", id)
	}
	// An id is a machine and a place in its list, and that list is
	// built afresh every frame: something closing shifts everything
	// after it up one. Checked against what the client was told it was
	// asking for, so a stale id is refused rather than quietly handing
	// over somebody else's shell to be typed into.
	if e.Kind.String() != kind || e.Label != label {
		return nil, fmt.Errorf(
			"what was %s %q here is now %s %q: ask again",
			kind, label, e.Kind, e.Label)
	}
	pane := a.paneFor(e)
	if pane == nil {
		return nil, errors.New("that is not something with a screen to watch")
	}
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

	// behind records that something was dropped, so the next read sends
	// the whole screen instead of carrying on mid-sentence.
	behind atomic.Bool

	// ended records that the program has gone, so a read that finds
	// nothing left says so rather than waiting for more.
	ended atomic.Bool

	closeOnce sync.Once
	done      chan struct{}

	// overOnce and over say the program has gone, for a Wait that would
	// otherwise sit there until somebody closed the session by hand.
	overOnce sync.Once
	over     chan struct{}

	// left is what a read has taken from the front of the queue and not
	// yet given back.
	left []byte
}

// watchQueue is how far a watcher may fall behind before what it has
// not read is dropped.
//
// Dropped rather than waited for. The pane is being drawn on this
// machine as well, and a watcher on a bad link must not be able to stop
// it.
//
// What is dropped cannot simply be skipped over. A chunk is whatever a
// read returned, cut wherever the read happened to end, so it can be
// the middle of an escape sequence -- and a parser left inside one eats
// whatever comes next. So a drop is followed by the whole screen, which
// is the only thing that puts an emulator back in a known state.
const watchQueue = 256

func newWatched(pane *term.Terminal) *watched {
	w := &watched{
		pane: pane,
		out:  make(chan []byte, watchQueue),
		done: make(chan struct{}),
		over: make(chan struct{}),
	}
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
		// Too far behind to take this one. Everything queued goes with
		// it and the whole screen is sent instead: half of what was
		// missed is worse than none of it, because what is missing can
		// be the start of an escape sequence.
		f.w.behind.Store(true)
		f.w.drain()
	}
	return len(p), nil
}

// Ended is called when the program has gone.
func (f feed) Ended() {
	f.w.ended.Store(true)
	f.w.overOnce.Do(func() { close(f.w.over) })
	// Woken, so a read waiting for output that is never coming returns
	// rather than sitting there for the life of the window.
	select {
	case f.w.out <- nil:
	default:
	}
}

// drain throws away what has been queued and not read.
func (w *watched) drain() {
	for {
		select {
		case <-w.out:
		default:
			return
		}
	}
}

// Read gives what the pane has said.
func (w *watched) Read(p []byte) (int, error) {
	for len(w.left) == 0 {
		// Caught up on by being sent the screen, rather than by being
		// sent the part of the stream that survived.
		if w.behind.Swap(false) {
			w.left = []byte(w.pane.Screen())
			break
		}
		select {
		case b := <-w.out:
			w.left = b
		case <-w.done:
			return 0, io.EOF
		}
		if len(w.left) == 0 && w.ended.Load() {
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
	if w.ended.Load() {
		// The program has gone. Saying the keystroke went in would be a
		// lie: there is nothing left to read it.
		return 0, io.EOF
	}
	w.pane.Send(p)
	return len(p), nil
}

// Resize does nothing. The pane is drawn on this machine too, and its
// size is this machine's business.
func (w *watched) Resize(int, int) error { return nil }

// Wait blocks until the program ends or the watcher stops watching.
//
// It is not the program's exit status, which belongs to whoever started
// it on this machine. Watching a shell says nothing about how it ended.
func (w *watched) Wait() error {
	select {
	case <-w.done:
	case <-w.over:
	}
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
