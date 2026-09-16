package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
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
	endWith  error

	out  *io.PipeReader
	outW *io.PipeWriter
	done chan struct{}

	finishOnce sync.Once
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
	return s.endWith
}

func (s *echoSession) Close() error {
	s.finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.closeErr
}

// finish ends the session once, however many goroutines end it.
//
// A session is closed from three places -- the copy of input, the copy
// of output, and the connection going -- so a check and a close that
// were not one step would close a channel twice.
func (s *echoSession) finish() {
	s.finishOnce.Do(func() {
		close(s.done)
		_ = s.outW.Close()
	})
}

func (s *echoSession) seenSizes() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int(nil), s.sizes...)
}

// takenOver starts a window serving and another that has reached it.
func takenOver(t *testing.T, open Opener) (*Server, *Window) {
	t.Helper()
	return takenOverWith(t, open, nil)
}

// takenOverWith is takenOver for a window that also lets a client work
// in what it already has running.
func takenOverWith(t *testing.T, open Opener, attach Attacher) (*Server, *Window) {
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
		Attach:  attach,
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Keys: []ssh.Signer{mine},
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
	type said struct {
		b   []byte
		err error
	}
	// Each read on a goroutine of its own, because a session that never
	// answers would otherwise hang the test until the alarm rather than
	// failing it.
	deadline := time.After(5 * time.Second)
	for {
		back := make(chan said, 1)
		go func() {
			buf := make([]byte, 256)
			n, err := s.Read(buf)
			back <- said{b: buf[:n], err: err}
		}()
		select {
		case one := <-back:
			got.Write(one.b)
			if strings.Contains(got.String(), want) {
				return got.String()
			}
			if one.err != nil {
				t.Fatalf("read: %v, having got %q", one.err, got.String())
			}
		case <-deadline:
			t.Fatalf("waited for %q, got %q", want, got.String())
			return ""
		}
	}
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
	if !strings.Contains(err.Error(), SessionChannel) {
		t.Errorf("it was told %v, without being told what is served", err)
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
		{"no address", DialConfig{Keys: []ssh.Signer{signer}, HostKey: ssh.InsecureIgnoreHostKey()}},
		{"no key", DialConfig{Addr: "127.0.0.1:1", HostKey: ssh.InsecureIgnoreHostKey()}},
		{"nothing to check by", DialConfig{Addr: "127.0.0.1:1", Keys: []ssh.Signer{signer}}},
	} {
		if _, err := Dial(context.Background(), c.cfg); err == nil {
			t.Errorf("%s: it connected anyway", c.why)
		}
	}
}

// A caller that sets both ways of signing in gets the ladder, because a
// ladder is what a caller with more than one key to try has and a flat
// list would try only the first.
func TestAWindowReachedWithBothWaysOfSigningInUsesTheLadder(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	wrong, _ := aKey(t, "somebody@else")
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
		Open:    func(int, int) (session.Session, error) { return nil, errors.New("nothing to open") },
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Keys holds the key the window will not have; Auth holds the one it
	// will.
	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Keys: []ssh.Signer{wrong},
		Auth: func(*ssh.ClientAuthContext) (ssh.AuthMethod, error) {
			return ssh.PublicKeys(mine), nil
		},
		HostKey: ssh.FixedHostKey(host.PublicKey()),
	})
	if err != nil {
		t.Fatalf("reach it with both set: %v", err)
	}
	_ = w.Close()
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
		Addr: s.Addr(), Keys: []ssh.Signer{mine},
		HostKey: ssh.FixedHostKey(other.PublicKey()),
	})

	if err == nil {
		t.Fatal("a window answering with the wrong key was taken over")
	}
}

// A program that ends badly says so, and the status crosses.
//
// The test that used to stand for this asserted only that a clean
// finish returns nothing, which is also what you get when nothing
// crosses the wire at all: the feature could be deleted and it passed.
func TestABadEndingCrossesTheWire(t *testing.T) {
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		s := newEchoSession(cols, rows)
		s.endWith = errors.New("it fell over")
		return s, nil
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	read(t, sess, "started at")

	if _, err := sess.Write([]byte("bye")); err != nil {
		t.Fatalf("write: %v", err)
	}

	err = waited(t, sess)
	if err == nil {
		t.Fatal("a program that fell over is reported as a clean finish")
	}
	if !strings.Contains(err.Error(), "status 1") {
		t.Errorf("it said %v, without saying how it ended", err)
	}
}

