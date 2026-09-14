package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// defaultIdentities are the key files tried when none were named.
var defaultIdentities = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

// Protocol names for the authentication methods, as RFC 4252 spells them.
// The server says which of these it will accept.
const (
	methodPublicKey = "publickey"
	methodKeyboard  = "keyboard-interactive"
	methodPassword  = "password"
)

// auth is what a connection will try, in order, and the agent socket it
// opened to do it.
type auth struct {
	ladder []rung
	agent  io.Closer

	// noAgent is why there is no agent, kept for the message shown when
	// nothing at all worked.
	noAgent error

	// at is how far up the ladder the connection has climbed.
	at int
}

// rung is one thing to try: a protocol method name, and how to build it
// when its turn comes.
//
// The method is built rather than kept, because building it is what
// opens a dialog, and a rung that is never reached must never ask the
// user anything.
type rung struct {
	method string
	build  func() ssh.AuthMethod
}

// next picks what to try after the last attempt failed.
//
// It exists because x/crypto will not do this for us: its own selection
// deduplicates by method name, so of several "publickey" methods only
// the first is ever tried. A machine with an agent and an encrypted key
// would offer the agent, be refused, and never try the key.
func (a *auth) next(ctx *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
	for a.at < len(a.ladder) {
		r := a.ladder[a.at]
		a.at++
		// Only what the server says it will accept. Offering a password
		// to a server that refuses passwords wastes an attempt and, with
		// a dialog behind it, asks the user for nothing.
		if !slices.Contains(ctx.AllowedMethods, r.method) {
			continue
		}
		return r.build(), nil
	}
	return nil, nil
}

// close lets go of the agent socket, for a connection that never
// happened.
func (a *auth) close() {
	if a != nil && a.agent != nil {
		_ = a.agent.Close()
	}
}

// authMethods assembles what to try, in the order a user expects.
//
// Keys that need no passphrase come first, all in one attempt: the ring,
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

	if agentSigners != nil || len(plain) > 0 || len(cfg.Ring.Paths()) > 0 {
		a.ladder = append(a.ladder, rung{method: methodPublicKey, build: func() ssh.AuthMethod {
			return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
				signers := cfg.Ring.Signers()
				if agentSigners != nil {
					got, err := agentSigners()
					if err != nil {
						return nil, fmt.Errorf("remote: read the SSH agent: %w", err)
					}
					signers = append(signers, got...)
				}
				return append(signers, plain...), nil
			})
		}})
	}

	if cfg.Ask == nil {
		return a, nil
	}
	for _, path := range locked {
		a.ladder = append(a.ladder, rung{method: methodPublicKey, build: func() ssh.AuthMethod {
			return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
				signer, err := cfg.Ring.Unlock(ctx, path, cfg.Ask)
				if err != nil {
					return nil, fmt.Errorf("remote: private key %s: %w", path, err)
				}
				return []ssh.Signer{signer}, nil
			})
		}})
	}
	a.ladder = append(a.ladder,
		rung{method: methodKeyboard, build: func() ssh.AuthMethod {
			return ssh.KeyboardInteractive(keyboardInteractive(ctx, cfg))
		}},
		rung{method: methodPassword, build: func() ssh.AuthMethod {
			return ssh.PasswordCallback(func() (string, error) {
				return cfg.Ask.Password(ctx, cfg.User, cfg.Host)
			})
		}},
	)
	return a, nil
}

// keyboardInteractive answers whatever the server decided to ask, which
// is usually a one-time code.
//
// Which machine is asking is added here rather than left to the server.
// Everything else in the dialog is the server's own wording, and a
// server that chose "Unlock a private key" and a plausible key path
// would otherwise produce a dialog indistinguishable from the local one
// -- and be handed the passphrase to the user's private key.
func keyboardInteractive(ctx context.Context, cfg Config) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, prompts []string, echo []bool) ([]string, error) {
		if len(prompts) == 0 {
			// Nothing to answer: the server is telling the user
			// something. It is how a server that signs people in
			// through a browser sends them the link, so the message is
			// shown and the empty answer goes back at once. The server
			// then holds the handshake open until the user has been
			// through it.
			cfg.Ask.Notice(ctx, Notice{
				User:        cfg.User,
				Host:        cfg.addr(),
				Name:        name,
				Instruction: instruction,
			})
			return nil, nil
		}
		return cfg.Ask.Question(ctx, Question{
			User:        cfg.User,
			Host:        cfg.addr(),
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
		// no rung of its own.
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

// bannerOf shows a server's banner, which is the other way a server
// tells someone how to sign in.
//
// Nil when there is nobody to tell: the banner is the server's own
// wording and there is nothing to do with it but show it.
func bannerOf(ctx context.Context, cfg Config) ssh.BannerCallback {
	if cfg.Ask == nil {
		return nil
	}
	return func(message string) error {
		if strings.TrimSpace(message) == "" {
			return nil
		}
		cfg.Ask.Notice(ctx, Notice{
			User: cfg.User,
			Host: cfg.addr(),
			Text: message,
		})
		return nil
	}
}
