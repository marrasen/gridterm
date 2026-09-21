package remote

import (
	"context"
	"crypto/x509"
	"errors"
	"os"
	"sort"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Ring holds private keys that have been unlocked.
//
// A passphrase is asked for once and the key is then used for every
// connection, which is the whole reason the ring exists: opening a
// second server should not ask again.
//
// Nothing here reaches disk and nothing here holds a passphrase. Only
// the decrypted key stays, in memory, until the window closes or the
// user locks the ring.
//
// A Ring is safe to use from several goroutines.
type Ring struct {
	mu      sync.Mutex
	signers map[string]ssh.Signer // by key file path

	// opening holds a channel per key being unlocked right now, closed
	// when that unlock finishes. Two connections wanting the same key
	// would otherwise put two dialogs on screen for it.
	opening map[string]chan struct{}

	// agentTrouble is why the SSH agent was last given up on, and nil
	// until one is. It belongs here for the same reason the keys do: it
	// is learned once and spares every connection after it.
	agentTrouble error
}

// NewRing returns an empty ring.
func NewRing() *Ring {
	return &Ring{
		signers: make(map[string]ssh.Signer),
		opening: make(map[string]chan struct{}),
	}
}

// Signers returns every key that has been unlocked, in a stable order so
// a server is offered them the same way twice.
func (r *Ring) Signers() []ssh.Signer {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	paths := make([]string, 0, len(r.signers))
	for p := range r.signers {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]ssh.Signer, 0, len(paths))
	for _, p := range paths {
		out = append(out, r.signers[p])
	}
	return out
}

// Paths returns the key files the ring holds, in a stable order.
func (r *Ring) Paths() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.signers))
	for p := range r.signers {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Has reports whether a key file has already been unlocked.
func (r *Ring) Has(path string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.signers[path]
	return ok
}

// Unlock reads a private key and keeps it, asking for a passphrase when
// it has one. A key already in the ring is returned without asking.
func (r *Ring) Unlock(ctx context.Context, path string, ask Ask) (ssh.Signer, error) {
	if r != nil {
		signer, wait, ok := r.claim(path)
		switch {
		case ok:
			return signer, nil
		case wait != nil:
			// Somebody else is already asking. Waiting for their answer
			// is better than a second dialog for the same key.
			select {
			case <-wait:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if signer, _, ok := r.claim(path); ok {
				return signer, nil
			}
			// Theirs failed. Ours is welcome to try.
		}
		defer r.release(path)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		if _, ok := errors.AsType[*ssh.PassphraseMissingError](err); !ok {
			return nil, err
		}
		if ask == nil {
			return nil, errors.New("the key has a passphrase and there is nothing to ask for it")
		}
		if signer, err = askUntilItOpens(ctx, path, b, ask); err != nil {
			return nil, err
		}
	}

	if r != nil {
		r.mu.Lock()
		r.signers[path] = signer
		r.mu.Unlock()
	}
	return signer, nil
}

// ErrWrongPassphrase is a passphrase that did not open the key.
//
// Its own error because it is the one failure here that is the user's to
// put right, and the only one worth asking about again. x/crypto's own
// wording for it is "x509: decryption password incorrect", which is
// neither this window's vocabulary nor, for an OpenSSH key, true.
var ErrWrongPassphrase = errors.New("the passphrase did not unlock it")

// askUntilItOpens asks for a passphrase until one opens the key or the
// user says no.
//
// There is no limit on the asking. The key file is on this machine and
// this user can already read it, so a count that gave up after three
// guards nothing: it only takes the user who mistyped a long passphrase
// twice and hands the connection on to whatever else it has to offer.
// Cancel is how the user says they cannot open it.
func askUntilItOpens(ctx context.Context, path string, b []byte, ask Ask) (ssh.Signer, error) {
	for wrong := 0; ; wrong++ {
		pass, err := ask.Passphrase(ctx, LockedKey{Path: path, Wrong: wrong})
		if err != nil {
			// The user said no, or the connection was given up on.
			// Neither is a reason to ask again.
			return nil, err
		}
		signer, err := ssh.ParsePrivateKeyWithPassphrase(b, []byte(pass))
		if err == nil {
			return signer, nil
		}
		if !errors.Is(err, x509.IncorrectPasswordError) {
			// Not the passphrase: the file is a key this cannot read, or
			// one that is damaged. Asking for it again would be asking
			// the user to fix something that is not theirs to fix.
			return nil, err
		}
	}
}

// claim reports the key if the ring already holds it. Otherwise it
// either takes responsibility for unlocking it, or hands back the
// channel to wait on while somebody else does.
func (r *Ring) claim(path string) (signer ssh.Signer, wait chan struct{}, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if signer, ok := r.signers[path]; ok {
		return signer, nil, true
	}
	if wait, busy := r.opening[path]; busy {
		return nil, wait, false
	}
	r.opening[path] = make(chan struct{})
	return nil, nil, false
}

// release says an unlock has finished, however it went.
func (r *Ring) release(path string) {
	r.mu.Lock()
	wait := r.opening[path]
	delete(r.opening, path)
	r.mu.Unlock()
	if wait != nil {
		close(wait)
	}
}

// AgentGaveUp remembers that the SSH agent did not answer.
//
// An agent that accepts a connection and then says nothing costs every
// connection the wait to find that out. Remembering it costs the first
// one only. Locking the keys forgets it, for an agent that has been
// started or unstuck since.
func (r *Ring) AgentGaveUp(why error) {
	if r == nil || why == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agentTrouble == nil {
		r.agentTrouble = why
	}
}

// AgentTrouble returns why the agent was given up on, or nil when it
// has not been.
func (r *Ring) AgentTrouble() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agentTrouble
}

// Forget drops one key, so the next connection asks for it again.
func (r *Ring) Forget(path string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.signers, path)
}

// Lock drops every key, and forgets that the SSH agent did not answer.
//
// It is how the user says "forget what you know about my credentials",
// and an agent started or unstuck since is one of those things.
func (r *Ring) Lock() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.signers)
	r.agentTrouble = nil
}