// A connection that drops is not a program that finished.
//
// Both used to come back as nothing at all, which made the whole
// exchange pointless: it exists so those two can be told apart.
func TestADroppedConnectionIsNotACleanFinish(t *testing.T) {
	s, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		return newEchoSession(cols, rows), nil
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	read(t, sess, "started at")

	// The serving window goes, with the program still running.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	err = waited(t, sess)
	if err == nil {
		t.Fatal("a connection that dropped is reported as a clean finish")
	}
	if !strings.Contains(err.Error(), "connection went") {
		t.Errorf("it said %v, without saying the connection went", err)
	}
}

// A pane can be closed while its input is stuck.
//
// Write blocks when the far end stops reading, and a lock shared with
// Close made Close wait for a Write that only a Close could unblock.
// On the goroutine that draws, that is the whole window frozen.
func TestASessionCanBeClosedWhileItsInputIsStuck(t *testing.T) {
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		// A program that never reads what it is sent.
		return newDeafSession(), nil
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// More than the channel window, so the write parks.
	stuck := make(chan struct{})
	go func() {
		defer close(stuck)
		_, _ = sess.Write(make([]byte, 4<<20))
	}()
	select {
	case <-stuck:
		t.Skip("the write went through, so there is nothing stuck to close")
	case <-time.After(250 * time.Millisecond):
	}

	closed := make(chan error, 1)
	go func() { closed <- sess.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing a session waited for a write that nothing would finish")
	}

	// And it really closed: the write it was stuck behind comes back,
	// and there is nothing left to read.
	select {
	case <-stuck:
	case <-time.After(5 * time.Second):
		t.Error("the write was left parked, so the session was not closed")
	}
	if _, err := io.ReadAll(sess); err != nil && !errors.Is(err, io.EOF) {
		t.Errorf("reading a closed session gave %v", err)
	}
}

