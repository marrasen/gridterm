package session

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// testServer is a minimal SSH server that behaves enough like a shell to
// test a client against: it reports the pty size it was given, reports
// window changes, and echoes what it is sent.
type testServer struct {
	addr    string
	hostKey ssh.PublicKey
	cfg     *ssh.ServerConfig
	ln      net.Listener

	mu       sync.Mutex
	lastSize [2]int // cols, rows

	conns []net.Conn

	// writeMu serialises channel writes. x/crypto documents concurrent
	// writes to one ssh.Channel as unsafe, and the request loop and the
	// echo goroutine both write.
	writeMu sync.Mutex
}

func (s *testServer) say(ch ssh.Channel, format string, args ...any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	fmt.Fprintf(ch, format, args...)
}

func (s *testServer) echoBack(ch ssh.Channel, b []byte) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = ch.Write(b)
}

const testPassword = "hunter2"

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	s := &testServer{hostKey: signer.PublicKey()}
	s.cfg = &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == testPassword {
				return nil, nil
			}
			return nil, errors.New("bad password")
		},
	}
	s.cfg.AddHostKey(signer)

	s.ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.addr = s.ln.Addr().String()

	go s.serve()
	t.Cleanup(func() {
		_ = s.ln.Close()
		s.closeClients()
	})
	return s
}

// closeClients cuts every accepted connection, which is what a dropped
// network looks like from the client's side.
func (s *testServer) closeClients() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (s *testServer) host() (string, int) {
	h, p, _ := net.SplitHostPort(s.addr)
	var port int
	fmt.Sscanf(p, "%d", &port)
	return h, port
}

func (s *testServer) size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSize[0], s.lastSize[1]
}

func (s *testServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *testServer) handle(nc net.Conn) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, s.cfg)
	if err != nil {
		_ = nc.Close()
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)

	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			return
		}
		go s.session(ch, chReqs)
	}
}

func (s *testServer) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			cols, rows := parsePtyReq(req.Payload)
			s.mu.Lock()
			s.lastSize = [2]int{cols, rows}
			s.mu.Unlock()
			_ = req.Reply(true, nil)

		case "window-change":
			cols, rows := parseWinch(req.Payload)
			s.mu.Lock()
			s.lastSize = [2]int{cols, rows}
			s.mu.Unlock()
			s.say(ch, "SIZE %dx%d\n", cols, rows)

		case "shell":
			_ = req.Reply(true, nil)
			cols, rows := s.size()
			s.say(ch, "READY %dx%d\n", cols, rows)
			go s.echo(ch)

		case "exec":
			_ = req.Reply(true, nil)
			cmd := string(req.Payload[4:])
			s.say(ch, "RAN %s\n", cmd)
			_, _ = ch.SendRequest("exit-status", false, exitStatus(0))
			return

		default:
			_ = req.Reply(false, nil)
		}
	}
}

// echo mirrors input back, and treats the line "bye" as a request to end
// the session with a non-zero status.
func (s *testServer) echo(ch ssh.Channel) {
	buf := make([]byte, 256)
	var line []byte
	for {
		n, err := ch.Read(buf)
		if n > 0 {
			line = append(line, buf[:n]...)
			s.echoBack(ch, buf[:n])
			if idx := strings.Index(string(line), "bye\n"); idx >= 0 {
				_, _ = ch.SendRequest("exit-status", false, exitStatus(7))
				_ = ch.Close()
				return
			}
		}
		if err != nil {
			_, _ = ch.SendRequest("exit-status", false, exitStatus(0))
			_ = ch.Close()
			return
		}
	}
}

func exitStatus(code uint32) []byte {
	return []byte{
		byte(code >> 24), byte(code >> 16), byte(code >> 8), byte(code),
	}
}

// parsePtyReq pulls the dimensions out of an RFC 4254 pty-req payload:
// a string TERM, then width, height, pixel width, pixel height.
func parsePtyReq(p []byte) (cols, rows int) {
	if len(p) < 4 {
		return 0, 0
	}
	termLen := int(be32(p))
	if termLen < 0 || 4+termLen > len(p) {
		return 0, 0
	}
	rest := p[4+termLen:]
	if len(rest) < 8 {
		return 0, 0
	}
	return int(be32(rest)), int(be32(rest[4:]))
}

// parseWinch reads a window-change payload: width, height, then pixels.
func parseWinch(p []byte) (cols, rows int) {
	if len(p) < 8 {
		return 0, 0
	}
	return int(be32(p)), int(be32(p[4:]))
}

