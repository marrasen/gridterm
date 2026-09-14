// Package remote is SSH: the machines gridterm can reach, and what runs
// on them.
//
// One Conn is one connection to one machine. Several things ride on it
// at once — shells, remote commands, file transfers and tunnels — which
// is the whole point of keeping the connection separate from the shell.
// Closing the connection closes everything riding on it.
//
// A Conn is safe to use from several goroutines. The things it opens are
// not, beyond what each one documents.
package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Config describes a machine to connect to.
type Config struct {
	// Host is the name or address to connect to. Port defaults to 22.
	Host string
	Port int

	// User defaults to the local username.
	User string

	// KnownHosts lists the files to verify the host key against. Empty
	// means the usual ~/.ssh/known_hosts.
	KnownHosts []string

	// Identities lists private key files to try. Empty means the usual
	// ~/.ssh/id_ed25519, id_ecdsa and id_rsa.
	Identities []string

	// NoAgent skips the SSH agent even when one is running.
	NoAgent bool

	// Passphrase is asked for a private key's passphrase. Nil skips
	// encrypted keys rather than failing.
	Passphrase func(keyfile string) (string, error)

	// Password is asked for the account password, after key
	// authentication has been tried. Nil skips password auth.
	Password func() (string, error)

	// HostKeyCallback replaces known-hosts checking entirely.
	//
	// This exists so tests can pin a generated host key. Setting it in a
	// real client — to ssh.InsecureIgnoreHostKey, say — removes the only
	// defence against a man-in-the-middle, and nothing else here will
	// notice.
	HostKeyCallback ssh.HostKeyCallback
}

// addr returns the host and port as one address, with the default port
// filled in.
func (c Config) addr() string {
	port := c.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
}

// Conn is a live connection to one machine.
type Conn struct {
	client *ssh.Client
	agent  io.Closer // the agent socket, if one was opened

	user, addr string

	// mu guards the riders and the closing flag. Close takes the riders
	// under it and lets go before closing them, so a rider closing
	// itself at the same moment does not deadlock against it.
	mu      sync.Mutex
	riders  map[io.Closer]struct{}
	closing bool

	// done is closed once Close has finished, so a second caller waits
	// for the first rather than reporting success while it is still
	// tearing the connection down.
	done     chan struct{}
	closeErr error
}

// ErrClosed is returned when something is opened on a connection that
// has already been closed.
var ErrClosed = errors.New("remote: the connection is closed")

// Dial opens a connection and authenticates.
func Dial(cfg Config) (*Conn, error) {
	if cfg.Host == "" {
		return nil, errors.New("remote: no host given")
	}
	user := cfg.User
	if user == "" {
		u, err := currentUser()
		if err != nil {
			return nil, fmt.Errorf("remote: no user given and none found: %w", err)
		}
		user = u
	}

	hostKey := cfg.HostKeyCallback
	if hostKey == nil {
		var err error
		if hostKey, err = knownHostsCallback(cfg.KnownHosts); err != nil {
			return nil, err
		}
	}

	auth, agentConn := authMethods(cfg)
	if len(auth) == 0 {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, errors.New("remote: no usable authentication method: " +
			"no agent, no readable private key, and no password source")
	}

	addr := cfg.addr()
	client, err := dial(addr, user, auth, hostKey)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, err
	}
	return newConn(client, agentConn, user, addr), nil
}

// newConn wraps an authenticated client.
func newConn(client *ssh.Client, agentConn io.Closer, user, addr string) *Conn {
	return &Conn{
		client: client,
		agent:  agentConn,
		user:   user,
		addr:   addr,
		riders: make(map[io.Closer]struct{}),
		done:   make(chan struct{}),
	}
}

// User returns the account this connection authenticated as.
func (c *Conn) User() string { return c.user }

// Addr returns the host:port this connection was made to.
func (c *Conn) Addr() string { return c.addr }

// String names the connection the way a user would: user@host:port.
func (c *Conn) String() string { return c.user + "@" + c.addr }

// add registers something riding on the connection, so closing the
// connection closes it too. It fails once the connection has closed,
// because a rider registered then would never be closed by anything.
func (c *Conn) add(r io.Closer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return ErrClosed
	}
	c.riders[r] = struct{}{}
	return nil
}

// drop forgets a rider that closed itself.
func (c *Conn) drop(r io.Closer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.riders != nil {
		delete(c.riders, r)
	}
}

// Close ends everything riding on the connection and then the
// connection itself. It is idempotent, and a second caller waits for the
// first to finish rather than reporting a success that has not happened.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		<-c.done
		return c.closeErr
	}
	c.closing = true
	riders := make([]io.Closer, 0, len(c.riders))
	for r := range c.riders {
		riders = append(riders, r)
	}
	c.riders = nil
	c.mu.Unlock()

	var errs []error
	// The riders first: a shell closing politely gives the remote a
	// chance to exit before the transport goes away underneath it.
	for _, r := range riders {
		if err := r.Close(); err != nil && !errors.Is(err, io.EOF) {
			errs = append(errs, err)
		}
	}
	if err := c.client.Close(); err != nil &&
		!errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
		errs = append(errs, err)
	}
	if c.agent != nil {
		_ = c.agent.Close()
	}

	c.closeErr = errors.Join(errs...)
	close(c.done)
	return c.closeErr
}
