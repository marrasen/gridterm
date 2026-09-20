// Package logs keeps what the window logged, so a pane can show it.
//
// The window logs to stderr, and a window started from Explorer has no
// console for stderr to reach. Keeping the lines here as well is what
// lets the user open them in a pane.
package logs

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// DefaultKeep is how many lines a Lines holds when no other number is
// asked for. Enough to cover a session's worth of connecting and
// failing, and small enough to be nothing next to a pane's scrollback.
const DefaultKeep = 2000

// Lines is what the window logs, kept for a pane to show.
//
// It is an io.Writer, so log.SetOutput points at it. Writing never
// blocks on whoever is reading: a log line is written from wherever the
// work is happening, including paths that must not wait on a pane.
//
// A Lines is safe to use from several goroutines.
type Lines struct {
	// writing orders the whole of Write. It is taken before mu and
	// never inside it, so a line reaches the writer below and the ring
	// in one order rather than two: a console and a pane that disagree
	// about which of two lines came first are worse than either alone.
	//
	// Its own lock rather than mu, because the writer below can be a
	// pipe nobody is draining, and holding mu through that would stop
	// every reader.
	writing sync.Mutex

	// also is where the lines go as well, which is the stderr they
	// always went to. A nil one writes nowhere else. Read and written
	// under mu; called under writing.
	also io.Writer

	mu   sync.Mutex
	wake *sync.Cond

	// kept is the last lines written, oldest first, and first is the
	// sequence number of kept[0]. Sequence numbers never restart, so a
	// reader that fell behind can tell how far.
	kept  [][]byte
	first int64
	keep  int

	// readers is how many panes are following, so a write with nobody
	// reading does no waking.
	readers int
}

// New returns a log that keeps the last keep lines and passes every one
// on to also, which is the stderr the window already wrote to.
//
// A keep of zero or less takes DefaultKeep.
func New(keep int, also io.Writer) *Lines {
	if keep <= 0 {
		keep = DefaultKeep
	}
	l := &Lines{also: also, keep: keep}
	l.wake = sync.NewCond(&l.mu)
	return l
}

// Write takes one log line. It satisfies io.Writer, which is what
// log.SetOutput wants.
//
// The line is copied: log reuses its buffer between calls, so keeping
// the slice would leave every kept line holding whatever was logged
// last.
func (l *Lines) Write(p []byte) (int, error) {
	// One writer at a time, for the whole of it. Whoever gets here
	// second writes second below and lands second in the ring.
	l.writing.Lock()
	defer l.writing.Unlock()

	l.mu.Lock()
	also := l.also
	l.mu.Unlock()
	var err error
	if also != nil {
		_, err = also.Write(p)
	}

	l.mu.Lock()
	l.kept = append(l.kept, bytes.Clone(p))
	if over := len(l.kept) - l.keep; over > 0 {
		// The oldest go, and first moves with them, so a reader knows
		// how many it missed rather than silently seeing a gap.
		l.kept = append(l.kept[:0], l.kept[over:]...)
		l.first += int64(over)
	}
	wake := l.readers > 0
	l.mu.Unlock()
	if wake {
		l.wake.Broadcast()
	}
	return len(p), err
}

// Also says where the lines go besides being kept, which is the stderr
// they always went to. A nil writer sends them nowhere else, which is
// what a test wants so its own log lines stay out of the test output.
func (l *Lines) Also(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.also = w
}

// Held is how many lines are kept right now.
func (l *Lines) Held() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.kept)
}

// Open returns a reader over the log: everything kept so far, and then
// each line as it is written.
//
// It is what a pane is given. Closing the reader leaves the log itself
// alone, so closing the pane does not stop the window logging.
func (l *Lines) Open() *Reader {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.readers++
	return &Reader{on: l, next: l.first}
}

// Reader is one pane following the log.
//
// Read gives back the lines with each newline written as a carriage
// return and a newline, because a terminal moves down a row on the one
// and back to the first column on the other. Written plain, every line
// would start further right than the last.
type Reader struct {
	on *Lines

	// next is the sequence number of the line this reader wants, and
	// rest is what is left of a line that did not fit in one Read.
	next int64
	rest []byte

	closed bool
}

// ErrLogClosed is what a reader answers once it has been closed.
//
// It wraps io.EOF, because that is what whoever is reading a session
// takes as the end of it. Without that, closing the pane showing the
// log would be reported as a session that failed, and the report would
// be written to the log the pane was showing.
var ErrLogClosed = fmt.Errorf("logs: the reader is closed: %w", io.EOF)

// Read fills p with log lines, waiting for one when there are none.
//
// It returns ErrLogClosed once Close has been called, which is the EOF
// the thing reading a session is looking for.
func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.rest) == 0 {
		line, err := r.nextLine()
		if err != nil {
			return 0, err
		}
		r.rest = line
	}
	n := copy(p, r.rest)
	r.rest = r.rest[n:]
	return n, nil
}

// nextLine waits for the next line and returns it ready for a terminal.
func (r *Reader) nextLine() ([]byte, error) {
	l := r.on
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		if r.closed {
			return nil, ErrLogClosed
		}
		// Lines written while this reader was not keeping up have gone.
		// Saying so beats a gap the reader cannot see.
		if r.next < l.first {
			lost := l.first - r.next
			r.next = l.first
			return []byte(missedLine(lost)), nil
		}
		if at := int(r.next - l.first); at < len(l.kept) {
			r.next++
			return forTerminal(l.kept[at]), nil
		}
		l.wake.Wait()
	}
}

// Write throws away what is typed into the pane. A log is something to
// read, and there is nothing at the far end to take it.
func (r *Reader) Write(p []byte) (int, error) { return len(p), nil }

// Resize does nothing. The log has no far end to tell about the size,
// and the pane reflows what it already holds.
func (r *Reader) Resize(cols, rows int) error { return nil }

// Close stops this reader and wakes it. The log itself carries on, so
// closing the pane does not stop the window logging.
func (r *Reader) Close() error {
	l := r.on
	l.mu.Lock()
	if !r.closed {
		r.closed = true
		l.readers--
	}
	l.mu.Unlock()
	l.wake.Broadcast()
	return nil
}

// Wait blocks until the reader is closed, which is what a session does
// when its program ends. A log never ends on its own.
func (r *Reader) Wait() error {
	l := r.on
	l.mu.Lock()
	defer l.mu.Unlock()
	for !r.closed {
		l.wake.Wait()
	}
	return nil
}

// forTerminal turns a log line into what a terminal needs, which is a
// carriage return before every newline.
func forTerminal(line []byte) []byte {
	out := make([]byte, 0, len(line)+8)
	for _, b := range line {
		if b == '\n' {
			out = append(out, '\r')
		}
		out = append(out, b)
	}
	// A log line always ends in a newline, but nothing here may assume
	// it: a line without one would run into the next.
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\r', '\n')
	}
	return out
}

// missedLine says how many lines a reader was too slow to see.
func missedLine(lost int64) string {
	what := " older log lines are "
	if lost == 1 {
		what = " older log line is "
	}
	return "-- gridterm: " + itoa(lost) + what + "no longer kept --\r\n"
}

// itoa spells a count, so this package needs no formatting import for
// its one number.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
