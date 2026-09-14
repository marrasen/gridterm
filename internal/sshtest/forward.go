package sshtest

import (
	"io"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
)

// forward answers a direct-tcpip channel by connecting to what it asked
// for and copying both ways.
//
// It is what a client does when it reaches one machine through another,
// and what a local port forward does. The server keeps a count so a test
// can tell how many streams went through it.
func (s *Server) forward(nch ssh.NewChannel) {
	addr, ok := forwardTarget(nch.ExtraData())
	if !ok {
		_ = nch.Reject(ssh.ConnectionFailed, "that is not a direct-tcpip request")
		return
	}

	// Recorded before anything is tried, so a test can tell an address
	// this machine was asked for from one it managed to reach.
	s.mu.Lock()
	s.asked = append(s.asked, addr)
	s.mu.Unlock()

	far, err := net.Dial("tcp", addr)
	if err != nil {
		_ = nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		_ = far.Close()
		return
	}

	s.mu.Lock()
	s.forwards++
	s.forwarded = append(s.forwarded, addr)
	s.mu.Unlock()

	go ssh.DiscardRequests(reqs)
	var once sync.Once
	shut := func() {
		once.Do(func() {
			_ = ch.Close()
			_ = far.Close()
		})
	}
	go func() {
		defer shut()
		_, _ = io.Copy(far, ch)
	}()
	go func() {
		defer shut()
		_, _ = io.Copy(ch, far)
	}()
}

// Forwards returns how many streams the server has carried to somewhere
// else, so a test can tell a hop from a direct connection.
func (s *Server) Forwards() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forwards
}

// Asked returns every address the server was asked to reach, whether or
// not it could, so a test can tell what a client sent from what worked.
func (s *Server) Asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// Forwarded returns the addresses the server reached.
func (s *Server) Forwarded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.forwarded...)
}

// forwardTarget reads the address out of a direct-tcpip request: a host
// string and a port, then where it came from, which is not used here.
func forwardTarget(payload []byte) (string, bool) {
	host, rest, ok := sshString(payload)
	if !ok || len(rest) < 4 {
		return "", false
	}
	port := be32(rest)
	if port > 65535 {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), true
}

// sshString reads a length-prefixed string off the front of a payload.
func sshString(b []byte) (string, []byte, bool) {
	if len(b) < 4 {
		return "", nil, false
	}
	n := int(be32(b))
	if n < 0 || 4+n > len(b) {
		return "", nil, false
	}
	return string(b[4 : 4+n]), b[4+n:], true
}
