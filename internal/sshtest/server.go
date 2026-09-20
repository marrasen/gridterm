// Package sshtest runs an SSH server inside the test process.
//
// It behaves enough like a shell to test a client against: it reports
// the pty size it was given, reports window changes, echoes what it is
// sent, and ends the session on the line "bye". Nothing here is for
// production use.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Password is the one password the server accepts.
const Password = "hunter2"

// exitOnBye is the status the server exits with when it reads "bye".
const exitOnBye = 7

// Server is a listening SSH server with a host key generated for the
// test that made it.
type Server struct {
	addr    string
	hostKey ssh.PublicKey
	cfg     *ssh.ServerConfig
	ln      net.Listener

	mu       sync.Mutex
	banner   string
	lastSize [2]int // cols, rows
	winches  int
	conns    []net.Conn
	accepted int
	live     int
	offered  []string
	onlyKey  string

	forwards  int
	forwarded []string
	asked     []string

	// agentAsked counts the sessions that asked for the SSH agent to be
	// carried here, and refuseAgent answers no the way an sshd with
	// AllowAgentForwarding off does.
	agentAsked  int
	refuseAgent bool

	// agentKeys is what the carried agent said it holds, read once the
	// request was granted, and agentErr is why it could not be read.
	agentKeys []string
	agentErr  error

	// carrying is the connection the agent was carried over, kept so a
	// test can go back to that agent and sign with it.
	carrying *ssh.ServerConn

	// sftps counts the SFTP sessions started, and noSFTP turns the
	// subsystem off the way an sshd without it does.
	sftps   int
	noSFTP  bool
	sftpErr error

	// sessions is how many session channels are open right now, so a
	// test can tell a channel that was closed from one left behind.
	sessions int
	// opened is how many session channels have ever been opened.
	opened int

	// stalling leaves a request to open a channel unanswered,
	// stallingSub leaves a request to start a subsystem unanswered,
	// stallingPty leaves a request for a pty unanswered, and deafInput
	// starts a shell that never reads what is sent to it.
	stalling    bool
	stallingSub bool
	stallingPty bool
	deafInput   bool

	// stallingForward leaves a request to listen on a port unanswered,
	// and stallingCancel a request to stop listening again.
	stallingForward bool
	stallingCancel  bool

	// frozen is closed by Freeze, and stopped when the test ends, so a
	// session held open by a frozen server lets go in the end.
	frozen     chan struct{}
	freezeOnce sync.Once
	stopped    chan struct{}
	// answer is closed by AnswerChannels, which lets stalled opens through.
	answer   chan struct{}
	answered sync.Once

	// bound are the ports the far machine has been asked to listen on.
	bound []bound

	// writeMu serialises channel writes. x/crypto documents concurrent
	// writes to one ssh.Channel as unsafe, and the request loop and the
	// echo goroutine both write.
	writeMu sync.Mutex
}

// New starts a server on a loopback port and stops it when the test ends.
func New(t testing.TB) *Server {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	s := &Server{
		hostKey: signer.PublicKey(),
		frozen:  make(chan struct{}),
		stopped: make(chan struct{}),
		answer:  make(chan struct{}),
	}
	s.cfg = &ssh.ServerConfig{
		// What a server says on the way in, which is how one that signs
		// people in through a browser sends the link. Empty unless a
		// test asks for one.
		BannerCallback: func(ssh.ConnMetadata) string {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.banner
		},
		PasswordCallback: func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == Password {
				return nil, nil
			}
			return nil, errors.New("bad password")
		},
		// Any key is accepted until a test says otherwise with Accept. A
		// test that cares which key was offered reads Offered.
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			print := ssh.FingerprintSHA256(key)
			s.mu.Lock()
			s.offered = append(s.offered, print)
			only := s.onlyKey
			s.mu.Unlock()
			if only != "" && only != print {
				return nil, errors.New("that key is not authorised")
			}
			return nil, nil
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
		close(s.stopped)
		_ = s.ln.Close()
		s.CloseClients()
	})
	return s
}