// A window with nothing to open says so on the error stream and reports
// it as a failure, not as a program that ran and finished.
func TestAWindowServingNothingSaysSoAndFails(t *testing.T) {
	told := make(chan error, 8)
	s, w := takenOverReporting(t, nil, told)
	_ = s

	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	if err := waited(t, sess); err == nil {
		t.Error("nothing to open was reported as a program that finished cleanly")
	}
	select {
	case got := <-told:
		if !strings.Contains(got.Error(), "nothing to open") {
			t.Errorf("the window was told %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Error("the window serving was told nothing about it")
	}
}

// And the reason travels on the channel's error stream, not among the
// bytes a program would have written.
//
// Kept apart on the wire even though a pane puts them on one screen:
// the two are different things, and a client that wanted to tell them
// apart -- to say "gridterm could not do that" rather than draw it as
// output -- can only do so if they arrived separately.
func TestTheReasonTravelsOnItsOwnStream(t *testing.T) {
	_, w := takenOver(t, func(int, int) (session.Session, error) {
		return nil, errors.New("there is no shell here")
	})

	// Straight down a channel, so what each stream carried can be seen.
	ch, reqs, err := w.client.OpenChannel(SessionChannel, ssh.Marshal(openSession{
		Cols: 80, Rows: 24,
	}))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	go ssh.DiscardRequests(reqs)
	defer ch.Close()

	said := make(chan string, 1)
	go func() {
		got, _ := io.ReadAll(ch.Stderr())
		said <- string(got)
	}()
	out, _ := io.ReadAll(ch)

	if strings.Contains(string(out), "there is no shell here") {
		t.Errorf("the reason arrived as program output: %q", out)
	}
	select {
	case got := <-said:
		if !strings.Contains(got, "there is no shell here") {
			t.Errorf("the error stream carried %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Error("the reason never arrived at all")
	}
}

// A pane does put them on one screen, because a terminal has only one.
// A pane that opened and closed with nothing in it would leave the user
// with no idea why.
func TestAPaneIsShownTheReason(t *testing.T) {
	_, w := takenOver(t, func(int, int) (session.Session, error) {
		return nil, errors.New("there is no shell here")
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	got, _ := io.ReadAll(sess)

	if !strings.Contains(string(got), "there is no shell here") {
		t.Errorf("the pane was shown %q", got)
	}
}

// A client that pipelines requests at a session that cannot start does
// not wedge the connection it came in on.
//
// The requests arrive down the connection's one read loop, and sixteen
// unread ones stop it: every other channel on that connection would
// then wait for ever, and the window would never hear the client leave.
func TestASessionThatCannotStartDoesNotWedgeTheConnection(t *testing.T) {
	slow := make(chan struct{})
	_, w := takenOver(t, func(int, int) (session.Session, error) {
		<-slow
		return nil, errors.New("not today")
	})

	ch, reqs, err := w.client.OpenChannel(SessionChannel, ssh.Marshal(openSession{Cols: 80, Rows: 24}))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	go ssh.DiscardRequests(reqs)
	for i := 0; i < 64; i++ {
		if _, err := ch.SendRequest(reqWindowChange, false,
			ssh.Marshal(windowChange{Cols: 80, Rows: uint32(24 + i)})); err != nil {
			break
		}
	}
	close(slow)

	// The connection still answers.
	done := make(chan error, 1)
	go func() {
		_, err := w.Open(80, 24)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the connection answered with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the connection was wedged by a session that could not start")
	}
}

// The size a client asks for is clamped where it is used, not where it
// was sent from: the machine that sent it is not the one that has to
// make a terminal that size.
func TestTheSizeIsClampedWhereItIsUsed(t *testing.T) {
	sizes := make(chan [2]int, 4)
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		sizes <- [2]int{cols, rows}
		return newEchoSession(cols, rows), nil
	})

	// Straight down the channel, so the client's own clamp is not in
	// the way.
	ch, reqs, err := w.client.OpenChannel(SessionChannel, ssh.Marshal(openSession{
		Cols: 4294967295, Rows: 0,
	}))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	go ssh.DiscardRequests(reqs)
	defer ch.Close()

	select {
	case got := <-sizes:
		if got[0] < 1 || got[0] > mostCells || got[1] < 1 || got[1] > mostCells {
			t.Errorf("the program was started at %dx%d", got[0], got[1])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was started")
	}
}

// Letting go of the other window really takes what was opened on it,
// rather than only refusing to open more.
func TestLettingGoOfAWindowEndsItsSessions(t *testing.T) {
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

	// The session ends rather than hanging: reading it comes back.
	ended := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(sess)
		ended <- err
	}()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("a session outlived the window it was opened on")
	}
	if _, err := w.Open(80, 24); err == nil {
		t.Error("a window that was let go of opened another session")
	}
}

// waited waits for a session to end and gives back what it said.
func waited(t *testing.T, s session.Session) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for the program never came back")
		return nil
	}
}

// takenOverReporting is takenOver with what the serving window is told
// handed back.
func takenOverReporting(t *testing.T, open Opener, told chan error) (*Server, *Window) {
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
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys, Open: open,
		OnError: func(err error) {
			select {
			case told <- err:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Keys: []ssh.Signer{mine}, HostKey: ssh.FixedHostKey(host.PublicKey()),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return s, w
}

// deafSession is a program that never reads what it is sent, which is
// what parks a write on the far end's window.
type deafSession struct{ done chan struct{} }

func newDeafSession() *deafSession { return &deafSession{done: make(chan struct{})} }

func (d *deafSession) Read(p []byte) (int, error) {
	<-d.done
	return 0, io.EOF
}

func (d *deafSession) Write(p []byte) (int, error) {
	<-d.done
	return 0, io.EOF
}

func (d *deafSession) Resize(int, int) error { return nil }
func (d *deafSession) Wait() error           { <-d.done; return nil }

func (d *deafSession) Close() error {
	select {
	case <-d.done:
	default:
		close(d.done)
	}
	return nil
}

// stubbornSession is a program that never reads what it is sent.
//
// That is the shape a teardown bug hides in: the copy into it parks in
// a write and the copy out of it parks in a read, so neither copy is
// the thing that notices the client has gone.
type stubbornSession struct {
	done    chan struct{}
	closed  chan struct{}
	writing chan struct{}
	once    sync.Once
	wrote   sync.Once
	slow    time.Duration
	talks   bool
}

func newStubbornSession(talks bool, slow time.Duration) *stubbornSession {
	return &stubbornSession{
		done: make(chan struct{}), closed: make(chan struct{}),
		writing: make(chan struct{}), slow: slow, talks: talks,
	}
}

func (b *stubbornSession) Read(p []byte) (int, error) {
	if !b.talks {
		<-b.done
		return 0, io.EOF
	}
	select {
	case <-b.done:
		return 0, io.EOF
	case <-time.After(time.Millisecond):
	}
	return copy(p, "still here\r\n"), nil
}

// Write never returns until the program is closed, which is what parks
// whoever is feeding it.
func (b *stubbornSession) Write(p []byte) (int, error) {
	b.wrote.Do(func() { close(b.writing) })
	<-b.done
	return 0, io.EOF
}

func (b *stubbornSession) Resize(int, int) error { return nil }
func (b *stubbornSession) Wait() error           { <-b.done; return nil }

func (b *stubbornSession) Close() error {
	b.once.Do(func() {
		time.Sleep(b.slow)
		close(b.done)
		close(b.closed)
	})
	return nil
}

// park waits until the serving side is stuck feeding the program.
func park(t *testing.T, sess session.Session, prog *stubbornSession) {
	t.Helper()
	if _, err := sess.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-prog.writing:
	case <-time.After(5 * time.Second):
		t.Fatal("the serving side never started feeding the program")
	}
}

// The program is hung up on when the client lets go of just that
// session, even though nothing else is going to end it.
//
// Waiting for it first waits for a program nothing has hung up on: a
// shell sitting at a prompt does not exit because the pane drawing it
// went away.
func TestAProgramIsHungUpOnWhenItsSessionGoes(t *testing.T) {
	made := make(chan *stubbornSession, 1)
	_, w := takenOver(t, func(int, int) (session.Session, error) {
		b := newStubbornSession(true, 0)
		made <- b
		return b, nil
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	read(t, sess, "still here")
	prog := <-made
	park(t, sess, prog)

	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-prog.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the program was left running with nothing drawing it")
	}
}

// And when the whole connection goes.
//
// Then neither copy notices: one is parked writing into a program that
// is not reading, the other parked reading from a program that is not
// writing. Without something watching the connection, the program and
// its pseudo-terminal are left behind for good.
func TestAProgramIsHungUpOnWhenTheConnectionGoes(t *testing.T) {
	made := make(chan *stubbornSession, 1)
	_, w := takenOver(t, func(int, int) (session.Session, error) {
		b := newStubbornSession(false, 0)
		made <- b
		return b, nil
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	prog := <-made
	park(t, sess, prog)

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-prog.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the program outlived the connection that was drawing it")
	}
}

// The window is told a client has gone only once the shells it started
// have been hung up on.
//
// Told first, the panel would lose the row while the shell behind it
// was still being closed.
func TestAClientIsSaidToHaveGoneOnlyOnceItsShellsAre(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	host, err := HostKey(t.TempDir() + "/host_key")
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	made := make(chan *stubbornSession, 1)
	var prog *stubbornSession
	running := make(chan bool, 1)
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys,
		Open: func(int, int) (session.Session, error) {
			// Slow to close, so a window that said the client had gone
			// before its shells were hung up on says so while this one
			// is still closing.
			b := newStubbornSession(false, 300*time.Millisecond)
			made <- b
			return b, nil
		},
		OnGone: func(c *Client, why error) {
			select {
			case <-prog.closed:
				running <- false
			default:
				running <- true
			}
		},
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Keys: []ssh.Signer{mine}, HostKey: ssh.FixedHostKey(host.PublicKey()),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	prog = <-made
	park(t, sess, prog)

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case stillRunning := <-running:
		if stillRunning {
			t.Error("the window was told the client went while its shell was still running")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the window was never told the client went")
	}
}

// A client is told what the window it took over has open, and is told
// again when that changes.
func TestAClientSeesWhatTheOtherWindowHasOpen(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	host, err := HostKey(t.TempDir() + "/host_key")
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	var mu sync.Mutex
	snap := Snapshot{Window: "Local", Open: []Open{
		{ID: "Local#0", Host: "Local", Kind: "Terminal", Label: "a shell", State: "settled"},
	}}
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys,
		Opens: func() Snapshot {
			mu.Lock()
			defer mu.Unlock()
			return snap
		},
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Keys: []ssh.Signer{mine},
		HostKey: ssh.FixedHostKey(host.PublicKey()),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// What it had open as the client arrived, without waiting for
	// anything on it to change.
	waitForOpens(t, w, func(got []Open) bool {
		return len(got) == 1 && got[0].Label == "a shell"
	}, "the first snapshot")

	// And again when it changes.
	mu.Lock()
	snap.Open = append(snap.Open, Open{
		ID: "margit#0", Host: "margit", Kind: "Files", Label: "/srv", State: "active",
	})
	next := snap
	mu.Unlock()
	s.Publish(next)

	waitForOpens(t, w, func(got []Open) bool {
		return len(got) == 2 && got[1].Host == "margit" && got[1].Kind == "Files"
	}, "the second snapshot")
}

// A window that says nothing about itself is still worked in. Not every
// window on the far end is of this build.
func TestAWindowThatSaysNothingIsStillWorkedIn(t *testing.T) {
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		return newEchoSession(cols, rows), nil
	})

	if got := w.Opens(); len(got) != 0 {
		t.Errorf("it said it had %v open", got)
	}
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()
	read(t, sess, "started at")
}

// waitForOpens waits for what the other window says it has open to
// look a certain way.
func waitForOpens(t *testing.T, w *Window, ok func([]Open) bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok(w.Opens()) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waited for %s, got %v", what, w.Opens())
}

// A client works in something the serving window already has running,
// and the window is asked for exactly what the client was told about.
func TestAWindowWorksInSomethingAlreadyRunning(t *testing.T) {
	asked := make(chan Attached, 1)
	running := newEchoSession(100, 40)
	_, w := takenOverWith(t, nil, func(want Attached, cols, rows int) (session.Session, error) {
		asked <- want
		return running, nil
	})

	sess, err := w.Attach(Open{
		ID: "7", Host: "margit", Kind: "Terminal", Label: "vim README.md",
	}, 80, 24)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = sess.Close() }()

	select {
	case got := <-asked:
		if got.ID != "7" {
			t.Errorf("it asked for %q, not %q", got.ID, "7")
		}
		// The machine and the kind as well, so the window can check the
		// name still stands for what the client was told about.
		if got.Kind != "Terminal" || got.Host != "margit" {
			t.Errorf("it was asked for %+v, want what the client was told", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the serving window was never asked")
	}

	// What was already on the screen over there.
	read(t, sess, "started at 100x40")

	// And typing here reaches it.
	if _, err := sess.Write([]byte("typed-from-here")); err != nil {
		t.Fatalf("write: %v", err)
	}
	read(t, sess, "typed-from-here")
}

// A window that is not offering what it has running says so, rather
// than starting something new for a client that asked to watch.
func TestAWindowThatCannotBeWorkedInSaysSo(t *testing.T) {
	var opened int32
	_, w := takenOverWith(t, func(cols, rows int) (session.Session, error) {
		atomic.AddInt32(&opened, 1)
		return newEchoSession(cols, rows), nil
	}, nil)

	sess, err := w.Attach(Open{ID: "1", Kind: "Terminal", Label: "bash"}, 80, 24)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = sess.Close() }()

	// The channel opens before the far end has decided, so the reason
	// arrives as the session's first words.
	got := read(t, sess, "cannot be worked in")
	if strings.Contains(got, "started at") {
		t.Errorf("it started something new instead: %q", got)
	}
	if n := atomic.LoadInt32(&opened); n != 0 {
		t.Errorf("it opened %d new sessions", n)
	}
}

// What the serving window says about something that has gone reaches
// the client rather than being swallowed.
func TestAskingForSomethingGoneSaysSo(t *testing.T) {
	_, w := takenOverWith(t, nil, func(want Attached, cols, rows int) (session.Session, error) {
		return nil, fmt.Errorf("there is nothing called %q open here any more", want.ID)
	})

	sess, err := w.Attach(Open{ID: "99", Kind: "Terminal", Label: "gone"}, 80, 24)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = sess.Close() }()

	read(t, sess, `nothing called "99" open here any more`)
}

// failingSession says something and then fails, for a test about a
// program whose output stops reaching the client part way.
type failingSession struct {
	said bool
	done chan struct{}
	err  error
}

func (s *failingSession) Read(p []byte) (int, error) {
	if !s.said {
		s.said = true
		return copy(p, []byte("half a screen")), nil
	}
	return 0, s.err
}

func (s *failingSession) Write(p []byte) (int, error) { return len(p), nil }
func (s *failingSession) Resize(int, int) error       { return nil }
func (s *failingSession) Wait() error                 { return nil }

func (s *failingSession) Close() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	return nil
}

