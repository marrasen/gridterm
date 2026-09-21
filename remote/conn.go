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
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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

	// ForwardAgent carries this machine's SSH agent to the far end, so a
	// second hop from there signs with the keys held here.
	//
	// It is for the whole connection, not one pane: while it is on,
	// anyone who is root on that machine can sign with these keys.
	ForwardAgent bool

	// KeysOnly offers keys and nothing else, which is what taking over
	// another gridterm window does: that end accepts no password and no
	// keyboard-interactive.
	KeysOnly bool

	// NoRing leaves out the keys already unlocked, for a connection that
	// must offer only the key files it was named.
	NoRing bool

	// agent opens the SSH agent. A nil one opens the agent running on
	// this machine; it is set so that a test can hand over one of its
	// own.
	agent agentSource

	// Ask reaches the user for a passphrase, a password, a one-time code
	// or a decision about an unknown host key. A nil one refuses
	// anything that would need asking: encrypted keys are skipped, no
	// password is offered, and an unknown host is a hard failure.
	Ask Ask

	// Saying is told each step of connecting as it is tried, for showing
	// somebody what a connection is doing. A nil one is not called.
	//
	// It is called from whichever goroutine is connecting, which is not
	// the one that draws, so an implementation that touches a window has
	// to hand the work to whatever does.
	Saying func(what string)

	// Wrong is told the steps that did not go well: a passphrase that
	// did not unlock a key, an SSH agent that stopped answering. A
	// window draws these differently from the steps that went well, so
	// an account of a connection that had trouble does not read like one
	// that had none. A nil one sends them to Saying instead.
	Wrong func(what string)

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

// Target names the machine the way a user would type it, leaving out the
// parts that are the default.
//
// It is what a connection made from a typed target is called, so two
// accounts on one machine, or two ports, are two names rather than one.
func (c Config) Target() string {
	addr := c.Host
	if c.Port != 0 && c.Port != 22 {
		addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	} else if strings.Contains(addr, ":") {
		// A bare IPv6 literal needs its brackets or it reads as a host
		// and a port.
		addr = "[" + addr + "]"
	}
	if c.User == "" {
		return addr
	}
	return c.User + "@" + addr
}

// SameMachine reports whether two configs name the same login on the
// same machine.
//
// The window keeps one connection per name, and a name that meant a
// different machine from one moment to the next would put a terminal on
// whichever was connected to first.
func (c Config) SameMachine(other Config) bool {
	port := func(cfg Config) int {
		if cfg.Port == 0 {
			return 22
		}
		return cfg.Port
	}
	return c.Host == other.Host && port(c) == port(other) && c.User == other.User
}

// Conn is a live connection to one machine.
type Conn struct {
	client *ssh.Client
	agent  io.Closer // the agent socket, if one was opened

	// forwardAgent says the far end may reach this machine's SSH agent,
	// so each session asks for it as it opens.
	forwardAgent bool

	user, addr string

	// via is the connection this one is carried inside, when it was
	// reached through another machine.
	via *Conn

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

	// gone is closed once the transport has ended, whether this window
	// closed the connection or the machine dropped off the network.
	// waitErr is why, and is only read once gone has closed.
	gone    chan struct{}
	waitErr error
}

// Connect opens a connection to a machine and authenticates.
//
// Cancelling ctx gives up, including while a dialog is waiting for a
// passphrase: authentication can sit on a question nobody is going to
// answer, and a window that is closing must not wait for it.
func Connect(ctx context.Context, cfg Config) (*Conn, error) {
	return connect(ctx, overTCP, nil, cfg)
}

// Through opens a connection to another machine from this one.
//
// No local port is opened for it. The second connection is carried
// inside a channel of the first, which is what ssh -J does and what a
// saved server reached "through" another one means. It rides on this
// connection: closing this one closes it too.
func (c *Conn) Through(ctx context.Context, cfg Config) (*Conn, error) {
	if c.isClosing() {
		return nil, fmt.Errorf("remote: %s: %w", c, ErrClosed)
	}
	return connect(ctx, c.reach, c, cfg)
}

// reach opens a plain connection from the far end of this one.
//
// It carries the same deadline a connection made from here does, so a
// machine the far end cannot get to fails with a message rather than
// leaving the window waiting for however long the remote takes to give
// up. Cancelling after this has returned does not touch the connection
// it hands back.
func (c *Conn) reach(ctx context.Context, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	return c.client.DialContext(ctx, "tcp", addr)
}

// Wait blocks until the connection is gone, and returns why.
//
// It is how the window notices a machine that dropped off the network
// rather than one it closed itself: nothing else here would ever say so.
// Any number of callers may wait, and each gets the same reason.
func (c *Conn) Wait() error {
	<-c.gone
	return c.waitErr
}

