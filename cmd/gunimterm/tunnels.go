package main

import (
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gridterm/logs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
)

// Tunnels: ports forwarded over a connection, as gridterm forwards
// them. Each is a row in the sidebar under its machine, saying what it
// is carrying. Its pane is its account, written as it goes: when it
// opened, each stream that failed, and, while asked, the traffic.

// Tunnel is a forwarded port, as the sidebar lists it.
type Tunnel struct {
	ID      string
	Machine string
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
}

// Intents for tunnels.
type (
	// OpenTunnel forwards a port over the connection to Machine, and
	// saves it for next time with Keep. One open to the network is
	// asked about first; Sure is the answer.
	OpenTunnel struct {
		Machine string
		Tunnel  remote.Tunnel
		Keep    bool
		Sure    bool
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

// kindTunnel is a tunnel's pane.
const kindTunnel = "tunnel"

// mostTunnelLines is how much of a tunnel's account is kept, and
// mostTapBytes how much of one chunk of traffic is written down.
const (
	mostTunnelLines = 500
	mostTapBytes    = 4 << 10
)

// tunnel is a tunnel the program holds.
type tunnel struct {
	f     *remote.Forwarder
	count *meter.Meter
	tap   *trafficTap
	seen  *logs.Lines
	// failed counts the streams that failed, and told is set once the
	// first has been shown: a proxy would show one for every dead name
	// a browser looks up.
	failed int
	told   bool
	// done is set once it has stopped: its row and account stay, to be
	// read, until it is cleared.
	done bool
}

// say writes a line into the tunnel's account.
func (t *tunnel) say(line string) { _, _ = t.seen.Write([]byte(line + "\n")) }

// note is what the tunnel's row says: the streams it carries, or how
// many failed while it carries none, and what has moved through it.
func (t *tunnel) note() string {
	live := t.f.Streams()
	var s string
	switch {
	case live == 1:
		s = "1 stream"
	case live > 1:
		s = strconv.Itoa(live) + " streams"
	case t.failed > 0:
		s = strconv.Itoa(t.failed) + " failed"
	default:
		s = "idle"
	}
	if in, out := t.count.Totals(); in+out > 0 {
		s += " · " + humanSize(int64(in+out))
	}
	return s
}

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
	conn, ok := a.conns[in.Machine]
	if !ok {
		return fmt.Errorf("nothing is connected to %s any more", in.Machine)
	}
	t := in.Tunnel
	if err := t.Validate(); err != nil {
		return err
	}
	if (t.Exposed() || t.Kind == remote.RemoteForward) && !in.Sure {
		go a.confirmTunnel(in)
		return nil
	}
	if a.settings != nil {
		saved := asSaved(in.Machine, a.serverID(in.Machine), t)
		switch {
		case in.Keep:
			_ = a.settings.KeepTunnel(saved, mostSavedTunnels)
		case slices.ContainsFunc(a.settings.Tunnels(), saved.Same):
			_ = a.settings.DropTunnel(saved)
		}
		a.st.SavedTunnels = a.settings.Tunnels()
	}
	a.tunnelSeq++
	id := "t" + strconv.Itoa(a.tunnelSeq)
	open := &tunnel{count: meter.New(), tap: &trafficTap{}, seen: logs.New(mostTunnelLines, nil)}
	f, err := conn.OpenTunnel(a.ctx, remote.TunnelConfig{
		Tunnel: t,
		Count:  counted{m: open.count, t: open.tap},
		OnError: func(err error) {
			a.events <- func() { a.tunnelFailed(id, err) }
		},
		OnStopped: func(err error) {
			a.events <- func() { a.tunnelStopped(id, "stopped: "+err.Error(), err) }
		},
	})
	if err != nil {
		return err
	}
	open.f = f
	a.tunnels[id] = open
	label := tunnelLabel(f)
	open.say("opened " + label + " over " + in.Machine)
	a.st.Tunnels = append(slices.Clone(a.st.Tunnels), Tunnel{ID: id, Machine: in.Machine, Label: label, Note: open.note(), Live: true})
	a.notify("Tunnel open", label+", over "+in.Machine, "")
	a.tickTunnels()
	return nil
}

// confirmTunnel asks before opening a tunnel open to the network, and
// opens it on yes. It runs on a goroutine of its own, as asking waits.
func (a *app) confirmTunnel(in OpenTunnel) {
	t := in.Tunnel
	where := "this machine"
	if t.Kind == remote.RemoteForward {
		where = in.Machine
	}
	said := "Anyone who can reach " + where + " on that port is connected to " + t.Target + ", with no authentication."
	if t.Kind == remote.DynamicForward {
		said = "Anyone who can reach " + where + " on that port can connect to anything " + in.Machine + " can reach, with no authentication."
	}
	if t.Kind == remote.RemoteForward {
		said += " " + in.Machine + " chooses where it listens. With GatewayPorts on, that is its whole network."
	}
	ans, err := a.ask(a.ctx, Ask{Title: "Open " + listenName(t) + "?", Text: said, Yes: "Open", Danger: true})
	if err != nil || !ans.Yes {
		return
	}
	in.Sure = true
	a.events <- func() {
		if err := a.openTunnel(in); err != nil {
			a.notify("Couldn't open the tunnel", err.Error(), "")
		}
	}
}

// listenName is the question's own name for what is being opened. A
// tunnel that asked for any free port has no number to give yet.
func listenName(t remote.Tunnel) string {
	if host, port, err := net.SplitHostPort(t.Listen); err == nil && port == "0" {
		return "a port on " + host + " to the network"
	}
	return t.Listen + " to the network"
}

// tunnelLabel names a tunnel, with the port it was given when it asked
// for any free one. A listener on this machine alone is named by its
// port: the loopback address before it is the same for every tunnel.
func tunnelLabel(f *remote.Forwarder) string {
	got := f.Tunnel()
	if host, port, err := net.SplitHostPort(got.Listen); err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			got.Listen = ":" + port
		}
	}
	return got.String()
}

