package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/serve"
)

// Reach is everything reaching another gridterm window needs.
type Reach struct {
	Addr, KeyFile string
	Ring          *Ring
	Ask           Ask

	// Known says where the windows already reached are recorded, and
	// Agent where the keys an SSH agent holds come from. Given rather
	// than called for, so a test can hand over a file and an agent of
	// its own.
	Known func() (string, error)
	Agent func() ([]ssh.Signer, io.Closer, error)

	// Saying is told what is being done now, for the row to show. It is
	// called from the goroutine doing it, so it hands the work to
	// whatever draws. A nil one is not called.
	Saying func(what string)

	// Patience is how long the other end has to get through the
	// handshake. Zero asks the serve package for its own.
	Patience time.Duration
}

// say tells the row what is being done now.
func (r Reach) say(what string) {
	if r.Saying != nil {
		r.Saying(what)
	}
}

// stepKeys, stepConnect, stepGivingUp and stepConnected are what a
// connection says it is doing, in the order it does them.
const (
	stepKeys      = "finding a key to offer"
	stepConnect   = "connecting"
	stepGivingUp  = "giving up"
	stepConnected = "connected"
)

// ReachWindow does the part that must not run on the goroutine that
// draws: reading the disk, unlocking a key, and the handshake itself.
func ReachWindow(ctx context.Context, r Reach) (*serve.Window, error) {
	type answer struct {
		win *serve.Window
		err error
	}
	back := make(chan answer, 1)
	go func() {
		win, err := r.reach(ctx)
		back <- answer{win: win, err: err}
	}()
	select {
	case got := <-back:
		return got.win, got.err
	case <-ctx.Done():
		r.say(stepGivingUp)
		// Given up on. Every step of reaching a window is bounded, so
		// the one still running ends on its own; it is waited for here
		// rather than abandoned, because it may yet come back holding a
		// connection that nothing else would close.
		go func() {
			if got := <-back; got.win != nil {
				_ = got.win.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// reach makes the connection, step by step. It runs on a goroutine of
// ReachWindow's, which is what lets giving up be answered at once
// however far this has got.
func (r Reach) reach(ctx context.Context) (win *serve.Window, err error) {
	known, err := r.Known()
	if err != nil {
		return nil, err
	}
	r.say(stepKeys)
	keys, closer, err := keysFor(ctx, r.KeyFile, r.Ring, r.Ask, r.Agent, r.Saying)
	if err != nil {
		return nil, err
	}
	// Held open until the handshake is done. A signer the agent holds
	// signs over that socket, so closing it first leaves a key that can
	// be offered and cannot be used -- and one such key fails the whole
	// handshake, with the others never tried.
	if closer != nil {
		// A socket to the agent that will not close is worth saying out
		// loud: it is one of eight the agent will hold, and nothing else
		// here would ever mention it.
		defer func() { err = errors.Join(err, closer.Close()) }()
	}

	check, err := HostKeyCheck(ctx, []string{known}, r.Ask)
	if err != nil {
		return nil, err
	}
	r.say(stepConnect)
	win, err = serve.Dial(ctx, serve.DialConfig{
		Addr: r.Addr, Keys: keys, HostKey: check, Patience: r.Patience,
		Saying: r.Saying,
	})
	if err != nil {
		return nil, err
	}
	r.say(stepConnected)
	return win, nil
}

// keysFor is what to offer the other window: the key file named, if one
// was, and otherwise whatever is already unlocked and whatever the
// agent holds.
//
// The closer is the connection to the agent, which the caller closes
// once it has finished signing with what it was given. It is nil when
// there is nothing to close.
func keysFor(ctx context.Context, keyFile string, ring *Ring, ask Ask,
	fromAgent func() ([]ssh.Signer, io.Closer, error), say func(string)) ([]ssh.Signer, io.Closer, error) {

	if say == nil {
		say = func(string) {}
	}
	if keyFile != "" {
		say("unlocking " + keyFile)
		signer, err := ring.Unlock(ctx, keyFile, ask)
		if err != nil {
			return nil, nil, err
		}
		return []ssh.Signer{signer}, nil, nil
	}
	keys := ring.Signers()
	var closer io.Closer
	agentErr := ring.AgentTrouble()
	if agentErr != nil {
		say("leaving the SSH agent alone: " + agentErr.Error() +
			". Forget unlocked keys to have it asked again")
	} else {
		say("asking the SSH agent what keys it holds")
		var agentSigners []ssh.Signer
		agentSigners, closer, agentErr = fromAgent()
		switch {
		case errors.Is(agentErr, ErrAgentSilent):
			// Remembered, so the next window taken over does not wait
			// for the same answer. One that is not running at all is
			// not remembered: finding that out costs nothing.
			ring.AgentGaveUp(agentErr)
			say("the SSH agent: " + agentErr.Error())
		case agentErr != nil:
			say("the SSH agent: " + agentErr.Error())
		default:
			say(fmt.Sprintf("the agent holds %d keys", len(agentSigners)))
		}
		keys = append(keys, agentSigners...)
	}

	// And the key files in the usual places, which is what connecting to
	// a machine offers. A user with one key in ~/.ssh expects it to be
	// used either way.
	plain, locked, err := UsualKeys()
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, nil, err
	}
	say(fmt.Sprintf("%d private keys in the usual places need no passphrase, %d do",
		len(plain), len(locked)))
	keys = append(keys, plain...)
	if len(keys) > 0 {
		return keys, closer, nil
	}

	// Nothing that could be read without asking. Only now is a
	// passphrase worth asking for, and only for the first key: a machine
	// with three would otherwise ask three times for a window the first
	// one would have reached.
	if len(locked) > 0 && ask != nil {
		say("unlocking " + locked[0])
		signer, err := ring.Unlock(ctx, locked[0], ask)
		if err != nil {
			if closer != nil {
				_ = closer.Close()
			}
			return nil, nil, err
		}
		return []ssh.Signer{signer}, closer, nil
	}

	if closer != nil {
		_ = closer.Close()
	}
	if agentErr != nil {
		// Why there were none, which is not always "there is no
		// agent": one that answered and then failed is a different
		// thing to go and fix.
		return nil, nil, fmt.Errorf(
			"no keys to offer, and the SSH agent could not be read: %w", agentErr)
	}
	return nil, nil, errors.New(
		"no keys to offer: name a key file, put one in ~/.ssh, or add one to the SSH agent")
}
