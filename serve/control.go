package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// watcher is one client listening for what this window has open.
//
// What it is sent is the latest snapshot, not every snapshot. A client
// on a slow link is behind by one picture of the window rather than by
// a queue of them, and the window it is watching never waits for it.
type watcher struct {
	// client is whose connection this control channel is on, so a
	// window can tell one client it is going without telling the rest.
	client *Client

	ch ssh.Channel

	// writeMu orders the writes to ch. The goroutine writing snapshots
	// and a window on its way out both write, and two writes at once
	// would interleave halves of two lines.
	writeMu sync.Mutex

	mu sync.Mutex
	// next is the snapshot not yet written, or nil when there is none.
	next []byte
	// stopped says no more will be written, so a send after the client
	// has gone is dropped rather than queued for nobody.
	stopped bool

	// wake carries the fact that next has been set. Buffered to one:
	// two snapshots waiting is the same as one, because only the later
	// one is written.
	wake chan struct{}
}

func newWatcher(c *Client, ch ssh.Channel) *watcher {
	return &watcher{client: c, ch: ch, wake: make(chan struct{}, 1)}
}

// Publish tells every client what this window has open.
//
// Called when that changes rather than on a timer: a client redrawing
// its panel from a snapshot that arrives sixty times a second is a
// client that never goes idle, which is the thing the whole display is
// built to avoid.
//
// It does not write to anybody. The window that has the snapshot is the
// one that draws, and a socket write from there would stop the screen
// for as long as the slowest client took to read. Each client has a
// goroutine of its own that does the writing.
func (s *Server) Publish(snap Snapshot) {
	line, err := json.Marshal(snap)
	if err != nil {
		s.onError(fmt.Errorf("serve: say what is open: %w", err))
		return
	}
	line = append(line, '\n')

	for _, w := range s.watchers() {
		w.put(line)
	}
}

// put leaves a snapshot for this watcher's own goroutine to write,
// replacing one it has not got to yet.
func (w *watcher) put(line []byte) {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.next = line
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// take is the snapshot waiting to be written, or nil.
func (w *watcher) take() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	line := w.next
	w.next = nil
	return line
}

// stop says nothing more will be written to this watcher and wakes its
// goroutine.
func (w *watcher) stop() {
	w.mu.Lock()
	w.stopped = true
	w.next = nil
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// send writes snapshots to one client until it goes.
//
// A client whose connection has gone is dropped. Nothing is reported
// for that: the connection going is already told through OnGone, and a
// window cannot be expected to care that the last snapshot missed
// somebody who had already left.
func (s *Server) send(w *watcher) {
	for range w.wake {
		line := w.take()
		if line == nil {
			w.mu.Lock()
			stopped := w.stopped
			w.mu.Unlock()
			if stopped {
				return
			}
			continue
		}
		if err := w.write(line); err != nil {
			s.dropWatcher(w)
			return
		}
	}
}

// write puts one line on the control channel, one writer at a time.
func (w *watcher) write(line []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	_, err := w.ch.Write(line)
	return err
}

// Going tells everyone watching that this window is about to close the
// connection, and why.
//
// Written here rather than left for each watcher's own goroutine: the
// close that follows would overtake a line still waiting its turn, and
// the client would be left with the socket error this is there to
// replace. A client that has already gone is not written to and is not
// reported.
func (s *Server) Going(why string) { s.going(nil, why) }

// GoingTo tells one client that this window is closing its connection,
// and why, for a window being thrown out rather than everybody.
func (s *Server) GoingTo(c *Client, why string) { s.going(c, why) }

// going writes the reason to every watcher, or to one client's when it
// is named.
//
// Each write goes on a goroutine of its own and the wait is bounded, so
// a client that has stopped reading cannot hold up a window that is
// closing. That is the same rule the snapshots follow, and the reason
// they are never written by the window's own goroutine.
func (s *Server) going(only *Client, why string) {
	line, err := json.Marshal(Snapshot{Going: why})
	if err != nil {
		s.onError(fmt.Errorf("serve: say why this window is going: %w", err))
		return
	}
	line = append(line, '\n')

	var wrote sync.WaitGroup
	for _, w := range s.watchers() {
		if only != nil && w.client != only {
			continue
		}
		wrote.Add(1)
		go func(w *watcher) {
			defer wrote.Done()
			_ = w.write(line)
		}(w)
	}
	done := make(chan struct{})
	go func() {
		wrote.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(sayGoingIn):
	}
}

// sayGoingIn is how long a window that is closing gives its last word to
// reach the clients that are still reading.
const sayGoingIn = 250 * time.Millisecond

// watchers returns who is listening right now.
func (s *Server) watchers() []*watcher {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*watcher(nil), s.watching...)
}

// addWatcher records a client listening, reporting whether the server
// is still open.
func (s *Server) addWatcher(w *watcher) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.watching = append(s.watching, w)
	return true
}

// dropWatcher forgets one that has gone.
func (s *Server) dropWatcher(w *watcher) {
	s.mu.Lock()
	for i, have := range s.watching {
		if have != w {
			continue
		}
		copy(s.watching[i:], s.watching[i+1:])
		s.watching[len(s.watching)-1] = nil
		s.watching = s.watching[:len(s.watching)-1]
		break
	}
	s.mu.Unlock()
	w.stop()
	// Closed outside the lock: the channel's own close talks to the
	// connection, and holding the server's lock across that would put
	// every other client behind one that has gone.
	s.letGoOf(w.ch)
}

// letGoOf closes a control channel and says so if it would not go. A
// connection that has already gone is not a failure to report.
func (s *Server) letGoOf(ch ssh.Channel) {
	if err := ch.Close(); err != nil && !Ended(err) {
		s.onError(fmt.Errorf("serve: close a control channel: %w", err))
	}
}

// runControl carries one client's view of what this window has open.
//
// ctx ends when the client's connection has finished.
func (s *Server) runControl(ctx context.Context, c *Client, ch ssh.Channel, reqs <-chan *ssh.Request) {
	// Drained rather than answered: nothing is asked down this channel,
	// and sixteen unread requests stop the connection's read loop.
	go ssh.DiscardRequests(reqs)

	w := newWatcher(c, ch)
	if !s.addWatcher(w) {
		s.letGoOf(ch)
		return
	}
	// The writing happens here, so nothing the window does waits on a
	// client's socket.
	writing := make(chan struct{})
	go func() {
		defer close(writing)
		s.send(w)
	}()
	defer func() {
		s.dropWatcher(w)
		s.letGoOf(ch)
		<-writing
	}()

	// What this window has open right now, before anything changes, so
	// a client that connects to a quiet window is not left with an
	// empty panel until something happens on it.
	if s.cfg.Opens != nil {
		s.Publish(s.cfg.Opens())
	}

	// Nothing is read from a client, so the only thing to wait for is
	// one of the two ends going.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, ch)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
