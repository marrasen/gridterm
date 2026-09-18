package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/render"
)

// What a slow window says about itself, once a second.
func TestTheWindowSaysHowLongItIsTaking(t *testing.T) {
	var said strings.Builder
	at := time.Now()
	s := newWatchStats(&said)
	s.clock = func() time.Time { return at }
	s.started = at

	// A second of frames, all drawn.
	for i := range 60 {
		if i == 59 {
			at = at.Add(time.Second)
		}
		s.frame(2*time.Millisecond, render.CompositorStats{RowsDrawn: 24, Quads: 1920}, uint64(i+1)*100*1024)
	}

	got := said.String()
	for _, want := range []string{"60 frames", "60 drawn", "2.0ms average", "MB read"} {
		if !strings.Contains(got, want) {
			t.Errorf("it said %q, with no %q in it", got, want)
		}
	}
}

// A pane closing takes its count away, so the total goes down. What was
// read since is then not knowable, and a difference of two unsigned
// numbers would say sixteen million terabytes.
func TestAPaneClosingDoesNotMakeTheCountAbsurd(t *testing.T) {
	var said strings.Builder
	at := time.Now()
	s := newWatchStats(&said)
	s.clock = func() time.Time { return at }
	s.started = at
	s.bytes = 10 << 20

	at = at.Add(time.Second)
	s.frame(time.Millisecond, render.CompositorStats{RowsDrawn: 1, Quads: 10}, 0)

	if got := said.String(); !strings.Contains(got, "0 B read") {
		t.Errorf("it said %q", got)
	}
}

// Sizes are said the way a person reads them.
func TestSizesAreSaidPlainly(t *testing.T) {
	for _, c := range []struct {
		n    uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 kB"},
		{3 << 20, "3.0 MB"},
	} {
		if got := inBytes(c.n); got != c.want {
			t.Errorf("%d said %q, want %q", c.n, got, c.want)
		}
	}
}
