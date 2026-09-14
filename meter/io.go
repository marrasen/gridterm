package meter

import (
	"io"
	"time"
)

// Writer counts what is written through it.
//
// It is how a tunnel shows on the panel without having to report
// anything: both legs of every stream copy through one of these, so the
// row says what is moving and how fast.
type Writer struct {
	// W is what the bytes are written to.
	W io.Writer

	// M counts them. A nil meter counts nothing, so a caller that does
	// not want a count does not have to make one.
	M *Meter

	// Out counts the bytes as leaving this machine rather than arriving.
	Out bool

	// Clock is where the moment comes from. Nil means time.Now, which is
	// what everything outside a test wants.
	Clock func() time.Time
}

// Write passes the bytes on and counts what was taken.
//
// A short write counts what was written rather than what was offered:
// the panel says what moved, and the error says the rest.
func (w Writer) Write(p []byte) (int, error) {
	n, err := w.W.Write(p)
	if n > 0 && w.M != nil {
		if w.Out {
			w.M.Moved(0, n, w.now())
		} else {
			w.M.Moved(n, 0, w.now())
		}
	}
	return n, err
}

// now is the moment to record.
func (w Writer) now() time.Time {
	if w.Clock != nil {
		return w.Clock()
	}
	return time.Now()
}
