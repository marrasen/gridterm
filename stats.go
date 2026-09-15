package main

import (
	"fmt"
	"io"
	"time"

	"github.com/marrasen/gridterm/render"
)

// watchStats reports how long the window is taking, once a second.
//
// It is for the question "why does this feel slow", which cannot be
// answered from a test: what a frame costs is what the graphics card
// took, and a test outside the game loop never gets that far. So the
// window says it itself, and whoever is looking at a slow window can
// read what it is spending its time on.
type watchStats struct {
	to    io.Writer
	clock func() time.Time

	// every is how often a line is written.
	every time.Duration

	started time.Time
	frames  int
	drawn   int
	slowest time.Duration
	total   time.Duration
	quads   int
	rows    int
	bytes   uint64
}

// newWatchStats starts reporting to a writer.
func newWatchStats(to io.Writer) *watchStats {
	s := &watchStats{to: to, clock: time.Now, every: time.Second}
	s.started = s.clock()
	return s
}

// frame records what one frame cost and what it drew.
//
// read is how many bytes every pane has taken from its program since the
// window opened, which is what says whether a slow window is busy or
// merely behind.
func (s *watchStats) frame(took time.Duration, got render.CompositorStats, read uint64) {
	s.frames++
	s.total += took
	if took > s.slowest {
		s.slowest = took
	}
	if !got.Skipped {
		s.drawn++
		s.quads += got.Quads
		s.rows += got.RowsDrawn
	}

	now := s.clock()
	since := now.Sub(s.started)
	if since < s.every {
		return
	}
	// A pane that closed takes its count with it, so the total can go
	// down. What was read since is then not knowable, and saying so is
	// better than reporting the difference of two unsigned numbers.
	moved := uint64(0)
	if read >= s.bytes {
		moved = read - s.bytes
	}
	fmt.Fprintf(s.to,
		"%d frames, %d drawn, %.1fms average, %.1fms slowest, %d rows, %d quads, %s read\n",
		s.frames, s.drawn,
		float64(s.total.Microseconds())/float64(max(s.frames, 1))/1000,
		float64(s.slowest.Microseconds())/1000,
		s.rows, s.quads, inBytes(moved))

	s.started, s.frames, s.drawn = now, 0, 0
	s.slowest, s.total = 0, 0
	s.quads, s.rows = 0, 0
	s.bytes = read
}

// inBytes says an amount the way a person reads one.
func inBytes(n uint64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// bytesRead is how much every pane has taken from its program since the
// window opened.
//
// Read off the meters the panel already keeps, so nothing is counted
// twice and nothing new is counted at all.
func (a *app) bytesRead() uint64 {
	var total uint64
	for _, e := range a.panes {
		if e.Meter == nil {
			continue
		}
		in, _ := e.Meter.Totals()
		total += in
	}
	return total
}
