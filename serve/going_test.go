package serve

import (
	"io"
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

func (c stuckChannel) CloseWrite() error { return nil }

// Read never comes back either, which is the same client seen from the
// other side: the window is waiting to hear it read the line.
func (c stuckChannel) Read([]byte) (int, error) {
	<-c.until
	return 0, io.EOF
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

func (c tellingChannel) CloseWrite() error { return nil }

// Let go of straight away, which is a client that read the line.
func (c tellingChannel) Read([]byte) (int, error) { return 0, io.EOF }

// readingChannel records what a window did to say goodbye: the line, the
// end-of-file after it, and the read that waits for the client to let
// go.
type readingChannel struct {
	ssh.Channel
	said       chan []byte
	closedWrit chan struct{}
	read       chan struct{}
	letGo      chan struct{}
}

func newReadingChannel() *readingChannel {
	return &readingChannel{
		said:       make(chan []byte, 1),
		closedWrit: make(chan struct{}),
		read:       make(chan struct{}),
		letGo:      make(chan struct{}),
	}
}

func (c *readingChannel) Write(p []byte) (int, error) {
	c.said <- append([]byte(nil), p...)
	return len(p), nil
}

func (c *readingChannel) CloseWrite() error {
	close(c.closedWrit)
	return nil
}

func (c *readingChannel) Read([]byte) (int, error) {
	close(c.read)
	<-c.letGo
	return 0, io.EOF
}

// The window waits for the client to read its last word before the
// connection goes.
//
// A write comes back once the channel has taken the bytes, which says
// nothing about whether they have been read, and the connection is torn
// down as soon as going returns. A line still in flight then is one the
// client never sees: its read fails with the socket error this whole
// mechanism exists to replace, and a window that stopped sharing is
// reported as a connection that dropped.
func TestAWindowWaitsForItsLastWordToBeRead(t *testing.T) {
	ch := newReadingChannel()
	s := &Server{}
	s.watching = []*watcher{newWatcher(nil, ch)}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.going(nil, GoingStopped)
	}()

	// The line, then the end-of-file after it, in that order and on the
	// same channel, so the client reads one before the other.
	select {
	case <-ch.said:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was written")
	}
	select {
	case <-ch.closedWrit:
	case <-time.After(5 * time.Second):
		t.Fatal("the write half was never closed, so the client is left waiting for more")
	}

	// And it is still waiting on the client at this point, rather than
	// having returned to let the connection be closed under it.
	select {
	case <-ch.read:
	case <-time.After(5 * time.Second):
		t.Fatal("the window never read back, so it cannot know the line landed")
	}
	select {
	case <-done:
		t.Fatal("the window stopped waiting before the client let go of its end")
	case <-time.After(20 * time.Millisecond):
	}

	close(ch.letGo)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the window went on waiting after the client let go")
	}
}
