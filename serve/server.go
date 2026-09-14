package serve

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// handshakeWindow is how long a connection has to authenticate before
// it is dropped. One that never finishes would otherwise hold a
// goroutine and a socket for as long as the window is open.
const handshakeWindow = 30 * time.Second

// Config is what a window needs to serve.
type Config struct {
	// Addr is the address to listen on, exactly as given. It is never
	// widened: a window told to listen on this machine only must not end
	// up reachable from the network.
	Addr string

	// HostKey is what a client checks this machine by.
	HostKey ssh.Signer

	// Allowed is the set of keys that may connect. An empty set serves
	// nobody, and Listen refuses to start with one rather than open a
	// port that turns every caller away.
	Allowed *Allowed

	// OnClient is told when a client arrives and when it goes. It is
	// called from the goroutine serving that client, so an
	// implementation has to hand the news to whatever draws.
	OnClient func(c *Client, gone bool)

	// OnError reports a failure the server cannot hand back: one
	// connection's, mostly, which must not take the listener down. A nil
	// OnError drops them.
	OnError func(error)
}

// Client is one window that has taken this one over.
type Client struct {
	// Name is who connected, from the comment on the key they used.
	Name string

	// Addr is where they connected from.
	Addr string

	// At is when they arrived.
	At time.Time

	conn *ssh.ServerConn
}

// Close hangs up on a client.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Server is a window listening for another to take it over.
//
// It is started by hand and never by default. Everything about it is
// off until the user turns it on: no port, no host key on disk, and
// nobody allowed to connect.
type Server struct {
	cfg Config
	ln  net.Listener

	mu      sync.Mutex
	clients []*Client
	closed  bool
}

// Listen starts serving.
//
// It refuses to start when nobody is allowed to connect. A listening
// port that turns every caller away is worse than no port: it says the
// window is being served when it is not, and it is one forgotten line
// in a file away from being served to whoever adds it.
func Listen(cfg Config) (*Server, error) {
	switch {
	case cfg.HostKey == nil:
		return nil, errors.New("serve: no host key to identify this machine by")
	case cfg.Allowed.Len() == 0:
		return nil, errors.New(
			"serve: no keys are allowed to connect, so there is nobody to serve")
	case cfg.Addr == "":
		return nil, errors.New("serve: no address to listen on")
	}

	s := &Server{cfg: cfg}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("serve: listen on %s: %w", cfg.Addr, err)
	}
	s.ln = ln
	go s.accept()
	return s, nil
}

// Addr is where the server is listening, which is not always what it
// was asked for: a port of 0 is whichever one the system gave it.
func (s *Server) Addr() string {
	if s == nil || s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Clients returns who is connected right now.
func (s *Server) Clients() []*Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Client(nil), s.clients...)
}

// Close stops listening and hangs up on everyone.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	clients := s.clients
	s.clients = nil
	s.mu.Unlock()

	err := s.ln.Close()
	for _, c := range clients {
		if got := c.Close(); got != nil {
			err = errors.Join(err, got)
		}
	}
	return err
}

// accept takes connections until the listener is closed.
func (s *Server) accept() {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			// Closed, or the listener itself failed. Either way there is
			// nothing left to accept on.
			if !s.isClosed() {
				s.onError(fmt.Errorf("serve: stopped listening on %s: %w", s.Addr(), err))
			}
			return
		}
		go s.handshake(nc)
	}
}

// handshake authenticates one connection.
func (s *Server) handshake(nc net.Conn) {
	// A connection that never finishes authenticating is dropped rather
	// than left holding a goroutine for the life of the window.
	timer := time.AfterFunc(handshakeWindow, func() { _ = nc.Close() })

	var who string
	cfg := &ssh.ServerConfig{
		// Public keys and nothing else. No password callback and no
		// keyboard-interactive: a window is served to the keys its owner
		// listed, and anything else would be a second way in that nobody
		// asked for.
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			name, ok := s.cfg.Allowed.Who(key)
			if !ok {
				return nil, fmt.Errorf("serve: %s is not allowed to connect", Fingerprint(key))
			}
			who = name
			return nil, nil
		},
	}
	cfg.AddHostKey(s.cfg.HostKey)

	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	timer.Stop()
	if err != nil {
		_ = nc.Close()
		// Reported rather than dropped: a refused connection is the
		// thing the owner of the window most wants to know about.
		s.onError(fmt.Errorf("serve: %s could not connect: %w", nc.RemoteAddr(), err))
		return
	}

	c := &Client{Name: who, Addr: conn.RemoteAddr().String(), At: time.Now(), conn: conn}
	if !s.add(c) {
		// Closed while this one was authenticating.
		_ = conn.Close()
		return
	}
	if s.cfg.OnClient != nil {
		s.cfg.OnClient(c, false)
	}

	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		// Nothing is served over the connection yet. Refused by name so
		// a client of a later version is told what is wrong rather than
		// left waiting.
		_ = nch.Reject(ssh.UnknownChannelType, "this gridterm serves no channels yet")
	}

	_ = conn.Wait()
	s.drop(c)
	if s.cfg.OnClient != nil {
		s.cfg.OnClient(c, true)
	}
}

// add records a client, reporting whether the server is still open.
func (s *Server) add(c *Client) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.clients = append(s.clients, c)
	return true
}

// drop forgets a client that has gone.
func (s *Server) drop(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, have := range s.clients {
		if have != c {
			continue
		}
		copy(s.clients[i:], s.clients[i+1:])
		s.clients[len(s.clients)-1] = nil
		s.clients = s.clients[:len(s.clients)-1]
		return
	}
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) onError(err error) {
	if s.cfg.OnError != nil {
		s.cfg.OnError(err)
	}
}
