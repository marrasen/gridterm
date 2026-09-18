package meter

import (
	"sync"
	"testing"
	"time"
)

// at is a fixed moment to measure from, so nothing here waits.
var at = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// The rule the panel is built on: opened until something moves, active
// for four seconds after it, settled after that, and closed at the end.
func TestStateFollowsWhatHasMoved(t *testing.T) {
	m := New()

	if got := m.StateAt(at); got != Opened {
		t.Fatalf("a new meter is %v, want opened", got)
	}

	m.Moved(64, 0, at)
	if got := m.StateAt(at); got != Active {
		t.Fatalf("just after a byte it is %v, want active", got)
	}
	// Still active a moment before the boundary.
	if got := m.StateAt(at.Add(Settle - time.Nanosecond)); got != Active {
		t.Fatalf("just before the boundary it is %v, want active", got)
	}
	// And settled at it.
	if got := m.StateAt(at.Add(Settle)); got != Settled {
		t.Fatalf("at the boundary it is %v, want settled", got)
	}
	if got := m.StateAt(at.Add(time.Hour)); got != Settled {
		t.Fatalf("an hour later it is %v, want settled", got)
	}

	// Another byte makes it active again, which is what a build printing
	// a line every few seconds looks like.
	m.Moved(0, 1, at.Add(time.Hour))
	if got := m.StateAt(at.Add(time.Hour)); got != Active {
		t.Fatalf("after another byte it is %v, want active", got)
	}

	m.Close()
	if got := m.StateAt(at.Add(time.Hour)); got != Closed {
		t.Fatalf("after closing it is %v, want closed", got)
	}
	// Closed stays closed, whatever the clock says.
	if got := m.StateAt(at); got != Closed {
		t.Fatalf("a closed meter at an earlier moment is %v, want closed", got)
	}
}

// A read that returned nothing must not keep a connection looking busy:
// a reader polling an idle shell would hold it active for ever.
func TestMovingNothingChangesNothing(t *testing.T) {
	m := New()
	m.Moved(0, 0, at)
	if got := m.StateAt(at); got != Opened {
		t.Fatalf("after moving nothing it is %v, want opened", got)
	}
	if _, ok := m.Last(); ok {
		t.Fatal("moving nothing recorded a moment")
	}

	m.Moved(10, 0, at)
	m.Moved(0, 0, at.Add(time.Hour))
	if got := m.StateAt(at.Add(time.Hour)); got != Settled {
		t.Fatalf("a read that returned nothing kept it %v, want settled", got)
	}
}

func TestTotalsCountBothWays(t *testing.T) {
	m := New()
	m.Moved(10, 0, at)
	m.Moved(0, 3, at)
	m.Moved(5, 7, at)

	in, out := m.Totals()
	if in != 15 || out != 10 {
		t.Fatalf("totals = %d in, %d out, want 15 and 10", in, out)
	}
}

// The zero value is a working meter, so a caller does not have to
// remember to build one.
func TestZeroMeterIsUsable(t *testing.T) {
	var m Meter
	if got := m.StateAt(at); got != Opened {
		t.Fatalf("the zero meter is %v, want opened", got)
	}
	m.Moved(1, 0, at)
	if got := m.StateAt(at); got != Active {
		t.Fatalf("after a byte the zero meter is %v, want active", got)
	}
}

// The goroutine moving bytes and the one drawing meet here with no lock
// between them.
func TestMeterFromSeveralGoroutines(t *testing.T) {
	m := New()
	const writers, each = 8, 256

	var wg sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		// A reader, the way the panel reads it every frame.
		for {
			select {
			case <-stop:
				return
			default:
				_ = m.StateAt(time.Now())
				m.Totals()
			}
		}
	}()
	for range writers {
		wg.Go(func() {
			for range each {
				m.Moved(1, 1, at)
			}
		})
	}
	wg.Wait()
	close(stop)

	in, out := m.Totals()
	if in != writers*each || out != writers*each {
		t.Fatalf("totals = %d in, %d out, want %d each", in, out, writers*each)
	}
}

func TestStateNames(t *testing.T) {
	want := map[State]string{
		Opened: "opened", Active: "active", Settled: "settled", Closed: "closed",
	}
	for state, name := range want {
		if got := state.String(); got != name {
			t.Errorf("%d.String() = %q, want %q", state, got, name)
		}
	}
	// Anything else has to say so rather than come out blank.
	if got := State(99).String(); got != "unknown" {
		t.Errorf("State(99).String() = %q, want unknown", got)
	}
}
