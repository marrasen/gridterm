package app

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/tunnel"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
)

// Tunnels: ports forwarded over a connection. Each is a row in the
// sidebar under its machine, saying what it is carrying. Its pane is its
// account, written as it goes: when it opened, each stream that failed,
// and, while asked, the traffic.

// Tunnel is a forwarded port, as the sidebar lists it.
type Tunnel struct {
	ID      string
	Machine machines.ID
	// Label says what it forwards, as ":8080 → db:5432".
	Label string
	// Note says what it is doing: the streams it carries, how many
	// failed, or that it stopped.
	Note string
	// Live is set while it forwards, and Watching while its traffic is
	// written into its account.
	Live, Watching bool
	// Pane is the pane showing its account, or "".
	Pane string
	// Meter counts what goes through it, for the sidebar to draw.
	Meter *meter.Meter
}

// Intents for tunnels.
type (
	// OpenTunnel forwards a port over the connection to Machine, and
	// saves it for next time with Keep. One open to the network is
	// asked about first; Sure is the answer. Saved says it was opened
	// from the saved list, whose order stays as it is.
	OpenTunnel struct {
		Machine machines.ID
		Tunnel  remote.Tunnel
		Keep    bool
		Sure    bool
		Saved   bool
	}
	// OpenSavedTunnel opens a tunnel kept from before.
	OpenSavedTunnel struct{ Saved settings.SavedTunnel }
	// CloseTunnel closes a tunnel, or clears the row of one stopped.
	CloseTunnel struct{ ID string }
	// WatchTunnel starts or stops writing a tunnel's traffic into its
	// account.
	WatchTunnel struct {
		ID string
		On bool
	}
	// ShowTunnel opens a tunnel's pane, or goes to it.
	ShowTunnel struct{ ID string }
)

// KindTunnel is a tunnel's pane.
const KindTunnel = "tunnel"

// tunnelIndex returns the index of tunnel id in the state, or -1.
func (a *app) tunnelIndex(id string) int {
	return slices.IndexFunc(a.st.Tunnels, func(t Tunnel) bool { return t.ID == id })
}

// setTunnel changes tunnel id's row, copying the rows first: the
// window holds the last ones published.
func (a *app) setTunnel(id string, change func(*Tunnel)) {
	i := a.tunnelIndex(id)
	if i < 0 {
		return
	}
	a.st.Tunnels = slices.Clone(a.st.Tunnels)
	change(&a.st.Tunnels[i])
}

// openTunnel forwards a port. One open to the network, and any remote
// one, since the far machine picks where it listens, is asked about
// first.
func (a *app) openTunnel(in OpenTunnel) error {
	conn, ok, err := a.connOf(in.Machine)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("nothing is connected to %s any more", a.machines.Name(in.Machine))
	}
	t := in.Tunnel
	if err := t.Validate(); err != nil {
		return err
	}
	if (t.Exposed() || t.Kind == remote.RemoteForward) && !in.Sure {
		go a.confirmTunnel(in, a.machines.Name(in.Machine))
		return nil
	}
	if a.settings != nil && !in.Saved {
		saved := tunnel.Saved(a.keptAs(in.Machine), a.serverID(in.Machine), t)
		switch {
		case in.Keep:
			if err := a.settings.KeepTunnel(saved, mostSavedTunnels); err != nil {
				a.failed("Couldn't save the tunnel", err.Error())
			}
		case slices.ContainsFunc(a.settings.Tunnels(), saved.Same):
			if err := a.settings.DropTunnel(saved); err != nil {
				a.failed("Couldn't forget the tunnel", err.Error())
			}
		}
		a.st.SavedTunnels = a.settings.Tunnels()
	}
	a.tunnelSeq++
	id := "t" + strconv.Itoa(a.tunnelSeq)
	open := tunnel.New()
	f, err := conn.OpenTunnel(a.ctx, remote.TunnelConfig{
		Tunnel: t,
		Count:  open.Counter(),
		OnError: func(err error) {
			a.events <- func() { a.tunnelFailed(id, err) }
		},
		OnStopped: func(err error) {
			a.events <- func() { _ = a.tunnelStopped(id, "stopped: "+err.Error(), err) }
		},
	})
	if err != nil {
		return err
	}
	open.Started(f)
	a.tunnels[id] = open
	label := open.Label()
	open.Say("opened " + label + " over " + a.machines.Name(in.Machine))
	a.st.Tunnels = append(slices.Clone(a.st.Tunnels), Tunnel{ID: id, Machine: in.Machine, Label: label, Note: open.Note(), Live: true, Meter: open.Meter()})
	a.worked("Tunnel open", label+", over "+a.machines.Name(in.Machine), "")
	a.tickTunnels()
	return nil
}

