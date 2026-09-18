package meter

import (
	"errors"
	"io"
	"testing"
	"time"
)

// short takes only part of what it is offered, the way a socket does
// when its buffer fills.
type short struct {
	took int
	err  error
}

func (s *short) Write(p []byte) (int, error) {
	n := min(s.took, len(p))
	return n, s.err
}

func TestWriterCountsWhatWentPast(t *testing.T) {
	m := New()
	w := Writer{W: io.Discard, M: m, Clock: func() time.Time { return at }}

	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	in, out := m.Totals()
	if in != 5 || out != 0 {
		t.Fatalf("totals = %d in, %d out, want 5 arriving", in, out)
	}
	if got := m.StateAt(at); got != Active {
		t.Fatalf("after a write it is %v, want active", got)
	}

	// The other way counts as leaving.
	if _, err := (Writer{W: io.Discard, M: m, Out: true,
		Clock: func() time.Time { return at }}).Write([]byte("bye")); err != nil {
		t.Fatalf("write the other way: %v", err)
	}
	if in, out = m.Totals(); in != 5 || out != 3 {
		t.Fatalf("totals = %d in, %d out, want 5 and 3", in, out)
	}
}

// A short write counts what was taken, not what was offered: the panel
// says what moved and the error says the rest.
func TestWriterCountsAShortWrite(t *testing.T) {
	m := New()
	want := errors.New("the buffer is full")
	w := Writer{W: &short{took: 2, err: want}, M: m, Clock: func() time.Time { return at }}

	n, err := w.Write([]byte("hello"))
	if n != 2 {
		t.Fatalf("wrote %d, want the 2 that were taken", n)
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want the one the writer gave", err)
	}
	if in, _ := m.Totals(); in != 2 {
		t.Fatalf("counted %d, want the 2 that moved", in)
	}
}

// A write that moved nothing must not make a connection look busy.
func TestWriterCountsNothingWhenNothingMoved(t *testing.T) {
	m := New()
	w := Writer{W: &short{}, M: m, Clock: func() time.Time { return at }}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := m.StateAt(at); got != Opened {
		t.Fatalf("after moving nothing it is %v, want opened", got)
	}
}

// A writer with no meter is still a writer: a caller that does not want
// a count does not have to make one.
func TestWriterWithoutAMeter(t *testing.T) {
	w := Writer{W: io.Discard}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// With no clock of its own it reads the real one, so a connection with a
// writer on it settles when it should.
func TestWriterWithoutAClockUsesTheRealOne(t *testing.T) {
	m := New()
	before := time.Now()
	if _, err := (Writer{W: io.Discard, M: m}).Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	last, ok := m.Last()
	if !ok {
		t.Fatal("nothing was recorded")
	}
	if last.Before(before) || last.After(time.Now()) {
		t.Fatalf("the moment recorded is %v, which is not now", last)
	}
}