// A session whose output stopped reaching the client is not a program
// that ran and finished.
//
// The program itself thinks it ended cleanly. The client is holding
// half a screen, and this is the only end that knows.
func TestASessionThatFailedPartWayDidNotEndCleanly(t *testing.T) {
	_, w := takenOverReporting(t, func(cols, rows int) (session.Session, error) {
		return &failingSession{
			done: make(chan struct{}),
			err:  errors.New("the pipe broke"),
		}, nil
	}, make(chan error, 8))

	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sess.Close() }()

	read(t, sess, "half a screen")
	if err := waited(t, sess); err == nil {
		t.Error("a program whose output stopped was reported as a clean finish")
	}
}

// Telling clients what this window has open does not wait for any of
// them.
//
// The window that has the snapshot is the one that draws. A socket
// write from there would stop the screen for as long as the slowest
// client took to read, and a client that has stopped reading would
// stop it for good.
func TestSayingWhatIsOpenDoesNotWaitForAClient(t *testing.T) {
	s, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		return newEchoSession(cols, rows), nil
	})

	// A client that opens the channel and never reads a byte of it.
	ch, reqs, err := w.client.OpenChannel(chanControl, nil)
	if err != nil {
		t.Fatalf("open control: %v", err)
	}
	go ssh.DiscardRequests(reqs)
	defer func() { _ = ch.Close() }()

	// Far more than any window will hold, so a write that waited would
	// still be waiting.
	big := serve1MB()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 64; i++ {
			s.Publish(big)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("saying what is open waited for a client that is not reading")
	}
}

