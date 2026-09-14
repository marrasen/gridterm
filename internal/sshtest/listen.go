package sshtest

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// bound is a port the far machine has been asked to listen on.
type bound struct {
	conn ssh.Conn
	host string
	port uint32
}

// Listening returns the addresses the server has been asked to listen on
// for a client, in the order it was asked.
func (s *Server) Listening() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.bound))
	for _, b := range s.bound {
		out = append(out, net.JoinHostPort(b.host, strconv.Itoa(int(b.port))))
	}
	return out
}

// ConnectTo is something on the far machine connecting to a port the
// client asked it to listen on.
//
// It opens the forwarded-tcpip channel a real sshd would open, and hands
// back the far end of it as a connection to read and write.
func (s *Server) ConnectTo(addr string) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	var at *bound
	for i, b := range s.bound {
		if b.port == uint32(port) && (b.host == host || host == "") {
			at = &s.bound[i]
			break
		}
	}
	s.mu.Unlock()
	if at == nil {
		return nil, fmt.Errorf("sshtest: nothing is listening on %s: %v", addr, s.Listening())
	}

	ch, reqs, err := at.conn.OpenChannel("forwarded-tcpip", ssh.Marshal(struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}{at.host, at.port, "127.0.0.1", 40000}))
	if err != nil {
		return nil, fmt.Errorf("sshtest: open a forwarded channel: %w", err)
	}
	go ssh.DiscardRequests(reqs)
	return &chanConn{Channel: ch}, nil
}

// forwardRequest handles the global requests that ask the far machine to
// listen on a port and to stop again.
func (s *Server) forwardRequest(conn ssh.Conn, req *ssh.Request) {
	switch req.Type {
	case "tcpip-forward":
		var ask struct {
			Host string
			Port uint32
		}
		if err := ssh.Unmarshal(req.Payload, &ask); err != nil {
			_ = req.Reply(false, nil)
			return
		}
		port := ask.Port
		if port == 0 {
			// A real sshd picks one. Any number will do here, as long as
			// it is not one already bound.
			port = s.freePort()
		}
		s.mu.Lock()
		s.bound = append(s.bound, bound{conn: conn, host: ask.Host, port: port})
		s.mu.Unlock()
		// The reply carries the port when one was asked for, which is how
		// the client learns where it ended up.
		if ask.Port == 0 {
			_ = req.Reply(true, ssh.Marshal(struct{ Port uint32 }{port}))
			return
		}
		_ = req.Reply(true, nil)

	case "cancel-tcpip-forward":
		var ask struct {
			Host string
			Port uint32
		}
		if err := ssh.Unmarshal(req.Payload, &ask); err != nil {
			_ = req.Reply(false, nil)
			return
		}
		s.mu.Lock()
		kept := s.bound[:0]
		for _, b := range s.bound {
			if b.conn == conn && b.port == ask.Port {
				continue
			}
			kept = append(kept, b)
		}
		s.bound = kept
		s.mu.Unlock()
		_ = req.Reply(true, nil)

	default:
		_ = req.Reply(false, nil)
	}
}

// freePort returns a port number nothing is bound to. The caller holds
// no lock.
func (s *Server) freePort() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	port := uint32(30000 + len(s.bound))
	for {
		taken := false
		for _, b := range s.bound {
			if b.port == port {
				taken = true
				break
			}
		}
		if !taken {
			return port
		}
		port++
	}
}

// chanConn is an SSH channel as a net.Conn, which is what a forwarded
// connection looks like to whatever is using it.
type chanConn struct {
	ssh.Channel
	once sync.Once
}

func (c *chanConn) LocalAddr() net.Addr  { return fakeAddr{} }
func (c *chanConn) RemoteAddr() net.Addr { return fakeAddr{} }

// A channel has no deadlines. Nothing in gridterm sets one on a
// forwarded connection, and saying so beats pretending it worked.
func (c *chanConn) SetDeadline(time.Time) error      { return errNoDeadline }
func (c *chanConn) SetReadDeadline(time.Time) error  { return errNoDeadline }
func (c *chanConn) SetWriteDeadline(time.Time) error { return errNoDeadline }

var errNoDeadline = errors.New("sshtest: a forwarded channel has no deadlines")

func (c *chanConn) Close() error {
	var err error
	c.once.Do(func() { err = c.Channel.Close() })
	return err
}

// fakeAddr stands in for an address a channel does not have.
type fakeAddr struct{}

func (fakeAddr) Network() string { return "ssh" }
func (fakeAddr) String() string  { return "forwarded" }
