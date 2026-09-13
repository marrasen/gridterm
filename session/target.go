package session

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ParseSSHTarget splits [user@]host[:port] into a config.
//
// The host may be an IPv6 literal, which is why the port is not simply
// split on the first colon: "::1" is an address, not a host and a port.
// A bracketed literal with a port, "[::1]:22", is the only unambiguous
// spelling and the one net.SplitHostPort understands.
func ParseSSHTarget(target string) (SSHConfig, error) {
	var cfg SSHConfig
	orig := target

	// Split on the last @: a username may not contain one, but an IPv6
	// zone or a copied URL might.
	if at := strings.LastIndex(target, "@"); at >= 0 {
		cfg.User, target = target[:at], target[at+1:]
	}

	if host, port, err := net.SplitHostPort(target); err == nil {
		n, convErr := strconv.Atoi(port)
		if convErr != nil || n < 1 || n > 65535 {
			return cfg, fmt.Errorf("ssh target %q: port %q is not between 1 and 65535",
				orig, port)
		}
		cfg.Host, cfg.Port = host, n
	} else {
		// No port. A bare IPv6 literal may still be bracketed.
		cfg.Host = strings.TrimSuffix(strings.TrimPrefix(target, "["), "]")
	}

	if cfg.Host == "" {
		return cfg, fmt.Errorf("ssh target %q: no host", orig)
	}
	return cfg, nil
}
