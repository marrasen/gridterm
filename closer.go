package main

import (
	"sync"
	"time"
)

// closer is the filesystems being let go of on goroutines of their own,
// and what those attempts reported.
//
// The goroutine that draws starts them and waits for them on the way
// out; the goroutines it starts report their failures here.
type closer struct {
	// running counts the closes still going.
	running sync.WaitGroup

	mu   sync.Mutex
	errs []error
}

// inBackground runs a close that cannot be waited for here on a
// goroutine of its own, and keeps what it reports.
func (c *closer) inBackground(letGo func() error) {
	// Counted before the goroutine starts, so a window closing in the
	// same frame still waits for it.
	c.running.Add(1)
	go func() {
		defer c.running.Done()
		if err := letGo(); err != nil {
			c.failed(err)
		}
	}()
}

// failed records a failure from a goroutine letting go of a filesystem.
// It runs off the drawing goroutine, so it leaves the failure where the
// window will find it rather than showing it here.
func (c *closer) failed(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, err)
}

// reported hands back what the closes have reported and forgets them, so
// each failure is shown once.
func (c *closer) reported() []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.errs
	c.errs = nil
	return out
}

// waitFor waits for the filesystems still being let go of, for the
// window on its way out.
//
// Bounded, for the same reason the file work is: cancelling cannot
// interrupt a read already under way, so a filesystem on a machine that
// has stopped answering is left rather than waited for. It hands back
// whatever the closes reported, including the ones that finished while
// it waited.
func (c *closer) waitFor(d time.Duration) []error {
	done := make(chan struct{})
	go func() {
		c.running.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
	return c.reported()
}
