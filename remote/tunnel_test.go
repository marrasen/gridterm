package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
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
	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
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
	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
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

// An address with no host of its own means the loopback of whichever
// machine does the connecting, and that is what is dialled.
//
// Passing what was typed straight through would send an empty host to
// the far end, which cannot look one up: the tunnel would open, be
// accepted by the dialog, and fail on every stream.
func TestATargetWithNoHostReachesTheLoopbackOfTheFarMachine(t *testing.T) {
	s := sshtest.New(t)
	echo := echoServer(t)
	_, port, err := net.SplitHostPort(echo)
	if err != nil {
		t.Fatalf("split %q: %v", echo, err)
	}
	c := connectTest(t, s)

	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
		Tunnel: Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: ":" + port},
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
	if got := say(t, near, "hello"); got != "HELLO" {
		t.Fatalf("the echo said %q, want HELLO", got)
	}
	want := net.JoinHostPort(loopback, port)
	if got := s.Forwarded(); len(got) != 1 || got[0] != want {
		t.Fatalf("the machine was asked to reach %v, want %q", got, want)
	}
}

// A stream that is still being connected is one the tunnel is holding:
// closing the tunnel has to cut it, or the socket is left behind with
// nothing able to close it.
func TestClosingATunnelCutsAStreamThatIsStillBeingSetUp(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
		// A dynamic tunnel waits for the client to say where it is going,
		// so a client that says nothing is held here.
		Tunnel: Tunnel{Kind: DynamicForward, Listen: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}

	near, err := net.Dial("tcp", f.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = near.Close() })
	// Nothing is said, so the tunnel is waiting for the greeting.
	waitForThis(t, "the stream to be taken up", func() bool { return f.held() == 1 })

	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := near.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	_, err = near.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("the stream was still open after the tunnel closed")
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		t.Fatal("a stream waiting to say where it was going was left open")
	}
}

// Closing the tunnel stops it accepting and cuts what is going through
// it. A stream left copying would hold the far end open for ever.
func TestClosingATunnelCutsItsStreams(t *testing.T) {
	s := sshtest.New(t)
	echo := echoServer(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
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

	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
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

	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
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
	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
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

	_, err = c.OpenTunnel(t.Context(), TunnelConfig{
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
	_, err := c.OpenTunnel(t.Context(), TunnelConfig{
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
		{"a target on the far machine itself", Tunnel{Kind: LocalForward, Listen: ":5432", Target: ":5432"}, true},
		{"a space inside the target port", Tunnel{Kind: LocalForward, Listen: ":1", Target: "db: 5432"}, true},
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

	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
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

// A listener that fails on its own ends the tunnel, and whoever opened
// it is told: only they can take its row away, and only they have
// somewhere to show the reason.
func TestATunnelSaysWhenItStopsAccepting(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	stopped := make(chan error, 1)
	var failures atomic.Int64
	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
		Tunnel:  Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: "127.0.0.1:9"},
		OnError: func(error) { failures.Add(1) },
		OnStopped: func(err error) {
			select {
			case stopped <- err:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	// The listener goes out from under the accept loop, which is what a
	// machine out of handles or a forward the far end cancelled looks
	// like from here.
	if err := f.ln.Close(); err != nil {
		t.Fatalf("close the listener: %v", err)
	}

	select {
	case err := <-stopped:
		if err == nil {
			t.Fatal("the tunnel stopped and said nothing about why")
		}
		if !strings.Contains(err.Error(), "stopped accepting") {
			t.Fatalf("the tunnel stopped with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the tunnel stopped accepting and nobody was told")
	}
	// Closing it afterwards is the same answer, not a second failure.
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// A tunnel closed on purpose says nothing: the user closed it and knows.
func TestATunnelClosedOnPurposeSaysNothing(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	var stopped atomic.Int64
	f, err := c.OpenTunnel(t.Context(), TunnelConfig{
		Tunnel:    Tunnel{Kind: LocalForward, Listen: "127.0.0.1:0", Target: "127.0.0.1:9"},
		OnStopped: func(error) { stopped.Add(1) },
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Long enough that the accept loop has certainly noticed.
	time.Sleep(50 * time.Millisecond)
	if n := stopped.Load(); n != 0 {
		t.Fatalf("closing the tunnel was reported %d times as it stopping on its own", n)
	}
}

// A client that hangs up while the far end is still sending leaves a
// broken pipe behind, which is a stream ending and not trouble.
func TestABrokenPipeIsAStreamEnding(t *testing.T) {
	err := fmt.Errorf("write: %w", &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)})
	if !ended(err) {
		t.Fatalf("%v is taken for trouble", err)
	}
}
