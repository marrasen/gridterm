package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// A handshake through another machine can be given up on.
//
// It is not a socket: it is a channel of the connection carrying it,
// which takes no deadline and need not stop when it is closed. Waiting
// for it left the window unable to give up on a machine that never
// answered, and holding the name of the machine in the way so that
// nothing could be opened on that either.
func TestAHandshakeThroughAnotherMachineCanBeGivenUpOn(t *testing.T) {
	near := sshtest.New(t)
	bastion := connectTest(t, near)

	host, port := sshtest.Deaf(t)
	cfg := testConfig(t, near)
	cfg.Host, cfg.Port = host, port

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		c, err := bastion.Through(ctx, cfg)
		if c != nil {
			_ = c.Close()
		}
		done <- err
	}()

	// Long enough to be inside the handshake rather than still opening
	// the channel to it.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Through = %v, want it to say it was cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("giving up on the handshake did not come back")
	}
}

// A machine that answers the socket and then says nothing is given up
// on by itself, so a connection nobody is watching does not wait for
// ever.
func TestAHandshakeThatIsNeverAnsweredStops(t *testing.T) {
	was := helloTimeout
	helloTimeout = 300 * time.Millisecond
	t.Cleanup(func() { helloTimeout = was })

	host, port := sshtest.Deaf(t)
	cfg := Config{
		Host: host, Port: port, User: "tester",
		NoAgent: true, NoIdentities: true,
		Ask:             newTestAsk(),
		HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
	}

	started := time.Now()
	c, err := Connect(context.Background(), cfg)
	if c != nil {
		_ = c.Close()
	}
	if err == nil {
		t.Fatal("a machine that said nothing was connected to")
	}
	if !strings.Contains(err.Error(), "did not say who it is") {
		t.Fatalf("Connect = %v, want it to say the machine never answered", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("it waited %s, want about %s", took, helloTimeout)
	}
}

// stubborn is a connection that closing does not stop.
//
// A connection carried inside another one behaves this way: it is a
// channel of that connection rather than a socket, it takes no deadline
// at all, and a read already waiting on it need not come back when this
// end closes it.
type stubborn struct {
	blocked chan struct{}
	closed  chan struct{}
}

func newStubborn() *stubborn {
	return &stubborn{blocked: make(chan struct{}), closed: make(chan struct{})}
}

func (s *stubborn) Read([]byte) (int, error)        { <-s.blocked; return 0, io.EOF }
func (s *stubborn) Write(p []byte) (int, error)     { return len(p), nil }
func (s *stubborn) LocalAddr() net.Addr             { return dummyAddr{} }
func (s *stubborn) RemoteAddr() net.Addr            { return dummyAddr{} }
func (s *stubborn) SetDeadline(time.Time) error     { return errors.New("ssh: deadline not supported") }
func (s *stubborn) SetReadDeadline(time.Time) error { return errors.New("ssh: deadline not supported") }
func (s *stubborn) SetWriteDeadline(time.Time) error {
	return errors.New("ssh: deadline not supported")
}

func (s *stubborn) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

type dummyAddr struct{}

func (dummyAddr) Network() string { return "stub" }
func (dummyAddr) String() string  { return "stub" }

// Giving up comes back at once even when closing the connection does
// not stop the handshake.
//
// This is the machine reached through another one that never answered:
// the window could not let go of it, and went on holding the name of
// the machine in the way, so nothing could be opened on that either.
func TestGivingUpOnAHandshakeThatWillNotStop(t *testing.T) {
	nc := newStubborn()
	t.Cleanup(func() { close(nc.blocked) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		c, err := dialOnce(ctx, func(context.Context, string) (net.Conn, error) { return nc, nil },
			"stuck:22", &ssh.ClientConfig{
				User:            "tester",
				Auth:            []ssh.AuthMethod{ssh.Password("x")},
				HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
			}, nil)
		if c != nil {
			_ = c.Close()
		}
		done <- err
	}()

	// Long enough to be inside the handshake rather than still writing
	// the version string.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dialOnce = %v, want it to say it was cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("giving up on the handshake did not come back")
	}
	// It closed the connection on the way out, which is what stops a
	// handshake that can be stopped.
	select {
	case <-nc.closed:
	default:
		t.Error("the connection was left open")
	}
}

// gated is a connection that holds its first read until it is let
// through, and that closing does not stop.
//
// It is the shape of a connection carried inside another one: the far
// end can still answer after this end has given up and closed it.
type gated struct {
	net.Conn
	open chan struct{}
	once sync.Once
}

func (g *gated) Read(p []byte) (int, error) {
	g.once.Do(func() { <-g.open })
	return g.Conn.Read(p)
}

// Close takes effect only once the read has been let through.
//
// Closing a channel of another connection is like this: this end is
// closed, a read already waiting on it carries on, and the far end goes
// on answering until it notices.
func (g *gated) Close() error {
	select {
	case <-g.open:
		return g.Conn.Close()
	default:
		return nil
	}
}

// A handshake that lands after it was given up on is closed.
//
// Giving up does not stop it: the far end may still answer, and the
// connection it makes belongs to nobody. Left open, it is a login on a
// machine that thinks somebody is working in it.
func TestAHandshakeThatLandsAfterGivingUpIsClosed(t *testing.T) {
	s := sshtest.New(t)
	open := make(chan struct{})
	var letThrough sync.Once
	release := func() { letThrough.Do(func() { close(open) }) }
	defer release()

	to := func(ctx context.Context, addr string) (net.Conn, error) {
		c, err := overTCP(ctx, addr)
		if err != nil {
			return nil, err
		}
		return &gated{Conn: c, open: open}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		c, err := dialOnce(ctx, to, s.Addr(), &ssh.ClientConfig{
			User:            "tester",
			Auth:            []ssh.AuthMethod{ssh.Password(sshtest.Password)},
			HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
		}, nil)
		if c != nil {
			_ = c.Close()
		}
		done <- err
	}()

	// Long enough to be inside the handshake rather than still reaching
	// the machine.
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("dialOnce = %v, want it to say it was cancelled", err)
	}

	// Now let the handshake finish. What it makes is nobody's, and has
	// to be closed rather than left on the server.
	release()
	waitForConns(t, s, 0)
}

