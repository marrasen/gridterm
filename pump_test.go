package main

import (
	"sync"
	"testing"
)

func TestPumpRunsInTheOrderPosted(t *testing.T) {
	var p pump
	var got []int
	for i := 0; i < 5; i++ {
		p.post(func() { got = append(got, i) })
	}
	p.run()

	if len(got) != 5 {
		t.Fatalf("ran %d of 5", len(got))
	}
	for i, n := range got {
		if n != i {
			t.Fatalf("ran %v, want them in order", got)
		}
	}
}

// Work posted by work being done waits for the next frame. Running it
// straight away would let one frame grow without bound.
func TestPumpDefersWorkPostedWhileRunning(t *testing.T) {
	var p pump
	ran := 0
	p.post(func() {
		ran++
		p.post(func() { ran++ })
	})

	p.run()
	if ran != 1 {
		t.Fatalf("ran %d in the first pass, want 1", ran)
	}
	if p.pending() != 1 {
		t.Fatalf("%d pieces of work waiting, want the one that was posted", p.pending())
	}
	p.run()
	if ran != 2 {
		t.Fatalf("ran %d after the second pass, want 2", ran)
	}
}

// Nothing is dropped. A connection waiting to ask for a passphrase would
// hang if its message went missing, so the queue grows instead of having
// a depth to overflow.
func TestPumpKeepsEverythingFromEveryGoroutine(t *testing.T) {
	var p pump
	const goroutines, each = 8, 64

	var wg sync.WaitGroup
	var mu sync.Mutex
	ran := 0
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				p.post(func() {
					mu.Lock()
					ran++
					mu.Unlock()
				})
			}
		}()
	}
	wg.Wait()

	p.run()
	if ran != goroutines*each {
		t.Fatalf("ran %d of %d", ran, goroutines*each)
	}
}

func TestPumpIgnoresNothingToDo(t *testing.T) {
	var p pump
	p.post(nil)
	if p.pending() != 0 {
		t.Fatal("a nil function was queued")
	}
	p.run()
}
