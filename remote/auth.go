package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	// agentClient talks to the agent over the socket above.
	agentClient agent.Agent

	// watching counts the watches set on the agent, so one that fires
	// just as it is replaced knows it is talking about something that
	// has already happened. Stopping a timer does not stop a run of it
	// that has already started.
	watching int

	// saying is told which way of signing in is being tried, so somebody
	// watching a connection that stops can see where it stopped. A nil
	// one is not called.
	saying func(string)

	// wrong is told the steps that did not go well, which a window shows
	// differently from the ones that did. A nil one falls back to
	// saying: the line is worth more than the colour it is drawn in.
	wrong func(string)

	// keyErr is the first key that could not be offered -- a passphrase
	// that did not unlock it, most often.
	//
	// Kept because x/crypto keeps only the last thing that did not work.
	// A wrong passphrase followed by a server refusing everything else
	// is reported as "no supported methods remain", which says nothing
	// about the passphrase, and a wrong passphrase followed by a
	// password that works is reported as nothing at all.
	keyErr error

	// ring is where an agent that did not answer is remembered, so the
	// next connection does not wait to find out again.
	ring *Ring

	// noAgent is why there is no agent, kept for the message shown when
	// nothing at all worked. agentBroke says it was reached and then
	// would not answer, as against not running at all.
	noAgent    error
	agentBroke bool

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

	// agent says this rung signs with the keys the SSH agent holds, so
	// the window stops watching the agent once it is past this one.
	agent bool

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
		if !r.agent {
			// Past the agent, so a watch on it is watching nothing. The
			// rungs after this one can wait on the user for as long as
			// they like.
			a.done()
		}
		saySo(a.saying, "trying "+r.what)
		return r.build(), nil
	}
	sayWrong(a.wrong, a.saying, "there is nothing left to sign in with")
	return nil, nil
}

// agentTrouble is the agent's own failure, for a connection that found
// nothing to offer. It is nil when there is simply no agent: a user sent
// to add a key to one would be sent after a fault that is not there.
func (a *auth) agentTrouble() error {
	if !a.agentBroke {
		return nil
	}
	return a.noAgent
}

// keyFailed records a key that could not be offered and says so out
// loud, because nothing downstream will: x/crypto treats it as one more
// thing that did not work and moves on to the next.
func (a *auth) keyFailed(err error) {
	a.mu.Lock()
	if a.keyErr == nil {
		a.keyErr = err
	}
	a.mu.Unlock()
	sayWrong(a.wrong, a.saying, err.Error())
}

// keyTrouble returns the first key that could not be offered, or nil
// when every key that was tried was at least offered.
func (a *auth) keyTrouble() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.keyErr
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
	a.watching++
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
	if a.agent == nil || a.agentGone {
		a.mu.Unlock()
		return nil
	}
	a.agentGone = true
	agent := a.agent
	a.mu.Unlock()
	// Outside the lock: closing a pipe somebody is reading can wait for
	// that read to be torn down, and everything else here would wait
	// with it.
	return agent.Close()
}

// weLetGoOfTheAgent reports whether the socket was closed from this
// side, so a read that failed because of that is not blamed on the
// agent.
func (a *auth) weLetGoOfTheAgent() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.agentGone
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
// past it in the time given. Closing it is what unblocks the agent, and
// nothing else will.
//
// blame says to remember the agent as one that will not answer, so
// later connections do not wait for it. Only the listing is worth
// remembering: it is a read from memory, and an agent that will not do
// that will not do it next time either. Signing waits on a person, and
// somebody slow to touch a key is not a broken agent.
func (a *auth) holdTheAgentTo(patience time.Duration, waitingFor, hint string, blame bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.agent == nil {
		return
	}
	a.watching++
	mine := a.watching
	if a.watch != nil {
		a.watch.Stop()
	}
	a.watch = time.AfterFunc(patience, func() {
		a.mu.Lock()
		if mine != a.watching || a.agent == nil || a.agentGone {
			// Replaced or stopped while this was starting. Whatever it
			// was waiting for has happened.
			a.mu.Unlock()
			return
		}
		// Marked gone under the same lock as the check above, so a
		// connection taking the agent over sees one answer or the other
		// and never a socket that is about to close.
		a.agentGone = true
		sock := a.agent
		a.mu.Unlock()

		why := fmt.Errorf("the SSH agent had %v %s and did not", patience, waitingFor)
		sayWrong(a.wrong, a.saying, why.Error()+", so this is letting go of it."+hint)
		if blame {
			// Remembered, so the next connection is not held up finding
			// out the same thing again.
			a.ring.AgentGaveUp(why)
		}
		// Outside the lock: closing a pipe somebody is reading can wait
		// for that read to be torn down.
		_ = sock.Close()
	})
}

