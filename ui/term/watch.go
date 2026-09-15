package term

import (
	"sync"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/vt"
)

// Watcher is somebody else looking at this terminal.
//
// It is how a window taken over from another machine shows a pane that
// is already running there. The pane keeps running here: the bytes are
// copied to the watcher rather than handed over, so the screen on this
// machine stays right and is still right when the watcher goes.
//
// Write is called with the screen as it stands and then with everything
// the program says, in order. It is called with a lock held that the
// terminal needs back before it can draw anything, so it must not block
// -- a queue with somewhere to put the bytes, not a socket to write
// them down.
//
// Ended is called once, when the program has gone. A watcher that was
// never told would wait for output from a shell that has exited.
type Watcher interface {
	Write(p []byte) (int, error)
	Ended()
}

// Watch shows this terminal to somebody else and returns what stops it.
//
// The watcher is given the screen as it stands and then what the
// program says next, with nothing lost in between and nothing shown
// twice. That ordering is the whole of this function: taken separately,
// a chunk can arrive before the screen it is already part of and be
// painted over by it, leaving a screen that is wrong until the program
// happens to redraw -- which a shell sitting at a prompt never does.
func (t *Terminal) Watch(w Watcher) func() {
	// The emulator's lock stops the reader parsing anything new, and
	// the watchers' lock stops it handing anything on. Both are held
	// across the snapshot and the append, so what the watcher is given
	// is the screen at one moment and its stream starts from there.
	t.mu.Lock()
	cols, rows := t.g.Size()
	g := grid.New(cols, rows, t.g.DefaultFG, t.g.DefaultBG)
	// The live screen, not the view: somebody at this machine may have
	// scrolled back into history, and what the far end wants is the
	// screen that the next output will land on.
	t.term.RenderLive(g)
	screen := vt.Repaint(g, t.term.Screenful())

	t.watchMu.Lock()
	if t.ended {
		t.watchMu.Unlock()
		t.mu.Unlock()
		w.Ended()
		return func() {}
	}
	t.watchers = append(t.watchers, w)
	_, err := w.Write([]byte(screen))
	t.watchMu.Unlock()
	t.mu.Unlock()

	if err != nil {
		t.Unwatch(w)
		return func() {}
	}
	var once sync.Once
	return func() { once.Do(func() { t.Unwatch(w) }) }
}

// Unwatch stops showing this terminal to somebody.
func (t *Terminal) Unwatch(w Watcher) {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	t.forget(w)
}

// forget takes a watcher off the list. The lock is already held.
func (t *Terminal) forget(w Watcher) {
	for i, have := range t.watchers {
		if have != w {
			continue
		}
		copy(t.watchers[i:], t.watchers[i+1:])
		t.watchers[len(t.watchers)-1] = nil
		t.watchers = t.watchers[:len(t.watchers)-1]
		return
	}
}

// Watched reports how many are looking at this terminal from elsewhere.
func (t *Terminal) Watched() int {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	return len(t.watchers)
}

// tell passes what the program said to whoever is watching.
//
// Under the same lock Watch appends with, so a watcher is never handed
// a chunk that the screen it was given already contained, and never
// misses one that it did not.
func (t *Terminal) tell(b []byte) {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	for _, w := range append([]Watcher(nil), t.watchers...) {
		if _, err := w.Write(b); err != nil {
			// A connection that has gone. Nothing here can do anything
			// about it, and the window is told the client left by the
			// thing that carries the client.
			t.forget(w)
		}
	}
}

// endWatchers tells everyone watching that the program has gone.
//
// Without it a watcher waits for output from a shell that has exited:
// its pane sits on a dead screen with nothing to say why, and the
// session carrying it is never closed.
func (t *Terminal) endWatchers() {
	t.watchMu.Lock()
	t.ended = true
	watching := t.watchers
	t.watchers = nil
	t.watchMu.Unlock()
	for _, w := range watching {
		w.Ended()
	}
}

// Screen is this terminal's live screen, as the escape sequences that
// would draw it.
//
// It is how a watcher that fell behind catches up: what it missed
// cannot be pieced back together, and half an escape sequence leaves an
// emulator in a state nothing will correct.
func (t *Terminal) Screen() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	cols, rows := t.g.Size()
	g := grid.New(cols, rows, t.g.DefaultFG, t.g.DefaultBG)
	t.term.RenderLive(g)
	return vt.Repaint(g, t.term.Screenful())
}

// Send puts input into the terminal as though it had been typed here.
//
// It is how somebody watching from another machine types into a pane
// that is running on this one.
func (t *Terminal) Send(b []byte) { t.send(b) }
