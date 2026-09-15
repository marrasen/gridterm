package remote

import (
	"context"
	"errors"
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

	a.holdTheAgentTo(50*time.Millisecond, "to say what keys it holds")
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

	a.holdTheAgentTo(50*time.Millisecond, "to sign")
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
	a.holdTheAgentTo(50*time.Millisecond, "to sign")

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
	if !strings.Contains(account, "not asking the SSH agent again") {
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