// Cancelled between the handshake finishing and this noticing: the
// connection is closed rather than handed back.
func TestAConnectionCancelledAsItFinishedIsClosed(t *testing.T) {
	s := sshtest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, err := dialOnce(ctx, overTCP, s.Addr(), &ssh.ClientConfig{
		User: "tester",
		Auth: []ssh.AuthMethod{ssh.Password(sshtest.Password)},
		HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error {
			// The user gives up while the handshake is still running,
			// which is the only way to land on that branch on purpose.
			cancel()
			return nil
		},
	}, nil)
	if c != nil {
		_ = c.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dialOnce = %v, want it to say it was cancelled", err)
	}
	waitForConns(t, s, 0)
}

// Signing in is not bounded by the time the far end had to say who it
// is.
//
// It can wait on a passphrase dialog, and a bound that went on running
// would close the connection while the user was typing.
func TestSigningInIsNotBoundedByTheHello(t *testing.T) {
	was := helloTimeout
	helloTimeout = 100 * time.Millisecond
	t.Cleanup(func() { helloTimeout = was })

	s := sshtest.New(t)
	cfg := testConfig(t, s)
	check := cfg.HostKeyCallback
	cfg.HostKeyCallback = func(hostname string, addr net.Addr, key ssh.PublicKey) error {
		// As long as somebody reading a fingerprint and deciding.
		time.Sleep(5 * helloTimeout)
		return check(hostname, addr, key)
	}
	c, err := Connect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = c.Close()
}

// waitForConns waits until the server holds this many live connections.
func waitForConns(t *testing.T, s *sshtest.Server, want int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; {
		if s.Live() == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server holds %d connections, want %d", s.Live(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// shutCounter counts how many times it was closed.
type shutCounter struct {
	mu sync.Mutex
	n  int
}

func (s *shutCounter) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return nil
}

func (s *shutCounter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// An agent that does not get a connection past it is let go of.
//
// Closing the socket is the only thing that unblocks it. Listing keys
// reads from memory, but signing with one can go to a smartcard and stop
// there for a PIN in a window nobody is looking at.
func TestAnAgentThatDoesNotAnswerIsLetGoOf(t *testing.T) {
	sock := &shutCounter{}
	var said []string
	a := &auth{agent: sock, saying: func(what string) { said = append(said, what) }}

	a.holdTheAgentTo(50*time.Millisecond, "to say what keys it holds", "", false)
	for deadline := time.Now().Add(5 * time.Second); sock.count() == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the agent socket was never closed")
		}
		time.Sleep(time.Millisecond)
	}
	if len(said) == 0 || !strings.Contains(strings.Join(said, "\n"), "letting go of it") {
		t.Errorf("it said %q, want it to say it let go of the agent", said)
	}

	// And the connection closing it too closes it only the once: an
	// os.File closed twice reports a failure that means nothing.
	if err := a.closeAgent(); err != nil {
		t.Fatalf("close the agent: %v", err)
	}
	if n := sock.count(); n != 1 {
		t.Fatalf("the agent socket was closed %d times, want once", n)
	}
}