// confirmTunnel asks before opening a tunnel open to the network, and
// opens it on yes. It runs on a goroutine of its own, as asking waits;
// called is what the machine it goes over is called.
func (a *app) confirmTunnel(in OpenTunnel, called string) {
	t := in.Tunnel
	where := "this machine"
	if t.Kind == remote.RemoteForward {
		where = called
	}
	said := "Anyone who can reach " + where + " on that port is connected to " + t.Target + ", with no authentication."
	if t.Kind == remote.DynamicForward {
		said = "Anyone who can reach " + where + " on that port can connect to anything " + called + " can reach, with no authentication."
	}
	if t.Kind == remote.RemoteForward {
		said += " " + called + " chooses where it listens. With GatewayPorts on, that is its whole network."
	}
	ans, err := a.ask(a.ctx, Ask{Title: "Open " + tunnel.ListenName(t) + "?", Text: said, Yes: "Open", Danger: true})
	if err != nil || !ans.Yes {
		return
	}
	in.Sure = true
	a.events <- func() {
		if err := a.openTunnel(in); err != nil {
			a.failed("Couldn't open the tunnel", err.Error())
			a.problem()
		}
	}
}

// closeTunnel closes a tunnel and takes its row away. Its pane stays,
// with what it did to read.
func (a *app) closeTunnel(id string) error {
	var err error
	if open, ok := a.tunnels[id]; ok {
		delete(a.tunnels, id)
		err = open.Close()
	}
	if i := a.tunnelIndex(id); i >= 0 {
		a.st.Tunnels = slices.Delete(slices.Clone(a.st.Tunnels), i, i+1)
	}
	return err
}

// tunnelFailed counts a stream that failed on a tunnel still open. The
// first is shown; the rest are counted on the row.
func (a *app) tunnelFailed(id string, err error) {
	open, ok := a.tunnels[id]
	if !ok || open.Done() {
		return
	}
	first := open.Failed(err)
	a.setTunnel(id, func(t *Tunnel) { t.Note = open.Note() })
	if first {
		a.failed("Trouble on the tunnel "+a.st.Tunnels[a.tunnelIndex(id)].Label, err.Error())
		a.problem()
	}
}

// tunnelStopped marks a tunnel that ended on its own. Its row stays,
// greyed, until it is cleared, and the user is told once, with err
// when there is one.
func (a *app) tunnelStopped(id, why string, err error) error {
	open, ok := a.tunnels[id]
	if !ok || open.Done() {
		return nil
	}
	closeErr := open.Stop(why)
	a.setTunnel(id, func(t *Tunnel) { t.Live, t.Watching, t.Note = false, false, "stopped" })
	if err != nil {
		a.failed("Tunnel "+a.st.Tunnels[a.tunnelIndex(id)].Label+" stopped", err.Error())
		a.problem()
	}
	return closeErr
}