func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// dialTest connects to the test server with host key checking pinned to
// its real key.
func dialTest(t *testing.T, s *testServer, mut func(*SSHConfig)) Session {
	t.Helper()
	host, port := s.host()
	cfg := SSHConfig{
		Host: host, Port: port, User: "tester",
		Cols: 80, Rows: 24,
		Password:        func() (string, error) { return testPassword, nil },
		HostKeyCallback: ssh.FixedHostKey(s.hostKey),
		// Without this the tests offer whatever keys the developer
		// happens to have, so they exercise a different authentication
		// path on a laptop than on a build machine — and a smartcard
		// agent would make them hang.
		NoAgent:    true,
		Identities: []string{filepath.Join(t.TempDir(), "no-such-key")},
	}
	if mut != nil {
		mut(&cfg)
	}
	sess, err := StartSSH(cfg)
	if err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func TestSSHStartsAShell(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)

	got := readUntilRemote(t, sess, "READY", 5*time.Second)
	if !strings.Contains(got, "READY 80x24") {
		t.Fatalf("greeting = %q, want READY 80x24", strings.TrimSpace(got))
	}
}

// The pty has to be requested with the right size before the shell
// starts, or the remote lays out its first prompt for the wrong width.
func TestSSHRequestsThePtyWithTheInitialSize(t *testing.T) {
	s := newTestServer(t)
	dialTest(t, s, func(c *SSHConfig) { c.Cols, c.Rows = 133, 42 })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cols, rows := s.size(); cols == 133 && rows == 42 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cols, rows := s.size()
	t.Fatalf("server saw %dx%d, want 133x42", cols, rows)
}

func TestSSHResizeSendsWindowChange(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)
	readUntilRemote(t, sess, "READY", 5*time.Second)

	if err := sess.Resize(101, 37); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	got := readUntilRemote(t, sess, "SIZE", 5*time.Second)
	if !strings.Contains(got, "SIZE 101x37") {
		t.Fatalf("server reported %q, want SIZE 101x37", strings.TrimSpace(got))
	}
}

func TestSSHResizeWithNonsenseSizeIsIgnored(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)
	readUntilRemote(t, sess, "READY", 5*time.Second)

	if err := sess.Resize(0, 0); err != nil {
		t.Errorf("Resize(0,0) = %v, want nil", err)
	}
	if cols, rows := s.size(); cols != 80 || rows != 24 {
		t.Fatalf("server saw %dx%d after a nonsense resize, want the original 80x24",
			cols, rows)
	}
}

func TestSSHWriteReachesTheRemote(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)
	readUntilRemote(t, sess, "READY", 5*time.Second)

	if _, err := sess.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := readUntilRemote(t, sess, "ping", 5*time.Second)
	if !strings.Contains(got, "ping") {
		t.Fatalf("echo = %q", got)
	}
}

// A remote shell exiting non-zero is an ordinary end of session, not a
// read error: the reader must see io.EOF.
func TestSSHReadReturnsEOFWhenTheRemoteExits(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)
	readUntilRemote(t, sess, "READY", 5*time.Second)

	if _, err := sess.Write([]byte("bye\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		b := make([]byte, 256)
		for {
			if _, err := sess.Read(b); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("Read returned %v, want io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read never returned after the remote exited")
	}
}

func TestSSHRunsACommand(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, func(c *SSHConfig) {
		c.Command = []string{"echo", "hello world"}
	})

	got := readUntilRemote(t, sess, "RAN", 5*time.Second)
	if !strings.Contains(got, "RAN 'echo' 'hello world'") {
		t.Fatalf("server ran %q, want the quoted argv", strings.TrimSpace(got))
	}
}

