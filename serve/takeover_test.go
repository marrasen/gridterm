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
	endWith  error

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
	return s.endWith
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

// And the reason goes on the error stream, not into the bytes a program
// would have written: sent that way, a client could not tell this
// window's words from the output of what it thought it started.
func TestTheReasonIsNotMixedIntoTheOutput(t *testing.T) {
	_, w := takenOver(t, func(int, int) (session.Session, error) {
		return nil, errors.New("there is no shell here")
	})
	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	got, _ := io.ReadAll(sess)

	if strings.Contains(string(got), "there is no shell here") {
		t.Errorf("the reason arrived as program output: %q", got)
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

	ch, reqs, err := w.client.OpenChannel(chanSession, ssh.Marshal(openSession{Cols: 80, Rows: 24}))
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
	ch, reqs, err := w.client.OpenChannel(chanSession, ssh.Marshal(openSession{
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
		Addr: s.Addr(), Key: mine, HostKey: ssh.FixedHostKey(host.PublicKey()),
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
		Addr: s.Addr(), Key: mine, HostKey: ssh.FixedHostKey(host.PublicKey()),
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
