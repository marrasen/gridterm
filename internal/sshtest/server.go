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
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
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
	lastSize [2]int // cols, rows
	conns    []net.Conn
	accepted int
	live     int
	offered  []string
	onlyKey  string

	forwards  int
	forwarded []string
	asked     []string

	// sftps counts the SFTP sessions started, and noSFTP turns the
	// subsystem off the way an sshd without it does.
	sftps  int
	noSFTP bool

	// bound are the ports the far machine has been asked to listen on.
	bound []bound

	// writeMu serialises channel writes. x/crypto documents concurrent
	// writes to one ssh.Channel as unsafe, and the request loop and the
	// echo goroutine both write.
	writeMu sync.Mutex
}

// New starts a server on a loopback port and stops it when the test ends.
func New(t *testing.T) *Server {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	s := &Server{hostKey: signer.PublicKey()}
	s.cfg = &ssh.ServerConfig{
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

// Size returns the last pty size the server was told.
func (s *Server) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSize[0], s.lastSize[1]
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
		switch nch.ChannelType() {
		case "session":
			ch, chReqs, err := nch.Accept()
			if err != nil {
				return
			}
			go s.session(ch, chReqs)
		case "direct-tcpip":
			// A client reaching somewhere else through this machine.
			go s.forward(nch)
		default:
			_ = nch.Reject(ssh.UnknownChannelType, "sessions and forwards only")
		}
	}
}

func (s *Server) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
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
			cols, rows := s.Size()
			s.say(ch, "READY %dx%d\n", cols, rows)
			go s.echo(ch)

		case "subsystem":
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
