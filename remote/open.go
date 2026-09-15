package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrNoAnswer is what an open fails with when the far machine takes the
// request and never answers it.
var ErrNoAnswer = errors.New("the machine did not answer")

// channelTimeout is how long the far machine has to answer a request to
// open something on it: a session, a subsystem, a listening port.
//
// It bounds one open as a whole, not one round trip. Starting a shell
// takes three of them, and a machine that answers two and stops on the
// third has taken the window just the same.
//
// It is not a bound on a terminal's reads and writes. A shell sits idle
// for hours and is still working, so nothing on that path times out.
// This covers the round trips that set one up, which run on the
// goroutine that draws: a machine that is still on the network but no
// longer answering would otherwise stop the whole window.
var channelTimeout = 20 * time.Second

// opened is what an open produced, so the goroutine running it can hand
// back both parts at once.
type opened[T any] struct {
	v   T
	err error
}

// openWithin runs a request that opens something on the far machine and
// gives up when ctx does.
//
// The request runs on its own goroutine because x/crypto takes no
// context for a channel open. what names the operation and the machine,
// which is what the error has to say.
func openWithin[T io.Closer](ctx context.Context, what string, open func() (T, error)) (T, error) {
	var zero T
	done := make(chan opened[T], 1)
	go func() {
		v, err := open()
		done <- opened[T]{v: v, err: err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			return zero, fmt.Errorf("remote: %s: %w", what, r.err)
		}
		return r.v, nil
	case <-ctx.Done():
		letGoOfOpen(done)
		return zero, gaveUp(ctx, what)
	}
}

// doWithin is openWithin for a request that answers with nothing but
// success or failure, so there is nothing to close when it arrives late.
//
// The goroutine left behind by a request given up on is waiting for a
// reply, and is freed only when the caller closes the session or channel
// the request was made on.
func doWithin(ctx context.Context, what string, do func() error) error {
	done := make(chan error, 1)
	go func() { done <- do() }()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("remote: %s: %w", what, err)
		}
		return nil
	case <-ctx.Done():
		return gaveUp(ctx, what)
	}
}

// gaveUp says why the wait ended: the machine had its time and said
// nothing, or the caller gave up first.
func gaveUp(ctx context.Context, what string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("remote: %s: %w within %s", what, ErrNoAnswer, channelTimeout)
	}
	return fmt.Errorf("remote: %s: %w", what, ctx.Err())
}

// letGoOfOpen closes whatever an open produces once it arrives, for a
// caller that stopped waiting: a machine that answers after the user has
// given up still leaves a channel open on itself.
//
// Nothing is reported, because by then there is no caller left to report
// it to.
func letGoOfOpen[T io.Closer](done <-chan opened[T]) {
	go func() {
		if r := <-done; r.err == nil {
			_ = r.v.Close()
		}
	}()
}
