package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
)

// echoServer answers every connection by sending back what it is sent,
// in upper case, so a test can tell its bytes from the ones it sent.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 256)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						_, _ = c.Write([]byte(strings.ToUpper(string(buf[:n]))))
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// counted is what the panel counts with, in the shape remote asks for.
type counted struct{ m *meter.Meter }

func (c counted) Wrap(w io.Writer, out bool) io.Writer {
	return meter.Writer{W: w, M: c.m, Out: out}
}

// say writes a line down a connection and reads the answer.
func say(t *testing.T, c net.Conn, what string) string {
	t.Helper()
	if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := c.Write([]byte(what)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(what))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buf)
}

// A local forward: a port here stands for a service the far machine can
// reach.
func TestLocalForwardCarriesBytesBothWays(t *testing.T) {
	s := sshtest.New(t)
	echo := echoServer(t)
	c := connectTest(t, s)

	m := meter.New()
	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: echo},
		Count:  counted{m},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	near, err := net.Dial("tcp", f.Addr())
	if err != nil {
		t.Fatalf("dial the tunnel: %v", err)
	}
	t.Cleanup(func() { _ = near.Close() })

	if got := say(t, near, "hello"); got != "HELLO" {
		t.Fatalf("the echo said %q, want HELLO", got)
	}
	// It really went through the machine rather than straight there.
	if n := s.Forwards(); n != 1 {
		t.Fatalf("the machine carried %d streams, want 1", n)
	}
	if got := s.Forwarded(); len(got) != 1 || got[0] != echo {
		t.Fatalf("the machine was asked to reach %v, want %q", got, echo)
	}

	// And the panel can say what moved, each way.
	waitForTotals(t, m, 5, 5)
	if got := f.Served(); got != 1 {
		t.Fatalf("the tunnel served %d streams, want 1", got)
	}
}

// A remote forward: a port on the far machine stands for a service this
// one can reach.
func TestRemoteForwardCarriesBytesBothWays(t *testing.T) {
	s := sshtest.New(t)
	echo := echoServer(t)
	c := connectTest(t, s)

	m := meter.New()
	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: RemoteForward, Listen: "127.0.0.1:9000", Target: echo},
		Count:  counted{m},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	if got := s.Listening(); len(got) != 1 || got[0] != "127.0.0.1:9000" {
		t.Fatalf("the far machine is listening on %v, want 127.0.0.1:9000", got)
	}

	// Something on the far machine connects to the forwarded port.
	far, err := s.ConnectTo("127.0.0.1:9000")
	if err != nil {
		t.Fatalf("connect to the forwarded port: %v", err)
	}
	t.Cleanup(func() { _ = far.Close() })

	if _, err := far.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(far, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "HELLO" {
		t.Fatalf("the echo said %q, want HELLO", buf)
	}
	// A remote forward connects from this machine. Asking the far one to
	// reach the address would be the wrong machine's view of it, and on a
	// real network usually no machine's view of it at all.
	if n := s.Forwards(); n != 0 {
		t.Fatalf("the far machine was asked to reach %v, and a remote forward connects from here",
			s.Forwarded())
	}
	waitForTotals(t, m, 5, 5)
}

// Bytes are counted the way the panel reads them: what leaves this
// machine is out, what arrives is in.
func TestATunnelCountsEachWaySeparately(t *testing.T) {
	s := sshtest.New(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	// Answers one byte for every four it is sent, so the two directions
	// cannot be mixed up.
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		for {
			n, err := c.Read(buf)
			if n > 0 {
				_, _ = c.Write(make([]byte, n/4))
			}
			if err != nil {
				return
			}
		}
	}()

	c := connectTest(t, s)
	m := meter.New()
	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: ln.Addr().String()},
		Count:  counted{m},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	near, err := net.Dial("tcp", f.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = near.Close() })
	if _, err := near.Write([]byte("12345678")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := near.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := io.ReadFull(near, make([]byte, 2)); err != nil {
		t.Fatalf("read: %v", err)
	}
	waitForTotals(t, m, 2, 8)
}

// Closing the tunnel stops it accepting and cuts what is going through
// it. A stream left copying would hold the far end open for ever.
func TestClosingATunnelCutsItsStreams(t *testing.T) {
	s := sshtest.New(t)
	echo := echoServer(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: echo},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	addr := f.Addr()

	near, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = near.Close() })
	say(t, near, "hello")
	waitForThis(t, "the stream to be counted", func() bool { return f.Streams() == 1 })

	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The stream it was carrying has gone. The deadline is the test
	// giving up rather than the answer: a read that only ends because
	// time ran out is a stream that is still open.
	if err := near.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	_, err = near.Read(make([]byte, 8))
	if err == nil {
		t.Fatal("the stream was still open after the tunnel closed")
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		t.Fatal("the stream was left open after the tunnel closed")
	}
	// And nothing new is accepted.
	if next, err := net.Dial("tcp", addr); err == nil {
		_ = next.Close()
		t.Fatal("the tunnel was still accepting after it closed")
	}
}

// A tunnel rides on the connection: closing the machine closes the
// tunnel, or a port would be left listening into nothing.
func TestClosingTheConnectionClosesItsTunnels(t *testing.T) {
	s := sshtest.New(t)
	echo := echoServer(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: echo},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	addr := f.Addr()
	if c.riderCount() != 1 {
		t.Fatalf("the connection holds %d riders, want the tunnel", c.riderCount())
	}

	if err := c.Close(); err != nil {
		t.Fatalf("close the connection: %v", err)
	}
	if next, err := net.Dial("tcp", addr); err == nil {
		_ = next.Close()
		t.Fatal("the tunnel was still listening after its connection closed")
	}
}