// closerFunc makes a function into an io.Closer.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// agentSource opens the SSH agent: the socket to hold until signing is
// done, and the agent speaking over it.
type agentSource func() (io.Closer, agent.Agent, error)

// localAgent opens the SSH agent running on this machine.
func localAgent() (io.Closer, agent.Agent, error) {
	conn, err := dialAgent()
	if err != nil {
		return nil, nil, err
	}
	return conn, agent.NewClient(conn), nil
}

// forwardingAgent is the agent to carry to the far end, and nil when
// there is none to carry: no agent was opened, or the socket was let go
// of because it would not answer.
func (a *auth) forwardingAgent() agent.Agent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.agentGone {
		return nil
	}
	return a.agentClient
}

// noAgentToCarry says why there is no SSH agent to carry to the far end.
func (a *auth) noAgentToCarry(cfg Config) error {
	a.mu.Lock()
	gone := a.agentGone
	a.mu.Unlock()
	switch {
	case cfg.NoAgent:
		return errors.New("the SSH agent is turned off for this connection")
	case a.agentBroke:
		return fmt.Errorf("the SSH agent was given up on earlier: %w."+
			" Forget unlocked keys to have it asked again", a.noAgent)
	case a.noAgent != nil:
		return fmt.Errorf("there is no SSH agent running here: %w", a.noAgent)
	case gone:
		return errors.New("the SSH agent stopped answering while this connection was being made")
	}
	return errors.New("there is no SSH agent to carry")
}