// Addr returns the host:port the server is listening on.
func (s *Server) Addr() string { return s.addr }

// HostKey returns the server's public host key.
func (s *Server) HostKey() ssh.PublicKey { return s.hostKey }

// Host returns the address split into a host and a port.
func (s *Server) Host() (string, int) {
	h, p, _ := net.SplitHostPort(s.addr)
	port, _ := strconv.Atoi(p)
	return h, port
}

// Accept narrows the server to one public key, named by its SHA256
// fingerprint, so a test can watch a client work its way through the
// keys it has until it finds the one that is authorised.
func (s *Server) Accept(fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onlyKey = fingerprint
}

// StallChannels makes the server stop answering a request to open a
// channel, without hanging up on the client.
//
// The request is neither accepted nor refused, which is what a machine
// that is still on the network but no longer answering looks like.
// Channels opened before the call carry on working until x/crypto's
// sixteen-slot buffer of incoming channels fills, after which every
// channel on the connection stops. Everything held this way is let go of
// when the test ends.
func (s *Server) StallChannels() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stalling = true
}

// StallPty makes the server take a request for a pty and never answer
// it, with the session channel left open.
//
// It is the step after StallChannels: the machine opens the session and
// stops on the round trip after it.
func (s *Server) StallPty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stallingPty = true
}

// StallForwards makes the server take a request to listen on a port for
// a client and never answer it.
func (s *Server) StallForwards() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stallingForward = true
}

// StallCancelForward makes the server take a request to stop listening
// on a port and never answer it, with the forward left in place.
func (s *Server) StallCancelForward() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stallingCancel = true
}

// StallSubsystems makes the server take a request to start a subsystem
// and never answer it, with the session channel left open.
//
// It is the step after StallChannels: the machine still opens channels
// and stops one round trip later.
func (s *Server) StallSubsystems() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stallingSub = true
}

// StopReadingInput makes the shell the server starts never read what is
// sent to it, which is what a remote program that has stopped reading
// its input does. A client writing into that shell fills the channel's
// flow-control window and then blocks.
//
// A deaf session never sends exit-status either, so it cannot end on its
// own: only closing it ends it.
func (s *Server) StopReadingInput() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deafInput = true
}

// AnswerChannels lets every stalled channel open through, the way a
// machine that comes back to life answers what it was asked while away.
func (s *Server) AnswerChannels() {
	s.mu.Lock()
	s.stalling = false
	s.mu.Unlock()
	s.answered.Do(func() { close(s.answer) })
}

// SessionsOpened returns how many session channels the server has ever
// opened, so a test can tell an open that was answered late from one
// that was never answered.
func (s *Server) SessionsOpened() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened
}

// Conns returns how many connections the server has accepted, so a test
// can tell one login from several.
func (s *Server) Conns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted
}

// Live returns how many connections the server still has open, so a
// test can tell a connection that was closed from one that was merely
// forgotten about.
func (s *Server) Live() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live
}

// Offered returns the fingerprint of every public key a client has
// offered, in order.
func (s *Server) Offered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.offered...)
}

// Sessions returns how many session channels are open right now, so a
// test can tell a channel that was closed from one left behind.
func (s *Server) Sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions
}

// Size returns the last pty size the server was told.
func (s *Server) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSize[0], s.lastSize[1]
}