// A dropped connection is not a clean logout. Reporting it as one hides
// the difference between closing a window and losing the network.
func TestSSHDroppedConnectionIsNotReportedAsACleanExit(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)
	readUntilRemote(t, sess, "READY", 5*time.Second)

	// Cut the connection underneath the session.
	s.closeClients()

	done := make(chan error, 1)
	go func() {
		b := make([]byte, 256)
		for {
			if _, err := sess.Read(b); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err == nil || err == io.EOF {
			t.Fatalf("Read returned %v for a dropped connection, want a real error", err)
		}
		if !strings.Contains(err.Error(), "closed by the remote host") {
			t.Fatalf("Read returned %v, want it to say the connection was closed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read never returned after the connection dropped")
	}
}

// The host key must be checked. A terminal that silently trusts an
// unknown key can be man-in-the-middled without anyone noticing.
func TestSSHRejectsTheWrongHostKey(t *testing.T) {
	s := newTestServer(t)
	other := newTestServer(t)
	host, port := s.host()

	_, err := StartSSH(SSHConfig{
		Host: host, Port: port, User: "tester",
		Cols: 80, Rows: 24,
		Password:        func() (string, error) { return testPassword, nil },
		HostKeyCallback: ssh.FixedHostKey(other.hostKey),
	})
	if err == nil {
		t.Fatal("StartSSH accepted a connection with the wrong host key")
	}
}

// With no known_hosts file and no override, the connection must fail
// rather than fall back to trusting anything.
func TestSSHWithoutKnownHostsRefusesToConnect(t *testing.T) {
	s := newTestServer(t)
	host, port := s.host()

	_, err := StartSSH(SSHConfig{
		Host: host, Port: port, User: "tester",
		Cols: 80, Rows: 24,
		KnownHosts: []string{"/nonexistent/known_hosts"},
		Password:   func() (string, error) { return testPassword, nil },
	})
	if err == nil {
		t.Fatal("StartSSH connected with no known_hosts to check against")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("error = %v, want it to mention known_hosts", err)
	}
}

func TestSSHRejectsABadPassword(t *testing.T) {
	s := newTestServer(t)
	host, port := s.host()

	_, err := StartSSH(SSHConfig{
		Host: host, Port: port, User: "tester",
		Cols: 80, Rows: 24,
		Password:        func() (string, error) { return "wrong", nil },
		HostKeyCallback: ssh.FixedHostKey(s.hostKey),
	})
	if err == nil {
		t.Fatal("StartSSH accepted a bad password")
	}
}

// The remote exits 7, which must still read as an ordinary end of
// session rather than an error.
func TestSSHWaitReportsTheRemoteExitStatus(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)
	readUntilRemote(t, sess, "READY", 5*time.Second)
	_, _ = sess.Write([]byte("bye\n"))
	drainRemote(t, sess)

	err := sess.Wait()
	var exit *ssh.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Wait returned %v (%T), want an *ssh.ExitError", err, err)
	}
	if got := exit.ExitStatus(); got != 7 {
		t.Fatalf("exit status = %d, want 7", got)
	}
	if second := sess.Wait(); !sameErr(err, second) {
		t.Fatalf("second Wait = %v, first = %v", second, err)
	}
}

func TestSSHCloseIsIdempotentAndConcurrent(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)
	if err := sess.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sess.Close(); err != nil {
				t.Errorf("concurrent Close: %v", err)
			}
		}()
	}
	wg.Wait()
}

// Closing the window with a keystroke still in flight used to race:
// closing the session stdin writes a flag that Write reads.
func TestSSHCloseRacingWriteIsSafe(t *testing.T) {
	s := newTestServer(t)
	sess := dialTest(t, s, nil)
	readUntilRemote(t, sess, "READY", 5*time.Second)
	drainRemote(t, sess)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if _, err := sess.Write([]byte("x")); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		_ = sess.Close()
	}()
	wg.Wait()
}

func TestSSHNoHostIsAnError(t *testing.T) {
	if _, err := StartSSH(SSHConfig{}); err == nil {
		t.Fatal("StartSSH accepted an empty host")
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"ls"}, `'ls'`},
		{[]string{"echo", "a b"}, `'echo' 'a b'`},
		{[]string{"echo", "it's"}, `'echo' 'it'\''s'`},
		{[]string{"echo", "$HOME"}, `'echo' '$HOME'`},
		{nil, ``},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.argv); got != tc.want {
			t.Errorf("shellQuote(%q) = %s, want %s", tc.argv, got, tc.want)
		}
	}
}

