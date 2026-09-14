package remote

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// SOCKS5 is what a browser or a command with a proxy setting speaks. A
// dynamic tunnel is a SOCKS5 server here that connects from the far
// machine, so every address is reached as that machine sees it.
//
// Only what is needed is implemented: version 5, no authentication, and
// CONNECT. BIND and UDP ASSOCIATE are refused, as they are by every
// other SSH client.
const (
	socksVersion = 5

	socksNoAuth  = 0x00
	socksConnect = 0x01

	socksIPv4   = 0x01
	socksDomain = 0x03
	socksIPv6   = 0x04

	socksOK               = 0x00
	socksFailed           = 0x01
	socksUnreachable      = 0x04
	socksRefused          = 0x05
	socksBadCommand       = 0x07
	socksBadAddressKind   = 0x08
	socksNoMethodInCommon = 0xff
)

// socksGreeting is how long the handshake has to arrive.
//
// Without it a connection that says nothing holds a goroutine and a
// socket for as long as the window is open, which is what a port scan
// looks like.
const socksGreeting = 30 * time.Second

// socksAsk reads the greeting and the request, and returns the address
// the client asked to reach.
//
// The reply to the request is not sent here: whether it worked is only
// known once the far machine has been asked to connect.
func socksAsk(c net.Conn) (string, error) {
	// Both ways: the greeting has to arrive, and the answers to it have
	// to be taken.
	if err := c.SetDeadline(time.Now().Add(socksGreeting)); err != nil {
		return "", fmt.Errorf("socks: %w", err)
	}
	// Cleared before the stream is handed on, or the copying would stop
	// at the same deadline.
	defer func() { _ = c.SetDeadline(time.Time{}) }()

	if err := socksHello(c); err != nil {
		return "", err
	}
	return socksRequest(c)
}

// socksHello answers the greeting, agreeing to no authentication.
func socksHello(c net.Conn) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil {
		return fmt.Errorf("socks: read the greeting: %w", err)
	}
	if head[0] != socksVersion {
		return fmt.Errorf("socks: version %d is not SOCKS5", head[0])
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return fmt.Errorf("socks: read the greeting: %w", err)
	}
	for _, m := range methods {
		if m == socksNoAuth {
			_, err := c.Write([]byte{socksVersion, socksNoAuth})
			if err != nil {
				return fmt.Errorf("socks: answer the greeting: %w", err)
			}
			return nil
		}
	}
	// Told which way it failed, so the client says something better than
	// "the connection closed".
	if _, err := c.Write([]byte{socksVersion, socksNoMethodInCommon}); err != nil {
		return fmt.Errorf("socks: answer the greeting: %w", err)
	}
	return errors.New("socks: the client would not connect without authenticating")
}

// socksRequest reads what the client wants to reach.
func socksRequest(c net.Conn) (string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		return "", fmt.Errorf("socks: read the request: %w", err)
	}
	if head[0] != socksVersion {
		return "", fmt.Errorf("socks: version %d is not SOCKS5", head[0])
	}
	if head[1] != socksConnect {
		// Both: why it was refused, and whether the client was told.
		return "", errors.Join(
			fmt.Errorf("socks: command %d is not one gridterm does", head[1]),
			socksAnswer(c, socksBadCommand))
	}

	var host string
	switch head[3] {
	case socksIPv4, socksIPv6:
		n := net.IPv4len
		if head[3] == socksIPv6 {
			n = net.IPv6len
		}
		raw := make([]byte, n)
		if _, err := io.ReadFull(c, raw); err != nil {
			return "", fmt.Errorf("socks: read the address: %w", err)
		}
		host = net.IP(raw).String()
	case socksDomain:
		n := make([]byte, 1)
		if _, err := io.ReadFull(c, n); err != nil {
			return "", fmt.Errorf("socks: read the address: %w", err)
		}
		raw := make([]byte, int(n[0]))
		if _, err := io.ReadFull(c, raw); err != nil {
			return "", fmt.Errorf("socks: read the address: %w", err)
		}
		host = string(raw)
		if !hostname(host) {
			// Whatever connected chose these bytes. They would go out in
			// the channel request, into the far machine's log, and into
			// a failure shown in this window -- which is a window that
			// also asks for passwords. Nothing but a host name gets that
			// far.
			return "", errors.Join(
				fmt.Errorf("socks: %q is not a host name", host),
				socksAnswer(c, socksBadAddressKind))
		}
	default:
		return "", errors.Join(
			fmt.Errorf("socks: address type %d is not one gridterm does", head[3]),
			socksAnswer(c, socksBadAddressKind))
	}

	raw := make([]byte, 2)
	if _, err := io.ReadFull(c, raw); err != nil {
		return "", fmt.Errorf("socks: read the port: %w", err)
	}
	port := binary.BigEndian.Uint16(raw)
	if host == "" {
		return "", errors.Join(
			errors.New("socks: the request names no host"),
			socksAnswer(c, socksFailed))
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// hostname reports whether a string is a plausible host name: the
// characters a name is made of, and no longer than DNS allows.
//
// Deliberately strict. A name that is refused here is a name the far
// machine could not have looked up anyway, and everything else that
// might be in those bytes -- a newline, a control character, a byte
// sequence that is not text at all -- travels into a log on the far
// machine and a dialog in this window.
func hostname(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// socksAnswer tells the client how the request went.
//
// The address in the answer is the all-zero one. It is meant to be the
// address the far end connected from, which an SSH channel does not
// have, and no client in practice looks at it for a CONNECT.
func socksAnswer(c net.Conn, code byte) error {
	// Ten bytes fit in any socket's buffer, so a client that has stopped
	// reading cannot hold this goroutine. The deadline says so rather
	// than leaving it to be worked out.
	if err := c.SetWriteDeadline(time.Now().Add(socksGreeting)); err != nil {
		return fmt.Errorf("socks: answer the request: %w", err)
	}
	defer func() { _ = c.SetWriteDeadline(time.Time{}) }()

	_, err := c.Write([]byte{socksVersion, code, 0x00, socksIPv4, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return fmt.Errorf("socks: answer the request: %w", err)
	}
	return nil
}

// socksCode turns a failure to reach the far end into the nearest thing
// SOCKS5 can say about it.
func socksCode(err error) byte {
	if err == nil {
		return socksOK
	}
	switch {
	case errors.Is(err, ErrClosed):
		return socksUnreachable
	default:
		// x/crypto reports every refusal from the far machine the same
		// way, so this cannot be told apart from a host that is not
		// there. "Refused" is the more common of the two by far.
		return socksRefused
	}
}
