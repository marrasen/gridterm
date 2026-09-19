package main

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui/term"
)

// attachTo gives a client what is already running in one of this
// window's panes.
//
// Called from a goroutine serving that client, so the work of finding
// the pane is handed to the one that draws and waited for here. Nothing
// in the widget tree may be touched from anywhere else.
func (a *app) attachTo(want serve.Attached, cols, rows int) (session.Session, error) {
	type found struct {
		sess session.Session
		err  error
	}
	back := make(chan found, 1)
	a.pump.post(func() {
		sess, err := a.watchPane(want, cols, rows)
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
func (a *app) watchPane(want serve.Attached, cols, rows int) (session.Session, error) {
	e, err := a.entryAsked(want)
	if err != nil {
		return nil, err
	}
	pane := a.paneFor(e)
	if pane == nil {
		return nil, errors.New("that is not something with a screen to watch")
	}
	return a.watchOpenPane(pane, cols, rows)
}

// watchOpenPane starts a watch on a pane of this window's, sized for the
// pane the other window draws it in.
//
// On the goroutine that draws, which is the only one that may touch the
// widget tree.
func (a *app) watchOpenPane(pane *term.Terminal, cols, rows int) (session.Session, error) {
	w, err := newWatched(pane)
	if err != nil {
		return nil, err
	}
	// The watcher sets the size. Asked for from whichever goroutine is
	// serving them, so it is handed to the one that draws: the widget
	// tree is that goroutine's.
	w.resize = func(cols, rows int) error {
		a.pump.post(func() { pane.Hold(cols, rows) })
		return nil
	}
	// The size they opened at, so their screen is the right shape from
	// the first frame rather than after the first resize. Straight
	// rather than through the pump: this already runs on the goroutine
	// that draws.
	pane.Hold(cols, rows)
	// And the size comes back to this window when nobody is watching.
	w.gone = func() {
		a.pump.post(func() {
			if pane.Watched() == 0 {
				pane.Release()
				a.markDirty()
			}
		})
	}
	return w, nil
}

// entryAsked finds what a snapshot called something, and checks the name
// still stands for the machine and the kind of thing the client was told
// it stood for.
//
// The label is not checked: a shell sets its own title, so it changes at
// every prompt and a legitimate client would be refused at random. The
// kind is checked, and the one kind that changes is a reader's, which no
// client is ever told about: a reader has no screen to hand over.
//
// This is integrity rather than a way in -- the key the client signed
// with is what decides that. An id names a place in a list the registry
// builds afresh, and if ids ever stop being a counter that is never
// reused, one handed out twice would quietly connect a client to
// something else.
func (a *app) entryAsked(want serve.Attached) (*conns.Entry, error) {
	if want.ID == "" {
		return nil, errors.New("that named nothing to work in")
	}
	for _, g := range a.registry.Groups(time.Now()) {
		for _, row := range g.Rows {
			if row.ID() != want.ID {
				continue
			}
			if g.Host != want.Host || row.Kind.String() != want.Kind {
				return nil, fmt.Errorf(
					"%q is no longer the %s on %s that you were told about",
					want.ID, want.Kind, want.Host)
			}
			return row.Entry, nil
		}
	}
	return nil, fmt.Errorf("there is nothing called %q open here any more", want.ID)
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

	// resize is how the watcher asks for the size it wants. A nil one
	// refuses: the pane is drawn on this machine as well, and its size
	// is this window's unless this window gives it up.
	resize func(cols, rows int) error

	// gone is called when this watcher stops, so the window can take
	// its screen back.
	gone func()

	// out carries what the pane says to whoever is reading this. It is
	// buffered: the pane's own reader writes into it, and a watcher on
	// a slow link must not hold up the screen on this machine.
	out chan []byte

	// behind records that something was dropped, so the next read asks
	// the pane for the whole screen instead of carrying on mid-sentence.
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
// whatever comes next. So a drop is made good by asking the pane for
// the whole screen, which is the only thing that puts an emulator back
// in a known state.
const watchQueue = 256

// newWatched starts watching a pane.
//
// It fails when the program in the pane has already gone: a session on
// a dead screen would open empty, end at once, and say nothing about
// why.
func newWatched(pane *term.Terminal) (*watched, error) {
	w := &watched{
		pane: pane,
		out:  make(chan []byte, watchQueue),
		done: make(chan struct{}),
		over: make(chan struct{}),
	}
	stop, err := pane.Watch(feed{w: w})
	if err != nil {
		return nil, err
	}
	w.stop = stop
	return w, nil
}

// feed is the watcher half of a watched pane.
//
// Its own type because Write means the opposite thing on each side: to
// a terminal's watcher it is what the program said, and to a session it
// is what somebody typed. One method cannot honestly be both.
type feed struct{ w *watched }

// Screen is called by the pane with the whole screen, which replaces
// anything queued and not yet read.
//
// Called with the pane's locks held, so nothing the program says can
// land between the throwing away and the screen going on the queue.
func (f feed) Screen(p []byte) error {
	select {
	case <-f.w.done:
		return io.EOF
	default:
	}
	f.w.drain()
	f.w.out <- append([]byte(nil), p...)
	return nil
}

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
		// it and the whole screen is asked for instead: half of what
		// was missed is worse than none of it, because what is missing
		// can be the start of an escape sequence.
		f.w.behind.Store(true)
		f.w.drain()
		// Woken, because a reader parked between noticing it was up to
		// date and waiting would otherwise sleep until the program
		// next said something -- which a shell at a prompt never does.
		// The queue was just emptied, so there is room.
		select {
		case f.w.out <- nil:
		default:
		}
	}
	return len(p), nil
}

// Ended is called when the program has gone.
func (f feed) Ended() {
	f.w.ended.Store(true)
	// Closed rather than put on the queue: a full queue would drop the
	// one thing that tells a parked reader there is nothing more
	// coming, and it would sleep for the life of the window.
	f.w.overOnce.Do(func() { close(f.w.over) })
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
		// sent the part of the stream that survived. The pane puts it
		// on the queue under its own locks, so nothing said in between
		// arrives after the screen that already contains it.
		if w.behind.Swap(false) {
			// A pane whose program went while this was catching up has
			// no screen to give. What it said before it went is still
			// queued, so the end is left to the read below.
			_ = w.pane.Resync(feed{w: w})
		}
		select {
		case b := <-w.out:
			w.left = b
		case <-w.done:
			return 0, io.EOF
		case <-w.over:
			// The program has gone. What it said before it went is
			// still queued, so that is read out before the end.
			select {
			case b := <-w.out:
				w.left = b
			default:
				return 0, io.EOF
			}
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
		return 0, term.ErrEnded
	default:
	}
	if w.ended.Load() {
		// The program has gone. Saying the keystroke went in would be a
		// lie: there is nothing left to read it.
		return 0, term.ErrEnded
	}
	w.pane.Send(p)
	return len(p), nil
}

// Resize refuses. The pane is drawn on this machine too, and shrinking
// somebody's shell to fit a pane they are not looking at would reach
// further than watching was ever asked to.
func (w *watched) Resize(cols, rows int) error {
	if w.resize == nil {
		return errors.New("the size of a pane that is drawn here too is not yours to set")
	}
	return w.resize(cols, rows)
}

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
		if w.gone != nil {
			w.gone()
		}
	})
	return nil
}
