package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	hostKey ssh.HostKeyCallback) (*ssh.Client, error) {

	// AuthCallback rather than Auth: x/crypto's own selection
	// deduplicates by method name, so of several public-key methods only
	// the first would ever be tried. What to try next is decided by the
	// ladder in auth.go instead.
	base := &ssh.ClientConfig{
		User:            user,
		AuthCallback:    next,
		HostKeyCallback: hostKey,
		Timeout:         dialTimeout,
	}
	client, err := dialOnce(ctx, to, addr, base)
	if err == nil {
		return client, nil
	}

	var ke *knownhosts.KeyError
	if errors.As(err, &ke) && len(ke.Want) > 0 {
		if algos := wantedKeyTypes(ke.Want); len(algos) > 0 {
			retry := *base
			retry.HostKeyAlgorithms = algos
			client, err2 := dialOnce(ctx, to, addr, &retry)
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

// dialOnce makes one attempt, which cancelling ctx gives up on.
//
// ssh.Dial cannot be cancelled: its Timeout covers reaching the host and
// nothing else, so a handshake that stops to ask the user a question
// would hold the goroutine until they answered. Doing the two halves
// separately is what lets a window that is closing let go.
func dialOnce(ctx context.Context, to reach, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	nc, err := to(ctx, addr)
	if err != nil {
		return nil, err
	}
	// Closing the connection is what unblocks the handshake, whichever
	// part of it is waiting.
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })

	cc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		// Only close it ourselves when the cancellation did not: closing
		// an already-closed connection is not an error worth reporting,
		// but the reason matters.
		if !stop() {
			_ = nc.Close()
			return nil, errors.Join(ctx.Err(), err)
		}
		_ = nc.Close()
		return nil, err
	}
	if !stop() {
		// Cancelled between the handshake finishing and us noticing.
		// The incoming channels are drained so the multiplexer is not
		// left with a reader nobody will ever run.
		go ssh.DiscardRequests(reqs)
		go func() {
			for ch := range chans {
				_ = ch.Reject(ssh.Prohibited, "connection cancelled")
			}
		}()
		_ = cc.Close()
		return nil, ctx.Err()
	}
	return ssh.NewClient(cc, chans, reqs), nil
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