// serve1MB is a snapshot big enough that sending it many times fills
// any window a client is not emptying.
func serve1MB() Snapshot {
	snap := Snapshot{Window: "big"}
	for i := 0; i < 200; i++ {
		snap.Open = append(snap.Open, Open{
			ID:    "x" + strings.Repeat("y", 100),
			Host:  "host",
			Kind:  "Terminal",
			Label: strings.Repeat("z", 100),
		})
	}
	return snap
}

// A window that does not serve its files says so by name, rather than
// leaving the client waiting on a channel nobody answers.
func TestAWindowThatDoesNotServeItsFilesSaysSo(t *testing.T) {
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		return newEchoSession(cols, rows), nil
	})

	ch, err := filesWithin(t, w)

	if err == nil {
		_ = ch.Close()
		t.Fatal("it opened a file session on a window that serves none")
	}
	if !strings.Contains(err.Error(), "does not serve its files") {
		t.Errorf("it said %v", err)
	}
}

// filesWithin asks for a file session, failing the test rather than
// hanging it when the other window goes quiet instead of answering.
//
// A test that hangs takes the whole package down with it at the binary
// timeout, and says nothing about which one was wrong.
func filesWithin(t *testing.T, w *Window) (*FileSession, error) {
	t.Helper()
	return askingFiles(t, w.Files)
}

