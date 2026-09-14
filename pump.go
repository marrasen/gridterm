package main

import "sync"

// pump carries work from the goroutines doing network I/O to the one
// drawing, which is the only one that may touch the widget tree.
//
// Nothing is dropped and nothing blocks. A connection waiting to ask for
// a passphrase would hang if its message were dropped, and a network
// goroutine must never wait on the drawing goroutine, so the queue grows
// instead of having a depth to overflow.
type pump struct {
	mu     sync.Mutex
	queued []func()
}

// post adds work to run on the next frame. It is safe to call from any
// goroutine, including the drawing one.
func (p *pump) post(fn func()) {
	if fn == nil {
		return
	}
	p.mu.Lock()
	p.queued = append(p.queued, fn)
	p.mu.Unlock()
}

// run does the work that has been posted.
//
// The queue is taken before anything runs, so work posted by the work
// being done waits for the next frame rather than extending this one.
func (p *pump) run() {
	p.mu.Lock()
	queued := p.queued
	p.queued = nil
	p.mu.Unlock()

	for _, fn := range queued {
		fn()
	}
}

// pending reports how much work is waiting, for a test that has to know
// the queue has drained.
func (p *pump) pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queued)
}
