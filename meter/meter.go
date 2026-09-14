// Package meter counts what has moved on a connection and how long ago.
//
// It exists so the connections panel can say whether something is doing
// anything without asking it. A terminal running a build, a tunnel
// carrying a download and a file transfer all look the same here: bytes,
// and when the last one went past.
//
// Nothing here reads the clock. Every answer is worked out from a time
// the caller passes in, which is what lets the rule be tested without
// waiting four seconds for it.
package meter

import (
	"sync/atomic"
	"time"
)

// Settle is how long after the last byte a connection stops counting as
// active. Long enough that a build printing a line every second stays
// active throughout, short enough that a finished one says so.
const Settle = 4 * time.Second

// State is what a connection looks like from outside.
type State uint8

const (
	// Opened is open with nothing moved yet.
	Opened State = iota

	// Active has moved something within the last Settle.
	Active

	// Settled is open and has been quiet for longer than that.
	Settled

	// Closed is finished. The row stays until it is dismissed, because
	// what a command did after it stopped is worth reading.
	Closed
)

// String names a state the way the panel shows it.
func (s State) String() string {
	switch s {
	case Opened:
		return "opened"
	case Active:
		return "active"
	case Settled:
		return "settled"
	case Closed:
		return "closed"
	}
	return "unknown"
}

// Meter counts the bytes that have gone each way and when the last one
// went.
//
// It is safe to use from several goroutines, which is the point: the
// goroutine moving bytes writes it and the one drawing reads it, with no
// lock between them.
//
// The zero Meter is open and has moved nothing.
type Meter struct {
	in, out atomic.Uint64

	// last is when the last byte went past, in Unix nanoseconds. Zero
	// means none has.
	last atomic.Int64

	closed atomic.Bool
}

// New returns a meter that has moved nothing.
func New() *Meter { return &Meter{} }

// Moved records bytes read from and written to the far end, at a given
// moment.
//
// Nothing is recorded for a call that moved nothing, so a reader polling
// an idle connection does not keep it looking active.
func (m *Meter) Moved(in, out int, now time.Time) {
	if in > 0 {
		m.in.Add(uint64(in))
	}
	if out > 0 {
		m.out.Add(uint64(out))
	}
	if in > 0 || out > 0 {
		m.last.Store(now.UnixNano())
	}
}

// Close says the connection has finished. It is idempotent.
func (m *Meter) Close() { m.closed.Store(true) }

// Totals returns how many bytes have gone each way.
func (m *Meter) Totals() (in, out uint64) {
	return m.in.Load(), m.out.Load()
}

// Last returns when the last byte went past, and whether any has.
func (m *Meter) Last() (time.Time, bool) {
	at := m.last.Load()
	if at == 0 {
		return time.Time{}, false
	}
	return time.Unix(0, at), true
}

// StateAt reports what the connection looks like at a given moment.
//
// A connection falls from active to settled on its own, with no timer:
// whatever is drawing works the state out again on every frame, and the
// answer changes when enough time has gone by.
func (m *Meter) StateAt(now time.Time) State {
	if m.closed.Load() {
		return Closed
	}
	at := m.last.Load()
	if at == 0 {
		return Opened
	}
	if now.Sub(time.Unix(0, at)) < Settle {
		return Active
	}
	return Settled
}
