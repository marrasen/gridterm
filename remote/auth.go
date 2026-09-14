package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// defaultIdentities are the key files tried when none were named.
var defaultIdentities = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

// auth is what a connection will try, in order, and the agent socket it
// opened to do it.
type auth struct {
	methods []ssh.AuthMethod
	agent   io.Closer

	// noAgent is why there is no agent, kept for the message shown when
	// nothing at all worked.
	noAgent error
}

// authMethods assembles what to try, in the order a user expects.
//
// Keys that need no passphrase come first, all in one method: the ring,
// the agent, and any key file that is not encrypted. Only if the server
// refuses all of those does anything prompt, and then one encrypted key
// at a time, so a machine with three keys does not ask three times for a
// connection the first one would have made.
func authMethods(ctx context.Context, cfg Config) (*auth, error) {
	a := &auth{}

	var agentSigners func() ([]ssh.Signer, error)
	if !cfg.NoAgent {
		conn, err := dialAgent()
		if err != nil {
			a.noAgent = err
		} else {
			a.agent = conn
			agentSigners = agent.NewClient(conn).Signers
		}
	}

	plain, locked, err := identities(cfg)
	if err != nil {
		a.close()
		return nil, err
	}

	ready := func() ([]ssh.Signer, error) {
		signers := cfg.Ring.Signers()
		if agentSigners != nil {
			got, err := agentSigners()
			if err != nil {
				return nil, fmt.Errorf("remote: read the SSH agent: %w", err)
			}
			signers = append(signers, got...)
		}
		return append(signers, plain...), nil
	}
	if agentSigners != nil || len(plain) > 0 || len(cfg.Ring.Paths()) > 0 {
		a.methods = append(a.methods, ssh.PublicKeysCallback(ready))
	}

	if cfg.Ask == nil {
		return a, nil
	}
	// One method per encrypted key, so the second is only reached — and
	// only asked about — when the first was refused.
	for _, path := range locked {
		a.methods = append(a.methods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			signer, err := cfg.Ring.Unlock(ctx, path, cfg.Ask)
			if err != nil {
				return nil, fmt.Errorf("remote: private key %s: %w", path, err)
			}
			return []ssh.Signer{signer}, nil
		}))
	}
	a.methods = append(a.methods,
		ssh.KeyboardInteractive(keyboardInteractive(ctx, cfg.Ask)),
		ssh.PasswordCallback(func() (string, error) {
			return cfg.Ask.Password(ctx, cfg.User, cfg.Host)
		}))
	return a, nil
}

// close lets go of the agent socket, for a connection that never
// happened.
func (a *auth) close() {
	if a != nil && a.agent != nil {
		_ = a.agent.Close()
	}
}

// keyboardInteractive answers whatever the server decided to ask, which
// is usually a one-time code.
func keyboardInteractive(ctx context.Context, ask Ask) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, prompts []string, echo []bool) ([]string, error) {
		if len(prompts) == 0 {
			// The server is telling the user something rather than
			// asking, so there is nothing to answer.
			return nil, nil
		}
		return ask.Question(ctx, Question{
			Name:        name,
			Instruction: instruction,
			Prompts:     prompts,
			Echo:        echo,
		})
	}
}

// identities sorts the key files into the ones that can be read without
// asking and the ones that need a passphrase.
//
// A key file the caller named is one it asked for, so a failure to read
// it fails the connection. One of the defaults is skipped instead: most
// machines have only one of the three, and failing because a stale
// id_rsa is unreadable would be unhelpful when the agent holds the key
// that works.
func identities(cfg Config) (plain []ssh.Signer, locked []string, err error) {
	if cfg.NoIdentities {
		return nil, nil, nil
	}
	paths, named := cfg.Identities, true
	if len(paths) == 0 {
		named = false
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, fmt.Errorf("remote: no home directory to look for keys in: %w", err)
		}
		for _, name := range defaultIdentities {
			paths = append(paths, filepath.Join(home, ".ssh", name))
		}
	}

	for _, p := range paths {
		// Already unlocked, so it is among the ring's signers and needs
		// no method of its own.
		if cfg.Ring.Has(p) {
			continue
		}
		signer, needsPass, err := readIdentity(p)
		switch {
		case err == nil:
			plain = append(plain, signer)
		case needsPass:
			locked = append(locked, p)
		case named:
			return nil, nil, fmt.Errorf("remote: private key %s: %w", p, err)
		}
	}
	return plain, locked, nil
}

// readIdentity loads one private key, reporting separately that it is
// encrypted so the caller can decide when to ask.
func readIdentity(path string) (signer ssh.Signer, needsPass bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	signer, err = ssh.ParsePrivateKey(b)
	if err == nil {
		return signer, false, nil
	}
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		return nil, true, err
	}
	return nil, false, err
}

// currentUser returns the local account name, used as the SSH user when
// none was given.
func currentUser() (string, error) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username, nil
	}
	// os/user needs cgo or a readable passwd database; the environment
	// is a reasonable second source.
	for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v, nil
		}
	}
	return "", errors.New("no current user")
}
