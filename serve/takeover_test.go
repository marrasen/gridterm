package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/session"
)

// echoSession is a program for a test to work in: it says what size it
// was started at, repeats what it is sent, and ends on "bye".
type echoSession struct {
	mu       sync.Mutex
	size     [2]int
	sizes    [][2]int
	closed   bool
	closeErr error

	out  *io.PipeReader
	outW *io.PipeWriter
	done chan struct{}
}

func newEchoSession(cols, rows int) *echoSession {
	r, w := io.Pipe()
	s := &echoSession{size: [2]int{cols, rows}, out: r, outW: w, done: make(chan struct{})}
	s.sizes = append(s.sizes, s.size)
	go func() { _, _ = fmt.Fprintf(w, "started at %dx%d\r\n", cols, rows) }()
	return s
}

func (s *echoSession) Read(p []byte) (int, error) { return s.out.Read(p) }

func (s *echoSession) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "bye") {
		s.finish()
		return len(p), nil
	}
	go func(b []byte) { _, _ = s.outW.Write(b) }(append([]byte(nil), p...))
	return len(p), nil
}

func (s *echoSession) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.size = [2]int{cols, rows}
	s.sizes = append(s.sizes, s.size)
	return nil
}

func (s *echoSession) Wait() error {
	<-s.done
	return nil
}

func (s *echoSession) Close() error {
	s.finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.closeErr
}

func (s *echoSession) finish() {
	select {
	case <-s.done:
	default:
		close(s.done)
		_ = s.outW.Close()
	}
}

func (s *echoSession) seenSizes() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int(nil), s.sizes...)
}

// takenOver starts a window serving and another that has reached it.
func takenOver(t *testing.T, open Opener) (*Server, *Window) {
	t.Helper()
	mine, line := aKey(t, "marcus@laptop")
	host, err := HostKey(t.TempDir() + "/host_key")
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys,
		Open:    open,
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Key: mine,
		HostKey: ssh.FixedHostKey(host.PublicKey()),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return s, w
}

// read waits for text to arrive from a session.
func read(t *testing.T, s session.Session, want string) string {
	t.Helper()
	var got strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, err := s.Read(buf)
		got.Write(buf[:n])
		if strings.Contains(got.String(), want) {
			return got.String()
		}
		if err != nil {
			t.Fatalf("read: %v, having got %q", err, got.String())
		}
	}
	t.Fatalf("waited for %q, got %q", want, got.String())
	return ""
}

// One window works in another's session, and cannot tell it from one of
// its own: bytes both ways, and a size.
func TestOneWindowWorksInAnothers(t *testing.T) {
	var started *echoSession
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		started = newEchoSession(cols, rows)
		return started, nil
	})

	sess, err := w.Open(100, 40)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	// The size arrives with the request, so the program has it as it
	// starts rather than after its first screen.
	read(t, sess, "started at 100x40")

	if _, err := sess.Write([]byte("hello there")); err != nil {
		t.Fatalf("write: %v", err)
	}
	read(t, sess, "hello there")

	if err := sess.Resize(120, 50); err != nil {
		t.Fatalf("resize: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := started.seenSizes(); len(got) > 1 && got[len(got)-1] == [2]int{120, 50} {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the program was never told the pane resized: %v", started.seenSizes())
}

// A program that ends is waited out, and how it ended crosses the wire:
// a client that only saw the channel close could not tell a program
// that finished from a connection that dropped.
func TestHowAProgramEndedCrossesTheWire(t *testing.T) {
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		return newEchoSession(cols, rows), nil
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	read(t, sess, "started at")

	if _, err := sess.Write([]byte("bye")); err != nil {
		t.Fatalf("write: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("it ended with %v, want a clean finish", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for the program never came back")
	}
}

// A window that cannot start anything says so down the channel, so the
// pane the client has already drawn has something in it.
func TestAWindowThatCannotOpenSaysSo(t *testing.T) {
	_, w := takenOver(t, func(int, int) (session.Session, error) {
		return nil, errors.New("there is no shell here")
	})

	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	if got := read(t, sess, "there is no shell here"); !strings.Contains(got, "gridterm") {
		t.Errorf("it said %q, without saying who could not do it", got)
	}
}

// A window serving nothing refuses rather than leaving the client with
// a pane that never fills.
func TestAWindowServingNothingRefuses(t *testing.T) {
	_, w := takenOver(t, nil)

	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	// The channel closes with nothing on it.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := sess.Read(make([]byte, 16)); err != nil {
			return
		}
	}
	t.Fatal("the session was left open on a window with nothing to open")
}

// An ordinary SSH client is told what this is rather than handed a
// shell it did not ask for.
func TestAnOrdinarySSHClientIsToldWhatThisIs(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	host, err := HostKey(t.TempDir() + "/host_key")
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys,
		Open:    func(c, r int) (session.Session, error) { return newEchoSession(c, r), nil },
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	client, err := connect(t, s, mine)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	_, err = client.NewSession()

	if err == nil {
		t.Fatal("an ordinary SSH client was given a session")
	}
	if !strings.Contains(err.Error(), chanSession) {
		t.Errorf("it was told %v, without being told what is served", err)
	}
}

// Letting go of the other window takes what was opened on it.
func TestLettingGoOfAWindowTakesItsSessions(t *testing.T) {
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		return newEchoSession(cols, rows), nil
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	read(t, sess, "started at")

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := io.ReadAll(sess); err != nil && !errors.Is(err, io.EOF) {
		// Either is fine: what matters is that it does not block.
		_ = err
	}
	if _, err := w.Open(80, 24); err == nil {
		t.Error("a window that was let go of opened another session")
	}
}

// Reaching a window takes a key, an address and something to check the
// machine by. A client that took whatever answered would hand a shell
// to whoever got there first.
func TestReachingAWindowSaysWhatIsMissing(t *testing.T) {
	signer, _ := aKey(t, "marcus@laptop")
	for _, c := range []struct {
		why string
		cfg DialConfig
	}{
		{"no address", DialConfig{Key: signer, HostKey: ssh.InsecureIgnoreHostKey()}},
		{"no key", DialConfig{Addr: "127.0.0.1:1", HostKey: ssh.InsecureIgnoreHostKey()}},
		{"nothing to check by", DialConfig{Addr: "127.0.0.1:1", Key: signer}},
	} {
		if _, err := Dial(context.Background(), c.cfg); err == nil {
			t.Errorf("%s: it connected anyway", c.why)
		}
	}
}

// A window answering with a key that is not the one expected is not
// taken over.
func TestAWindowWithTheWrongKeyIsRefused(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	other, _ := aKey(t, "somebody@else")
	host, err := HostKey(t.TempDir() + "/host_key")
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys, OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	_, err = Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Key: mine,
		HostKey: ssh.FixedHostKey(other.PublicKey()),
	})

	if err == nil {
		t.Fatal("a window answering with the wrong key was taken over")
	}
}
