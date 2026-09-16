//go:build windows

package session

import (
	"errors"
	"fmt"

	"github.com/aymanbagabas/go-pty"
	"golang.org/x/sys/windows"
)

// detachSlave does nothing on Windows. A ConPTY has no slave handle to
// hand back; releaseTerminal is what lets go of it, and that has to wait
// until the child has exited.
func detachSlave(pty.Pty) bool { return false }

// closeErrIsBenign is never needed on Windows because nothing closes the
// pty twice.
func closeErrIsBenign(error) bool { return false }

// releaseTerminal frees the pseudoconsole, leaving the pipes open, and
// reports whether it could.
//
// ClosePseudoConsole returns only once the console host has written the
// child's last output to the pipe and closed its end, so a pending read
// drains that output and then sees the end of the file by itself. Closing
// the pipes here instead would discard whatever was still buffered.
func releaseTerminal(p pty.Pty) bool {
	c, ok := p.(pty.ConPty)
	if !ok {
		return false
	}
	windows.ClosePseudoConsole(windows.Handle(c.Fd()))
	return true
}

// closeReleased closes the pipes of a pty whose pseudoconsole has already
// been freed.
func closeReleased(p pty.Pty) error {
	c, ok := p.(pty.ConPty)
	if !ok {
		return fmt.Errorf("close pty: %T is not a ConPty", p)
	}
	return errors.Join(c.InputPipe().Close(), c.OutputPipe().Close())
}
