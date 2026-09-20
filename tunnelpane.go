package main

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/logs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui/term"
)

// mostTunnelLines is how much of a tunnel's account a pane keeps, and
// mostTapBytes how much of one chunk of traffic is written down.
//
// Watching a busy tunnel produces more than anybody reads. The oldest
// go, the way they do in the window's own log.
const (
	mostTunnelLines = 500
	mostTapBytes    = 4 << 10
)

// trafficTap copies what goes through a tunnel into its account, while
// somebody is watching.
//
// Off until asked for. A tunnel carries whatever it carries, including
// what somebody logged into a database is typing, and keeping that
// because a pane happened to be open is not something to do quietly.
type trafficTap struct {
	mu sync.Mutex

	// to is where the traffic goes, and nil while nobody is watching.
	to *logs.Lines
}

// watch starts copying the traffic into a log, and stop stops.
func (t *trafficTap) watch(to *logs.Lines) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.to = to
}

func (t *trafficTap) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.to = nil
}

// on reports whether the traffic is being watched.
func (t *trafficTap) on() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.to != nil
}

// saw writes one chunk of traffic down, and does nothing while nobody
// is watching.
//
// Called from the goroutines carrying the streams, so it takes the
// lock for long enough to read where the traffic goes and no longer.
func (t *trafficTap) saw(out bool, p []byte) {
	t.mu.Lock()
	to := t.to
	t.mu.Unlock()
	if to == nil {
		return
	}
	// The write cannot fail: a log with nowhere else to go keeps the
	// line in memory and says so.
	_, _ = to.Write(chunkLines(out, p))
}

// tapWriter is one side of one stream, written down on its way past.
type tapWriter struct {
	w   io.Writer
	t   *trafficTap
	out bool
}

func (w tapWriter) Write(p []byte) (int, error) {
	// Written down before it is sent, so a write that fails still says
	// what was being sent when it did.
	w.t.saw(w.out, p)
	return w.w.Write(p)
}

// Wrap returns a writer that counts what goes through it, and writes
// it down as well while the tunnel is being watched.
func (c counted) Wrap(w io.Writer, out bool) io.Writer {
	counting := meter.Writer{W: w, M: c.m, Out: out}
	if c.t == nil {
		return counting
	}
	return tapWriter{w: counting, t: c.t, out: out}
}

// chunkLines is one chunk of traffic as a pane shows it: which way it
// went and how much, then the bytes as text.
func chunkLines(out bool, p []byte) []byte {
	arrow := "←"
	if out {
		arrow = "→"
	}
	head := arrow + " " + strconv.Itoa(len(p)) + " bytes"
	body := p
	if len(body) > mostTapBytes {
		body = body[:mostTapBytes]
		head += ", first " + strconv.Itoa(mostTapBytes) + " shown"
	}
	var b strings.Builder
	b.WriteString(head)
	b.WriteByte('\n')
	b.Write(readableBytes(body))
	b.WriteByte('\n')
	return []byte(b.String())
}

// readableBytes is a chunk of traffic with everything that would draw
// over the pane taken out.
//
// A tab and a line break are kept, because what goes through a tunnel
// to a web server is text laid out with them. Everything else that is
// not printable becomes a dot, the way a hex dump shows it.
func readableBytes(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for _, c := range p {
		switch {
		case c == '\n' || c == '\t':
			out = append(out, c)
		case c == '\r':
			// Dropped: the line break beside it is the line break.
		case c < ' ' || c >= 0x7f:
			out = append(out, '.')
		default:
			out = append(out, c)
		}
	}
	return out
}

// say writes a line into a tunnel's account.
func (open *tunnel) say(line string) {
	if open.seen == nil {
		return
	}
	_, _ = open.seen.Write([]byte(line + "\n"))
}

