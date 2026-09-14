package remote

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
)

// socksDial speaks SOCKS5 to a dynamic tunnel and asks it to reach an
// address, returning the answer code and the stream.
func socksDial(t *testing.T, at, target string, kind byte) (byte, net.Conn) {
	t.Helper()
	c, err := net.Dial("tcp", at)
	if err != nil {
		t.Fatalf("dial the tunnel: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}

	// The greeting: version 5, one method, no authentication.
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		t.Fatalf("greet: %v", err)
	}
	answer := make([]byte, 2)
	if _, err := io.ReadFull(c, answer); err != nil {
		t.Fatalf("read the greeting answer: %v", err)
	}
	if answer[0] != 5 || answer[1] != 0 {
		t.Fatalf("the tunnel answered the greeting with %v, want no authentication", answer)
	}

	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatalf("split %q: %v", target, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}

	req := []byte{5, 1, 0}
	switch kind {
	case socksIPv4:
		ip := net.ParseIP(host).To4()
		if ip == nil {
			t.Fatalf("%q is not an IPv4 address", host)
		}
		req = append(req, socksIPv4)
		req = append(req, ip...)
	default:
		req = append(req, socksDomain, byte(len(host)))
		req = append(req, host...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := c.Write(req); err != nil {
		t.Fatalf("request: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatalf("read the answer: %v", err)
	}
	if reply[0] != 5 {
		t.Fatalf("the answer is version %d, want 5", reply[0])
	}
	return reply[1], c
}

// A dynamic tunnel reaches whatever each stream asks for, as the far
// machine sees it.
func TestDynamicForwardReachesWhatItIsAsked(t *testing.T) {
	s := sshtest.New(t)
	echo := echoServer(t)
	c := connectTest(t, s)

	m := meter.New()
	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: DynamicForward, Listen: "127.0.0.1:0"},
		Count:  counted{m},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	code, stream := socksDial(t, f.Addr(), echo, socksIPv4)
	if code != socksOK {
		t.Fatalf("the tunnel answered %d, want it to have connected", code)
	}
	if got := say(t, stream, "hello"); got != "HELLO" {
		t.Fatalf("the echo said %q, want HELLO", got)
	}

	// It went through the machine, to the address the stream asked for.
	if got := s.Forwarded(); len(got) != 1 || got[0] != echo {
		t.Fatalf("the machine was asked to reach %v, want %q", got, echo)
	}
	waitForTotals(t, m, 5, 5)

	// A second stream can go somewhere else entirely.
	other := echoServer(t)
	code, stream = socksDial(t, f.Addr(), other, socksDomain)
	if code != socksOK {
		t.Fatalf("the second stream was answered %d", code)
	}
	if got := say(t, stream, "again"); got != "AGAIN" {
		t.Fatalf("the second echo said %q", got)
	}
	if got := s.Forwarded(); len(got) != 2 || got[1] != other {
		t.Fatalf("the machine was asked to reach %v, want %q second", got, other)
	}
}

// A stream the far machine cannot connect is told so in the answer,
// rather than being left waiting or cut without a word.
func TestDynamicForwardSaysWhenItCannotConnect(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: DynamicForward, Listen: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	// Port 9 on the far machine, which the test server refuses.
	code, _ := socksDial(t, f.Addr(), "127.0.0.1:9", socksIPv4)
	if code == socksOK {
		t.Fatal("the tunnel said it had connected to a port nothing answers")
	}
}

// Anything but CONNECT is refused, and said to be refused.
func TestDynamicForwardRefusesWhatItDoesNotDo(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: DynamicForward, Listen: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	conn, err := net.Dial("tcp", f.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatalf("greet: %v", err)
	}
	if _, err := io.ReadFull(conn, make([]byte, 2)); err != nil {
		t.Fatalf("read the greeting answer: %v", err)
	}
	// BIND, which no SSH client does.
	if _, err := conn.Write([]byte{5, 2, 0, socksIPv4, 127, 0, 0, 1, 0, 80}); err != nil {
		t.Fatalf("request: %v", err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read the answer: %v", err)
	}
	if reply[1] != socksBadCommand {
		t.Fatalf("the answer is %d, want it to say the command is not done here", reply[1])
	}
}

// A client that will not connect without authenticating is told so and
// let go, rather than left holding a stream nothing will answer.
func TestDynamicForwardTurnsDownAClientThatWantsAPassword(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	var failures []error
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	f, err := c.OpenTunnel(TunnelConfig{
		Tunnel: Tunnel{Kind: DynamicForward, Listen: "127.0.0.1:0"},
		OnError: func(err error) {
			<-mu
			failures = append(failures, err)
			mu <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	conn, err := net.Dial("tcp", f.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	// One method: username and password, which gridterm does not do.
	if _, err := conn.Write([]byte{5, 1, 2}); err != nil {
		t.Fatalf("greet: %v", err)
	}
	answer := make([]byte, 2)
	if _, err := io.ReadFull(conn, answer); err != nil {
		t.Fatalf("read the greeting answer: %v", err)
	}
	if answer[1] != socksNoMethodInCommon {
		t.Fatalf("the tunnel answered %v, want it to say there is no method in common", answer)
	}

	waitForThis(t, "the refusal to be reported", func() bool {
		<-mu
		defer func() { mu <- struct{}{} }()
		return len(failures) > 0
	})
	<-mu
	defer func() { mu <- struct{}{} }()
	if !strings.Contains(failures[0].Error(), "authenticat") {
		t.Fatalf("the failure says %v", failures[0])
	}
}

// A dynamic tunnel takes no address of its own: every stream says where
// it is going, so one here would be a setting that does nothing.
func TestDynamicForwardTakesNoTarget(t *testing.T) {
	t.Run("with a target", func(t *testing.T) {
		err := Tunnel{Kind: DynamicForward, Listen: ":1080", Target: "db:5432"}.Validate()
		if err == nil {
			t.Fatal("a dynamic tunnel with an address was accepted")
		}
	})
	t.Run("without one", func(t *testing.T) {
		if err := (Tunnel{Kind: DynamicForward, Listen: ":1080"}).Validate(); err != nil {
			t.Fatalf("a plain dynamic tunnel was refused: %v", err)
		}
	})
	t.Run("named on the panel", func(t *testing.T) {
		got := Tunnel{Kind: DynamicForward, Listen: "127.0.0.1:1080"}.String()
		if !strings.Contains(got, "socks5") || !strings.Contains(got, "1080") {
			t.Fatalf("a dynamic tunnel reads as %q", got)
		}
	})
}
