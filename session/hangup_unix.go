//go:build !windows

package session

import (
	"errors"
	"syscall"
)

// errPtyHangup reports whether err is the master-side read error a Unix
// pty gives once the last slave file descriptor is closed. Linux returns
// EIO; it means the child is gone, not that the read failed.
func errPtyHangup(err error) bool {
	return errors.Is(err, syscall.EIO)
}
