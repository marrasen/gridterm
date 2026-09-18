package serve

import (
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// stuckChannel is a control channel whose writes never come back, which
// is a client that stopped reading and left its window's buffers full.
type stuckChannel struct {
	ssh.Channel
	until chan struct{}
}

func (c stuckChannel) Write(p []byte) (int, error) {
	<-c.until
	return len(p), nil
}

// A client that stopped reading does not hold up a window that is
// closing.
//
// The snapshots have their own goroutine per client for exactly this
// reason. The last word said before a connection closes has to be
// written where the closing happens, so it carries its own bound
// instead.
func TestAClientThatStoppedReadingDoesNotHoldUpAWindowClosing(t *testing.T) {
	until := make(chan struct{})
	defer close(until)
	s := &Server{}
	s.watching = []*watcher{newWatcher(nil, stuckChannel{until: until})}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.going(nil, GoingStopped)
	}()

	select {
	case <-done:
	case <-time.After(sayGoingIn + 5*time.Second):
		t.Fatal("saying why the window is going waited on a client that had stopped reading")
	}
}

// And a client that is reading is written to.
func TestAClientReadingIsToldWhyTheWindowIsGoing(t *testing.T) {
	said := make(chan []byte, 1)
	s := &Server{}
	s.watching = []*watcher{newWatcher(nil, tellingChannel{said: said})}

	s.going(nil, GoingStopped)

	select {
	case line := <-said:
		want := `{"window":"","open":null,"going":"stopped"}` + "\n"
		if string(line) != want {
			t.Errorf("it wrote %q, want %q", line, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was written")
	}
}

// tellingChannel keeps what was written to it.
type tellingChannel struct {
	ssh.Channel
	said chan []byte
}

func (c tellingChannel) Write(p []byte) (int, error) {
	c.said <- append([]byte(nil), p...)
	return len(p), nil
}