// showTunnel opens the pane for a tunnel, or goes to the one already
// open.
//
// It is what clicking a tunnel's row does. The pane says what the
// tunnel has been doing, offers to watch what goes through it, and is
// where the tunnel is closed from.
func (a *app) showTunnel(e *conns.Entry) {
	if pane := a.tunnelPanes[e]; pane != nil {
		a.focus(pane)
		return
	}
	open := a.tunnels[e]
	if open == nil {
		a.reportError("Could not open that tunnel's pane",
			errors.New("the tunnel has already closed"))
		return
	}
	pane, err := a.newTerminalOn(open.seen.Open(), e.Host, conns.Log, "tunnel "+e.Label)
	if err != nil {
		a.reportError("Could not open that tunnel's pane", err)
		return
	}
	if err := a.placePane(pane); err != nil {
		// The pane is reading the account on a goroutine of its own
		// already. Left here it would read it for ever, into a pane
		// nowhere on the screen.
		delete(a.panes, pane)
		delete(a.started, pane)
		a.reportError("Could not open that tunnel's pane", errors.Join(err, pane.Close()))
		return
	}
	if a.tunnelPanes == nil {
		a.tunnelPanes = map[*conns.Entry]*term.Terminal{}
	}
	a.tunnelPanes[e] = pane
	a.askAboutTunnel(pane, e)
	a.showPane(pane)
}

// askAboutTunnel puts the tunnel's own choices along the bottom of its
// pane: watching what goes through it, and closing it.
//
// Put back after each choice, because a choice answered takes the
// question away and these are not questions but the controls the pane
// has.
func (a *app) askAboutTunnel(pane *term.Terminal, e *conns.Entry) {
	open := a.tunnels[e]
	if open == nil {
		// The tunnel has closed. What it did is still worth reading, so
		// the pane stays with nothing left to press.
		pane.Ask("")
		return
	}
	watch := term.Choice{Label: "Watch the traffic", Do: func() error {
		return a.watchTunnel(pane, e, true)
	}}
	if open.watching.on() {
		watch = term.Choice{Label: "Stop watching", Do: func() error {
			return a.watchTunnel(pane, e, false)
		}}
	}
	pane.Ask(e.Label,
		watch,
		term.Choice{Label: "Close the tunnel", Do: func() error {
			return a.closeTunnelFromPane(pane, e)
		}})
}

// watchTunnel turns the copying of a tunnel's traffic on or off.
func (a *app) watchTunnel(pane *term.Terminal, e *conns.Entry, on bool) error {
	open := a.tunnels[e]
	if open == nil {
		return errors.New("the tunnel has already closed")
	}
	if on {
		open.watching.watch(open.seen)
		open.say("watching what goes through it")
	} else {
		open.watching.stop()
		open.say("stopped watching")
	}
	a.askAboutTunnel(pane, e)
	return nil
}

// closeTunnelFromPane closes the tunnel and leaves the pane showing
// what it did.
func (a *app) closeTunnelFromPane(pane *term.Terminal, e *conns.Entry) error {
	open := a.tunnels[e]
	if open != nil {
		open.watching.stop()
	}
	err := a.closeTunnel(e)
	a.askAboutTunnel(pane, e)
	if err != nil {
		return fmt.Errorf("close %s: %w", e.Label, err)
	}
	return nil
}

// forgetTunnelPanes lets go of the panes whose tunnel or whose pane has
// gone, and stops the copying with them.
func (a *app) forgetTunnelPanes() {
	for e, pane := range a.tunnelPanes {
		if _, live := a.panes[pane]; live {
			continue
		}
		// The pane has been closed. Nothing is reading the traffic any
		// more, so nothing writes it down.
		if open := a.tunnels[e]; open != nil {
			open.watching.stop()
		}
		delete(a.tunnelPanes, e)
	}
}

// tunnelPaneToldItStopped takes the tunnel's controls off its pane,
// for a tunnel that has closed. What it did is still worth reading, so
// the pane stays.
func (a *app) tunnelPaneToldItStopped(e *conns.Entry) {
	if pane := a.tunnelPanes[e]; pane != nil {
		a.askAboutTunnel(pane, e)
	}
}
