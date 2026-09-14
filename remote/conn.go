// Package remote is SSH: the machines gridterm can reach, and what runs
// on them.
//
// One Conn is one connection to one machine, and more than one thing can
// ride on it at a time. Today that means shells; tunnels and file
// transfers are the reason the connection is kept separate from them.
// Closing the connection closes everything riding on it.
//
// A Conn is safe to use from several goroutines. A Shell is not, beyond
// what it documents.
package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
)

// ErrClosed is returned when something is opened on a connection that
// has already been closed.
var ErrClosed = errors.New("the connection is closed")

// rider is something open on a connection.
//
// closeRider tears it down and must not call Conn.Close. The connection
// may already be closing, and a rider that waited for that to finish
// would be waiting for itself.
type rider interface {
	closeRider() error
}

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
	// ~/.ssh/id_ed25519, id_ecdsa and id_rsa. A file named here that
	// cannot be read or decrypted fails the connection; one of the
	// defaults is skipped.
	Identities []string

	// NoAgent skips the SSH agent even when one is running, and
	// NoIdentities skips key files entirely, for a connection that
	// should only ever offer a password.
	NoAgent      bool
	NoIdentities bool

	// Ask reaches the user for a passphrase, a password, a one-time code
	// or a decision about an unknown host key. A nil one refuses
	// anything that would need asking: encrypted keys are skipped, no
	// password is offered, and an unknown host is a hard failure.
	Ask Ask

	// Ring holds keys already unlocked, so a passphrase is asked for
	// once and then used for every connection. A nil one holds nothing
	// and keeps nothing.
	Ring *Ring

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
	riders  map[rider]struct{}
	closing bool

	// done is closed once Close has finished, so a second caller waits
	// for the first rather than reporting success while the connection
	// is still being torn down.
	done     chan struct{}
	closeErr error
}

// Connect opens a connection to a machine and authenticates.
//
// Cancelling ctx gives up, including while a dialog is waiting for a
// passphrase: authentication can sit on a question nobody is going to
// answer, and a window that is closing must not wait for it.
func Connect(ctx context.Context, cfg Config) (*Conn, error) {
	if cfg.Host == "" {
		return nil, errors.New("remote: no host given")
	}
	if cfg.User == "" {
		u, err := currentUser()
		if err != nil {
			return nil, fmt.Errorf("remote: no user given and none found: %w", err)
		}
		// Written back, not kept in a local: the dialogs are built from
		// the config, and a password dialog that says "@host" does not
		// say whose password it wants.
		cfg.User = u
	}

	// A dialog the user dismisses cancels the connection rather than
	// being treated as one more thing that did not work.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ask := newCancelAsk(cfg.Ask, cancel)
	cfg.Ask = ask.asker()

	hostKey := cfg.HostKeyCallback
	if hostKey == nil {
		keys, err := loadKnownHosts(cfg.KnownHosts)
		if err != nil {
			return nil, err
		}
		hostKey = keys.callback(ctx, cfg.Ask)
	}

	a, err := authMethods(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if len(a.ladder) == 0 {
		a.close()
		return nil, nothingToAuthenticateWith(a.noAgent)
	}

	addr := cfg.addr()
	client, dialErr := dial(ctx, addr, cfg.User, a.next, hostKey)
	if dialErr != nil {
		a.close()
		// What the user said, when they said anything: "the dialog was
		// cancelled" beats x/crypto reporting that no method remained.
		if why := ask.reason(); why != nil {
			return nil, why
		}
		return nil, dialErr
	}
	// Cancelled while the handshake was finishing. Handing back a live
	// connection here would open a terminal the user has given up on.
	if err := ctx.Err(); err != nil {
		_ = client.Close()
		a.close()
		return nil, err
	}
	return newConn(client, a.agent, cfg.User, addr), nil
}

// nothingToAuthenticateWith explains a connection that had no way to
// even try, naming the agent's own failure when there was one.
func nothingToAuthenticateWith(agentErr error) error {
	msg := "remote: nothing to authenticate with: " +
		"no agent, no readable private key, and no way to ask for a password"
	if agentErr == nil {
		return errors.New(msg)
	}
	return fmt.Errorf("%s: %w", msg, agentErr)
}

// newConn wraps an authenticated client.
func newConn(client *ssh.Client, agentConn io.Closer, user, addr string) *Conn {
	return &Conn{
		client: client,
		agent:  agentConn,
		user:   user,
		addr:   addr,
		riders: make(map[rider]struct{}),
		done:   make(chan struct{}),
	}
}

// String names the connection the way a user would: user@host:port.
func (c *Conn) String() string { return c.user + "@" + c.addr }

// closing reports whether Close has started.
func (c *Conn) isClosing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closing
}

// register records something riding on the connection, so closing the
// connection closes it too. It fails once the connection has closed,
// because a rider registered then would never be closed by anything.
func (c *Conn) register(r rider) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return fmt.Errorf("remote: %s: %w", c, ErrClosed)
	}
	c.riders[r] = struct{}{}
	return nil
}

// drop forgets a rider that closed itself.
func (c *Conn) drop(r rider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.riders, r)
}

// riderCount returns how many things are open on the connection.
func (c *Conn) riderCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.riders)
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
	riders := make([]rider, 0, len(c.riders))
	for r := range c.riders {
		riders = append(riders, r)
	}
	c.riders = nil
	c.mu.Unlock()

	// The riders first, so a shell gets its polite hangup before the
	// transport goes away underneath it. In parallel, because each one
	// waits out its own drain period and several panes on one machine
	// would otherwise close one after another.
	var (
		wg      sync.WaitGroup
		errMu   sync.Mutex
		errs    []error
		collect = func(err error) {
			if err == nil {
				return
			}
			errMu.Lock()
			errs = append(errs, err)
			errMu.Unlock()
		}
	)
	for _, r := range riders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			collect(r.closeRider())
		}()
	}
	wg.Wait()

	if err := c.client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		errs = append(errs, err)
	}
	if c.agent != nil {
		if err := c.agent.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}

	c.closeErr = errors.Join(errs...)
	close(c.done)
	return c.closeErr
}
