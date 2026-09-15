package serve

import (
	"errors"
	"io"
	"net"
	"syscall"
)

// ended reports whether an error means the connection has already gone
// rather than something going wrong with it.
//
// A close on a connection that dropped is not a failure worth telling
// anybody about: the thing being closed is the thing that went.
func ended(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED)
}
