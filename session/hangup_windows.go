//go:build windows

package session

import (
	"errors"

	"golang.org/x/sys/windows"
)

// errPtyHangup reports whether err is how a ConPTY signals that the
// attached process has exited. The output pipe is broken rather than
// returning EOF, and a closed pseudoconsole handle surfaces as
// ERROR_INVALID_HANDLE.
func errPtyHangup(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_INVALID_HANDLE) ||
		errors.Is(err, windows.ERROR_OPERATION_ABORTED)
}
