//go:build windows

package session

import "github.com/aymanbagabas/go-pty"

// detachSlave does nothing on Windows. A ConPTY has no slave handle to
// close, and closing the pseudoconsole is what flushes its remaining
// output — so the reaper's Close is both necessary and lossless here.
func detachSlave(pty.Pty) bool { return false }

// closeErrIsBenign is never needed on Windows because nothing closes the
// pty twice.
func closeErrIsBenign(error) bool { return false }