// A connection that gets past the agent stops watching it, so the socket
// it hands on is not closed under it.
func TestGettingPastTheAgentStopsWatchingIt(t *testing.T) {
	sock := &shutCounter{}
	a := &auth{agent: sock}

	a.holdTheAgentTo(50*time.Millisecond, "to sign", "", false)
	a.done()
	time.Sleep(200 * time.Millisecond)
	if n := sock.count(); n != 0 {
		t.Fatalf("the agent socket was closed %d times, want not at all", n)
	}
	// And the connection owns it from there.
	closer := a.agentCloser()
	if closer == nil {
		t.Fatal("the connection was given no way to close the agent socket")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close the agent: %v", err)
	}
	if n := sock.count(); n != 1 {
		t.Fatalf("the agent socket was closed %d times, want once", n)
	}
}

// Once the ladder is past the agent, the window stops watching it.
//
// Signing in can wait on the user for as long as they take, and a watch
// still running would close the agent socket under a connection that had
// stopped asking it anything.
func TestPastTheAgentTheWatchStops(t *testing.T) {
	sock := &shutCounter{}
	a := &auth{agent: sock}
	a.ladder = []rung{
		{method: methodPublicKey, agent: true, what: "the agent's keys",
			build: func() ssh.AuthMethod { return nil }},
		{method: methodPassword, what: "a password",
			build: func() ssh.AuthMethod { return nil }},
	}
	both := &ssh.ClientAuthContext{AllowedMethods: []string{methodPublicKey, methodPassword}}

	if _, err := a.next(both); err != nil {
		t.Fatalf("the agent's rung: %v", err)
	}
	a.holdTheAgentTo(50*time.Millisecond, "to sign", "", false)

	if _, err := a.next(both); err != nil {
		t.Fatalf("the rung after it: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := sock.count(); n != 0 {
		t.Fatalf("the agent socket was closed %d times, want not at all", n)
	}
}

// An agent that did not answer is asked once, not once per connection.
//
// One that accepts a connection and then says nothing costs every
// connection the wait to find that out. It is remembered instead, so it
// costs the first one only.
func TestAnAgentThatDidNotAnswerIsNotAskedAgain(t *testing.T) {
	s := sshtest.New(t)
	ring := NewRing()
	ring.AgentGaveUp(errors.New("the SSH agent had 10s to say what keys it holds and did not"))

	var said []string
	cfg := testConfig(t, s)
	cfg.NoAgent = false
	cfg.Ring = ring
	cfg.Saying = func(what string) { said = append(said, what) }

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	account := strings.Join(said, "\n")
	if !strings.Contains(account, "leaving the SSH agent alone") {
		t.Fatalf("the account does not say the agent was left alone:\n%s", account)
	}
	if strings.Contains(account, "asking the SSH agent what keys it holds") {
		t.Fatalf("it asked the agent anyway:\n%s", account)
	}
}

// Locking the keys forgets the agent too, for one started or unstuck
// since.
func TestLockingTheKeysForgetsTheAgentTrouble(t *testing.T) {
	ring := NewRing()
	ring.AgentGaveUp(errors.New("it said nothing"))
	if ring.AgentTrouble() == nil {
		t.Fatal("the agent's trouble was not remembered")
	}
	// The first reason stands: a later one is the same agent saying
	// nothing in a different way.
	ring.AgentGaveUp(errors.New("something else"))
	if got := ring.AgentTrouble(); got.Error() != "it said nothing" {
		t.Errorf("it remembers %q, want the first reason", got)
	}
	ring.Lock()
	if why := ring.AgentTrouble(); why != nil {
		t.Errorf("it still remembers %v", why)
	}
}

// A gridterm window refuses an ordinary SSH session, and the failure
// says what to do about it.
//
// The refusal that crosses the wire says only that a channel type is
// unknown. A user who saved a window as a machine got that and nothing
// else, with no hint that one field in the dialog was wrong.
func TestASessionOnAGridtermWindowSaysWhatItIs(t *testing.T) {
	refusal := errors.New(
		`ssh: rejected: unknown channel type ("this is gridterm, and it serves session@gridterm")`)
	if !isGridterm(refusal) {
		t.Fatal("it does not recognise a gridterm window")
	}
	if isGridterm(errors.New("ssh: rejected: administratively prohibited")) {
		t.Error("it calls an ordinary refusal a gridterm window")
	}
	if isGridterm(nil) {
		t.Error("it calls no failure a gridterm window")
	}

	// And the message reads once. The refusal arrives already wrapped in
	// "open a session on <machine>", and embedding that whole sentence
	// gave the user the same machine twice in one line.
	said := notAMachine("box:22", fmt.Errorf("remote: open a session on tester@box:22: %w", refusal)).Error()
	if n := strings.Count(said, "box:22"); n != 1 {
		t.Errorf("the message names the machine %d times, want once:\n%s", n, said)
	}
	if strings.Contains(said, "open a session on") {
		t.Errorf("the message reports the open a second time:\n%s", said)
	}
	if !strings.Contains(said, "session@gridterm") {
		t.Errorf("the message drops what the far end actually said:\n%s", said)
	}
}

// Closing a pane whose write is parked comes straight back.
//
// A write to the session channel blocks on SSH flow control as soon as
// the far program stops reading its input, and Close runs on the
// goroutine that draws. A lock shared between the two is how the whole
// window stops.
func TestClosingAShellWhoseWriteIsParkedComesBack(t *testing.T) {
	s := sshtest.New(t)
	s.StopReadingInput()
	sh := startTest(t, s, nil)
	readUntil(t, sh, "READY", 5*time.Second)

	// More than the channel's flow-control window, into a program that
	// reads none of it, so this write never finishes on its own.
	wrote := make(chan struct{})
	go func() {
		defer close(wrote)
		_, _ = sh.Write(make([]byte, 4<<20))
	}()
	// Long enough to be parked inside the write rather than still on its
	// way into it.
	time.Sleep(200 * time.Millisecond)
	select {
	case <-wrote:
		t.Fatal("the write finished, so nothing was parked and this proves nothing")
	default:
	}

	closed := make(chan error, 1)
	go func() { closed <- sh.Close() }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("closing a shell whose write is parked never came back")
	}

	// And the parked write lets go once the channel has gone, or the
	// goroutine carrying a keystroke would be held for the life of the
	// program.
	select {
	case <-wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("the parked write never came back after the shell was closed")
	}
}

