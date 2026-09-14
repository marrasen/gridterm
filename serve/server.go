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

// handshakesAtOnce is how many callers may be part way through
// connecting. Anyone past that is hung up on straight away.
//
// Nobody has authenticated at this point, so this is the one number
// standing between a stranger who can reach the port and the window
// holding the user's shells. Without it, a caller in a loop pins a
// goroutine and a socket per connection for the whole handshake window.
// One person moving between machines needs one or two.
const handshakesAtOnce = 8

// quietFor is how long the server waits before reporting another
// refused connection.
//
// Refusals are worth telling the user about, and a stranger hammering
// the port can make thousands a second. They are counted and told
// together instead, so the news still arrives without the queue that
// carries it growing without bound.
const quietFor = 5 * time.Second

// pubKeyAlgos are the signature algorithms a client may authenticate
// with.
//
// Pinned rather than left to the library's default, which still carries
// SHA-1 RSA and DSA. OpenSSH stopped accepting the first by default in
// 8.8 and has removed the second.
var pubKeyAlgos = []string{
	ssh.KeyAlgoED25519,
	ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
	ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256,
	ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512,
}

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

	// OnJoin is told when a client arrives and OnGone when one goes,
	// with why it went: a client lost to a network fault and one that
	// hung up are different things to be told about.
	//
	// Both are called from the goroutine serving that client, so an
	// implementation has to hand the news to whatever draws.
	OnJoin func(c *Client)
	OnGone func(c *Client, why error)

	// OnStopped is told when the server has stopped listening for a
	// reason other than having been closed, which is the end of it: no
	// further client can connect. Whoever is serving has to say so and
	// stop saying the window is being served.
	OnStopped func(err error)

	// OnError reports a failure that does not stop the server: one
	// connection's, mostly. A nil OnError drops them.
	OnError func(error)

	// Handshake is how long a caller has to authenticate. Zero means
	// handshakeWindow, which is what the program uses; a test sets it
	// short rather than waiting out the real one.
	Handshake time.Duration

	// Open starts something for a client to work in. A nil one serves
	// nothing, and a client that asks is told so.
	Open Opener
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

	// arriving are the connections that have been accepted and have not
	// finished authenticating. Closing has to reach them too, or a
	// caller that says nothing holds a socket open for the whole
	// handshake window after the window stopped serving.
	arriving map[net.Conn]struct{}

	// refused counts the connections turned away since the last time
	// the user was told, toldAt is when that was, and turnedAway counts
	// every one of them for a test to wait on.
	refused    int
	toldAt     time.Time
	turnedAway int
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

	s := &Server{cfg: cfg, arriving: map[net.Conn]struct{}{}}
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

// HostKey returns the public half of the key this server presents.
//
// Asked of the running server rather than read back from disk. The file
// can be removed or replaced while the window is serving, and a caller
// that read it would hand the user a fingerprint to check that the
// server never presents -- which teaches them to click past the one
// warning that matters.
func (s *Server) HostKey() ssh.PublicKey {
	if s == nil || s.cfg.HostKey == nil {
		return nil
	}
	return s.cfg.HostKey.PublicKey()
}

// Clients returns who is connected right now.
func (s *Server) Clients() []*Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Client(nil), s.clients...)
}

// Close stops listening and hangs up on everyone, including on anyone
// part way through connecting.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	clients := s.clients
	s.clients = nil
	arriving := make([]net.Conn, 0, len(s.arriving))
	for nc := range s.arriving {
		arriving = append(arriving, nc)
	}
	clear(s.arriving)
	s.mu.Unlock()

	err := s.ln.Close()
	for _, c := range clients {
		if got := c.Close(); got != nil {
			err = errors.Join(err, got)
		}
	}
	// The half-connected go without their errors being gathered: they
	// have no session yet, and a socket closed under a handshake fails
	// that handshake by design.
	for _, nc := range arriving {
		_ = nc.Close()
	}
	return err
}

// accept takes connections until the listener is closed.
func (s *Server) accept() {
	for {
		nc, err := s.ln.Accept()
		if err == nil {
			go s.handshake(nc)
			continue
		}
		if s.isClosed() {
			return
		}
		// The listener itself failed, which is the end of it: nothing
		// can be accepted on it again. Serving stops rather than
		// carrying on looking like a window that can still be reached.
		_ = s.Close()
		if s.cfg.OnStopped != nil {
			s.cfg.OnStopped(fmt.Errorf("serve: stopped listening on %s: %w", s.Addr(), err))
		}
		return
	}
}