// filesOnWithin is filesWithin for a machine the other window is
// connected to.
func filesOnWithin(t *testing.T, w *Window, host string) (*FileSession, error) {
	t.Helper()
	return askingFiles(t, func() (*FileSession, error) { return w.FilesOn(host) })
}

// askingFiles waits out one ask for a file session.
func askingFiles(t *testing.T, ask func() (*FileSession, error)) (*FileSession, error) {
	t.Helper()
	type got struct {
		f   *FileSession
		err error
	}
	back := make(chan got, 1)
	go func() {
		f, err := ask()
		back <- got{f: f, err: err}
	}()
	select {
	case g := <-back:
		return g.f, g.err
	case <-time.After(10 * time.Second):
		t.Fatal("the other window never answered the ask for its files")
		return nil, nil
	}
}

// A file session says which machine it is for, so a client can ask the
// window for a machine that window is connected to rather than for its
// own disk.
func TestAFileSessionNamesTheMachineItIsFor(t *testing.T) {
	asked := make(chan string, 2)
	s, w := takenOver(t, nil)
	s.cfg.Files = func(_ context.Context, host string, ch io.ReadWriteCloser) error {
		asked <- host
		return nil
	}

	// Plain files are the machine the window itself is on, which is no
	// name at all.
	f, err := filesWithin(t, w)
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	if got := whatWasAsked(t, asked); got != "" {
		t.Errorf("Files asked for %q, want the window's own machine", got)
	}
	_ = f.Close()

	// And a named one is that machine, spelled the way the window over
	// there spells it.
	f, err = filesOnWithin(t, w, "margit")
	if err != nil {
		t.Fatalf("files on margit: %v", err)
	}
	if got := whatWasAsked(t, asked); got != "margit" {
		t.Errorf("FilesOn asked for %q", got)
	}
	_ = f.Close()
}

// whatWasAsked is the machine the window was asked for, or a failed test
// when it was asked for nothing.
func whatWasAsked(t *testing.T, asked <-chan string) string {
	t.Helper()
	select {
	case host := <-asked:
		return host
	case <-time.After(5 * time.Second):
		t.Fatal("the window serving was never asked for any files")
		return ""
	}
}

// withinTime runs something that talks to the other window, failing the
// test rather than hanging it.
func withinTime(t *testing.T, what string, do func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- do() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: the other window never answered", what)
	}
}