// tunnelsDiedOn stops the tunnels over a connection that has gone. A
// local one listens here, which the far end going does nothing to, so
// each is closed. A connection let go of on purpose takes its tunnels'
// rows with it; one that dropped leaves them, stopped, until cleared.
func (a *app) tunnelsDiedOn(machine machines.ID, letGo bool) error {
	var errs []error
	for _, t := range slices.Clone(a.st.Tunnels) {
		switch {
		case t.Machine != machine:
		case letGo:
			errs = append(errs, a.closeTunnel(t.ID))
		case t.Live:
			errs = append(errs, a.tunnelStopped(t.ID, "stopped: the connection closed", nil))
		}
	}
	return errors.Join(errs...)
}

// watchTunnel starts or stops writing a tunnel's traffic down.
func (a *app) watchTunnel(in WatchTunnel) {
	open, ok := a.tunnels[in.ID]
	if !ok || open.Done() {
		return
	}
	open.Watch(in.On)
	a.setTunnel(in.ID, func(t *Tunnel) { t.Watching = in.On })
}

// showTunnel goes to a tunnel's pane, opening it first when it has
// none: a terminal reading the tunnel's account.
func (a *app) showTunnel(id string) {
	i := a.tunnelIndex(id)
	if i < 0 {
		return
	}
	t := a.st.Tunnels[i]
	if t.Pane != "" && a.has(t.Pane) {
		a.bringHere(t.Pane)
		return
	}
	open, ok := a.tunnels[id]
	if !ok {
		return
	}
	a.next++
	pane := "p" + strconv.Itoa(a.next)
	sh := screen.Open(open.Account().Open(), a.palette, a.hooks(pane))
	a.addPane(Pane{ID: pane, Title: "Tunnel " + t.Label, Machine: t.Machine, Kind: KindTunnel, Tunnel: id}, sh, Placement{})
	a.setTunnel(id, func(t *Tunnel) { t.Pane = pane })
}

// tunnelPaneGone lets a tunnel know its pane has closed, and stops
// writing its traffic down: nobody is reading it.
func (a *app) tunnelPaneGone(pane string) {
	for _, t := range a.st.Tunnels {
		if t.Pane != pane {
			continue
		}
		if open, ok := a.tunnels[t.ID]; ok && t.Watching && !open.Done() {
			open.Watch(false)
		}
		a.setTunnel(t.ID, func(t *Tunnel) { t.Pane, t.Watching = "", false })
	}
}

// tickTunnels brings the tunnels' notes up to date each second, while
// any is open. Only a change is published.
func (a *app) tickTunnels() {
	if a.ticking {
		return
	}
	a.ticking = true
	var tick func()
	tick = func() {
		a.quiet = true
		live := 0
		for id, open := range a.tunnels {
			if open.Done() {
				continue
			}
			live++
			if n := open.Note(); n != a.st.Tunnels[a.tunnelIndex(id)].Note {
				a.setTunnel(id, func(t *Tunnel) { t.Note = n })
				a.quiet = false
			}
		}
		if live == 0 {
			a.ticking = false
			return
		}
		time.AfterFunc(time.Second, func() { a.events <- tick })
	}
	time.AfterFunc(time.Second, func() { a.events <- tick })
}

// openSavedTunnel opens a tunnel kept from before, over the machine it
// was kept on, by the name that machine has now.
func (a *app) openSavedTunnel(saved settings.SavedTunnel) error {
	t, err := tunnel.Read(saved)
	if err != nil {
		return err
	}
	machine, err := a.machineNow(saved.Host, saved.HostID)
	if err != nil {
		return err
	}
	return a.openTunnel(OpenTunnel{Machine: machine, Tunnel: t, Keep: true, Saved: true})
}

// serverID is the ID of the saved server named machine, or "".
func (a *app) serverID(machine machines.ID) string {
	// Beyond a window: the window's.
	machine, _, _ = machine.Far()
	if _, ok := a.machines.Saved(machine); ok {
		return string(machine)
	}
	return ""
}

// mostSavedTunnels is how many tunnels are kept.
const mostSavedTunnels = 50
