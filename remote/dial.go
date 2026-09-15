package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// dialTimeout bounds the attempt to reach the machine, so an
// unresponsive host fails with a message rather than hanging for however
// long TCP takes to give up.
//
// It covers reaching the host and nothing after it. Authentication can
// stop to ask the user for a passphrase, and a deadline that ran while a
// dialog was open would close the connection under them; cancelling the
// context is what ends that.
const dialTimeout = 20 * time.Second

// reach opens a plain connection to an address.
//
// It is what separates a connection made from here from one made through
// another machine: everything after it -- the handshake, the host key,
// the ladder of things to authenticate with -- is the same either way.
type reach func(ctx context.Context, addr string) (net.Conn, error)

// overTCP reaches an address from this machine.
func overTCP(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	return d.DialContext(ctx, "tcp", addr)
}

// dial connects, retrying once with the host key types that known_hosts
// actually holds for this address.
//
// Without that retry the client and OpenSSH disagree about which host
// key to use: x/crypto prefers rsa-sha2-256 while OpenSSH prefers
// ed25519 and reorders its offer towards what the client already knows.
// A server with both keys then presents the one known_hosts does not
// record, and knownhosts reports "key mismatch" — the man-in-the-middle
// alarm — when nothing at all is wrong.
func dial(ctx context.Context, to reach, addr, user string, next ssh.ClientAuthCallback,
	hostKey ssh.HostKeyCallback, banner ssh.BannerCallback, saying func(string)) (*ssh.Client, error) {

	// AuthCallback rather than Auth: x/crypto's own selection
	// deduplicates by method name, so of several public-key methods only
	// the first would ever be tried. What to try next is decided by the
	// ladder in auth.go instead.
	base := &ssh.ClientConfig{
		User:            user,
		AuthCallback:    next,
		HostKeyCallback: hostKey,
		BannerCallback:  banner,
		Timeout:         dialTimeout,
	}
	client, err := dialOnce(ctx, to, addr, base, saying)
	if err == nil {
		return client, nil
	}

	var ke *knownhosts.KeyError
	if errors.As(err, &ke) && len(ke.Want) > 0 {
		if algos := wantedKeyTypes(ke.Want); len(algos) > 0 {
			retry := *base
			retry.HostKeyAlgorithms = algos
			saySo(saying, "its host key is not the sort this expected; asking again for the one known_hosts holds")
			client, err2 := dialOnce(ctx, to, addr, &retry, saying)
			if err2 == nil {
				return client, nil
			}
			// Both, because the first attempt is the one that reports a
			// key mismatch and the second is the one that says why the
			// retry did not help either.
			err = errors.Join(err, err2)
		}
	}
	return nil, describeHostKeyError(err, addr)
}

// helloTimeout is how long the far end has to say who it is.
//
// It covers the version exchange and the key exchange, and stops as soon
// as the host key arrives. Nothing after it is bounded here: signing in
// can wait on a passphrase dialog, and a deadline running while one is
// open would close the connection under the user.
//
// A variable because the tests shorten it. Nothing in the program
// writes it.
var helloTimeout = 20 * time.Second

// handshake is everything ssh.NewClientConn produced, so the goroutine
// running it can hand back all of it at once.
type handshake struct {
	cc    ssh.Conn
	chans <-chan ssh.NewChannel
	reqs  <-chan *ssh.Request
	err   error
}

