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
	return r.perSecIn, r.perSecOut
}

// Bytes writes a byte count the way a person reads one: three
// significant figures and a unit, so the width of the number does not
// jump about as it grows.
func Bytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
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
