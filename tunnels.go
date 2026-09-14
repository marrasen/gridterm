package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// tunnel is a forward the window is holding.
type tunnel struct {
	f *remote.Forwarder

	// on is the connection it runs over, so closing that closes this.
	on *machine

	// count is what the panel reads, shared by every stream: the row
	// says what the tunnel is doing, and each stream is the same tunnel
	// doing it.
	count counted
}

// counted is how a tunnel tells the panel what has moved through it.
type counted struct{ m *meter.Meter }

// Wrap returns a writer that counts what goes through it.
func (c counted) Wrap(w io.Writer, out bool) io.Writer {
	return meter.Writer{W: w, M: c.m, Out: out}
}

// tunnelHost returns the machine to run a tunnel over: the one the user
// is looking at, if it is one gridterm has a connection to.
func (a *app) tunnelHost() (string, error) {
	host := a.currentHost()
	if a.machines[host] == nil {
		return "", fmt.Errorf(
			"a tunnel runs over a connection to another machine, and %s is not one",
			groupName(host))
	}
	return host, nil
}

// openTunnelHere asks for a port to forward over the connection to the
// machine the user is looking at.
func (a *app) openTunnelHere() error {
	host, err := a.tunnelHost()
	if err != nil {
		return err
	}

	f := a.newForm("Tunnel over " + host)
	f.Lines = []string{
		"A port on one machine that stands for a service the",
		"other one can reach.",
	}
	listen := f.AddField("Listen on", a.newField("[address:]port", 0))
	target := f.AddField("Reach", a.newField("host:port", 0))

	// The direction is on the buttons rather than in a field: which
	// machine listens is the whole of what a tunnel is, and a word for it
	// in a box would be one more thing to get wrong.
	open := func(kind remote.TunnelKind) func() error {
		return func() error {
			t := remote.Tunnel{
				Kind:   kind,
				Listen: strings.TrimSpace(listen.Text()),
				Target: strings.TrimSpace(target.Text()),
			}
			if err := t.Validate(); err != nil {
				// Returned rather than shown here, so the dialog stays
				// open with what was typed still there to correct.
				return err
			}
			// Not from here: this dialog closes as soon as this returns,
			// and closing one takes anything stacked on top of it.
			a.pump.post(func() { a.confirmTunnel(host, t) })
			return nil
		}
	}
	f.AddButton(ui.Button{Title: "Listen here", Do: open(remote.LocalForward)})
	f.AddButton(ui.Button{Title: "Listen on " + host, Do: open(remote.RemoteForward)})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// openSocksHere asks for a SOCKS5 proxy over the connection to the
// machine the user is looking at.
//
// A dialog of its own rather than a third button on the tunnel one: it
// takes no address to reach, because every stream through it says where
// it is going.
func (a *app) openSocksHere() error {
	host, err := a.tunnelHost()
	if err != nil {
		return err
	}

	f := a.newForm("SOCKS proxy over " + host)
	f.Lines = []string{
		"A proxy on this machine that reaches whatever it is",
		"asked for, as " + host + " sees it.",
	}
	listen := f.AddField("Listen on", a.newField("[address:]port", 0))
	f.AddButton(ui.Button{Title: "Open", Do: func() error {
		t := remote.Tunnel{
			Kind:   remote.DynamicForward,
			Listen: strings.TrimSpace(listen.Text()),
		}
		if err := t.Validate(); err != nil {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return err
		}
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(func() { a.confirmTunnel(host, t) })
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// confirmTunnel asks again when the tunnel would be open to the rest of
// the network, and otherwise opens it.
//
// A forward on anything but the loopback address lets every machine that
// can reach the listening one through the tunnel, with no password asked
// and nothing written down. That is sometimes exactly what is wanted,
// and it is never what somebody wants by accident.
func (a *app) confirmTunnel(host string, t remote.Tunnel) {
	if !t.Exposed() {
		a.openTunnel(host, t)
		return
	}
	where := "this machine"
	if t.Kind == remote.RemoteForward {
		where = host
	}
	through := "to " + t.Target
	if t.Kind == remote.DynamicForward {
		// It has no one address: whoever connects chooses, which is what
		// makes an open one worth asking about twice.
		through = "to wherever it asks for, as " + host + " sees it"
	}
	f := a.newConfirm("Open "+t.Listen+" to the network?", []string{
		"Anything that can reach " + where + " on that port will be",
		"let through " + through + ", with nothing asked.",
	})
	f.AddButton(ui.Button{Title: "Open it", Do: func() error {
		a.pump.post(func() { a.openTunnel(host, t) })
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	// Opens on the button that changes nothing.
	f.FocusButton(1)
	a.showForm(f, nil)
}

// openTunnel starts a forward and puts it on the panel.
func (a *app) openTunnel(host string, t remote.Tunnel) {
	m := a.machines[host]
	if m == nil {
		a.reportError("Could not open the tunnel",
			fmt.Errorf("nothing is connected to %s any more", host))
		return
	}

	count := counted{m: meter.New()}
	e := &conns.Entry{
		Host:  host,
		Kind:  conns.Tunnel,
		Label: t.String(),
		Meter: count.m,
	}
	f, err := m.conn.OpenTunnel(remote.TunnelConfig{
		Tunnel: t,
		Count:  count,
		// Called from the goroutines carrying the streams, so it is
		// handed to the one that draws before anything is shown.
		OnError: func(err error) {
			a.pump.post(func() { a.tunnelFailed(e, err) })
		},
	})
	if err != nil {
		a.reportError("Could not open the tunnel", err)
		return
	}
	// The address it ended up on, which is the only way to learn the port
	// when the tunnel asked for any free one.
	e.Label = tunnelLabel(t, f)
	e.Close = func() error { return a.closeTunnel(e) }
	a.tunnels[e] = &tunnel{f: f, on: m, count: count}
	a.registry.Add(e)
	a.markDirty()
}

// closeTunnel stops a forward and takes its row off the panel.
func (a *app) closeTunnel(e *conns.Entry) error {
	open := a.tunnels[e]
	if open == nil {
		a.registry.Drop(e)
		return nil
	}
	delete(a.tunnels, e)
	a.registry.Drop(e)
	open.count.m.Close()
	return open.f.Close()
}

// tunnelsOn returns the rows of the tunnels running over a connection.
func (a *app) tunnelsOn(m *machine) []*conns.Entry {
	var rows []*conns.Entry
	for e, open := range a.tunnels {
		if open.on == m {
			rows = append(rows, e)
		}
	}
	return rows
}

// dropTunnelsOn closes the tunnels running over a connection that is
// being closed, and takes their rows with them.
func (a *app) dropTunnelsOn(m *machine) error {
	var errs []error
	for _, e := range a.tunnelsOn(m) {
		errs = append(errs, a.closeTunnel(e))
	}
	return errors.Join(errs...)
}

// tunnelsDiedOn marks the tunnels of a connection that dropped on its
// own, leaving their rows behind to be read.
func (a *app) tunnelsDiedOn(m *machine) {
	for _, e := range a.tunnelsOn(m) {
		open := a.tunnels[e]
		delete(a.tunnels, e)
		open.count.m.Close()
		// It went with the connection, so there is nothing left to stop.
		// The row stays until it is cleared, the way a finished command
		// stays.
		e.Close = func() error {
			a.registry.Drop(e)
			return nil
		}
	}
}

// tunnelLabel names a tunnel on the panel, with the port it was given
// when it asked for any free one.
func tunnelLabel(t remote.Tunnel, f *remote.Forwarder) string {
	if _, port, err := net.SplitHostPort(t.Listen); err == nil && strings.TrimSpace(port) != "0" {
		return t.String()
	}
	got := t
	got.Listen = f.Addr()
	return got.String()
}

// tunnelFailed says what went wrong on a tunnel that is already open.
//
// A stream that could not be connected does not close the tunnel: the
// next one may work, and the user asked for the tunnel rather than for
// that one stream. It is still theirs to know about.
func (a *app) tunnelFailed(e *conns.Entry, err error) {
	if a.tunnels[e] == nil {
		// Closed in the meantime, and a stream being cut is what closing
		// looks like from inside.
		return
	}
	a.reportError("Trouble on the tunnel "+e.Label, err)
}

// closeTunnels ends every tunnel the window is holding, for a window
// that is closing.
func (a *app) closeTunnels() error {
	var errs []error
	for e := range a.tunnels {
		errs = append(errs, a.closeTunnel(e))
	}
	return errors.Join(errs...)
}