// closeTunnel closes a tunnel and takes its row away. Its pane stays,
// with what it did to read.
func (a *app) closeTunnel(id string) error {
	var err error
	if open, ok := a.tunnels[id]; ok {
		delete(a.tunnels, id)
		if !open.done {
			err = open.f.Close()
			open.tap.stop()
			open.count.Close()
			open.say("closed")
		}
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
	if !ok || open.done {
		return
	}
	open.failed++
	open.say("a stream failed: " + err.Error())
	a.setTunnel(id, func(t *Tunnel) { t.Note = open.note() })
	if !open.told {
		open.told = true
		a.notify("Trouble on the tunnel "+a.st.Tunnels[a.tunnelIndex(id)].Label, err.Error(), "")
	}
}

// tunnelStopped marks a tunnel that ended on its own. Its row stays,
// greyed, until it is cleared, and the user is told once, with err
// when there is one.
func (a *app) tunnelStopped(id, why string, err error) {
	open, ok := a.tunnels[id]
	if !ok || open.done {
		return
	}
	open.done = true
	_ = open.f.Close()
	open.tap.stop()
	open.count.Close()
	open.say(why)
	a.setTunnel(id, func(t *Tunnel) { t.Live, t.Watching, t.Note = false, false, "stopped" })
	if err != nil {
		a.notify("Tunnel "+a.st.Tunnels[a.tunnelIndex(id)].Label+" stopped", err.Error(), "")
	}
}

// tunnelsDiedOn stops the tunnels over a connection that has gone. A
// local one listens here, which the far end going does nothing to, so
// each is closed.
func (a *app) tunnelsDiedOn(machine string) {
	for _, t := range a.st.Tunnels {
		if t.Machine == machine && t.Live {
			a.tunnelStopped(t.ID, "stopped: the connection closed", nil)
		}
	}
}

// watchTunnel starts or stops writing a tunnel's traffic down.
func (a *app) watchTunnel(in WatchTunnel) {
	open, ok := a.tunnels[in.ID]
	if !ok || open.done {
		return
	}
	if in.On {
		open.tap.watch(open.seen)
		open.say("watching what goes through it")
	} else {
		open.tap.stop()
		open.say("stopped watching")
	}
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
		a.st.Focus = t.Pane
		return
	}
	open, ok := a.tunnels[id]
	if !ok {
		return
	}
	a.next++
	pane := "p" + strconv.Itoa(a.next)
	sh := openShell(open.seen.Open(), a.palette, a.hooks(pane))
	a.addPane(Pane{ID: pane, Title: "Tunnel " + t.Label, Machine: t.Machine, Kind: kindTunnel, Tunnel: id}, sh, placement{})
	a.setTunnel(id, func(t *Tunnel) { t.Pane = pane })
}

