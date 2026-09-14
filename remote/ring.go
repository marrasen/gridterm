package remote

import (
	"context"
	"errors"
	"fmt"
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
		var needsPass *ssh.PassphraseMissingError
		if !errors.As(err, &needsPass) {
			return nil, err
		}
		if ask == nil {
			return nil, errors.New("the key has a passphrase and there is nothing to ask for it")
		}
		pass, err := ask.Passphrase(ctx, path)
		if err != nil {
			return nil, err
		}
		if signer, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(pass)); err != nil {
			return nil, fmt.Errorf("the passphrase did not unlock it: %w", err)
		}
	}

	if r != nil {
		r.mu.Lock()
		r.signers[path] = signer
		r.mu.Unlock()
	}
	return signer, nil
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

// Forget drops one key, so the next connection asks for it again.
func (r *Ring) Forget(path string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.signers, path)
}

// Lock drops every key.
func (r *Ring) Lock() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.signers)
}