// A tunnel that closes itself is forgotten by the connection, or every
// tunnel ever opened would be held until the window closed.
func TestTheConnectionForgetsAClosedTunnel(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: "127.0.0.1:9"},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n := c.riderCount(); n != 0 {
		t.Fatalf("the connection still holds %d riders", n)
	}
	// Closing twice is the same answer, not a second failure.
	if err := f.Close(); err != nil {
		t.Fatalf("close again: %v", err)
	}
}

// A stream that cannot reach the other end is reported rather than
// dropped: the user opened this tunnel and is waiting for it to work.
func TestAStreamThatCannotBeConnectedIsReported(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	var mu sync.Mutex
	var failures []error
	f, err := c.OpenTunnel(TunnelConfig{
		// Port 9 on the far machine, which the test server refuses.
		Tunnel: Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: "127.0.0.1:9"},
		OnError: func(err error) {
			mu.Lock()
			defer mu.Unlock()
			failures = append(failures, err)
		},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	near, err := net.Dial("tcp", f.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer near.Close()

	waitForThis(t, "the failure to be reported", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(failures) > 0
	})
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(failures[0].Error(), "127.0.0.1:9") {
		t.Fatalf("the failure does not say where it could not reach: %v", failures[0])
	}
}

// A port already in use is reported where the tunnel was asked for, not
// from a goroutine nobody is watching.
func TestATunnelSaysWhenItCannotListen(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = taken.Close() })

	_, err = c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: taken.Addr().String(), Target: "127.0.0.1:9"},
	})
	if err == nil {
		t.Fatal("a tunnel opened on a port already in use")
	}
	if c.riderCount() != 0 {
		t.Fatal("the connection is holding a tunnel that never opened")
	}
}

// A tunnel on a connection that has closed is refused, or it would be
// held by nothing and closed by nothing.
func TestATunnelOnAClosedConnectionIsRefused(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: "127.0.0.1:9"},
	})
	if err == nil {
		t.Fatal("a tunnel opened on a connection that had closed")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("error = %v, want ErrClosed", err)
	}
}

// What a tunnel is asked for has to make sense before anything listens.
func TestTunnelValidate(t *testing.T) {
	cases := []struct {
		name string
		t    Tunnel
		ok   bool
	}{
		{"a plain local forward", Tunnel{Kind: LocalForward, Listen: ":5432", Target: "db:5432"}, true},
		{"a plain remote forward", Tunnel{Kind: RemoteForward, Listen: ":8080", Target: "localhost:80"}, true},
		{"any free port", Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: "db:5432"}, true},
		{"no port to listen on", Tunnel{Kind: LocalForward, Listen: "127.0.0.1", Target: "db:5432"}, false},
		{"no target port", Tunnel{Kind: LocalForward, Listen: ":5432", Target: "db"}, false},
		{"a target port of zero", Tunnel{Kind: LocalForward, Listen: ":5432", Target: "db:0"}, false},
		{"a port that is not a number", Tunnel{Kind: LocalForward, Listen: ":http", Target: "db:5432"}, false},
		{"a port past the end", Tunnel{Kind: LocalForward, Listen: ":70000", Target: "db:5432"}, false},
		{"nothing at all", Tunnel{}, false},
		{"a kind that is neither", Tunnel{Kind: 9, Listen: ":1", Target: "db:2"}, false},
	}
	for _, tc := range cases {
		err := tc.t.Validate()
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s was accepted", tc.name)
		}
	}
}

// A tunnel says whether it is open to the rest of the network, so the
// window can ask before opening a hole in it.
func TestTunnelExposed(t *testing.T) {
	cases := []struct {
		listen  string
		exposed bool
	}{
		{":5432", false},
		{"127.0.0.1:5432", false},
		{"localhost:5432", false},
		{"[::1]:5432", false},
		{"0.0.0.0:5432", true},
		{"192.168.1.10:5432", true},
		{"[::]:5432", true},
	}
	for _, tc := range cases {
		got := Tunnel{Kind: LocalForward, Listen: tc.listen, Target: "db:5432"}.Exposed()
		if got != tc.exposed {
			t.Errorf("Tunnel{Listen: %q}.Exposed() = %v, want %v", tc.listen, got, tc.exposed)
		}
	}
}

// An address with no host of its own listens on this machine only. A
// tunnel that quietly listened on every address would be a hole in the
// machine that nobody asked for.
func TestATunnelWithNoHostListensOnLoopbackOnly(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: ":0", Target: "127.0.0.1:9"},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	host, _, err := net.SplitHostPort(f.Addr())
	if err != nil {
		t.Fatalf("the tunnel is listening on %q: %v", f.Addr(), err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("the tunnel is listening on %s, want this machine only", host)
	}
}

// waitForTotals waits for a meter to have counted what went past.
func waitForTotals(t *testing.T, m *meter.Meter, in, out uint64) {
	t.Helper()
	waitForThis(t, fmt.Sprintf("%d in and %d out", in, out), func() bool {
		gotIn, gotOut := m.Totals()
		return gotIn == in && gotOut == out
	})
	gotIn, gotOut := m.Totals()
	if gotIn != in || gotOut != out {
		t.Fatalf("the meter counted %d in and %d out, want %d and %d", gotIn, gotOut, in, out)
	}
}

// waitForThis waits for something to become true, and says what it was
// waiting for when it never does.
func waitForThis(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