// A machine that takes a request to open a session and never answers is
// given up on. Opening a terminal runs on the goroutine that draws, so
// waiting for that machine is the whole window.
func TestOpeningAShellOnASilentMachineGivesUp(t *testing.T) {
	was := channelTimeout
	channelTimeout = 300 * time.Millisecond
	t.Cleanup(func() { channelTimeout = was })

	s := sshtest.New(t)
	c := connectTest(t, s)
	// Connected first, so the machine is reachable and answering right
	// up to the moment the session is asked for.
	s.StallChannels()

	done := make(chan error, 1)
	go func() {
		sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})
		if sh != nil {
			_ = sh.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a shell started on a machine that never answered")
		}
		if !errors.Is(err, ErrNoAnswer) {
			t.Fatalf("Shell = %v, want it to say the machine did not answer", err)
		}
		// Named, because the user has to decide which machine to look at.
		if !strings.Contains(err.Error(), s.Addr()) {
			t.Fatalf("Shell = %v, want it to name %s", err, s.Addr())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening a shell on a silent machine never came back")
	}
}

// A machine that answers after the client gave up has opened a session
// nobody wants. The client closes it rather than leaving it behind.
func TestASessionAnsweredAfterGivingUpIsClosed(t *testing.T) {
	was := channelTimeout
	channelTimeout = 300 * time.Millisecond
	t.Cleanup(func() { channelTimeout = was })

	s := sshtest.New(t)
	c := connectTest(t, s)
	s.StallChannels()

	sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})
	if sh != nil {
		_ = sh.Close()
	}
	if !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("Shell = %v, want it to say the machine did not answer", err)
	}
	if s.SessionsOpened() != 0 {
		t.Fatal("the server opened a session while stalled, so this proves nothing")
	}

	s.AnswerChannels()
	waitForThis(t, "the late answer to open a session", func() bool { return s.SessionsOpened() == 1 })
	waitForThis(t, "the unwanted session to be closed", func() bool { return s.Sessions() == 0 })
}

