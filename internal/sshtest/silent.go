package sshtest

import (
	"net"
	"strconv"
	"sync"
	"testing"
)

// SilentMachine listens, accepts, and then says nothing at all. It
// returns the address to reach it on.
//
// It is what a firewall that accepts, a port forwarded to nothing, or
// another service on the port looks like from here: the connection
// completes at the TCP level and then hangs in the SSH handshake for
// ever. A port nothing listens on is refused in well under a
// millisecond and gives a test no time to do anything.
func SilentMachine(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// Kept under a lock rather than sent down a channel: the goroutine
	// can be holding a connection at the moment the test ends, and a
	// channel closed under it panics.
	var (
		mu   sync.Mutex
		held []net.Conn
	)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Held open and never spoken to. Closing it would let the
			// client fail on its own.
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})
	return ln.Addr().String()
}

// Deaf is SilentMachine with the address split, for a caller that names
// a host and a port separately.
func Deaf(t testing.TB) (host string, port int) {
	t.Helper()
	h, p, err := net.SplitHostPort(SilentMachine(t))
	if err != nil {
		t.Fatalf("split the address: %v", err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("read the port: %v", err)
	}
	return h, n
}