// dialOnce makes one attempt, which cancelling ctx gives up on.
//
// ssh.Dial cannot be cancelled: its Timeout covers reaching the host and
// nothing else, so a handshake that stops to ask the user a question
// would hold the goroutine until they answered. Doing the two halves
// separately is what lets a window that is closing let go.
//
// The handshake runs on a goroutine of its own so that giving up is
// immediate whatever it is doing. Closing the connection is what usually
// stops it, and does when it is a socket. A connection carried inside
// another one takes no deadline at all and need not stop until the
// machine carrying it answers, so this waits for none of that.
func dialOnce(ctx context.Context, to reach, addr string, cfg *ssh.ClientConfig,
	saying func(string)) (*ssh.Client, error) {

	saySo(saying, "reaching "+addr)
	nc, err := to(ctx, addr)
	if err != nil {
		return nil, err
	}
	saySo(saying, "asking "+addr+" who it is, and signing in as "+cfg.User)

	// A copy, so noticing that the far end has answered does not change
	// the config the caller passed in.
	once := *cfg
	late := make(chan struct{})
	var (
		mu       sync.Mutex
		answered bool
	)
	hello := time.AfterFunc(helloTimeout, func() {
		mu.Lock()
		defer mu.Unlock()
		if !answered {
			close(late)
		}
	})
	defer hello.Stop()
	// Left nil when the caller left it nil, so x/crypto still refuses a
	// connection with nothing checking the host key.
	if check := cfg.HostKeyCallback; check != nil {
		once.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			mu.Lock()
			first := !answered
			answered = true
			mu.Unlock()
			hello.Stop()
			// Only the first time. This runs again at every later key
			// exchange, and by then the pane carries a shell: a line
			// written into it would land in the middle of whatever the
			// user is running.
			if !first {
				return check(hostname, remote, key)
			}
			saySo(saying, "it answered, with a "+key.Type()+" host key")
			if err := check(hostname, remote, key); err != nil {
				return err
			}
			saySo(saying, "its host key is accepted")
			return nil
		}
	}

	done := make(chan handshake, 1)
	go func() {
		cc, chans, reqs, err := ssh.NewClientConn(nc, addr, &once)
		done <- handshake{cc: cc, chans: chans, reqs: reqs, err: err}
	}()

	var h handshake
	select {
	case h = <-done:
	case <-late:
		walkAway(nc, done)
		// No address and no "remote:" here: describeHostKeyError adds
		// both on the way out.
		return nil, errors.New("it did not say who it is within " + helloTimeout.String())
	case <-ctx.Done():
		walkAway(nc, done)
		return nil, ctx.Err()
	}
	if h.err != nil {
		_ = nc.Close()
		if why := ctx.Err(); why != nil {
			return nil, errors.Join(why, h.err)
		}
		return nil, h.err
	}
	// Cancelled between the handshake finishing and this noticing.
	// Handing back a live connection would open a terminal on a machine
	// the user has stopped waiting for.
	if why := ctx.Err(); why != nil {
		letGo(h)
		return nil, why
	}
	saySo(saying, "signed in to "+addr+" as "+cfg.User)
	return ssh.NewClient(h.cc, h.chans, h.reqs), nil
}

// walkAway lets go of a handshake that is not coming back.
//
// The connection is closed, which is what a socket needs to stop.
// Whatever the handshake ends up producing is closed when it arrives, so
// nothing here waits for a machine that may never answer.
//
// It has a price when the handshake really never stops: two goroutines
// stay parked, along with the channel of the machine carrying it and
// everything the connection's config holds. They go when that machine
// is closed. Nothing is reported from here, because by then there is no
// caller left to report it to.
func walkAway(nc net.Conn, done <-chan handshake) {
	_ = nc.Close()
	go func() {
		if h := <-done; h.err == nil {
			letGo(h)
		}
	}()
}

// letGo closes a connection that was made and is not wanted, draining
// what the far end opens on it so the multiplexer is not left with a
// reader nobody will run.
func letGo(h handshake) {
	go ssh.DiscardRequests(h.reqs)
	go func() {
		for ch := range h.chans {
			_ = ch.Reject(ssh.Prohibited, "connection cancelled")
		}
	}()
	_ = h.cc.Close()
}

// saySo tells whoever is watching what is being done now, if anybody
// is.
func saySo(saying func(string), what string) {
	if saying != nil {
		saying(what)
	}
}

// wantedKeyTypes lists the key algorithms known_hosts holds for a host,
// most specific first and without duplicates.
func wantedKeyTypes(want []knownhosts.KnownKey) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range want {
		if k.Key == nil {
			continue
		}
		t := k.Key.Type()
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		// An RSA entry authorises the modern SHA-2 signature algorithms
		// for the same key; offering only "ssh-rsa" would be refused by
		// a server that has disabled SHA-1.
		if t == ssh.KeyAlgoRSA {
			out = append(out, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512)
		}
	}
	return out
}

// describeHostKeyError separates "I have never seen this host" from "the
// key changed", which knownhosts reports as the same type and which mean
// very different things to a user.
func describeHostKeyError(err error, addr string) error {
	// The checker already said what it found, including whether part of
	// known_hosts could not be read, so its wording is kept.
	if errors.Is(err, errUnknownHost) {
		return fmt.Errorf("remote: connect to %s: %w", addr, err)
	}
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) {
		return fmt.Errorf("remote: connect to %s: %w", addr, err)
	}
	if len(ke.Want) == 0 {
		return fmt.Errorf("remote: %s is not in known_hosts; "+
			"connect once with ssh to record its host key", addr)
	}
	return fmt.Errorf("remote: host key for %s does not match known_hosts. "+
		"The host may have been rebuilt, or this may be a "+
		"man-in-the-middle: %w", addr, ke)
}
