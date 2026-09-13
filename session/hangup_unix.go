//go:build !windows

package session

import (
	"errors"
	"os"
	"syscall"
)

// errPtyHangup reports whether err is the master-side read error a Unix
// pty gives once the last slave file descriptor is closed. Linux returns
// EIO; it means the child is gone, not that the read failed.
//
// This is the normal end of a session here, because StartLocal closes
// this process's slave handle as soon as the child is running.
func errPtyHangup(err error) bool {
	return errors.Is(err, syscall.EIO)
}

// errIsClosed reports whether err is a double close of an already closed
// file.
func errIsClosed(err error) bool {
	return errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF)
}