// The same for a file pane: the session it needs is opened the same way.
func TestOpeningFilesOnASilentMachineGivesUp(t *testing.T) {
	was := channelTimeout
	channelTimeout = 300 * time.Millisecond
	t.Cleanup(func() { channelTimeout = was })

	s := sshtest.New(t)
	c := connectTest(t, s)
	s.StallChannels()

	done := make(chan error, 1)
	go func() {
		f, err := c.Files(t.Context())
		if f != nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a file session started on a machine that never answered")
		}
		if !errors.Is(err, ErrNoAnswer) {
			t.Fatalf("Files = %v, want it to say the machine did not answer", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening a file session on a silent machine never came back")
	}
}

// A machine that opens the channel and then says nothing about the
// subsystem is given up on too. It is one round trip further on, and
// unbounded in the same way.
func TestAskingForSFTPOnASilentMachineGivesUp(t *testing.T) {
	was := channelTimeout
	channelTimeout = 300 * time.Millisecond
	t.Cleanup(func() { channelTimeout = was })

	s := sshtest.New(t)
	s.StallSubsystems()
	c := connectTest(t, s)

	done := make(chan error, 1)
	go func() {
		f, err := c.Files(t.Context())
		if f != nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("SFTP started on a machine that never answered the request")
		}
		if !strings.Contains(err.Error(), "sftp subsystem") || !errors.Is(err, ErrNoAnswer) {
			t.Fatalf("Files = %v, want it to name the subsystem and say nothing came back", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("asking for SFTP on a silent machine never came back")
	}
	// Nothing is left open on the machine: a session held for every
	// attempt would use up the ten a real sshd allows.
	deadline := time.Now().Add(5 * time.Second)
	for s.Sessions() > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := s.Sessions(); got != 0 {
		t.Fatalf("the machine still has %d sessions open", got)
	}
}

// Giving up on a machine that is still opening a session is the user
// closing the dialog, and it must not wait for the machine either.
func TestOpeningAShellCanBeCancelled(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)
	s.StallChannels()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		sh, err := c.Shell(ctx, ShellConfig{Cols: 80, Rows: 24})
		if sh != nil {
			_ = sh.Close()
		}
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Shell = %v, want it to say it was cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("giving up on opening a shell did not come back")
	}
}

// A machine that opens the session and then never grants the pty is
// given up on.
//
// Opening a shell is three round trips, not one. Bounding only the first
// left the other two on the goroutine that draws, so a machine that
// answered the channel and stopped there still took the window.
func TestOpeningAShellWhoseMachineNeverGrantsAPtyGivesUp(t *testing.T) {
	was := channelTimeout
	channelTimeout = 300 * time.Millisecond
	t.Cleanup(func() { channelTimeout = was })

	s := sshtest.New(t)
	s.StallPty()
	c := connectTest(t, s)

	done := make(chan error, 1)
	go func() {
		sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})
		if sh != nil {
			_ = sh.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a shell started on a machine that never granted a pty")
		}
		if !errors.Is(err, ErrNoAnswer) {
			t.Fatalf("Shell = %v, want it to say the machine did not answer", err)
		}
		if !strings.Contains(err.Error(), "pty") {
			t.Fatalf("Shell = %v, want it to name the pty", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening a shell on a machine that never granted a pty never came back")
	}

	// The session it did open is closed: one left behind for every
	// attempt would use up the ten a real sshd allows.
	deadline := time.Now().Add(5 * time.Second)
	for s.Sessions() > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := s.Sessions(); got != 0 {
		t.Fatalf("the machine still has %d sessions open", got)
	}
}

