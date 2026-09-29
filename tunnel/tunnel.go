// Package tunnel holds a port forwarded over a connection: what it is
// carrying, how many of its streams failed, and its account, the lines
// written as it goes, with the traffic while somebody watches.
package tunnel

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/marrasen/kakel/logs"
	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/words"
)

// mostLines is how much of a tunnel's account is kept, and mostTapBytes
// how much of one chunk of traffic is written down.
const (
	mostLines    = 500
	mostTapBytes = 4 << 10
)

// Held is a tunnel the program holds. Its methods belong to the
// program's goroutine; what it counts and writes down comes from the
// streams' own.
type Held struct {
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

// New is a tunnel about to open: pass Counter to the connection that
// forwards it, and the forwarder it gives back to Started.
func New() *Held {
	return &Held{count: meter.New(), tap: &trafficTap{}, seen: logs.New(mostLines, nil)}
}

// Counter counts what moves through the tunnel, and writes it down
// while it is watched.
func (h *Held) Counter() remote.Counter { return counted{m: h.count, t: h.tap} }

// Started notes the forwarder the tunnel opened as.
func (h *Held) Started(f *remote.Forwarder) { h.f = f }

// Forwarder is what forwards the tunnel.
func (h *Held) Forwarder() *remote.Forwarder { return h.f }

// Meter counts what goes through it, for the sidebar to draw.
func (h *Held) Meter() *meter.Meter { return h.count }

// Account is the tunnel's account, for its pane to show.
func (h *Held) Account() *logs.Lines { return h.seen }

// Done reports whether it has stopped.
func (h *Held) Done() bool { return h.done }

// Say writes a line into the tunnel's account.
func (h *Held) Say(line string) { _, _ = h.seen.Write([]byte(line + "\n")) }

// Note is what the tunnel's row says: the streams it carries, or how
// many failed while it carries none, and what has moved through it.
func (h *Held) Note() string {
	live := h.f.Streams()
	var s string
	switch {
	case live == 1:
		s = "1 stream"
	case live > 1:
		s = strconv.Itoa(live) + " streams"
	case h.failed > 0:
		s = strconv.Itoa(h.failed) + " failed"
	default:
		s = "idle"
	}
	if in, out := h.count.Totals(); in+out > 0 {
		s += " · " + words.Size(int64(in+out))
	}
	return s
}

// Failed counts a stream that failed, and reports whether it is the
// first, which is the one to tell the user about.
func (h *Held) Failed(err error) (first bool) {
	h.failed++
	h.Say("a stream failed: " + err.Error())
	first = !h.told
	h.told = true
	return first
}

// Stop stops it for why, keeping its account to be read.
func (h *Held) Stop(why string) error {
	h.done = true
	err := h.f.Close()
	h.tap.stop()
	h.count.Close()
	h.Say(why)
	return err
}

// Close closes it, when it has not stopped already.
func (h *Held) Close() error {
	if h.done {
		return nil
	}
	err := h.f.Close()
	h.tap.stop()
	h.count.Close()
	h.Say("closed")
	return err
}

// Watch starts or stops writing its traffic into its account.
func (h *Held) Watch(on bool) {
	if on {
		h.tap.watch(h.seen)
		h.Say("watching what goes through it")
		return
	}
	h.tap.stop()
	h.Say("stopped watching")
}

// Label names the tunnel, with the port it was given when it asked for
// any free one. A listener on this machine alone is named by its port:
// the loopback address before it is the same for every tunnel.
func (h *Held) Label() string {
	got := h.f.Tunnel()
	if host, port, err := net.SplitHostPort(got.Listen); err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			got.Listen = ":" + port
		}
	}
	return got.String()
}

// ListenName is what a question calls a tunnel open to the network. One
// that asked for any free port has no number to give yet.
func ListenName(t remote.Tunnel) string {
	if host, port, err := net.SplitHostPort(t.Listen); err == nil && port == "0" {
		return "a port on " + host + " to the network"
	}
	return t.Listen + " to the network"
}

// Saved is a tunnel written the way kakel keeps it, on the machine host
// kept as it is, or the saved server with ID id.
func Saved(host, id string, t remote.Tunnel) settings.SavedTunnel {
	return settings.SavedTunnel{Host: host, HostID: id, Kind: t.Kind.String(), Listen: t.Listen, Target: t.Target}
}

// Read is a kept tunnel read back.
func Read(saved settings.SavedTunnel) (remote.Tunnel, error) {
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
