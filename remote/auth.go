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
	"sync"
	"time"

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

	// mu guards the agent socket and the watch on it, which a timer
	// goroutine closes and the connecting goroutine closes too.
	mu        sync.Mutex
	agent     io.Closer
	agentGone bool
	watch     *time.Timer

	// saying is told which way of signing in is being tried, so somebody
	// watching a connection that stops can see where it stopped. A nil
	// one is not called.
	saying func(string)

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

	// what names this way of signing in the way a person would, for the
	// account of the connection the window shows.
	what string

	build func() ssh.AuthMethod
}

// next picks what to try after the last attempt failed.
//
// It exists because x/crypto will not do this for us: its own selection
// deduplicates by method name, so of several "publickey" methods only
// the first is ever tried. A machine with an agent and an encrypted key
// would offer the agent, be refused, and never try the key.
func (a *auth) next(ctx *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
	if a.at == 0 {
		saySo(a.saying, "it will accept "+strings.Join(ctx.AllowedMethods, ", "))
	}
	for a.at < len(a.ladder) {
		r := a.ladder[a.at]
		a.at++
		// Only what the server says it will accept. Offering a password
		// to a server that refuses passwords wastes an attempt and, with
		// a dialog behind it, asks the user for nothing.
		if !slices.Contains(ctx.AllowedMethods, r.method) {
			continue
		}
		saySo(a.saying, "trying "+r.what)
		return r.build(), nil
	}
	saySo(a.saying, "there is nothing left to sign in with")
	return nil, nil
}

// close lets go of the agent socket, for a connection that never
// happened.
func (a *auth) close() {
	if a == nil {
		return
	}
	a.done()
	_ = a.closeAgent()
}

// done stops watching the agent, for a connection that got past it.
func (a *auth) done() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.watch != nil {
		a.watch.Stop()
		a.watch = nil
	}
}

// closeAgent closes the agent socket, once, whoever gets there first.
//
// Both the connection and the watch on the agent let go of it, and an
// os.File closed twice reports a failure that means nothing.
func (a *auth) closeAgent() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.agent == nil || a.agentGone {
		return nil
	}
	a.agentGone = true
	return a.agent.Close()
}

// agentCloser hands the agent socket to the connection without letting
// it be closed twice. It is nil when there is no agent.
func (a *auth) agentCloser() io.Closer {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.agent == nil {
		return nil
	}
	return closerFunc(a.closeAgent)
}

// holdTheAgentTo closes the agent socket if the connection has not got
// past it in the time given.
//
// Closing it is what unblocks the agent, and nothing else will. Listing
// keys is a read from memory, but signing with one can go to a smartcard
// and stop there for a PIN in a window nobody is looking at, which is
// how a connection came to hang with no way to tell what it was waiting
// for.
func (a *auth) holdTheAgentTo(patience time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.agent == nil || a.watch != nil {
		return
	}
	a.watch = time.AfterFunc(patience, func() {
		saySo(a.saying, fmt.Sprintf(
			"the SSH agent has not answered in %v, so this is letting go of it", patience))
		_ = a.closeAgent()
	})
}

