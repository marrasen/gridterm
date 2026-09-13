// Package session is the byte source a terminal is attached to: a local
// shell over a pseudo-terminal, or a remote one over SSH.
//
// The interface is deliberately small. Everything above it — the VT
// emulator, the grid, the renderer — only ever sees bytes and a size, so
// swapping a local shell for a remote host changes nothing else.
package session

import "io"

// Session is a running program the terminal is talking to.
//
// Read blocks until output arrives and returns io.EOF when the program
// exits. Write sends input, and may block when the program has stopped
// reading it — callers on a UI thread must not call it directly. Resize
// tells the program the window changed. Close hangs the program up and,
// if it does not take the hint, kills it.
//
// Read and Write may be called concurrently with each other, which is
// how a terminal works: one goroutine pumps output while the UI thread
// sends keystrokes. Neither may be called concurrently with itself.
type Session interface {
	io.ReadWriteCloser

	// Resize reports a new window size in character cells.
	Resize(cols, rows int) error

	// Wait blocks until the program exits and returns its error, if any.
	// It is idempotent: every call returns the same result.
	Wait() error
}
