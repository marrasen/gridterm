package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
)

// linkOpener opens an address a pane printed, from the machine that
// pane is on.
//
// A machine of its own, because "localhost" in the output of a program
// on a server is that server's localhost. Opening it here would reach
// whatever happens to be on this machine's port, or nothing.
func (a *app) linkOpener(host string) func(string) {
	return func(at string) {
		if err := a.openLinkFrom(host, at); err != nil {
			a.reportError("Could not open "+at, err)
		}
	}
}

// openLinkFrom opens an address the way the machine it came from
// means it.
//
// An address on this machine, or one that names a machine the whole
// network can reach, goes straight to the browser. One that names the
// far machine's own localhost gets a tunnel first, and the browser is
// sent to this end of it.
func (a *app) openLinkFrom(host, at string) error {
	target, ok := serviceOnTheFarEnd(host, at)
	if !ok {
		return openInBrowser(at)
	}
	local, err := a.tunnelTo(host, target)
	if err != nil {
		return err
	}
	here, err := throughTunnel(at, local)
	if err != nil {
		return err
	}
	return openInBrowser(here)
}

// serviceOnTheFarEnd is the address to reach on the far machine, and
// whether this link needs a tunnel at all.
//
// It needs one when the pane is on another machine and the address
// names that machine's own loopback. Anything else is reachable from
// here, or is not ours to guess about.
func serviceOnTheFarEnd(host, at string) (target string, ok bool) {
	if host == conns.Local {
		return "", false
	}
	u, err := url.Parse(at)
	if err != nil || u.Port() == "" {
		return "", false
	}
	if !isLoopbackName(u.Hostname()) {
		return "", false
	}
	// Reached from the far machine, which is where the tunnel connects
	// from, so its own loopback is the right name to ask it for.
	return net.JoinHostPort("127.0.0.1", u.Port()), true
}

// isLoopbackName reports whether a host names the machine the program
// printing it is running on.
//
// The unspecified addresses count. A program listening on 0.0.0.0
// prints it and means "reach me on this machine".
func isLoopbackName(name string) bool {
	if strings.EqualFold(name, "localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// tunnelTo is a port on this machine that reaches a target on another
// one, opening a tunnel for it when there is not one already.
//
// An existing one is used again: a development server clicked twice
// should not leave two tunnels on the sidebar, and the browser tab
// already open is on the first one's port.
func (a *app) tunnelTo(host, target string) (string, error) {
	if at, ok := a.tunnelAlready(host, target); ok {
		return at, nil
	}
	// Any free port, on the loopback address: this is opened by a
	// click rather than asked for, so it is not one to put on the
	// network.
	t := remote.Tunnel{
		Kind:   remote.LocalForward,
		Listen: "127.0.0.1:0",
		Target: target,
	}
	f, err := a.startTunnel(host, t)
	if err != nil {
		return "", err
	}
	return f.Addr(), nil
}

// tunnelAlready is the local address of a tunnel this window already
// has to a target on a machine, and whether there is one.
func (a *app) tunnelAlready(host, target string) (string, bool) {
	for e, open := range a.tunnels {
		if e.Host != host {
			continue
		}
		got := open.f.Tunnel()
		if got.Kind == remote.LocalForward && sameTarget(got.Target, target) {
			return open.f.Addr(), true
		}
	}
	return "", false
}

// sameTarget reports whether two addresses name one service, with the
// names the far end uses for its own loopback treated as one.
func sameTarget(a, b string) bool {
	ah, ap, err := net.SplitHostPort(a)
	if err != nil {
		return a == b
	}
	bh, bp, err := net.SplitHostPort(b)
	if err != nil {
		return a == b
	}
	if ap != bp {
		return false
	}
	if isLoopbackName(ah) && isLoopbackName(bh) {
		return true
	}
	return strings.EqualFold(ah, bh)
}

// throughTunnel is the address to give the browser: the one the pane
// printed, with the far machine's host and port swapped for this end
// of the tunnel.
func throughTunnel(at, local string) (string, error) {
	u, err := url.Parse(at)
	if err != nil {
		return "", fmt.Errorf("read the address %s: %w", at, err)
	}
	_, port, err := net.SplitHostPort(local)
	if err != nil {
		return "", fmt.Errorf("read the port of the tunnel at %s: %w", local, err)
	}
	// Always 127.0.0.1, whatever the listener calls itself: the tunnel
	// asked for the loopback address and that is the name a browser
	// resolves without asking anybody.
	u.Host = net.JoinHostPort("127.0.0.1", port)
	return u.String(), nil
}
