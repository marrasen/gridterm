package remote

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ParseTarget splits [user@]host[:port] into a config.
//
// The host may be an IPv6 literal, which is why the port is not simply
// split on the first colon: "::1" is an address, not a host and a port.
// A bracketed literal with a port, "[::1]:22", is the only unambiguous
// spelling and the one net.SplitHostPort understands.
func ParseTarget(target string) (Config, error) {
	var cfg Config
	orig := target

	// Split on the last @: a username may not contain one, but an IPv6
	// zone or a copied URL might.
	if at := strings.LastIndex(target, "@"); at >= 0 {
		cfg.User, target = target[:at], target[at+1:]
	}

	if host, port, err := net.SplitHostPort(target); err == nil {
		n, convErr := strconv.Atoi(port)
		if convErr != nil || n < 1 || n > 65535 {
			return cfg, fmt.Errorf("remote: target %q: port %q is not between 1 and 65535",
				orig, port)
		}
		cfg.Host, cfg.Port = host, n
	} else {
		// No port. A bare IPv6 literal may still be bracketed.
		cfg.Host = strings.TrimSuffix(strings.TrimPrefix(target, "["), "]")
	}

	if cfg.Host == "" {
		return cfg, fmt.Errorf("remote: target %q: no host", orig)
	}
	// A host is written verbatim into known_hosts once its key is
	// trusted, and neither this nor knownhosts.Line escapes anything. A
	// space or a newline in it would put a second, unasked-for trust
	// line in that file. Today the resolver refuses such a name long
	// before it gets that far; that is luck, not a check.
	if i := strings.IndexFunc(cfg.Host, badInHost); i >= 0 {
		return Config{}, fmt.Errorf("remote: target %q: the host name contains %q",
			orig, cfg.Host[i:i+1])
	}
	return cfg, nil
}

// badInHost reports a character that has no business in a host name.
//
// The @ is there because ParseTarget splits on the last one, so anything
// left in the host is a second one -- and Target would render it back
// into a string that reads as a different user on a different machine.
func badInHost(r rune) bool {
	return r <= ' ' || r == 0x7f || r == '#' || r == '@'
}
