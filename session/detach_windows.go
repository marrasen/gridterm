//go:build windows

package session

// detachSlave does nothing on Windows. A ConPTY has no slave handle to
// hand back; releaseTerminal is what lets go of it, and that has to wait
// until the child has exited.
func detachSlave(terminal) bool { return false }

// closeErrIsBenign is never needed on Windows because nothing closes the
// pty twice.
func closeErrIsBenign(error) bool { return false }

// releaseTerminal frees the pseudoconsole, leaving the pipes open so a
// pending read drains the child's last output, and reports whether there
// was a pseudoconsole to free.
func releaseTerminal(p terminal) bool { return p.Release() }

// closeReleased closes the pipes of a pty whose pseudoconsole has already
// been freed.
func closeReleased(p terminal) error { return p.Close() }
