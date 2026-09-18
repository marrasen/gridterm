package serve

import (
	"errors"

	"golang.org/x/sys/windows"
)

// platformEnded reports whether a Winsock error means the peer has gone.
//
// syscall.ECONNRESET on Windows is a placeholder that no socket ever
// returns, so the Winsock numbers have to be matched themselves: a read
// on a connection the other end closed comes back as WSAECONNRESET, and
// errors.Is does not relate the two.
func platformEnded(err error) bool {
	return errors.Is(err, windows.WSAECONNRESET) ||
		errors.Is(err, windows.WSAECONNABORTED)
}
