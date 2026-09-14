package remote

import (
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
		NoAgent:    true,
		Identities: []string{filepath.Join(t.TempDir(), "no-such-key")},
	}
}

// dialTest opens a connection to the test server and closes it when the
// test ends.
func dialTest(t *testing.T, s *sshtest.Server, mut func(*Config)) *Conn {
	t.Helper()
	cfg := testConfig(t, s)
	if mut != nil {
		mut(&cfg)
	}
	c, err := Dial(cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// startTest opens a connection and one shell on it, the one-shot way
// that -ssh uses.
func startTest(t *testing.T, s *sshtest.Server, mut func(*ShellConfig)) session.Session {
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

// One connection carries several shells at once. This is the whole point
// of keeping the connection separate from the shell: a second terminal
// on a machine must not mean a second login.
func TestConnCarriesSeveralShells(t *testing.T) {
	s := sshtest.New(t)
	c := dialTest(t, s, nil)

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

// Closing the connection closes what is riding on it. A shell left
// running on a dead transport reads nothing and never ends.
func TestConnCloseClosesItsShells(t *testing.T) {
	s := sshtest.New(t)
	c := dialTest(t, s, nil)

	sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	readUntil(t, sh, "READY", 5*time.Second)

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		b := make([]byte, 256)
		for {
			if _, err := sh.Read(b); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the shell never ended after its connection closed")
	}
}

// A shell opened on a closed connection would be held by nothing and
// closed by nothing.
func TestConnShellAfterCloseIsRefused(t *testing.T) {
	s := sshtest.New(t)
	c := dialTest(t, s, nil)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := c.Shell(ShellConfig{Cols: 80, Rows: 24}); err == nil {
		t.Fatal("Shell on a closed connection succeeded")
	}
}

// A shell that closes itself must not leave the connection holding a
// record of it, or a long-lived connection accumulates dead shells.
func TestConnForgetsAClosedShell(t *testing.T) {
	s := sshtest.New(t)
	c := dialTest(t, s, nil)

	sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	if err := sh.Close(); err != nil {
		t.Fatalf("close the shell: %v", err)
	}

	c.mu.Lock()
	n := len(c.riders)
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("the connection still holds %d riders after its only shell closed", n)
	}
}

func TestConnCloseIsIdempotentAndConcurrent(t *testing.T) {
	s := sshtest.New(t)
	c := dialTest(t, s, nil)
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

func TestConnNoHostIsAnError(t *testing.T) {
	if _, err := Dial(Config{}); err == nil {
		t.Fatal("Dial accepted an empty host")
	}
}

func TestConnRejectsABadPassword(t *testing.T) {
	s := sshtest.New(t)
	_, err := Dial(Config{
		Host: mustHost(s), Port: mustPort(s), User: "tester",
		Password:        func() (string, error) { return "wrong", nil },
		HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
		NoAgent:         true,
		Identities:      []string{filepath.Join(t.TempDir(), "none")},
	})
	if err == nil {
		t.Fatal("Dial accepted a bad password")
	}
}

// With nothing to authenticate with, the failure must say so rather than
// dialling and being refused for a reason nobody can act on.
func TestConnWithNoAuthMethodSaysSo(t *testing.T) {
	s := sshtest.New(t)
	_, err := Dial(Config{
		Host: mustHost(s), Port: mustPort(s), User: "tester",
		HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
		NoAgent:         true,
		Identities:      []string{filepath.Join(t.TempDir(), "none")},
	})
	if err == nil {
		t.Fatal("Dial connected with no authentication method")
	}
	if !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("error = %v, want it to name the missing authentication", err)
	}
}

// The one-shot form owns its connection, so closing the shell must close
// the connection too rather than leaving a login open on the server.
func TestStartShellClosesItsConnection(t *testing.T) {
	s := sshtest.New(t)
	sess, err := StartShell(testConfig(t, s), ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("StartShell: %v", err)
	}
	readUntil(t, sess, "READY", 5*time.Second)

	owned, ok := sess.(*ownedShell)
	if !ok {
		t.Fatalf("StartShell returned %T, want an owned shell", sess)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	owned.conn.mu.Lock()
	closing := owned.conn.closing
	owned.conn.mu.Unlock()
	if !closing {
		t.Fatal("closing the shell left its connection open")
	}
	// Twice, because the window can reach Close by more than one path.
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func mustHost(s *sshtest.Server) string { h, _ := s.Host(); return h }
func mustPort(s *sshtest.Server) int    { _, p := s.Host(); return p }

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
