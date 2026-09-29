// Package sessiontest has sessions for tests to run a pane on without a
// program behind it.
package sessiontest

import (
	"io"
	"sync"
)

// Typed is a session that records what it is sent. Its program says
// nothing until it is closed.
type Typed struct {
	mu   sync.Mutex
	got  []byte
	done chan struct{}
}

// New is a Typed session, open.
func New() *Typed { return &Typed{done: make(chan struct{})} }

func (s *Typed) Read([]byte) (int, error) { <-s.done; return 0, io.EOF }

func (s *Typed) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, p...)
	return len(p), nil
}

func (s *Typed) Close() error          { close(s.done); return nil }
func (s *Typed) Resize(int, int) error { return nil }
func (s *Typed) Wait() error           { return nil }

// Sent is everything the session has been sent.
func (s *Typed) Sent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.got)
}

// Printed is a session whose program prints once and then waits.
type Printed struct {
	*Typed
	mu  sync.Mutex
	out []byte
}

// NewPrinted is a session whose program prints out.
func NewPrinted(out []byte) *Printed { return &Printed{Typed: New(), out: out} }

func (s *Printed) Read(p []byte) (int, error) {
	s.mu.Lock()
	if len(s.out) > 0 {
		n := copy(p, s.out)
		s.out = s.out[n:]
		s.mu.Unlock()
		return n, nil
	}
	s.mu.Unlock()
	return s.Typed.Read(p)
}
