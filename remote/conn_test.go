package remote

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/session"
)

// A remote shell has to be usable wherever a local one is, or the
// terminal above it would have to know which it had.
var _ session.Session = (*Shell)(nil)
var _ session.Session = (*OwnedShell)(nil)

// testConfig connects to the test server with host key checking pinned
// to its real key.
func testConfig(t *testing.T, s *sshtest.Server) Config {
	t.Helper()
	host, port := s.Host()
	return Config{
		Host: host, Port: port, User: "tester",
		Password:        func() (string, error) { return sshtest.Password, nil },
		HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
		// Without these the tests offer whatever keys the developer
		// happens to have, so they exercise a different authentication
		// path on a laptop than on a build machine — and a smartcard
		// agent would make them hang.
		NoAgent:      true,
		NoIdentities: true,
	}
}

// connectTest opens a connection to the test server and closes it when
// the test ends.
func connectTest(t *testing.T, s *sshtest.Server) *Conn {
	t.Helper()
	c, err := Connect(testConfig(t, s))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// startTest opens a connection and one shell on it, the one-shot way
// that -ssh uses.
func startTest(t *testing.T, s *sshtest.Server, mut func(*ShellConfig)) *OwnedShell {
	t.Helper()
	sh := ShellConfig{Cols: 80, Rows: 24}
	if mut != nil {
		mut(&sh)
	}
	sess, err := StartShell(testConfig(t, s), sh)
	if err != nil {
		t.Fatalf("StartShell: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// testRider stands in for a tunnel or a file transfer: something the
// connection has to close that is not a shell.
type testRider struct {
	mu     sync.Mutex
	closed int
	delay  time.Duration
}

func (r *testRider) closeRider() error {
	time.Sleep(r.delay)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
	return nil
}

func (r *testRider) closes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// One connection carries several shells at once. This is what the split
// is for: a second terminal on a machine must not mean a second login.
func TestConnCarriesSeveralShells(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	first, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("first Shell: %v", err)
	}
	if got := readUntil(t, first, "READY", 5*time.Second); !strings.Contains(got, "READY") {
		t.Fatalf("first shell said %q", strings.TrimSpace(got))
	}

	second, err := c.Shell(ShellConfig{Cols: 100, Rows: 40})
	if err != nil {
		t.Fatalf("second Shell: %v", err)
	}
	if got := readUntil(t, second, "READY", 5*time.Second); !strings.Contains(got, "READY") {
		t.Fatalf("second shell said %q", strings.TrimSpace(got))
	}

	// The point of the whole exercise: one connection on the wire.
	if n := s.Conns(); n != 1 {
		t.Fatalf("the server accepted %d connections for two shells, want 1", n)
	}

	// Closing one leaves the other working.
	if err := first.Close(); err != nil {
		t.Fatalf("close the first shell: %v", err)
	}
	if _, err := second.Write([]byte("ping\n")); err != nil {
		t.Fatalf("the second shell died with the first: %v", err)
	}
	if got := readUntil(t, second, "ping", 5*time.Second); !strings.Contains(got, "ping") {
		t.Fatalf("second shell echoed %q", strings.TrimSpace(got))
	}
}

// Closing the connection closes what is riding on it. The rider here is
// not a shell, so the test cannot pass on x/crypto tearing the transport
// down by itself.
func TestConnCloseClosesItsRiders(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	r := &testRider{}
	if err := c.register(r); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := r.closes(); got != 1 {
		t.Fatalf("the rider was closed %d times, want 1", got)
	}
}

// Several riders close at once. One at a time, each waiting out its own
// drain period, makes closing a connection with four panes take four
// times as long as closing one.
func TestConnCloseClosesRidersAtOnce(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	const delay = 200 * time.Millisecond
	riders := make([]*testRider, 4)
	for i := range riders {
		riders[i] = &testRider{delay: delay}
		if err := c.register(riders[i]); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	took := time.Since(start)

	for i, r := range riders {
		if got := r.closes(); got != 1 {
			t.Errorf("rider %d was closed %d times, want 1", i, got)
		}
	}
	// Generous: the point is that it is not four times the delay.
	if took > 2*delay {
		t.Fatalf("closing 4 riders took %v, want about %v; they closed one after another",
			took, delay)
	}
}

// A shell riding on a connection goes when the connection does.
func TestConnCloseClosesItsShells(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	readUntil(t, sh, "READY", 5*time.Second)

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := sh.Write([]byte("ping\n")); err == nil {
		t.Fatal("the shell still took input after its connection closed")
	}
}

// A shell opened on a closed connection would be held by nothing and
// closed by nothing. The error has to be ErrClosed and not whatever the
// dead transport happened to report, or nothing can tell the two apart.
func TestConnShellAfterCloseIsRefused(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err == nil {
		t.Fatal("Shell on a closed connection succeeded")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Shell on a closed connection = %v, want ErrClosed", err)
	}
}

// A shell that closes itself must not leave the connection holding a
// record of it, or a long-lived connection accumulates dead shells.
func TestConnForgetsAClosedShell(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	if err := sh.Close(); err != nil {
		t.Fatalf("close the shell: %v", err)
	}
	if n := c.riderCount(); n != 0 {
		t.Fatalf("the connection still holds %d riders after its only shell closed", n)
	}
}

// The same when the remote ends the program itself. Nothing calls Close
// in that case, so without the shell letting go the record would stay
// for as long as the connection lives.
func TestConnForgetsAShellWhoseRemoteExited(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	readUntil(t, sh, "READY", 5*time.Second)
	// "bye" makes the test server end the session on its own.
	if _, err := sh.Write([]byte("bye\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	drain(sh)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.riderCount() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the connection still holds %d riders after the remote exited", c.riderCount())
}

func TestConnCloseIsIdempotentAndConcurrent(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)
	if _, err := c.Shell(ShellConfig{Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("Shell: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Close(); err != nil {
				t.Errorf("concurrent Close: %v", err)
			}
		}()
	}
	wg.Wait()
}

// The second caller has to wait for the first. Reporting success while
// the connection is still being torn down would let a caller open the
// next one against a server that still holds this session.
func TestConnSecondCloseWaitsForTheFirst(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	const delay = 200 * time.Millisecond
	r := &testRider{delay: delay}
	if err := c.register(r); err != nil {
		t.Fatalf("register: %v", err)
	}

	started := make(chan struct{})
	go func() {
		close(started)
		_ = c.Close()
	}()
	<-started
	// Long enough to be inside the first Close, short enough to be well
	// before it finishes.
	time.Sleep(delay / 4)

	_ = c.Close()
	if got := r.closes(); got != 1 {
		t.Fatalf("the second Close returned with the rider closed %d times, want 1", got)
	}
}

// Opening a shell and closing the connection at the same time is what a
// window does when a pane is created as the user quits.
func TestConnShellRacingCloseIsSafe(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
		if err == nil {
			_ = sh.Close()
		}
	}()
	go func() {
		defer wg.Done()
		_ = c.Close()
	}()
	wg.Wait()
}

func TestConnectNoHostIsAnError(t *testing.T) {
	if _, err := Connect(Config{}); err == nil {
		t.Fatal("Connect accepted an empty host")
	}
}

func TestConnectRejectsABadPassword(t *testing.T) {
	s := sshtest.New(t)
	cfg := testConfig(t, s)
	cfg.Password = func() (string, error) { return "wrong", nil }
	if _, err := Connect(cfg); err == nil {
		t.Fatal("Connect accepted a bad password")
	}
}

// With nothing to authenticate with, the failure must say so rather than
// dialling and being refused for a reason nobody can act on.
func TestConnectWithNothingToAuthenticateWithSaysSo(t *testing.T) {
	s := sshtest.New(t)
	cfg := testConfig(t, s)
	cfg.Password = nil
	_, err := Connect(cfg)
	if err == nil {
		t.Fatal("Connect connected with no authentication method")
	}
	if !strings.Contains(err.Error(), "authenticate") {
		t.Fatalf("error = %v, want it to name the missing authentication", err)
	}
}

// A key the caller named is one it asked for, so failing to read it
// fails the connection instead of being skipped in silence.
func TestConnectReportsANamedKeyItCannotRead(t *testing.T) {
	s := sshtest.New(t)
	cfg := testConfig(t, s)
	missing := filepath.Join(t.TempDir(), "id_nowhere")
	cfg.NoIdentities = false
	cfg.Identities = []string{missing}

	_, err := Connect(cfg)
	if err == nil {
		t.Fatal("Connect ignored a private key it was told to use")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error = %v, want it to name the key file", err)
	}
}

// The one-shot form owns its connection, so closing the shell must close
// the connection too rather than leaving a login open on the server.
func TestStartShellClosesItsConnection(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)
	readUntil(t, sess, "READY", 5*time.Second)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Asking the connection for another shell is the observable test of
	// whether it went away.
	if _, err := sess.Conn().Shell(ShellConfig{Cols: 80, Rows: 24}); !errors.Is(err, ErrClosed) {
		t.Fatalf("the connection outlived the shell it carried: %v", err)
	}
	// Twice, because the window can reach Close by more than one path.
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// readUntil reads output until it contains want, or the timeout passes.
func readUntil(t *testing.T, r io.Reader, want string, timeout time.Duration) string {
	t.Helper()
	var (
		mu   sync.Mutex
		sb   strings.Builder
		done = make(chan struct{})
	)
	go func() {
		defer close(done)
		b := make([]byte, 1024)
		for {
			n, err := r.Read(b)
			if n > 0 {
				mu.Lock()
				sb.Write(b[:n])
				hit := strings.Contains(sb.String(), want)
				mu.Unlock()
				if hit {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
	mu.Lock()
	defer mu.Unlock()
	return sb.String()
}

// drain reads and discards output so a test that is not asserting on it
// cannot stall the remote.
func drain(r io.Reader) {
	go func() { _, _ = io.Copy(io.Discard, r) }()
}
