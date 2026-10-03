package term

import (
	"sync/atomic"
	"testing"
	"time"
)

// Output that keeps coming in full reads is shown once a read comes
// back short, or once it has waited long enough, or as the session
// ends; output in short reads, as typing's echo, at once.
func TestABurstIsShownWhole(t *testing.T) {
	var told atomic.Int32
	b := &burst{arrived: func() { told.Add(1) }}

	b.wrote(false)
	if told.Load() != 1 {
		t.Fatal("a short read was not shown at once")
	}
	b.wrote(true)
	b.wrote(true)
	b.wrote(true)
	if told.Load() != 1 {
		t.Fatal("output in full reads was shown part way")
	}
	b.wrote(false)
	if told.Load() != 2 {
		t.Fatal("the short read ending a burst did not show it")
	}
	// The timer of the burst that ended does nothing.
	time.Sleep(2 * burstMost)
	if told.Load() != 2 {
		t.Fatalf("a burst already shown was shown again: %d", told.Load())
	}

	b.wrote(true)
	time.Sleep(2 * burstMost)
	if told.Load() != 3 {
		t.Fatal("a burst that never paused was not shown in time")
	}

	b.wrote(true)
	b.end()
	if told.Load() != 4 {
		t.Fatal("a burst held as the session ended was not shown")
	}
	b.end()
	if told.Load() != 4 {
		t.Fatal("ending with nothing held showed something")
	}
}
