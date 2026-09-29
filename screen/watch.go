package screen

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/kakel/session"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// Watch is a session on terminal t, open in a pane here: the screen as
// it stands and then everything the program writes, with what is typed
// going to the program. resize hears the size the watcher wants, and
// gone that the watcher has gone; both are called on the watcher's
// goroutine. A program that has gone already gives the screen it left,
// and then ends.
func Watch(t *uiterm.Terminal, resize func(cols, rows int), gone func()) (session.Session, error) {
	w := &watched{pane: t, out: make(chan []byte, 256), done: make(chan struct{}), over: make(chan struct{}), resize: resize, gone: gone}
	stop, err := t.Watch(feed{w})
	if errors.Is(err, uiterm.ErrEnded) {
		// Gone already, as a quick command is by the time it is asked
		// for: the screen it left, and then the end.
		w.out <- []byte(t.Replay())
		w.stop = func() {}
		feed{w}.Ended()
		return w, nil
	}
	if err != nil {
		return nil, err
	}
	w.stop = stop
	return w, nil
}

// watched is the session a watcher reads.
type watched struct {
	pane      *uiterm.Terminal
	stop      func()
	resize    func(cols, rows int)
	gone      func()
	out       chan []byte
	behind    atomic.Bool
	ended     atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
	overOnce  sync.Once
	over      chan struct{}
	left      []byte
}

// feed is how the pane hands a watcher what it writes.
type feed struct{ w *watched }

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

func (f feed) Write(p []byte) (int, error) {
	select {
	case <-f.w.done:
		return 0, io.EOF
	default:
	}
	select {
	case f.w.out <- append([]byte(nil), p...):
	default:
		// Too far behind to catch up byte by byte: the screen as it
		// stands is sent again instead.
		f.w.behind.Store(true)
		f.w.drain()
		select {
		case f.w.out <- nil:
		default:
		}
	}
	return len(p), nil
}

func (f feed) Ended() {
	f.w.ended.Store(true)
	f.w.overOnce.Do(func() { close(f.w.over) })
}

func (w *watched) drain() {
	for {
		select {
		case <-w.out:
		default:
			return
		}
	}
}

func (w *watched) Read(p []byte) (int, error) {
	for len(w.left) == 0 {
		if w.behind.Swap(false) {
			_ = w.pane.Resync(feed{w})
		}
		select {
		case b := <-w.out:
			w.left = b
		case <-w.done:
			return 0, io.EOF
		case <-w.over:
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

func (w *watched) Write(p []byte) (int, error) {
	select {
	case <-w.done:
		return 0, uiterm.ErrEnded
	default:
	}
	if w.ended.Load() {
		return 0, uiterm.ErrEnded
	}
	w.pane.Send(p)
	return len(p), nil
}

func (w *watched) Resize(cols, rows int) error {
	w.resize(cols, rows)
	return nil
}

// Wait waits for the watcher to go or the program to end, and gives
// how the program ended, which it learns a moment after it has gone:
// the watcher says it, as a program of its own would. A watcher that
// went first has no ending to give.
func (w *watched) Wait() error {
	select {
	case <-w.over:
	default:
		select {
		case <-w.done:
			return nil
		case <-w.over:
		}
	}
	for deadline := time.Now().Add(statusWait); ; {
		if why, over := w.pane.Ending(); over {
			return why
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// statusWait is how long an ended program's status has to arrive.
const statusWait = 2 * time.Second

func (w *watched) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		w.stop()
		w.gone()
	})
	return nil
}
