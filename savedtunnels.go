package main

import (
	"errors"
	"fmt"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
)

// mostSavedTunnels is how many tunnels are kept. Saving one past that
// drops the one saved longest ago.
const mostSavedTunnels = 50

// savedTunnels are the tunnels the user asked to keep, offered by the
// dialog that opens one and by the palette.
type savedTunnels struct {
	// remembered is where they are kept between runs. Nothing is saved
	// while it is nil.
	remembered *settings.Settings
}

// newSavedTunnels builds a list that remembers nothing until it is
// given the settings.
func newSavedTunnels() *savedTunnels { return &savedTunnels{} }

// remember gives the list the settings it reads and writes.
func (c *savedTunnels) remember(set *settings.Settings) { c.remembered = set }

// all is every saved tunnel, newest first.
func (c *savedTunnels) all() []settings.SavedTunnel {
	if c.remembered == nil {
		return nil
	}
	return c.remembered.Tunnels()
}

// has reports whether a tunnel is already kept.
func (c *savedTunnels) has(t settings.SavedTunnel) bool {
	for _, saved := range c.all() {
		if saved.Same(t) {
			return true
		}
	}
	return false
}

// keep saves a tunnel at the front of the list.
func (c *savedTunnels) keep(t settings.SavedTunnel) error {
	if c.remembered == nil {
		return errNoTunnelsToSaveInto
	}
	return c.remembered.KeepTunnel(t, mostSavedTunnels)
}

// forget takes a tunnel off the list.
func (c *savedTunnels) forget(t settings.SavedTunnel) error {
	if c.remembered == nil {
		return errNoTunnelsToSaveInto
	}
	return c.remembered.DropTunnel(t)
}

// errNoTunnelsToSaveInto is what a list with no settings behind it
// answers.
var errNoTunnelsToSaveInto = errors.New(
	"this window has no settings to keep a tunnel in")

// asSaved is a tunnel written the way it is kept.
func asSaved(host string, t remote.Tunnel) settings.SavedTunnel {
	return settings.SavedTunnel{
		Host: host, Kind: t.Kind.String(), Listen: t.Listen, Target: t.Target,
	}
}

// asTunnel is a saved tunnel read back, and an error for a kind this
// version does not know.
func asTunnel(saved settings.SavedTunnel) (remote.Tunnel, error) {
	var kind remote.TunnelKind
	switch saved.Kind {
	case remote.LocalForward.String():
		kind = remote.LocalForward
	case remote.RemoteForward.String():
		kind = remote.RemoteForward
	case remote.DynamicForward.String():
		kind = remote.DynamicForward
	default:
		return remote.Tunnel{}, fmt.Errorf("a tunnel kept as %q is not one this knows", saved.Kind)
	}
	return remote.Tunnel{Kind: kind, Listen: saved.Listen, Target: saved.Target}, nil
}

// savedTunnelTitle is what the palette calls a saved tunnel.
func savedTunnelTitle(saved settings.SavedTunnel) string {
	t, err := asTunnel(saved)
	if err != nil {
		return "Open the tunnel kept as " + saved.Kind + " on " + groupName(saved.Host)
	}
	return "Open " + t.String() + " over " + groupName(saved.Host)
}

// openSavedTunnel opens a tunnel the user kept, over the machine it
// was kept on.
//
// Through the same question a typed one goes through: a tunnel open to
// the network is asked about every time, because it was saved once and
// is opened whenever.
func (a *app) openSavedTunnel(saved settings.SavedTunnel) error {
	t, err := asTunnel(saved)
	if err != nil {
		return err
	}
	if err := t.Validate(); err != nil {
		return err
	}
	if a.about(saved.Host).machine == nil {
		return fmt.Errorf("nothing is connected to %s", groupName(saved.Host))
	}
	a.confirmTunnel(saved.Host, t)
	return nil
}

// listenOn are the addresses the tunnels kept for a machine listen on,
// for a field to offer. Blank first: typing a new one is the usual
// answer, and it is what cycling comes back round to.
func (c *savedTunnels) listenOn(host string) []string {
	out := []string{""}
	for _, saved := range c.all() {
		if saved.Host == host {
			out = append(out, saved.Listen)
		}
	}
	if len(out) == 1 {
		return nil
	}
	return out
}

// findListen is the tunnel kept for a machine that listens on an
// address, and whether there is one.
func (c *savedTunnels) findListen(host, listen string) (settings.SavedTunnel, bool) {
	for _, saved := range c.all() {
		if saved.Host == host && saved.Listen == listen {
			return saved, true
		}
	}
	return settings.SavedTunnel{}, false
}

// keepOrForgetTunnel writes a tunnel down or takes it off the list,
// which is what the tick box on the dialog says to do.
func (a *app) keepOrForgetTunnel(keep bool, host string, t remote.Tunnel) error {
	saved := asSaved(host, t)
	switch {
	case keep:
		return a.savedTuns.keep(saved)
	case a.savedTuns.has(saved):
		// Taken off the list and then unticked, which is how the user
		// says to forget it.
		return a.savedTuns.forget(saved)
	}
	return nil
}
