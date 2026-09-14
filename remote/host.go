package remote

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"
)

// Host is a machine the user has saved.
//
// It holds no secret and never will. A passphrase is asked for when it
// is needed and kept in the Ring for as long as the window is open; a
// password is asked for every time.
type Host struct {
	// Name is what the user calls this machine. It is what the panel
	// shows and what Via names, so it has to be unique in a Book.
	Name string `json:"name"`

	// Address and Port are the machine itself. A zero port means 22.
	Address string `json:"address"`
	Port    int    `json:"port,omitempty"`

	// User is the account to log in as. Empty means the local username.
	User string `json:"user,omitempty"`

	// Via is the Name of another saved host to reach this one through,
	// with no local port opened for it.
	Via string `json:"via,omitempty"`

	// Identities lists private key files to prefer. Empty means the
	// usual three under ~/.ssh.
	Identities []string `json:"identities,omitempty"`

	// Term is the TERM value sent to the machine. Empty means
	// xterm-256color.
	Term string `json:"term,omitempty"`
}

// Target returns the machine as a user would type it: user@host:port,
// leaving out the parts that are the default.
func (h Host) Target() string {
	addr := h.Address
	if h.Port != 0 && h.Port != 22 {
		addr = net.JoinHostPort(h.Address, strconv.Itoa(h.Port))
	} else if strings.Contains(addr, ":") {
		// A bare IPv6 literal needs its brackets or it reads as a host
		// and a port.
		addr = "[" + addr + "]"
	}
	if h.User == "" {
		return addr
	}
	return h.User + "@" + addr
}

// Config returns what Connect needs to reach this machine. What asks the
// user, and what holds the keys already unlocked, is the caller's to
// fill in.
func (h Host) Config() Config {
	return Config{
		Host:       h.Address,
		Port:       h.Port,
		User:       h.User,
		Identities: h.Identities,
	}
}

// Shell returns what to run on this machine at a given size.
func (h Host) Shell(cols, rows int) ShellConfig {
	return ShellConfig{Cols: cols, Rows: rows, Term: h.Term}
}

// Validate reports what is wrong with a host, or nil.
//
// The address goes into known_hosts once its key is trusted, so it is
// checked the same way a typed target is: neither this nor
// knownhosts.Line escapes anything, and a newline in it would write a
// second, unasked-for line of trust.
func (h Host) Validate() error {
	switch {
	case strings.TrimSpace(h.Name) == "":
		return fmt.Errorf("the server needs a name")
	case strings.IndexFunc(h.Name, unprintable) >= 0:
		return fmt.Errorf("the name contains something that cannot be shown")
	case h.Address == "":
		return fmt.Errorf("the server needs an address")
	case strings.IndexFunc(h.Address, badInHost) >= 0:
		return fmt.Errorf("the address contains a character that is not allowed in a host name")
	case h.Port < 0 || h.Port > 65535:
		return fmt.Errorf("the port %d is not between 1 and 65535", h.Port)
	case strings.IndexFunc(h.User, badInHost) >= 0:
		return fmt.Errorf("the user name contains a character that is not allowed")
	case h.Via == h.Name && h.Via != "":
		return fmt.Errorf("%q cannot be reached through itself", h.Name)
	}
	return nil
}

// unprintable reports a character a name has no business holding.
func unprintable(r rune) bool {
	return unicode.IsControl(r) || r == 0x7f
}

// HostFromTarget builds a host from what a user typed, taking the name
// from the address when none was given.
func HostFromTarget(name, target string) (Host, error) {
	cfg, err := ParseTarget(target)
	if err != nil {
		return Host{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = cfg.Host
	}
	h := Host{
		Name:    strings.TrimSpace(name),
		Address: cfg.Host,
		Port:    cfg.Port,
		User:    cfg.User,
	}
	return h, h.Validate()
}