// SignWithCarriedAgent signs with the first key the carried agent holds,
// which is what a second hop out of this machine does.
func (s *Server) SignWithCarriedAgent(data []byte) (*ssh.Signature, ssh.PublicKey, error) {
	s.mu.Lock()
	sc := s.carrying
	s.mu.Unlock()
	if sc == nil {
		return nil, nil, errors.New("no SSH agent has been carried here")
	}
	ch, reqs, err := sc.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open the agent channel: %w", err)
	}
	defer ch.Close()
	go ssh.DiscardRequests(reqs)

	ag := agent.NewClient(ch)
	keys, err := ag.List()
	if err != nil {
		return nil, nil, fmt.Errorf("list what the agent holds: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil, errors.New("the carried agent holds no keys")
	}
	sig, err := ag.Sign(keys[0], data)
	if err != nil {
		return nil, nil, fmt.Errorf("sign with the carried agent: %w", err)
	}
	return sig, keys[0], nil
}

// RefuseAgent makes the server turn down a request to carry the SSH
// agent here, the way an sshd with AllowAgentForwarding off does.
func (s *Server) RefuseAgent(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refuseAgent = on
}

// AgentAsked returns how many sessions asked for the SSH agent to be
// carried here.
func (s *Server) AgentAsked() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentAsked
}

// AgentKeys is what the carried agent said it holds, by comment, and why
// it could not be read.
func (s *Server) AgentKeys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.agentKeys), s.agentErr
}

// WindowChanges returns how many window-change requests the server has
// been sent, so a test can tell a size that was sent once from one sent
// for every drag.
func (s *Server) WindowChanges() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.winches
}

// CloseClients cuts every accepted connection, which is what a dropped
// network looks like from the client's side.
func (s *Server) CloseClients() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// KnownHostsLine returns a known_hosts entry for this server.
func (s *Server) KnownHostsLine() string {
	host, port := s.Host()
	return knownhosts.Line([]string{net.JoinHostPort(host, strconv.Itoa(port))}, s.hostKey)
}

// LineFor returns a known_hosts entry naming this server's address but
// holding another server's key, which is what a changed key looks like.
func (s *Server) LineFor(key ssh.PublicKey) string {
	host, port := s.Host()
	return knownhosts.Line([]string{net.JoinHostPort(host, strconv.Itoa(port))}, key)
}

// WriteKnownHosts builds a known_hosts file and returns its path.
func WriteKnownHosts(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

func (s *Server) say(ch ssh.Channel, format string, args ...any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	fmt.Fprintf(ch, format, args...)
}

func (s *Server) echoBack(ch ssh.Channel, b []byte) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = ch.Write(b)
}

func (s *Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.accepted++
		s.live++
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *Server) handle(nc net.Conn) {
	defer func() {
		s.mu.Lock()
		s.live--
		s.mu.Unlock()
	}()
	sc, chans, reqs, err := ssh.NewServerConn(nc, s.cfg)
	if err != nil {
		_ = nc.Close()
		return
	}
	defer sc.Close()
	// The global requests, which is how a client asks this machine to
	// listen on a port for it.
	go func() {
		for req := range reqs {
			s.forwardRequest(sc, req)
		}
	}()

	for nch := range chans {
		s.mu.Lock()
		stall := s.stalling
		s.mu.Unlock()
		if stall {
			// Neither accepted nor refused, and the connection is left
			// up. Held until AnswerChannels or the end of the test.
			select {
			case <-s.stopped:
				return
			case <-s.answer:
			}
		}
		switch nch.ChannelType() {
		case "session":
			ch, chReqs, err := nch.Accept()
			if err != nil {
				return
			}
			go s.session(sc, ch, chReqs)
		case "direct-tcpip":
			// A client reaching somewhere else through this machine.
			go s.forward(nch)
		default:
			_ = nch.Reject(ssh.UnknownChannelType, "sessions and forwards only")
		}
	}
}

// readCarriedAgent opens the channel back to the client's SSH agent and
// writes down what it holds, which is what proves the agent arrived.
func (s *Server) readCarriedAgent(sc *ssh.ServerConn) {
	ch, reqs, err := sc.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		s.mu.Lock()
		s.agentErr = fmt.Errorf("open the agent channel: %w", err)
		s.mu.Unlock()
		return
	}
	defer ch.Close()
	go ssh.DiscardRequests(reqs)

	keys, err := agent.NewClient(ch).List()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.carrying = sc
	if err != nil {
		s.agentErr = fmt.Errorf("list what the agent holds: %w", err)
		return
	}
	s.agentKeys, s.agentErr = nil, nil
	for _, k := range keys {
		s.agentKeys = append(s.agentKeys, k.Comment)
	}
}

