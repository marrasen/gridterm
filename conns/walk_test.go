package conns

import (
	"testing"
	"time"
)

// Each walks every connection in the order they were opened.
func TestEachWalksInTheOrderTheyOpened(t *testing.T) {
	r := New()
	one := &Entry{Host: Local, Label: "one"}
	two := &Entry{Host: "kettle", Label: "two"}
	r.Add(one)
	r.Add(two)

	var got []string
	r.Each(func(e *Entry) bool {
		got = append(got, e.Label)
		return true
	})

	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("it walked %v, want one then two", got)
	}
}

// A walk that says stop stops.
func TestEachStopsWhenTheCallerSaysSo(t *testing.T) {
	r := New()
	r.Add(&Entry{Host: Local, Label: "one"})
	r.Add(&Entry{Host: Local, Label: "two"})

	n := 0
	r.Each(func(*Entry) bool {
		n++
		return false
	})

	if n != 1 {
		t.Errorf("it called back %d times, want the one it was stopped after", n)
	}
}

// Walking asks the heap for nothing, which is the whole reason it is
// here rather than Groups.
func TestWalkingAsksTheHeapForNothing(t *testing.T) {
	r := New()
	for range 8 {
		r.Add(&Entry{Host: Local, Label: "one", Kind: Terminal})
	}
	now := time.Now()
	var sink int
	walk := func() {
		r.Each(func(e *Entry) bool {
			sink += int(e.State(now))
			return true
		})
	}
	walk()

	got := testing.AllocsPerRun(20, walk)

	if got != 0 {
		t.Errorf("a walk allocated %v times, want none", got)
	}
	_ = sink
}