// A machine that takes a request to listen on a port and says nothing is
// given up on. Opening a tunnel runs on the goroutine that draws.
func TestOpeningARemoteTunnelOnASilentMachineGivesUp(t *testing.T) {
	was := channelTimeout
	channelTimeout = 300 * time.Millisecond
	t.Cleanup(func() { channelTimeout = was })

	s := sshtest.New(t)
	c := connectTest(t, s)
	s.StallForwards()

	done := make(chan error, 1)
	go func() {
		f, err := c.OpenTunnel(t.Context(), TunnelConfig{
			Tunnel: Tunnel{Kind: RemoteForward, Listen: "127.0.0.1:9100", Target: "127.0.0.1:9101"},
		})
		if f != nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a tunnel opened on a machine that never answered")
		}
		if !errors.Is(err, ErrNoAnswer) {
			t.Fatalf("OpenTunnel = %v, want it to say the machine did not answer", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening a remote tunnel on a silent machine never came back")
	}
}

// Closing a remote tunnel on a machine that stops answering comes back.
//
// The listener belongs to the far machine, so closing it asks that
// machine to stop listening and waits for the reply. Unbounded, that
// wait is the whole window: closing a tunnel runs on the goroutine that
// draws, and so does closing the connection carrying it.
func TestClosingARemoteTunnelOnASilentMachineComesBack(t *testing.T) {
	was := channelTimeout
	channelTimeout = 300 * time.Millisecond
	t.Cleanup(func() { channelTimeout = was })

	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
		Tunnel: Tunnel{Kind: RemoteForward, Listen: "127.0.0.1:9200", Target: "127.0.0.1:9201"},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	// Opened first, so the machine answered right up to the moment the
	// tunnel was closed.
	s.StallCancelForward()

	done := make(chan error, 1)
	go func() { done <- f.Close() }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNoAnswer) {
			t.Fatalf("Close = %v, want it to say the machine did not answer", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing a remote tunnel on a silent machine never came back")
	}
}

// heldWriter is a stdin that holds one write until the test lets it
// through, so a test can say exactly when a write is in flight.
type heldWriter struct {
	io.WriteCloser
	began  chan struct{}
	let    chan struct{}
	shut   chan struct{}
	once   sync.Once
	shutOn sync.Once
}

func (h *heldWriter) Write(p []byte) (int, error) {
	h.once.Do(func() { close(h.began) })
	<-h.let
	return h.WriteCloser.Write(p)
}

func (h *heldWriter) Close() error {
	h.shutOn.Do(func() { close(h.shut) })
	return h.WriteCloser.Close()
}

// A write that is in flight when the pane is closed, and that finishes,
// still gets the polite hangup.
//
// Deciding from one reading of the counter treated an ordinary keystroke
// on the wire as a wedged program: no end-of-file, no drain, and output
// already sent thrown away.
func TestClosingAShellWhoseWriteLandsStillSaysGoodbye(t *testing.T) {
	s := sshtest.New(t)
	sh := startTest(t, s, nil)
	readUntil(t, sh, "READY", 5*time.Second)
	// Kept read, the way a pane does. A session whose output nobody
	// takes cannot finish: the copy into the pipe blocks and Wait never
	// comes back.
	go func() { _, _ = io.Copy(io.Discard, sh) }()

	held := &heldWriter{
		began: make(chan struct{}),
		let:   make(chan struct{}),
		shut:  make(chan struct{}),
	}
	sh.Shell.writeMu.Lock()
	held.WriteCloser = sh.Shell.stdin
	sh.Shell.stdin = held
	sh.Shell.writeMu.Unlock()

	wrote := make(chan struct{})
	go func() {
		defer close(wrote)
		_, _ = sh.Write([]byte("hello\n"))
	}()
	// In flight: past the counter and inside the write itself.
	<-held.began

	closed := make(chan error, 1)
	go func() { closed <- sh.Shell.Close() }()

	// Well inside the grace period, which is what tells a keystroke about
	// to land from a program that has stopped reading.
	time.Sleep(20 * time.Millisecond)
	close(held.let)

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the shell never came back")
	}
	<-wrote

	select {
	case <-held.shut:
	default:
		t.Fatal("stdin was never closed, so the remote shell got no end-of-file")
	}
	// And the drain happened: the remote saw the end-of-file, finished,
	// and this waited for it rather than cutting the channel.
	select {
	case <-sh.Shell.done:
	default:
		t.Error("Close came back before the remote had finished")
	}
	// And the shell is closed to its caller, whatever x/crypto would say.
	if _, err := sh.Write([]byte("late")); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Write after Close = %v, want io.ErrClosedPipe", err)
	}
}