func (s *Server) session(sc *ssh.ServerConn, ch ssh.Channel, reqs <-chan *ssh.Request) {
	s.mu.Lock()
	s.sessions++
	s.opened++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.sessions--
		s.mu.Unlock()
	}()
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			cols, rows := parsePtyReq(req.Payload)
			s.mu.Lock()
			s.lastSize = [2]int{cols, rows}
			stall := s.stallingPty
			s.mu.Unlock()
			if stall {
				// The channel is open and the request is neither
				// granted nor turned down.
				continue
			}
			_ = req.Reply(true, nil)

		case "auth-agent-req@openssh.com":
			s.mu.Lock()
			s.agentAsked++
			refuse := s.refuseAgent
			s.mu.Unlock()
			if refuse {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			// Read here rather than on a goroutine of its own, so a test
			// that opened a shell knows the answer is in.
			s.readCarriedAgent(sc)

		case "window-change":
			cols, rows := parseWinch(req.Payload)
			s.mu.Lock()
			s.lastSize = [2]int{cols, rows}
			s.winches++
			s.mu.Unlock()
			s.say(ch, "SIZE %dx%d\n", cols, rows)

		case "shell":
			_ = req.Reply(true, nil)
			cols, rows := s.Size()
			s.say(ch, "READY %dx%d\n", cols, rows)
			s.mu.Lock()
			deaf := s.deafInput
			s.mu.Unlock()
			// A shell that never reads its input, so a client writing
			// into it fills the flow-control window and blocks.
			if !deaf {
				go s.echo(ch)
			}

		case "subsystem":
			s.mu.Lock()
			stall := s.stallingSub
			s.mu.Unlock()
			if stall {
				// The channel is open and the request is neither
				// granted nor turned down.
				continue
			}
			// SFTP, which is a subsystem rather than a command.
			_ = req.Reply(s.sftpRequest(ch, req), nil)

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
func (s *Server) echo(ch ssh.Channel) {
	buf := make([]byte, 256)
	var line []byte
	for {
		n, err := ch.Read(buf)
		if n > 0 {
			line = append(line, buf[:n]...)
			s.echoBack(ch, buf[:n])
			// A command that floods the terminal, for measuring how
			// fast a client can read one.
			if strings.Contains(string(line), "flood\n") {
				line = nil
				s.flood(ch)
				continue
			}
			if strings.Contains(string(line), "bye\n") {
				_, _ = ch.SendRequest("exit-status", false, exitStatus(exitOnBye))
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
	return []byte{byte(code >> 24), byte(code >> 16), byte(code >> 8), byte(code)}
}

// parsePtyReq pulls the dimensions out of an RFC 4254 pty-req payload: a
// string TERM, then width, height, pixel width, pixel height.
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

// SayOnTheWayIn makes the server send this to whoever connects, the way
// one that signs people in through a browser sends a link.
func (s *Server) SayOnTheWayIn(what string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.banner = what
}

// FloodSize is how much a "flood" command writes.
const FloodSize = 4 << 20

// flood writes FloodSize bytes of ordinary output as fast as the client
// will take it, which is what a find(1) over a big tree looks like.
func (s *Server) flood(ch ssh.Channel) {
	const line = "/usr/lib/x86_64-linux-gnu/perl-base/unicore/lib/Gc/Cntrl.pl\r\n"
	block := strings.Repeat(line, 1024)
	for sent := 0; sent < FloodSize; sent += len(block) {
		if _, err := ch.Write([]byte(block)); err != nil {
			return
		}
	}
}
