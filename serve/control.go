package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// watcher is one client listening for what this window has open.
type watcher struct {
	ch ssh.Channel

	// mu keeps two snapshots from being written into each other. They
	// come from the goroutine that draws, one at a time, but a watcher
	// that has gone is dropped from another.
	mu sync.Mutex
}

// Publish tells every client what this window has open.
//
// Called when that changes rather than on a timer: a client redrawing
// its panel from a snapshot that arrives sixty times a second is a
// client that never goes idle, which is the thing the whole display is
// built to avoid.
//
// A client whose connection has gone is dropped. Nothing is reported
// for that: the connection going is already told through OnGone, and a
// window cannot be expected to care that the last snapshot missed
// somebody who had already left.
func (s *Server) Publish(snap Snapshot) {
	line, err := json.Marshal(snap)
	if err != nil {
		s.onError(fmt.Errorf("serve: say what is open: %w", err))
		return
	}
	line = append(line, '\n')

	for _, w := range s.watchers() {
		if err := w.write(line); err != nil {
			s.dropWatcher(w)
		}
	}
}

// write sends one snapshot.
func (w *watcher) write(line []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.ch.Write(line)
	return err
}

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
	// Closed outside the lock: the channel's own close talks to the
	// connection, and holding the server's lock across that would put
	// every other client behind one that has gone.
	_ = w.ch.Close()
}

// runControl carries one client's view of what this window has open.
func (s *Server) runControl(ch ssh.Channel, reqs <-chan *ssh.Request, gone <-chan struct{}) {
	// Drained rather than answered: nothing is asked down this channel,
	// and sixteen unread requests stop the connection's read loop.
	go ssh.DiscardRequests(reqs)

	w := &watcher{ch: ch}
	if !s.addWatcher(w) {
		_ = ch.Close()
		return
	}
	defer func() {
		s.dropWatcher(w)
		_ = ch.Close()
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
	case <-gone:
	}
}