// tunnelPaneGone lets a tunnel know its pane has closed, and stops
// writing its traffic down: nobody is reading it.
func (a *app) tunnelPaneGone(pane string) {
	for _, t := range a.st.Tunnels {
		if t.Pane != pane {
			continue
		}
		if open, ok := a.tunnels[t.ID]; ok && t.Watching && !open.done {
			open.tap.stop()
			open.say("stopped watching")
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
			if open.done {
				continue
			}
			live++
			if n := open.note(); n != a.st.Tunnels[a.tunnelIndex(id)].Note {
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
	t, err := asTunnel(saved)
	if err != nil {
		return err
	}
	return a.openTunnel(OpenTunnel{Machine: a.savedMachine(saved), Tunnel: t, Keep: true})
}

// savedMachine is the machine a saved tunnel runs over: the saved
// server it was kept for, under its name now, or the name it was kept
// under.
func (a *app) savedMachine(saved settings.SavedTunnel) string {
	for _, h := range a.st.Saved {
		if saved.HostID != "" && h.ID == saved.HostID {
			return h.Name
		}
	}
	return saved.Host
}

// serverID is the ID of the saved server named machine, or "".
func (a *app) serverID(machine string) string {
	for _, h := range a.st.Saved {
		if h.Name == machine {
			return h.ID
		}
	}
	return ""
}

// mostSavedTunnels is how many tunnels are kept, as gridterm keeps them.
const mostSavedTunnels = 50

// asSaved is a tunnel written the way gridterm keeps it.
func asSaved(host, id string, t remote.Tunnel) settings.SavedTunnel {
	return settings.SavedTunnel{Host: host, HostID: id, Kind: t.Kind.String(), Listen: t.Listen, Target: t.Target}
}

// asTunnel is a kept tunnel read back.
func asTunnel(saved settings.SavedTunnel) (remote.Tunnel, error) {
	for _, k := range []remote.TunnelKind{remote.LocalForward, remote.RemoteForward, remote.DynamicForward} {
		if saved.Kind == k.String() {
			return remote.Tunnel{Kind: k, Listen: saved.Listen, Target: saved.Target}, nil
		}
	}
	return remote.Tunnel{}, fmt.Errorf("a tunnel kept as %q is not one this knows", saved.Kind)
}

// counted counts what moves through a tunnel, and writes it down while
// the tunnel is watched.
type counted struct {
	m *meter.Meter
	t *trafficTap
}

// Wrap implements [remote.Counter].
func (c counted) Wrap(w io.Writer, out bool) io.Writer {
	return tapWriter{w: meter.Writer{W: w, M: c.m, Out: out}, t: c.t, out: out}
}

// trafficTap copies what goes through a tunnel into its account, while
// somebody is watching. Off until asked for: a tunnel carries whatever
// it carries, passwords included.
type trafficTap struct {
	mu sync.Mutex
	to *logs.Lines
}

func (t *trafficTap) watch(to *logs.Lines) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.to = to
}

func (t *trafficTap) stop() { t.watch(nil) }

// saw writes one chunk down while the tunnel is watched. It runs on
// the streams' goroutines.
func (t *trafficTap) saw(out bool, p []byte) {
	t.mu.Lock()
	to := t.to
	t.mu.Unlock()
	if to != nil {
		_, _ = to.Write(chunkLines(out, p))
	}
}

// tapWriter is one side of one stream, written down on its way past.
type tapWriter struct {
	w   io.Writer
	t   *trafficTap
	out bool
}

func (w tapWriter) Write(p []byte) (int, error) {
	w.t.saw(w.out, p)
	return w.w.Write(p)
}

// chunkLines is one chunk of traffic as the account shows it: which
// way it went and how much, then the bytes as text, with what would
// draw over the pane as dots.
func chunkLines(out bool, p []byte) []byte {
	head := "← "
	if out {
		head = "→ "
	}
	head += strconv.Itoa(len(p)) + " bytes"
	if len(p) > mostTapBytes {
		p = p[:mostTapBytes]
		head += ", first " + strconv.Itoa(mostTapBytes) + " shown"
	}
	var b strings.Builder
	b.WriteString(head + "\n")
	for _, c := range p {
		switch {
		case c == '\n' || c == '\t':
			b.WriteByte(c)
		case c == '\r':
		case c < ' ' || c >= 0x7f:
			b.WriteByte('.')
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('\n')
	return []byte(b.String())
}
