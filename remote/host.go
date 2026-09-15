package remote

import (
	"fmt"
	"net"
	"slices"
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

	// Window says this is another gridterm serving, taken over rather
	// than logged in to.
	//
	// Address and Port are where it serves, and the first of Identities
	// is the key to offer it. User, Via and Term mean nothing to one:
	// there is no account to log in to and no machine to go through.
	Window bool `json:"window,omitempty"`
}

// ServePort is the port a gridterm serves on when none is given.
const ServePort = 2222

// ServeAddr returns where a saved window serves, as host:port.
func (h Host) ServeAddr() string {
	port := h.Port
	if port == 0 {
		port = ServePort
	}
	return net.JoinHostPort(h.Address, strconv.Itoa(port))
}

// KeyFile is the key file a saved window offers, or empty for whatever
// is to hand.
func (h Host) KeyFile() string {
	if len(h.Identities) == 0 {
		return ""
	}
	return h.Identities[0]
}

// Target returns the machine as a user would type it: user@host:port,
// leaving out the parts that are the default.
func (h Host) Target() string {
	if h.Window {
		return h.ServeAddr()
	}
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

// clone returns a copy that shares nothing with the original, so a
// caller cannot change what is saved without saving it.
func (h Host) clone() Host {
	h.Identities = slices.Clone(h.Identities)
	return h
}

// tidy trims what a user typed, so a name with a stray space does not
// become a different server from the one they meant.
func (h Host) tidy() Host {
	h.Name = strings.TrimSpace(h.Name)
	h.Address = strings.TrimSpace(h.Address)
	h.User = strings.TrimSpace(h.User)
	h.Via = strings.TrimSpace(h.Via)
	h.Term = strings.TrimSpace(h.Term)
	kept := make([]string, 0, len(h.Identities))
	for _, path := range h.Identities {
		if path = strings.TrimSpace(path); path != "" {
			kept = append(kept, path)
		}
	}
	h.Identities = kept
	return h
}

// CommandName reduces a server name to the part a command id is built
// from. Names hold spaces and capitals; an id is lowercase and has
// none, so that a key binding naming one is readable.
//
// It lives here because a Book has to refuse two names that reduce to
// the same thing: one of the two would be unreachable from the menu and
// the palette.
func CommandName(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
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
	case CommandName(h.Name) == "":
		return fmt.Errorf("the name has to have something in it a command can be named after")
	}
	for _, path := range h.Identities {
		if path == "" {
			return fmt.Errorf("one of the key files has no name")
		}
		if strings.IndexFunc(path, unprintable) >= 0 {
			return fmt.Errorf("a key file name contains something that cannot be shown")
		}
	}
	if strings.IndexFunc(h.Term, unprintable) >= 0 {
		return fmt.Errorf("the terminal type contains something that cannot be shown")
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