// connect is the body of both: the only difference is how the address is
// reached and what the result rides on.
func connect(ctx context.Context, to reach, via *Conn, cfg Config) (*Conn, error) {
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
		keys, err := loadKnownHosts(cfg.KnownHosts, cfg.Saying)
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
	client, dialErr := dial(ctx, to, addr, cfg.User, a.next, hostKey, bannerOf(ctx, cfg), cfg.Saying)
	if dialErr != nil {
		a.close()
		// What the user said, when they said anything: "the dialog was
		// cancelled" beats x/crypto reporting that no method remained.
		if why := ask.reason(); why != nil {
			return nil, why
		}
		// A key that could not be offered, joined onto what x/crypto
		// says rather than replacing it. The server's refusal is still
		// the reason the connection failed; a passphrase that did not
		// unlock a key is why it had less to offer, and x/crypto no
		// longer holds it by the time this is reached.
		if why := a.keyTrouble(); why != nil {
			return nil, errors.Join(why, dialErr)
		}
		return nil, dialErr
	}
	// Cancelled while the handshake was finishing. Handing back a live
	// connection here would open a terminal the user has given up on.
	if err := ctx.Err(); err != nil {
		// The reason the caller gets is the user's own decision, because
		// every caller checks for that. A connection just made that will
		// not close is said out loud instead: joined onto the
		// cancellation it would be swallowed by all of them.
		if closeErr := client.Close(); closeErr != nil && cfg.Saying != nil {
			cfg.Saying("could not close the connection that was given up on: " +
				closeErr.Error())
		}
		a.close()
		return nil, err
	}
	// Past the agent, so the watch on it stops. The connection owns the
	// socket from here.
	a.done()
	c := newConn(client, a.agentCloser(), cfg.User, addr)
	if cfg.ForwardAgent {
		// Refused rather than connected without it, since the user asked for the agent
		if err := c.carryTheAgent(a.forwardingAgent(), a.noAgentToCarry(cfg)); err != nil {
			return nil, errors.Join(err, c.Close())
		}
		saySo(cfg.Saying, "the SSH agent will be carried to this machine")
	}
	if via != nil {
		c.via = via
		if err := via.register(c); err != nil {
			// Both: the connection is no use, and a failure to close
			// something just opened is worth saying out loud.
			return nil, errors.Join(err, c.Close())
		}
	}
	return c, nil
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

// noKeysToOffer explains a take-over that had no key to try, naming the
// agent's own failure when there was one.
func noKeysToOffer(agentErr error) error {
	const where = "remote: no keys to offer: " +
		"name a key file, put one in ~/.ssh, or add one to the SSH agent"
	if agentErr != nil {
		return fmt.Errorf("%s. The SSH agent could not be read: %w", where, agentErr)
	}
	return errors.New(where)
}

// newConn wraps an authenticated client.
func newConn(client *ssh.Client, agentConn io.Closer, user, addr string) *Conn {
	c := &Conn{
		client: client,
		agent:  agentConn,
		user:   user,
		addr:   addr,
		riders: make(map[rider]struct{}),
		done:   make(chan struct{}),
		gone:   make(chan struct{}),
	}
	// One watcher for the whole connection, so Closed can answer without
	// blocking and every caller of Wait gets the same reason.
	go func() {
		c.waitErr = client.Wait()
		close(c.gone)
	}()
	return c
}

// carryTheAgent lets the far end open a channel back to this machine's
// SSH agent, and marks the connection so each session asks for it. why
// is what to say when there is no agent to carry.
func (c *Conn) carryTheAgent(ag agent.Agent, why error) error {
	if ag == nil {
		return fmt.Errorf("the SSH agent cannot be carried to %s: %w."+
			" Turn the SSH agent off for this server to connect without it", c.String(), why)
	}
	if err := agent.ForwardToAgent(c.client, ag); err != nil {
		return fmt.Errorf("remote: carry the SSH agent to %s: %w", c.String(), err)
	}
	c.forwardAgent = true
	return nil
}

// String names the connection the way a user would: user@host:port.
func (c *Conn) String() string { return c.user + "@" + c.addr }

// Closed reports whether the connection is no longer usable, so a caller
// can tell a session that ended by itself from the connection going under
// it.
//
// Both ways it can go: this window closed it, or the transport under it
// ended because the machine dropped off the network. The second one is
// nobody's decision here, so a connection that only asked whether Close
// had been called would say a dead machine was still reachable.
func (c *Conn) Closed() bool {
	if c.isClosing() {
		return true
	}
	select {
	case <-c.gone:
		return true
	default:
		return false
	}
}

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

// Via returns the connection this one is carried inside, or nil.
func (c *Conn) Via() *Conn { return c.via }

// Close ends everything riding on the connection and then the connection
// itself. It is idempotent, and a second caller waits for the first to
// finish rather than reporting a success that has not happened.
func (c *Conn) Close() error {
	err := c.closeRider()
	if c.via != nil {
		c.via.drop(c)
	}
	return err
}

// closeRider is Close without letting go of what carries this
// connection, for a machine that is closing everything riding on it and
// will throw the whole record away anyway.
func (c *Conn) closeRider() error {
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
		wg.Go(func() {
			collect(r.closeRider())
		})
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
