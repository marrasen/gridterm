package serve

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

// echoListener answers each connection by sending back what it reads.
func echoListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }()
		}
	}()
	return ln.Addr().String()
}

// A stream through another window reaches what that window dials, and
// carries bytes both ways.
func TestAStreamGoesThroughTheOtherWindow(t *testing.T) {
	echo := echoListener(t)
	asked := make(chan [2]string, 1)
	_, w := takenOverServing(t, Config{Dial: func(ctx context.Context, host, addr string) (net.Conn, error) {
		asked <- [2]string{host, addr}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", echo)
	}})
	c, err := w.DialOn("srv", "db:5432")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := <-asked; got != [2]string{"srv", "db:5432"} {
		t.Fatalf("the other window was asked for %q", got)
	}
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("read back %q, %v", buf, err)
	}
}

// A stream the other window cannot dial is refused with its reason, and
// one that carries none says so.
func TestAStreamThatCannotBeDialledSaysWhy(t *testing.T) {
	_, w := takenOverServing(t, Config{Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("db:5432 is not reachable from srv")
	}})
	if _, err := w.DialOn("srv", "db:5432"); err == nil || !strings.Contains(err.Error(), "not reachable from srv") {
		t.Fatalf("refused, it said %v", err)
	}
	_, none := takenOverServing(t, Config{})
	if _, err := none.DialOn("srv", "db:5432"); err == nil || !strings.Contains(err.Error(), "does not carry tunnels") {
		t.Fatalf("with no way to dial, it said %v", err)
	}
}
