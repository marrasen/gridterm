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

// releaseTerminal frees the pseudoconsole, leaving the pipes open so a
// pending read drains the child's last output, and reports whether the pty
// was a ConPty.
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
