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
// Write is called from the goroutine reading the session, one call per
// chunk read, and must not block for long: everything this terminal
// draws waits behind it.
type Watcher interface {
	Write(p []byte) (int, error)
}

// Watch shows this terminal to somebody else and returns what stops it.
//
// The watcher is first given the screen as it stands, so it sees what
// is already there rather than an empty pane waiting for the program to
// say something next. After that it is given what the program says, as
// it says it.
//
// A watcher whose Write fails is dropped: it is a connection that has
// gone, and there is nothing this terminal can do about it.
func (t *Terminal) Watch(w Watcher) func() {
	t.mu.Lock()
	// The screen under the same lock the reader parses into, so what
	// the watcher is given cannot be a screen half way through being
	// changed.
	cols, rows := t.g.Size()
	g := grid.New(cols, rows, t.g.DefaultFG, t.g.DefaultBG)
	t.term.Render(g)
	t.mu.Unlock()

	t.watchMu.Lock()
	t.watchers = append(t.watchers, w)
	t.watchMu.Unlock()

	// Outside the lock: the screen can be long, and the reader must not
	// wait on a connection while it holds what it parses into.
	if _, err := w.Write([]byte(vt.Repaint(g))); err != nil {
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
func (t *Terminal) tell(b []byte) {
	t.watchMu.Lock()
	watching := append([]Watcher(nil), t.watchers...)
	t.watchMu.Unlock()
	if len(watching) == 0 {
		return
	}
	for _, w := range watching {
		if _, err := w.Write(b); err != nil {
			// A connection that has gone. Nothing here can do anything
			// about it, and the window is told the client left by the
			// thing that carries the client.
			t.Unwatch(w)
		}
	}
}

// Send puts input into the terminal as though it had been typed here.
//
// It is how somebody watching from another machine types into a pane
// that is running on this one.
func (t *Terminal) Send(b []byte) { t.send(b) }
