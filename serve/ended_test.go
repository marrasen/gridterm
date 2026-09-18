package serve

import (
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
)

// A close on a connection that has already gone is not a failure worth
// telling anybody about: the thing being closed is the thing that went.
//
// Reporting those would put a dialog up every time a client's network
// dropped, which is the ordinary way a session ends.
func TestEndedKnowsAConnectionThatHasGone(t *testing.T) {
	for _, err := range []error{
		io.EOF,
		net.ErrClosed,
		syscall.ECONNRESET,
		syscall.ECONNABORTED,
		// Wrapped, which is how they arrive.
		errors.Join(errors.New("close a session"), io.EOF),
	} {
		if !Ended(err) {
			t.Errorf("%v is read as something going wrong", err)
		}
	}
	for _, err := range []error{
		errors.New("the pipe would not close"),
		syscall.EACCES,
	} {
		if Ended(err) {
			t.Errorf("%v is read as a connection that had already gone", err)
		}
	}
}
