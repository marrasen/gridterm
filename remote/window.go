package remote

import (
	"context"
	"errors"
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

	// Known says where the windows already reached are recorded. Given
	// rather than looked up, so a test can hand over a file of its own.
	Known func() (string, error)

	// Agent is where the keys an SSH agent holds come from. A nil one
	// asks the agent running on this machine; it is here so that a test
	// can hand over an agent of its own.
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
	cfg := Config{Ring: r.Ring, Ask: r.Ask, Saying: r.Saying, KeysOnly: true, agent: r.agentSource()}
	if r.KeyFile != "" {
		cfg.Identities = []string{r.KeyFile}
	}
	a, err := authMethods(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Held open until the handshake is done. A signer the agent holds
	// signs over that socket, so closing it first leaves a key that can
	// be offered and cannot be used -- and one such key fails the whole
	// handshake, with the others never tried.
	defer func() {
		a.done()
		if closer := a.agentCloser(); closer != nil {
			// A socket to the agent that will not close is worth saying
			// out loud: it is one of eight the agent will hold, and
			// nothing else here would ever mention it.
			err = errors.Join(err, closer.Close())
		}
	}()
	if len(a.ladder) == 0 {
		return nil, noKeysToOffer(a.noAgent)
	}

	check, err := hostKeyCheck(ctx, []string{known}, r.Ask)
	if err != nil {
		return nil, err
	}
	r.say(stepConnect)
	win, err = serve.Dial(ctx, serve.DialConfig{
		Addr: r.Addr, Auth: a.next, HostKey: check, Patience: r.Patience,
		Saying: r.Saying,
	})
	if err != nil {
		return nil, err
	}
	r.say(stepConnected)
	return win, nil
}

// agentSource turns the agent the caller handed over into the source the
// ladder opens. It is nil when the caller named none, which leaves the
// ladder to open the agent running on this machine.
func (r Reach) agentSource() agentSource {
	if r.Agent == nil {
		return nil
	}
	return func() (io.Closer, func() ([]ssh.Signer, error), error) {
		keys, closer, err := r.Agent()
		if err != nil {
			return nil, nil, err
		}
		return closer, func() ([]ssh.Signer, error) { return keys, nil }, nil
	}
}