// readUntilRemote is readUntil for a Session with no local pty, kept
// separate so the local tests can stay Unix-only.
func readUntilRemote(t *testing.T, s Session, want string, timeout time.Duration) string {
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
			n, err := s.Read(b)
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

// drainRemote reads and discards output so a test that is not asserting
// on it cannot stall the remote.
func drainRemote(t *testing.T, s Session) {
	t.Helper()
	go func() { _, _ = io.Copy(io.Discard, s) }()
}

func sameErr(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Error() == b.Error()
}

// writeKnownHosts builds a known_hosts file and returns its path.
func writeKnownHosts(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

func knownHostsLine(s *testServer) string {
	host, port := s.host()
	return knownhosts.Line([]string{net.JoinHostPort(host, strconv.Itoa(port))}, s.hostKey)
}

// The real known_hosts path, rather than the FixedHostKey the other
// tests pin. Everything about host key checking lives in this function.
func TestSSHAcceptsAHostInKnownHosts(t *testing.T) {
	s := newTestServer(t)
	host, port := s.host()

	sess, err := StartSSH(SSHConfig{
		Host: host, Port: port, User: "tester", Cols: 80, Rows: 24,
		KnownHosts: []string{writeKnownHosts(t, knownHostsLine(s))},
		Password:   func() (string, error) { return testPassword, nil },
		NoAgent:    true,
		Identities: []string{filepath.Join(t.TempDir(), "none")},
	})
	if err != nil {
		t.Fatalf("StartSSH with a matching known_hosts entry: %v", err)
	}
	_ = sess.Close()
}

// An unknown host and a changed key are different things and must say
// so. Reporting an unknown host as "key mismatch" trains the user to
// ignore the one message that matters.
func TestSSHUnknownHostSaysSo(t *testing.T) {
	s := newTestServer(t)
	other := newTestServer(t)
	host, port := s.host()

	_, err := StartSSH(SSHConfig{
		Host: host, Port: port, User: "tester", Cols: 80, Rows: 24,
		// A file with an entry for a different host.
		KnownHosts: []string{writeKnownHosts(t, knownHostsLine(other))},
		Password:   func() (string, error) { return testPassword, nil },
		NoAgent:    true,
		Identities: []string{filepath.Join(t.TempDir(), "none")},
	})
	if err == nil {
		t.Fatal("StartSSH accepted a host that is not in known_hosts")
	}
	if !strings.Contains(err.Error(), "not in known_hosts") {
		t.Fatalf("error = %v, want it to say the host is not in known_hosts", err)
	}
}

func TestSSHChangedHostKeySaysSo(t *testing.T) {
	s := newTestServer(t)
	other := newTestServer(t)
	host, port := s.host()

	// An entry for this address, but holding a different key.
	otherHost, otherPort := other.host()
	line := knownhosts.Line(
		[]string{net.JoinHostPort(host, strconv.Itoa(port))}, other.hostKey)
	_ = otherHost
	_ = otherPort

	_, err := StartSSH(SSHConfig{
		Host: host, Port: port, User: "tester", Cols: 80, Rows: 24,
		KnownHosts: []string{writeKnownHosts(t, line)},
		Password:   func() (string, error) { return testPassword, nil },
		NoAgent:    true,
		Identities: []string{filepath.Join(t.TempDir(), "none")},
	})
	if err == nil {
		t.Fatal("StartSSH accepted a host whose key had changed")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want it to say the key does not match", err)
	}
	if !strings.Contains(err.Error(), "man-in-the-middle") {
		t.Fatalf("error = %v, want it to name the risk", err)
	}
}

// OpenSSH skips a line it cannot parse. knownhosts.New rejects the whole
// file, so one truncated entry — or one written by a newer OpenSSH with
// a key type this version does not know — would otherwise disable SSH
// for every host.
func TestSSHSkipsUnparseableKnownHostsLines(t *testing.T) {
	s := newTestServer(t)
	host, port := s.host()

	kh := writeKnownHosts(t,
		"garbage-line-with-no-key",
		"somehost ssh-ed25519 not-base64!!",
		"somehost ssh-brandnew AAAA",
		"# a comment",
		knownHostsLine(s),
	)

	sess, err := StartSSH(SSHConfig{
		Host: host, Port: port, User: "tester", Cols: 80, Rows: 24,
		KnownHosts: []string{kh},
		Password:   func() (string, error) { return testPassword, nil },
		NoAgent:    true,
		Identities: []string{filepath.Join(t.TempDir(), "none")},
	})
	if err != nil {
		t.Fatalf("StartSSH: %v; one bad line disabled the whole file", err)
	}
	_ = sess.Close()
}

func TestSSHKnownHostsWithOnlyBadLinesIsAnError(t *testing.T) {
	s := newTestServer(t)
	host, port := s.host()

	_, err := StartSSH(SSHConfig{
		Host: host, Port: port, User: "tester", Cols: 80, Rows: 24,
		KnownHosts: []string{writeKnownHosts(t, "garbage", "more garbage")},
		Password:   func() (string, error) { return testPassword, nil },
		NoAgent:    true,
		Identities: []string{filepath.Join(t.TempDir(), "none")},
	})
	if err == nil {
		t.Fatal("StartSSH connected with no usable known_hosts entries")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("error = %v, want it to mention known_hosts", err)
	}
}

func TestSessionEnd(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantEOF bool
	}{
		{"clean exit", nil, true},
		{"non-zero exit", &ssh.ExitError{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionEnd(tc.err)
			if (got == io.EOF) != tc.wantEOF {
				t.Errorf("sessionEnd(%v) = %v, want EOF=%v", tc.err, got, tc.wantEOF)
			}
		})
	}

	// A session that ends with no exit status is a dropped connection,
	// not a logout.
	got := sessionEnd(&ssh.ExitMissingError{})
	if got == io.EOF || got == nil {
		t.Fatalf("sessionEnd(ExitMissingError) = %v, want a real error", got)
	}
}