// authMethods assembles what to try, in the order a user expects.
//
// Keys that need no passphrase come first, all in one attempt: the ring,
// the agent, and any key file that is not encrypted. Only if the server
// refuses all of those does anything prompt, and then one encrypted key
// at a time, so a machine with three keys does not ask three times for a
// connection the first one would have made.
func authMethods(ctx context.Context, cfg Config) (*auth, error) {
	a := &auth{saying: cfg.Saying, wrong: cfg.Wrong, ring: cfg.Ring}

	var agentSigners func() ([]ssh.Signer, error)
	switch why := cfg.Ring.AgentTrouble(); {
	case cfg.NoAgent:
		saySo(cfg.Saying, "the SSH agent is not being used for this one")
	case why != nil:
		// Asked once already and it did not answer. Asking again would
		// cost this connection the same wait for the same answer.
		a.noAgent, a.agentBroke = why, true
		saySo(cfg.Saying, "leaving the SSH agent alone: "+why.Error()+
			". Forget unlocked keys to have it asked again")
	default:
		open := cfg.agent
		if open == nil {
			open = localAgent
		}
		conn, ag, err := open()
		if err != nil {
			// Not remembered: finding out there is no agent is opening a
			// socket that is not there, which costs nothing. One started
			// after this connection is found by the next one.
			a.noAgent = err
			saySo(cfg.Saying, "there is no SSH agent here: "+err.Error())
		} else {
			a.agent, a.agentClient = conn, ag
			agentSigners = ag.Signers
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
		saySo(cfg.Saying, fmt.Sprintf("private keys with no passphrase: %d; "+
			"private keys that need one: %d", len(plain), len(locked)))
	}

	// The keys this window already has, first and separately from the
	// agent's. An agent that will not answer must not take the key files
	// down with it: they are what would have worked.
	//
	// A nil ring holds nothing, which is how NoRing leaves the keys
	// already unlocked out without leaving out the key files.
	ring := cfg.Ring
	if cfg.NoRing {
		ring = nil
	}
	if len(plain) > 0 || len(ring.Paths()) > 0 {
		a.ladder = append(a.ladder, rung{method: methodPublicKey, what: "the keys already to hand", build: func() ssh.AuthMethod {
			return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
				signers := append(ring.Signers(), plain...)
				saySo(cfg.Saying, fmt.Sprintf("offering %d keys of its own", len(signers)))
				return signers, nil
			})
		}})
	}

	if agentSigners != nil {
		a.ladder = append(a.ladder, rung{method: methodPublicKey, agent: true,
			what: "the keys the SSH agent holds", build: func() ssh.AuthMethod {
				return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
					// Listing is a read from memory, so it gets little
					// time, and an agent that will not do it is one to
					// stop waiting for.
					a.holdTheAgentTo(agentListing, "to say what keys it holds", "", true)
					saySo(cfg.Saying, "asking the SSH agent what keys it holds")
					got, err := agentSigners()
					if err != nil {
						// Said as well as returned: x/crypto keeps only
						// the last attempt's error, so this one would be
						// lost behind whatever the server says at the
						// end.
						sayWrong(cfg.Wrong, cfg.Saying, "the SSH agent: "+err.Error())
						if !a.weLetGoOfTheAgent() {
							// Only when the agent is what went wrong.
							// The read also fails when this end closes
							// the socket, because the user gave up or
							// because the watch ran out and has already
							// said why, and the agent is not to blame
							// for either.
							cfg.Ring.AgentGaveUp(err)
						}
						return nil, fmt.Errorf("remote: read the SSH agent: %w", err)
					}
					saySo(cfg.Saying, fmt.Sprintf(
						"the agent holds %d keys, and they are being offered", len(got)))
					// Signing is allowed to be slow, and happens after
					// this returns. Nothing is remembered about it: a
					// key on a hardware token waits for somebody to
					// touch it, and somebody slow is not a broken agent.
					a.holdTheAgentTo(agentGrace, "to sign",
						" It may be waiting for you to touch a key or type a PIN.", false)
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
					err = fmt.Errorf("remote: private key %s: %w", path, err)
					// Said and kept here. Returning it only would lose
					// it: x/crypto counts a key that could not be
					// offered as one more thing that did not work, and
					// carries on to the next way of signing in without
					// a word about it.
					//
					// Unless the connection was given up on, which is
					// what a dismissed dialog does. That is the user's
					// own decision, and the account of it is "given up
					// on" rather than a key that went wrong.
					if ctx.Err() == nil {
						a.keyFailed(err)
					}
					return nil, err
				}
				return []ssh.Signer{signer}, nil
			})
		}})
	}
	if cfg.KeysOnly {
		return a, nil
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
// A key file the caller named is one it asked for, so anything wrong
// with it fails the connection.
//
// One of the defaults is different. Most machines have only one of the
// three, so a name that is not there is skipped, and so is one that is
// there and is not a key this understands: a stale id_rsa should not
// stop a connection the agent holds the key for. A default that is
// there and cannot be read is neither of those. It fails, because a
// connection that quietly offers fewer keys ends in "no supported
// methods remain" with nothing saying why.
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
		// no rung of its own. Not so when the ring is being left out:
		// then this is the only place the key can come from.
		if !cfg.NoRing && cfg.Ring.Has(p) {
			continue
		}
		signer, needsPass, read, err := readIdentity(p)
		switch {
		case err == nil:
			plain = append(plain, signer)
		case needsPass:
			locked = append(locked, p)
		case named:
			return nil, nil, fmt.Errorf("remote: private key %s: %w", p, err)
		case !read && !errors.Is(err, fs.ErrNotExist):
			// One of the usual names that is there and cannot be read.
			// Not having a key is a reason to try another method;
			// not being able to tell is not, and quietly offering fewer
			// keys ends in "no supported methods remain" with nothing
			// saying why.
			return nil, nil, fmt.Errorf("remote: private key %s: %w", p, err)
		}
	}
	return plain, locked, nil
}

// readIdentity loads one private key.
//
// needsPass reports separately that it is encrypted, so the caller can
// decide when to ask. read reports that the file itself was read: a
// file that is there and is not a key this understands is a different
// thing from one that could not be got at, and only the caller knows
// which of the two it may skip.
func readIdentity(path string) (signer ssh.Signer, needsPass, read bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false, false, err
	}
	signer, err = ssh.ParsePrivateKey(b)
	if err == nil {
		return signer, false, true, nil
	}
	if _, ok := errors.AsType[*ssh.PassphraseMissingError](err); ok {
		return nil, true, true, err
	}
	return nil, false, true, err
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

// agentListing is how long the agent has to say what keys it holds,
// which it reads from memory.
//
// agentGrace is how long it then has to sign with one. Long, because
// signing is allowed to be slow: a key on a hardware token waits for
// somebody to touch it, and one on a smartcard waits for a PIN.
//
// Variables so the tests can shorten them. Nothing in the program writes
// them.
var (
	agentListing = 10 * time.Second
	agentGrace   = 60 * time.Second
)
