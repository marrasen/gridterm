package meter

import (
	"fmt"
	"time"
)

// RateWindow is how often a speed is worked out again.
//
// A speed taken over a shorter gap than this is mostly noise, and a row
// whose number changes every frame is a row that dirties itself every
// frame -- which is what the whole display is built to avoid.
const RateWindow = time.Second

// Rate turns a meter's running totals into a speed.
//
// The zero Rate is ready to use and reports nothing until it has two
// samples to work from.
type Rate struct {
	in, out uint64
	at      time.Time

	perSecIn  uint64
	perSecOut uint64

	// past is the last Samples speeds, oldest first, so a row can show
	// the shape of the run rather than one number from it. It is the
	// faster of the two directions: which way the bytes went is a
	// different question from whether anything is happening.
	past  [Samples]uint64
	depth int
}

// Samples is how many seconds of speed a Rate remembers.
const Samples = 12

// Past returns the speeds it remembers, oldest first, with nothing in
// front of the first second it saw.
//
// A copy, because the caller is handed a window onto a run that keeps
// moving: the array behind it is written again every second.
func (r *Rate) Past() []uint64 {
	out := make([]uint64, r.depth)
	copy(out, r.past[Samples-min(r.depth, Samples):])
	return out
}

// remember adds one second to the history, pushing the oldest out.
func (r *Rate) remember(speed uint64) {
	copy(r.past[:], r.past[1:])
	r.past[Samples-1] = speed
	if r.depth < Samples {
		r.depth++
	}
}

// Sample returns the speed each way in bytes per second.
//
// It is worked out again at most once per RateWindow, so calling this on
// every frame costs nothing and gives the same answer until there is
// something new to say.
func (r *Rate) Sample(m *Meter, now time.Time) (in, out uint64) {
	gotIn, gotOut := m.Totals()
	if r.at.IsZero() {
		r.in, r.out, r.at = gotIn, gotOut, now
		return 0, 0
	}
	gap := now.Sub(r.at)
	if gap < RateWindow {
		return r.perSecIn, r.perSecOut
	}
	seconds := gap.Seconds()
	r.perSecIn = uint64(float64(gotIn-r.in) / seconds)
	r.perSecOut = uint64(float64(gotOut-r.out) / seconds)
	r.in, r.out, r.at = gotIn, gotOut, now
	if gap >= RateWindow*2 {
		// More than one window went by unmeasured: the sidebar was
		// hidden, or the window was not drawing. The speed over the
		// whole gap is still the answer to how fast, but it cannot
		// stand in for the last second of a run, and the seconds before
		// it were never measured either. So the run is forgotten rather
		// than filled in with an average pretending to be part of it.
		r.past, r.depth = [Samples]uint64{}, 0
		return r.perSecIn, r.perSecOut
	}
	r.remember(max(r.perSecIn, r.perSecOut))
	return r.perSecIn, r.perSecOut
}

// Bytes writes a byte count the way a person reads one: three
// significant figures and a unit.
func Bytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	// exp is capped at the last suffix there is. Without it a number
	// past an exabyte would index past the end of the table.
	div, exp := uint64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := [...]string{"kB", "MB", "GB", "TB", "PB"}[exp]
	if value < 10 {
		return fmt.Sprintf("%.1f %s", value, suffix)
	}
	return fmt.Sprintf("%.0f %s", value, suffix)
}

// Speed writes a speed, or nothing at all when there is none. A row
// showing "0 B/s" while nothing is happening is a row of noise.
func Speed(perSec uint64) string {
	if perSec == 0 {
		return ""
	}
	return Bytes(perSec) + "/s"
}

// Bars turns a run of speeds into bar heights from 0 to max, scaled to
// the fastest second in the run.
//
// Scaled to itself rather than to an absolute speed, because what the
// graph is for is the shape: a shell printing a few bytes a second and a
// copy moving megabytes both have quiet spells and busy ones, and the
// same picture should show either.
func Bars(speeds []uint64, max int) []int {
	if max <= 0 {
		return nil
	}
	var top uint64
	for _, s := range speeds {
		if s > top {
			top = s
		}
	}
	out := make([]int, len(speeds))
	if top == 0 {
		return out
	}
	for i, s := range speeds {
		// Rounded up, so a second that moved anything at all is a bar
		// rather than a gap.
		out[i] = int((uint64(max)*s + top - 1) / top)
	}
	return out
}