// handshake authenticates one connection.
func (s *Server) handshake(nc net.Conn) {
	switch s.arrived(nc) {
	case arrivedClosed:
		// Closed while this one was being accepted.
		_ = nc.Close()
		return
	case arrivedBusy:
		// Too many callers part way through connecting. Hung up on
		// rather than queued: nobody here has authenticated, and a
		// queue is what a stranger with a loop would be filling.
		_ = nc.Close()
		s.onRefused(fmt.Errorf(
			"serve: %s was turned away, %d callers are already connecting",
			nc.RemoteAddr(), handshakesAtOnce))
		return
	}
	// A connection that never finishes authenticating is dropped rather
	// than left holding a goroutine for the life of the window.
	window := s.cfg.Handshake
	if window <= 0 {
		window = handshakeWindow
	}
	timer := time.AfterFunc(window, func() { _ = nc.Close() })

	var who string
	cfg := &ssh.ServerConfig{
		// Public keys and nothing else. No password callback and no
		// keyboard-interactive: a window is served to the keys its owner
		// listed, and anything else would be a second way in that nobody
		// asked for.
		PublicKeyAuthAlgorithms: pubKeyAlgos,
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if _, ok := key.(*ssh.Certificate); ok {
				// A certificate has an expiry, principals and a
				// revocation list behind it, and none of that is
				// checked here. The list holds keys, not authorities.
				return nil, errors.New("serve: a certificate is not a key this window knows")
			}
			name, ok := s.cfg.Allowed.Who(key)
			if !ok {
				return nil, fmt.Errorf("serve: %s is not allowed to connect", Fingerprint(key))
			}
			// Carried on the connection rather than in a variable of
			// this function. This runs for every key a client offers,
			// including ones it only asks about and never signs with,
			// so what it decides has to travel with the key it decided
			// about.
			return &ssh.Permissions{Extensions: map[string]string{whoExt: name}}, nil
		},
		// Called only once the client has proved it holds the key, and
		// handed the permissions that key was approved with. It is the
		// one place the name and the key that signed are known to be
		// the same key.
		VerifiedPublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey,
			perms *ssh.Permissions, algo string) (*ssh.Permissions, error) {
			if perms == nil || perms.Extensions[whoExt] == "" {
				return nil, errors.New("serve: the key that signed was never approved")
			}
			return perms, nil
		},
	}
	cfg.AddHostKey(s.cfg.HostKey)

	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	inTime := timer.Stop()
	s.departed(nc)
	switch {
	case err != nil:
		_ = nc.Close()
		// Told about rather than dropped, but counted rather than told
		// one at a time: a stranger with a loop makes thousands.
		s.onRefused(fmt.Errorf("serve: %s could not connect: %w", nc.RemoteAddr(), err))
		return
	case !inTime:
		// The handshake finished just as the window for it ran out, and
		// the socket has been closed under it. Treated as the timeout
		// it was rather than as a client that is connected on a
		// connection that is not.
		_ = conn.Close()
		s.onRefused(fmt.Errorf("serve: %s took too long to connect", nc.RemoteAddr()))
		return
	}
	if conn.Permissions != nil {
		who = conn.Permissions.Extensions[whoExt]
	}

	c := &Client{Name: who, Addr: conn.RemoteAddr().String(), At: time.Now(), conn: conn}
	if !s.add(c) {
		// Closed while this one was authenticating.
		_ = conn.Close()
		return
	}
	if s.cfg.OnJoin != nil {
		s.cfg.OnJoin(c)
	}

	go ssh.DiscardRequests(reqs)
	go s.serveChannels(chans)

	why := conn.Wait()
	s.drop(c)
	if s.cfg.OnGone != nil {
		s.cfg.OnGone(c, why)
	}
}

// whoExt is where the name of the key that signed is kept between the
// authentication callback and the connection it authenticated.
const whoExt = "gridterm-who"

// What arrived decides about a connection that has just been accepted.
type arrival int

const (
	arrivedOK     arrival = iota // it may go on to authenticate
	arrivedClosed                // the server is closed
	arrivedBusy                  // too many are already connecting
)

// arrived records a connection being authenticated, or says why it may
// not be.
func (s *Server) arrived(nc net.Conn) arrival {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return arrivedClosed
	case len(s.arriving) >= handshakesAtOnce:
		return arrivedBusy
	}
	s.arriving[nc] = struct{}{}
	return arrivedOK
}

// onRefused tells the user a caller was turned away, at most once every
// quietFor and with a count of the ones since.
//
// Counted rather than dropped. The user asked for this port to be open
// and is the only one who can decide what a stream of refusals means,
// so the news has to reach them -- but not once per packet, down a
// queue that has no depth to overflow.
func (s *Server) onRefused(err error) {
	s.mu.Lock()
	now := time.Now()
	s.refused++
	s.turnedAway++
	if !s.toldAt.IsZero() && now.Sub(s.toldAt) < quietFor {
		s.mu.Unlock()
		return
	}
	since := s.refused - 1
	s.refused, s.toldAt = 0, now
	s.mu.Unlock()

	if since > 0 {
		err = fmt.Errorf("%w (and %d more since)", err, since)
	}
	s.onError(err)
}

// departed forgets a connection that has finished authenticating, one
// way or the other.
func (s *Server) departed(nc net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.arriving, nc)
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
