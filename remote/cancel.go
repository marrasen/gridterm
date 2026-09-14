package remote

import (
	"context"
	"sync"
)

// cancelAsk stops the connection the moment the user says no.
//
// x/crypto treats a failing authentication method as one more thing that
// did not work and moves on to the next. Without this, dismissing the
// passphrase dialog would be answered by the password dialog: a second
// question about a decision the user has just made. Cancelling the
// context closes the connection under the handshake, which is the only
// way to stop it from outside.
//
// The first reason is kept, because it is the one worth showing. What
// x/crypto reports afterwards is only that nothing worked.
type cancelAsk struct {
	ask    Ask
	cancel context.CancelFunc

	mu  sync.Mutex
	err error
}

// newCancelAsk wraps an Ask, or returns nil when there is nothing to
// wrap: a nil Ask means nothing may be asked at all.
func newCancelAsk(ask Ask, cancel context.CancelFunc) *cancelAsk {
	if ask == nil {
		return nil
	}
	return &cancelAsk{ask: ask, cancel: cancel}
}

// asker returns the Ask to hand around, as an interface so a nil wrapper
// stays a nil Ask rather than becoming a non-nil interface holding nil.
func (a *cancelAsk) asker() Ask {
	if a == nil {
		return nil
	}
	return a
}

// reason returns why the user stopped, or nil when they did not.
func (a *cancelAsk) reason() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// stopped reports the refusal already recorded, so nothing else is
// asked.
//
// Cancelling the context is not enough on its own: closing the
// connection takes effect on another goroutine, and x/crypto can reach
// the next method first. Refusing here is what makes "no second
// question" a guarantee rather than a race.
func (a *cancelAsk) stopped() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// fail records the first refusal and gives up on the connection.
func (a *cancelAsk) fail(err error) error {
	a.mu.Lock()
	if a.err == nil {
		a.err = err
	}
	a.mu.Unlock()
	a.cancel()
	return err
}

func (a *cancelAsk) Passphrase(ctx context.Context, keyfile string) (string, error) {
	if err := a.stopped(); err != nil {
		return "", err
	}
	s, err := a.ask.Passphrase(ctx, keyfile)
	if err != nil {
		return "", a.fail(err)
	}
	return s, nil
}

func (a *cancelAsk) Password(ctx context.Context, user, host string) (string, error) {
	if err := a.stopped(); err != nil {
		return "", err
	}
	s, err := a.ask.Password(ctx, user, host)
	if err != nil {
		return "", a.fail(err)
	}
	return s, nil
}

func (a *cancelAsk) Question(ctx context.Context, q Question) ([]string, error) {
	if err := a.stopped(); err != nil {
		return nil, err
	}
	answers, err := a.ask.Question(ctx, q)
	if err != nil {
		return nil, a.fail(err)
	}
	return answers, nil
}

func (a *cancelAsk) TrustHostKey(ctx context.Context, key HostKey) (bool, error) {
	if err := a.stopped(); err != nil {
		return false, err
	}
	ok, err := a.ask.TrustHostKey(ctx, key)
	if err != nil {
		return false, a.fail(err)
	}
	return ok, nil
}