// And one that does serves them over the connection the shells ride on.
func TestTheFilesOfAServingWindowCross(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "over-there.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	served := make(chan error, 1)
	s, w := takenOver(t, nil)
	s.cfg.Files = func(_ context.Context, _ string, ch io.ReadWriteCloser) error {
		srv, err := sftp.NewServer(ch)
		if err != nil {
			served <- err
			return err
		}
		defer func() { _ = srv.Close() }()
		err = srv.Serve()
		if errors.Is(err, io.EOF) {
			err = nil
		}
		served <- err
		return err
	}

	ch, err := filesWithin(t, w)
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	var client *sftp.Client
	withinTime(t, "start a file session", func() error {
		var err error
		client, err = sftp.NewClientPipe(ch, ch)
		return err
	})
	var entries []os.FileInfo
	withinTime(t, "read "+dir, func() error {
		var err error
		entries, err = client.ReadDir(dir)
		return err
	})
	found := false
	for _, e := range entries {
		if e.Name() == "over-there.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("the file was not there: %v", entries)
	}

	if err := client.Close(); err != nil {
		t.Errorf("close the client: %v", err)
	}
	_ = ch.Close()
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("serving gave %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("the file session never finished")
	}
}

// Only so many file sessions ride on one connection.
//
// A file session is a dozen goroutines and as many open files as it is
// asked for, none of which shows in the window being served. Without a
// cap a client could quietly make that machine run out of handles.
func TestOnlySoManyFileSessionsAtOnce(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	s, w := takenOver(t, nil)
	s.cfg.Files = func(_ context.Context, _ string, ch io.ReadWriteCloser) error {
		// Held open until the test is done with it.
		<-stop
		return nil
	}

	var open []*FileSession
	defer func() {
		for _, f := range open {
			_ = f.Close()
		}
	}()
	for i := 0; i < mostFileSessions; i++ {
		f, err := filesWithin(t, w)
		if err != nil {
			t.Fatalf("file session %d: %v", i, err)
		}
		open = append(open, f)
	}

	f, err := filesWithin(t, w)
	if err == nil {
		_ = f.Close()
		t.Fatal("it opened one more than it serves")
	}
	if !strings.Contains(err.Error(), "as many file sessions") {
		t.Errorf("it said %v", err)
	}
}

