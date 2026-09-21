package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/logs"
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

	// trouble is how many streams have failed on this tunnel, and told
	// records that the user has been shown one of them.
	//
	// A dialog for every failed stream would be a dialog for every dead
	// name a browser looks up through a SOCKS proxy, and every one of
	// them costs a full-window layer. The first one says what is wrong;
	// the rest are counted on the row.
	trouble int
	told    bool

	// seen is what this tunnel has been doing, for its pane to show:
	// when it opened, every stream that failed, and the traffic itself
	// while somebody is watching.
	seen *logs.Lines

	// watching is what copies the traffic into seen, and is off until
	// the pane asks for it.
	watching *trafficTap
}

// counted is how a tunnel tells the panel what has moved through it,
// and how the pane watching it reads what goes past.
type counted struct {
	m *meter.Meter
	t *trafficTap
}

// confirmTunnel asks again when the tunnel would be open to the rest of
// the network, and otherwise opens it.
//
// A forward on anything but the loopback address lets every machine that
// can reach the listening one through the tunnel, with no password asked
// and nothing written down. That is sometimes exactly what is wanted,
// and it is never what somebody wants by accident.
func (a *app) confirmTunnel(host string, t remote.Tunnel) {
	// A remote forward is asked about whatever address it names. Where
	// the far machine really binds it is the far machine's decision: a
	// server set to share forwarded ports binds them for its whole
	// network however the client asked, and gridterm is never told.
	if !t.Exposed() && t.Kind != remote.RemoteForward {
		a.openTunnel(host, t)
		return
	}

	where := "this machine"
	if t.Kind == remote.RemoteForward {
		where = host
	}
	said := "Anyone who can reach " + where + " on that port is connected to " +
		t.Target + ", with no authentication."
	if t.Kind == remote.DynamicForward {
		// It has no one address: whoever connects chooses, which is what
		// makes an open one worth asking about twice.
		said = "Anyone who can reach " + where + " on that port can connect to " +
			"anything " + host + " can reach, with no authentication."
	}
	if t.Kind == remote.RemoteForward {
		said += " " + host + " chooses where it listens. With GatewayPorts on," +
			" that is its whole network."
	}
	// Wrapped to what a dialog shows without trimming: the clause this
	// question turns on is the last one, and a line cut off at the edge
	// of the box would lose it.
	f := a.newConfirm(dlgOpenToNetwork+listenName(t)+"?", wrapLines(said, errorLineWidth))
	f.AddButton(ui.Button{Title: btnOpen, Do: func() error {
		a.pump.post(func() { a.openTunnel(host, t) })
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	// Opens on the button that changes nothing.
	f.FocusButton(1)
	a.showForm(f, nil)
}

// listenName is the question's own name for what is being opened. A
// tunnel that asked for any free port has no number to give yet.
func listenName(t remote.Tunnel) string {
	if host, port, err := net.SplitHostPort(t.Listen); err == nil && port == "0" {
		return "a port on " + host + " to the network"
	}
	return t.Listen + " to the network"
}

// openTunnel starts a forward and puts it on the panel.
func (a *app) openTunnel(host string, t remote.Tunnel) {
	if _, err := a.startTunnel(host, t); err != nil {
		a.reportError("Could not open the tunnel", err)
	}
}

// startTunnel is openTunnel with the forward handed back, for a caller
// that needs the port it ended up on.
func (a *app) startTunnel(host string, t remote.Tunnel) (*remote.Forwarder, error) {
	m := a.about(host).machine
	if m == nil {
		return nil, fmt.Errorf("nothing is connected to %s any more", host)
	}

	seen := logs.New(mostTunnelLines, nil)
	count := counted{m: meter.New(), t: &trafficTap{}}
	e := &conns.Entry{
		Host:  host,
		Kind:  conns.Tunnel,
		Label: t.String(),
		Meter: count.m,
	}
	f, err := m.conn.OpenTunnel(a.ctx, remote.TunnelConfig{
		Tunnel: t,
		Count:  count,
		// Called from the goroutines carrying the streams, so both are
		// handed to the one that draws before anything is shown.
		OnError: func(err error) {
			a.pump.post(func() { a.tunnelFailed(e, err) })
		},
		OnStopped: func(err error) {
			a.pump.post(func() { a.tunnelStopped(e, err) })
		},
	})
	if err != nil {
		return nil, err
	}
	// The address it ended up on, which is the only way to learn the port
	// when the tunnel asked for any free one.
	e.Label = tunnelLabel(f)
	e.Close = func() error { return a.closeTunnel(e) }
	e.Reveal = func() { a.showTunnel(e) }
	open := &tunnel{f: f, on: m, count: count, seen: seen, watching: count.t}
	a.tunnels[e] = open
	open.say("opened " + e.Label + " over " + groupName(host))
	a.registry.Add(e)
	a.markDirty()
	return f, nil
}

// closeTunnel stops a forward and takes its row off the panel.
//
// The row goes whether or not the forwarder closed cleanly: a second
// attempt would give the same answer, so there is nothing left for the
// user to press. What went wrong is returned, and reaches them.
func (a *app) closeTunnel(e *conns.Entry) error {
	open := a.tunnels[e]
	if open == nil {
		a.registry.Drop(e)
		return nil
	}
	delete(a.tunnels, e)
	err := open.f.Close()
	open.watching.stop()
	open.say("closed")
	open.count.m.Close()
	a.tunnelPaneToldItStopped(e)
	a.registry.Drop(e)
	a.markDirty()
	return err
}

// note is what the tunnel's row says when nothing is moving through it.
//
// What it is carrying, or how many streams have failed when it is
// carrying nothing: a tunnel whose every stream fails looks exactly like
// an idle one otherwise, and the difference is the whole question.
func (open *tunnel) note() string {
	live := open.f.Streams()
	if live == 0 && open.trouble > 0 {
		return strconv.Itoa(open.trouble) + " failed"
	}
	return streams(live)
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

// dropTunnelsOn takes the rows of a connection's tunnels away, for a
// connection that has just been closed.
//
// The forwarders are not closed here. Closing the connection closed them
// as riders and kept what went wrong; closing them again would return
// the same answer a second time and report every failure twice.
func (a *app) dropTunnelsOn(m *machine) {
	for _, e := range a.tunnelsOn(m) {
		open := a.tunnels[e]
		delete(a.tunnels, e)
		open.count.m.Close()
		a.registry.Drop(e)
	}
}

// tunnelsDiedOn closes the tunnels of a connection that dropped on its
// own, leaving their rows behind to be read.
//
// The forwarder has to be closed even though the connection has gone: a
// local forward listens on a socket of this machine, which the far end
// dropping does nothing to. Left alone it would keep accepting, answer
// every stream with a refusal, and hold the port for the life of the
// window with nothing able to take it back.
func (a *app) tunnelsDiedOn(m *machine) error {
	var errs []error
	for _, e := range a.tunnelsOn(m) {
		open := a.tunnels[e]
		delete(a.tunnels, e)
		errs = append(errs, open.f.Close())
		open.count.m.Close()
		// Nothing left to stop, so the row stays until it is cleared,
		// the way a finished command stays. It says closed rather than
		// how many streams it had a moment ago.
		e.Note = ""
		drop := func() error {
			a.registry.Drop(e)
			return nil
		}
		e.Close, e.Clear = drop, drop
	}
	return errors.Join(errs...)
}

// tunnelLabel names a tunnel on the panel, with the port it was given
// when it asked for any free one.
//
// The panel is narrow, so a listener on this machine alone is named by
// its port: the loopback address in front of it is the same for every
// tunnel and would push out the end that says where it goes.
func tunnelLabel(f *remote.Forwarder) string {
	// What it is really listening on, which is the only way to learn the
	// port when the tunnel asked for any free one.
	got := f.Tunnel()
	if host, port, err := net.SplitHostPort(got.Listen); err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			got.Listen = ":" + port
		}
	}
	return got.String()
}

// tunnelFailed says what went wrong on one stream of a tunnel that is
// still open.
//
// A stream that could not be connected does not close the tunnel: the
// next one may work, and the user asked for the tunnel rather than for
// that one stream. The first failure is shown, because something is
// wrong and they cannot see it otherwise; the rest are counted on the
// row, or a proxy would open a dialog for every dead name a browser
// looks up.
func (a *app) tunnelFailed(e *conns.Entry, err error) {
	open := a.tunnels[e]
	if open == nil {
		// Closed in the meantime, and a stream being cut is what closing
		// looks like from inside.
		return
	}
	open.trouble++
	open.say("a stream failed: " + err.Error())
	a.markDirty()
	if open.told {
		return
	}
	open.told = true
	a.reportError("Trouble on the tunnel "+e.Label, err)
}

// tunnelStopped takes away a tunnel that has ended on its own.
//
// The listener failed, so nothing will ever be accepted on it again. The
// row goes grey rather than staying as something that looks open, and
// the user is told once: they asked for this tunnel and it is gone.
func (a *app) tunnelStopped(e *conns.Entry, err error) {
	open := a.tunnels[e]
	if open == nil {
		return
	}
	delete(a.tunnels, e)
	open.watching.stop()
	open.say("stopped: " + err.Error())
	open.count.m.Close()
	a.tunnelPaneToldItStopped(e)
	e.Note = ""
	drop := func() error {
		a.registry.Drop(e)
		return nil
	}
	e.Close, e.Clear = drop, drop
	a.markDirty()
	a.reportError("Tunnel "+e.Label+" stopped", err)
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