// closerFunc makes a function into an io.Closer.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// authMethods assembles what to try, in the order a user expects.
//
// Keys that need no passphrase come first, all in one attempt: the ring,
// the agent, and any key file that is not encrypted. Only if the server
// refuses all of those does anything prompt, and then one encrypted key
// at a time, so a machine with three keys does not ask three times for a
// connection the first one would have made.
func authMethods(ctx context.Context, cfg Config) (*auth, error) {
	a := &auth{saying: cfg.Saying}

	var agentSigners func() ([]ssh.Signer, error)
	if cfg.NoAgent {
		saySo(cfg.Saying, "the SSH agent is not being used for this one")
	} else {
		conn, err := dialAgent()
		if err != nil {
			a.noAgent = err
			saySo(cfg.Saying, "there is no SSH agent here: "+err.Error())
		} else {
			a.agent = conn
			agentSigners = agent.NewClient(conn).Signers
			saySo(cfg.Saying, "an SSH agent is running")
		}
	}

	plain, locked, err := identities(cfg)
	if err != nil {
		a.close()
		return nil, err
	}
	if cfg.NoIdentities {
		saySo(cfg.Saying, "no private key files are being looked at for this one")
	} else {
		saySo(cfg.Saying, fmt.Sprintf("%d private keys need no passphrase, %d do",
			len(plain), len(locked)))
	}

	// The keys this window already has, first and separately from the
	// agent's. An agent that will not answer must not take the key files
	// down with it: they are what would have worked.
	if len(plain) > 0 || len(cfg.Ring.Paths()) > 0 {
		a.ladder = append(a.ladder, rung{method: methodPublicKey, what: "the keys already to hand", build: func() ssh.AuthMethod {
			return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
				signers := append(cfg.Ring.Signers(), plain...)
				saySo(cfg.Saying, fmt.Sprintf("offering %d keys of its own", len(signers)))
				return signers, nil
			})
		}})
	}

	if agentSigners != nil {
		a.ladder = append(a.ladder, rung{method: methodPublicKey, what: "the keys the SSH agent holds", build: func() ssh.AuthMethod {
			// From here until the connection is made or fails, the agent
			// has this long to answer -- the signing as well as the
			// listing, because signing is the half that can go to a
			// smartcard and stop.
			a.holdTheAgentTo(agentGrace)
			return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
				saySo(cfg.Saying, "asking the SSH agent what keys it holds")
				got, err := agentSigners()
				if err != nil {
					// Said as well as returned: x/crypto keeps only the
					// last attempt's error, so this one would be lost
					// behind whatever the server says at the end.
					saySo(cfg.Saying, "the SSH agent: "+err.Error())
					return nil, fmt.Errorf("remote: read the SSH agent: %w", err)
				}
				saySo(cfg.Saying, fmt.Sprintf("the agent holds %d keys, and they are being offered", len(got)))
				return got, nil
			})
		}})
	}

	if cfg.Ask == nil {
		return a, nil
	}
	for _, path := range locked {
		a.ladder = append(a.ladder, rung{method: methodPublicKey, what: "the private key " + path, build: func() ssh.AuthMethod {
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
		rung{method: methodKeyboard, what: "whatever the server asks", build: func() ssh.AuthMethod {
			return ssh.KeyboardInteractive(keyboardInteractive(ctx, cfg))
		}},
		rung{method: methodPassword, what: "a password", build: func() ssh.AuthMethod {
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

// AgentKeys are the keys the SSH agent holds, and the closer for the
// connection to it.
//
// Exported for one gridterm reaching another, which offers the same
// keys the user would reach any other machine with. The caller closes
// what it is given; a nil closer means there was no agent.
func AgentKeys() (keys []ssh.Signer, closer io.Closer, err error) {
	conn, err := dialAgent()
	if err != nil {
		return nil, nil, fmt.Errorf("remote: no SSH agent: %w", err)
	}
	type answer struct {
		keys []ssh.Signer
		err  error
	}
	back := make(chan answer, 1)
	go func() {
		keys, err := agent.NewClient(conn).Signers()
		back <- answer{keys: keys, err: err}
	}()
	select {
	case got := <-back:
		if got.err != nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("remote: read the SSH agent: %w", got.err)
		}
		return got.keys, conn, nil
	case <-time.After(agentPatience):
		// Closing it is what unblocks the read, so the goroutine above
		// ends rather than being left holding the connection.
		_ = conn.Close()
		return nil, nil, fmt.Errorf(
			"remote: the SSH agent did not answer within %v", agentPatience)
	}
}

// agentPatience is how long the SSH agent has to say what it holds.
//
// It answers from memory, so this is long only by the standards of that:
// it is here because an agent that has wedged would otherwise stop a
// connection for ever, with nothing saying why.
const agentPatience = 5 * time.Second

// agentGrace is how long the agent has to get a connection past it,
// listing its keys and signing with one.
//
// Long, because signing is allowed to be slow: a key on a hardware token
// waits for the user to touch it, and one on a smartcard waits for a
// PIN. Short enough that a question nobody is going to answer does not
// hold the connection for the rest of the day.
//
// A variable because the tests shorten it. Nothing in the program writes
// it.
var agentGrace = 60 * time.Second
