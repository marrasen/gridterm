package main

import (
	"net"
	"os"

	winsock "golang.org/x/sys/windows"
)

// platformLetGos are the errors a Windows socket really returns when the
// other end goes, in the shape the net package wraps them in.
//
// syscall.ECONNRESET on Windows is a placeholder no socket returns, so a
// test built on it passes while the window still reports a window that
// only let go.
func platformLetGos() []letGo {
	wrap := func(e error) error {
		return &net.OpError{Op: "read", Net: "tcp",
			Err: &os.SyscallError{Syscall: "wsarecv", Err: e}}
	}
	return []letGo{
		{"a Winsock reset", wrap(winsock.WSAECONNRESET)},
		{"a Winsock abort", wrap(winsock.WSAECONNABORTED)},
	}
}
