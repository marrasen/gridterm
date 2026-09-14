package sshtest

import (
	"net"
	"strconv"
	"sync"
	"testing"
)

// Deaf listens and then says nothing at all.
//
// A connection to it completes at the TCP level and then hangs in the
// SSH handshake for ever, which is what a test needs to cancel a dial
// that is genuinely still running. A port nothing listens on is refused
// in well under a millisecond and gives a test no time to do anything.
func Deaf(t *testing.T) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var mu sync.Mutex
	var held []net.Conn
	done := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Held open and never spoken to. Closing it would let the
			// client fail on its own.
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()

	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})

	h, p, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(p)
	return h, n
}
