package remote

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// dialTimeout bounds the connection attempt, so an unresponsive host
// fails with a message rather than hanging for however long TCP takes to
// give up.
const dialTimeout = 20 * time.Second

// dial connects, retrying once with the host key types that known_hosts
// actually holds for this address.
//
// Without that retry the client and OpenSSH disagree about which host
// key to use: x/crypto prefers rsa-sha2-256 while OpenSSH prefers
// ed25519 and reorders its offer towards what the client already knows.
// A server with both keys then presents the one known_hosts does not
// record, and knownhosts reports "key mismatch" — the man-in-the-middle
// alarm — when nothing at all is wrong.
func dial(addr, user string, auth []ssh.AuthMethod, hostKey ssh.HostKeyCallback) (*ssh.Client, error) {
	base := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         dialTimeout,
	}
	client, err := ssh.Dial("tcp", addr, base)
	if err == nil {
		return client, nil
	}

	var ke *knownhosts.KeyError
	if errors.As(err, &ke) && len(ke.Want) > 0 {
		if algos := wantedKeyTypes(ke.Want); len(algos) > 0 {
			retry := *base
			retry.HostKeyAlgorithms = algos
			client, err2 := ssh.Dial("tcp", addr, &retry)
			if err2 == nil {
				return client, nil
			}
			err = err2
		}
	}
	return nil, describeHostKeyError(err, addr)
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