// A file session that could not start says why, on the machine that
// asked for it as well as the one it failed on.
func TestAFileSessionThatFailedSaysWhyToTheClient(t *testing.T) {
	told := make(chan error, 8)
	s, w := takenOverReporting(t, nil, told)
	s.cfg.Files = func(_ context.Context, _ string, ch io.ReadWriteCloser) error {
		return errors.New("the disk is not there")
	}

	f, err := filesWithin(t, w)
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	defer func() { _ = f.Close() }()

	if why := f.Said(); !strings.Contains(why, "the disk is not there") {
		t.Errorf("the client was told %q", why)
	}
	// Asked twice, because a failure is read once for the message and
	// once by whatever reports it.
	if why := f.Said(); !strings.Contains(why, "the disk is not there") {
		t.Errorf("asking again gave %q", why)
	}
	select {
	case got := <-told:
		if !strings.Contains(got.Error(), "the disk is not there") {
			t.Errorf("the window was told %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Error("the window serving was told nothing about it")
	}
}

// Reaching a machine that answers and then says nothing gives up.
//
// ssh.ClientConfig.Timeout does not bound this: it bounds the TCP
// connection that ssh.Dial makes, and this makes its own so that
// cancelling can close it. Without a deadline of its own the window
// says "opening" for ever, with no way back but restarting it.
func TestAMachineThatAnswersAndSaysNothingIsGivenUpOn(t *testing.T) {
	mine, _ := aKey(t, "marcus@laptop")
	host, err := HostKey(t.TempDir() + "/host_key")
	if err != nil {
		t.Fatalf("host key: %v", err)
	}

	addr := sshtest.SilentMachine(t)
	done := make(chan error, 1)
	go func() {
		_, err := Dial(context.Background(), DialConfig{
			Addr: addr, Keys: []ssh.Signer{mine},
			HostKey:  ssh.FixedHostKey(host.PublicKey()),
			Patience: 300 * time.Millisecond,
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("it says it reached a machine that never spoke")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("it waited for a machine that answered and then said nothing")
	}
}

// And giving up on one comes back at once, rather than waiting out the
// deadline.
func TestGivingUpOnASilentMachineComesBackAtOnce(t *testing.T) {
	mine, _ := aKey(t, "marcus@laptop")
	host, err := HostKey(t.TempDir() + "/host_key")
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	addr := sshtest.SilentMachine(t)
	done := make(chan error, 1)
	go func() {
		_, err := Dial(ctx, DialConfig{
			Addr: addr, Keys: []ssh.Signer{mine},
			HostKey: ssh.FixedHostKey(host.PublicKey()),
			// Long, so what ends this can only be the giving up.
			Patience: 5 * time.Minute,
		})
		done <- err
	}()
	// Long enough to be in the handshake rather than the connect.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("it says it reached a machine that never spoke")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("giving up waited for the deadline instead of taking effect")
	}
}

// A connection that was made is not held to the handshake's deadline. A
// window somebody is working in sits idle for as long as they like.
func TestAConnectionThatWasMadeHasNoDeadline(t *testing.T) {
	_, w := takenOver(t, func(cols, rows int) (session.Session, error) {
		return newEchoSession(cols, rows), nil
	})

	// Longer than the handshake was given, with nothing said on it.
	time.Sleep(200 * time.Millisecond)

	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sess.Close() }()
	read(t, sess, "started at 80x24")
}

// Reaching a window says each step as it is tried, so one that stops
// says where.
//
// The window had one word for the whole of it -- "connecting" -- which
// covered the socket, the handshake, the host key and the keys offered.
// A user whose window never answered had that word and nothing else.
func TestDialSaysEachStep(t *testing.T) {
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
		Open:    func(cols, rows int) (session.Session, error) { return newEchoSession(cols, rows), nil },
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var said []string
	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Keys: []ssh.Signer{mine},
		HostKey: ssh.FixedHostKey(host.PublicKey()),
		Saying:  func(what string) { said = append(said, what) },
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	account := strings.Join(said, "\n")
	for _, want := range []string{
		"reaching " + s.Addr(),
		"asking " + s.Addr() + " who it is",
		"host key",
		"its host key is accepted",
		"signing in as gridterm, offering 1 keys",
	} {
		if !strings.Contains(account, want) {
			t.Errorf("the account does not say %q:\n%s", want, account)
		}
	}
}

// servingFiles is a window serving file sessions and another that has
// reached it.
func servingFiles(t *testing.T, files Filer) (*Server, *Window) {
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
		Files:   files,
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	w, err := Dial(context.Background(), DialConfig{
		Addr: s.Addr(), Keys: []ssh.Signer{mine},
		HostKey: ssh.FixedHostKey(host.PublicKey()),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return s, w
}

// A file session opened with no payload at all asks for the machine
// being served.
//
// That is what a client of an older build sends: it knew only one
// machine to ask for, so there was nothing to say.
func TestAFileSessionWithNoPayloadAsksForTheServedMachine(t *testing.T) {
	asked := make(chan string, 1)
	_, w := servingFiles(t, func(_ context.Context, host string, ch io.ReadWriteCloser) error {
		asked <- host
		// Read until the client goes, which is what a Filer must do.
		_, _ = io.Copy(io.Discard, ch)
		return nil
	})

	ch, reqs, err := w.client.OpenChannel(chanFiles, nil)
	if err != nil {
		t.Fatalf("open a file session: %v", err)
	}
	go ssh.DiscardRequests(reqs)
	defer func() { _ = ch.Close() }()

	select {
	case got := <-asked:
		if got != "" {
			t.Errorf("it was asked for %q, want the machine being served", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no file session was started")
	}
}

// A payload this build cannot read is refused by name, and the reason
// reaches the client.
//
// The encoding is positional, with no room for a field one end knows and
// the other does not, so this is what a gridterm of another build looks
// like from here.
func TestAFileSessionRequestOfAnotherBuildIsRefused(t *testing.T) {
	started := make(chan struct{}, 1)
	_, w := servingFiles(t, func(context.Context, string, io.ReadWriteCloser) error {
		started <- struct{}{}
		return nil
	})

	// One byte: a string on the wire needs four for its length alone.
	_, _, err := w.client.OpenChannel(chanFiles, []byte{0x01})
	if err == nil {
		t.Fatal("a request this build cannot read was accepted")
	}
	if !strings.Contains(err.Error(), "same build") {
		t.Errorf("it was refused with %q, want the reason a user can act on", err)
	}
	// Given a moment: the refusal comes back from the goroutine answering
	// channels, and a session started an instant later would slip past a
	// read that only looked once.
	select {
	case <-started:
		t.Error("a file session was started for a request that could not be read")
	case <-time.After(200 * time.Millisecond):
	}
}
