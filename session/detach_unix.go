//go:build !windows

package session

import (
	"os"

	"github.com/aymanbagabas/go-pty"
)

// detachSlave closes this process's copy of the pty slave and reports
// whether it could.
//
// A pty master only reports the child's exit once every slave handle is
// closed, and go-pty keeps one open for the life of the pty. Closing the
// master instead would work, but the kernel discards whatever output is
// still buffered — a short command's entire output can be lost that way.
// Closing the slave lets the master drain first and then return EIO.
func detachSlave(p pty.Pty) bool {
	u, ok := p.(pty.UnixPty)
	if !ok {
		return false
	}
	s := u.Slave()
	if s == nil {
		return false
	}
	// go-pty's Close will close this again and report an error, which
	// closeErrIsBenign filters out.
	return s.Close() == nil
}

// closeErrIsBenign reports whether err from closing the pty is only the
// double close detachSlave caused.
func closeErrIsBenign(err error) bool {
	return err != nil && os.IsNotExist(err) ||
		err != nil && errIsClosed(err)
}
