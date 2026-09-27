package main

import (
	"image/color"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/meter"
)

// What a sidebar row shows besides its words, as gridterm's rows show
// it: a mark in front saying how the thing is doing, green while it is
// there, breathing while bytes go past, grey once it has finished; a
// little picture of what kind of thing it is; how far a piece of work
// has got, filling the row; the last seconds of a tunnel's traffic, as
// a graph; and, while the pointer is on a row that can close, a cross
// that closes it.

// rowMarks are a row's marks and where they were laid out.
type rowMarks struct {
	kind     string
	live     func(now time.Time) meter.State
	fill     float32
	filling  bool
	traffic  *meter.Meter
	depth    int
	heading  bool
	closable bool
	// rate turns the traffic's totals into the speeds the graph draws.
	rate meter.Rate

	markX, iconX float32
	graph, cross geom.Rect
	height       float32
}

// set takes a row's marks from its item.
func (m *rowMarks) set(it sideItem) {
	if m.traffic != it.traffic {
		m.rate = meter.Rate{}
	}
	m.kind, m.live, m.fill, m.filling, m.traffic = it.kind, it.live, it.fill, it.filling, it.traffic
	m.depth, m.heading, m.closable = it.depth, it.heading, it.closes != nil && !it.heading
}

// Measures of the marks.
const (
	markRoom  = 12
	iconRoom  = 20
	iconSize  = 13
	graphRoom = 24
	crossRoom = 18
	depthStep = 12
)

// startRoom is the room the marks take before the words.
func (m *rowMarks) startRoom(*sideRow) float32 {
	room := float32(m.depth*depthStep) + markRoom
	if m.kind != "" && !m.heading {
		room += iconRoom
	}
	return room
}

// endRoom is the room the marks take at the end.
func (m *rowMarks) endRoom(*sideRow) float32 {
	var room float32
	if m.closable {
		room += crossRoom
	}
	if m.traffic != nil {
		room += graphRoom
	}
	return room
}

// laid works out where the marks go in a row padX in from each side,
// whose words end at end.
func (m *rowMarks) laid(padX, end, height, width float32) {
	m.height = height
	m.markX = padX + float32(m.depth*depthStep) + 3
	m.iconX = padX + float32(m.depth*depthStep) + markRoom
	right := width - padX
	m.cross = geom.Rect{}
	if m.closable {
		m.cross = geom.Rc(right-crossRoom+2, (height-14)/2, 14, 14)
		right -= crossRoom
	}
	m.graph = geom.Rect{}
	if m.traffic != nil {
		m.graph = geom.Rc(right-graphRoom+4, (height-12)/2, graphRoom-6, 12)
	}
	_ = end
}

// onCross reports whether p, in the row's space, is on its cross.
func (m *rowMarks) onCross(p geom.Point) bool {
	return !m.cross.Empty() && m.cross.Inset(geom.Uniform(-3)).Contains(p)
}

// Colours of the mark.
var (
	markThere = color.NRGBA{R: 0x4c, G: 0xaf, B: 0x50, A: 0xff}
	markBusy  = color.NRGBA{R: 0x9b, G: 0xe6, B: 0x8e, A: 0xff}
)

// markColour is the mark's colour for a state at now: green while
// there, moving towards the busy colour and back while bytes go past,
// in time with the glow of a shared pane, and faint once finished.
func markColour(state meter.State, now time.Time, th gunim.Frame) color.NRGBA {
	switch state {
	case meter.Closed:
		return faint.Get(th.Theme)
	case meter.Active:
		return anim.Mix(anim.ColorCodec, markThere, markBusy, float32(glowAt(now)))
	}
	return markThere
}

// paintUnder draws what goes under the words: how far the work has got.
func (m *rowMarks) paintUnder(p *paint.Painter, f gunim.Frame, inset geom.Rect) {
	if !m.filling || m.fill <= 0 {
		return
	}
	c := widget.Accent.Get(f.Theme)
	c.A = 0x38
	w := inset.Size().W * min(m.fill, 1)
	p.RRect(geom.Rc(inset.Min.X, inset.Min.Y, w, inset.Size().H), 6, paint.Solid(c))
}

// paint draws the mark, the icon, the graph and the cross.
func (m *rowMarks) paint(p *paint.Painter, f gunim.Frame, r *sideRow) {
	now := f.Now
	mid := m.height / 2
	if m.live != nil {
		colour := markColour(m.live(now), now, f)
		p.RRect(geom.Rc(m.markX-3.5, mid-3.5, 7, 7), 3.5, paint.Solid(colour))
	}
	if m.kind != "" && !m.heading {
		ink := widget.Ink.Get(f.Theme)
		ink.A = 0xb0
		paintIcon(p, m.kind, geom.Rc(m.iconX, mid-iconSize/2, iconSize, iconSize), ink)
	}
	if m.traffic != nil {
		m.paintGraph(p, f, now)
	}
	if m.closable {
		if t := r.hover.Value(); t > 0.01 {
			ink := widget.Ink.Get(f.Theme)
			ink.A = uint8(float32(0xc0) * min(t, 1))
			paintCross(p, m.cross, ink)
		}
	}
}

// paintGraph draws the last seconds of traffic as bars, when anything
// has moved; a row of nothing but zeroes is noise.
func (m *rowMarks) paintGraph(p *paint.Painter, f gunim.Frame, now time.Time) {
	m.rate.Sample(m.traffic, now)
	past := m.rate.Past()
	moved := false
	for _, s := range past {
		if s > 0 {
			moved = true
		}
	}
	if !moved {
		return
	}
	const most = 8
	bars := meter.Bars(past, most)
	c := markThere
	c.A = 0xc0
	w := m.graph.Size().W / float32(meter.Samples)
	for i, b := range bars {
		h := m.graph.Size().H * float32(b) / most
		x := m.graph.Max.X - float32(len(bars)-i)*w
		p.RRect(geom.Rc(x, m.graph.Max.Y-h, max(w-1, 1), h), 0, paint.Solid(c))
	}
}

// paintCross draws a cross in r.
func paintCross(p *paint.Painter, r geom.Rect, c color.NRGBA) {
	at := r.Center()
	for _, rad := range []float32{math.Pi / 4, -math.Pi / 4} {
		func() {
			defer p.Push(paint.Rotate(rad, at))()
			p.RRect(geom.Rc(at.X-5, at.Y-0.75, 10, 1.5), 0.75, paint.Solid(c))
		}()
	}
}

// paintIcon draws the little picture for a kind of row in r, from
// rounded rectangles: a terminal, a folder, a page, a tunnel, a lock, a
// window, and a page for each kind of file work.
func paintIcon(p *paint.Painter, kind string, r geom.Rect, c color.NRGBA) {
	x, y, w, h := r.Min.X, r.Min.Y, r.Size().W, r.Size().H
	line := func(x0, y0, lw, lh float32) { p.RRect(geom.Rc(x0, y0, lw, lh), min(lw, lh)/2, paint.Solid(c)) }
	outline := func(x0, y0, ow, oh, radius float32) {
		p.RRectStroke(geom.Rc(x0, y0, ow, oh), radius, paint.Fill{}, paint.Stroke{Width: 1.3, Color: c})
	}
	switch kind {
	case "files":
		// A folder: a tab and a body.
		line(x, y+2, w*0.45, 2)
		outline(x+0.5, y+3.5, w-1, h-5, 2)
	case "reader", "log":
		// A page with lines on it.
		outline(x+1.5, y+0.5, w-3, h-1, 1.5)
		for i := range 3 {
			lw := w - 7
			if kind == "log" && i%2 == 1 {
				lw -= 3
			}
			line(x+3.5, y+3.5+float32(i)*2.7, lw, 1.2)
		}
	case "tunnel":
		// Two ends and what runs between them.
		p.RRect(geom.Rc(x, y+h/2-2.5, 5, 5), 2.5, paint.Solid(c))
		p.RRect(geom.Rc(x+w-5, y+h/2-2.5, 5, 5), 2.5, paint.Solid(c))
		line(x+4, y+h/2-0.6, w-8, 1.2)
	case "secrets":
		// A padlock.
		outline(x+3, y+0.5, w-6, 7, 3)
		p.RRect(geom.Rc(x+1, y+5.5, w-2, h-6), 1.5, paint.Solid(c))
	case "jobs", "copy", "move", "delete":
		// Pages: one behind the other for a copy, one going for a move,
		// one struck through for a delete.
		switch kind {
		case "delete":
			outline(x+2, y+1, w-4, h-2, 1.5)
			line(x+3.5, y+h/2-0.6, w-7, 1.2)
		case "move":
			outline(x+1, y+2, w-6, h-4, 1.5)
			line(x+w-6, y+h/2-0.6, 6, 1.2)
		default:
			outline(x+3, y, w-5, h-3, 1.5)
			outline(x+0.5, y+3, w-5, h-3, 1.5)
		}
	case "window", "served":
		// A window: a title bar and a pane.
		outline(x+0.5, y+1, w-1, h-2, 2)
		line(x+1, y+1.5, w-2, 2.5)
	default:
		// A terminal: a screen with a prompt on it, or for a command,
		// a filled one.
		outline(x+0.5, y+1, w-1, h-2, 2)
		at := geom.Pt(x+4.5, y+h/2)
		for _, rad := range []float32{math.Pi / 4, -math.Pi / 4} {
			func() {
				defer p.Push(paint.Rotate(rad, geom.Pt(at.X+2, at.Y)))()
				line(at.X-0.2, at.Y-0.6, 3, 1.2)
			}()
		}
		line(x+7.5, y+h/2+2, 3, 1.2)
		if kind == "command" {
			p.RRect(geom.Rc(x+w-4, y+2.5, 2, 2), 1, paint.Solid(c))
		}
	}
}

// markRows gives the sidebar's rows their marks, and puts the file work
// under way under the machine it works on.
func (w *window) markRows(rows []sideItem, st State) []sideItem {
	panes := map[string]Pane{}
	for _, p := range st.Panes {
		panes[p.ID] = p
	}
	tunnels := map[string]Tunnel{}
	for _, t := range st.Tunnels {
		tunnels[t.ID] = t
	}
	connected := func(m string) bool {
		return m == "" || slices.Contains(st.Connected, m) ||
			slices.ContainsFunc(st.Windows, func(rw RemoteWindow) bool { return rw.Name == m })
	}
	for i := range rows {
		r := &rows[i]
		switch {
		case r.heading && strings.HasPrefix(r.key, "machine:"):
			m := strings.TrimPrefix(r.key, "machine:")
			if _, _, far := strings.Cut(m, farSep); far {
				// A machine a window reached is there while the window is.
				r.live = func(time.Time) meter.State { return meter.Opened }
				continue
			}
			switch {
			case slices.Contains(st.Dialing, m):
				r.live = func(time.Time) meter.State { return meter.Active }
			case connected(m):
				r.live = func(time.Time) meter.State { return meter.Settled }
			case slices.Contains(st.Dropped, m), slices.ContainsFunc(st.Panes, func(p Pane) bool { return p.Machine == m }):
				// Its connection went, and its panes stay to be read.
				r.live = func(time.Time) meter.State { return meter.Closed }
			}
			if slices.ContainsFunc(st.Windows, func(rw RemoteWindow) bool { return rw.Name == m }) {
				r.kind = "window"
			}
		case strings.HasPrefix(r.key, "tunnel:"):
			t := tunnels[strings.TrimPrefix(r.key, "tunnel:")]
			r.kind, r.traffic = "tunnel", t.Meter
			live, m := t.Live, t.Meter
			r.live = func(now time.Time) meter.State {
				if !live || m == nil {
					return meter.Closed
				}
				return m.StateAt(now)
			}
		case strings.HasPrefix(r.key, "client:"):
			r.kind = "served"
			r.live = func(time.Time) meter.State { return meter.Settled }
		case strings.HasPrefix(r.key, "window:"):
			r.kind = "terminal"
		case r.pane != "":
			p, ok := panes[r.pane]
			if !ok {
				continue
			}
			r.kind = paneKindIcon(p)
			sh := w.shells.get(p.ID)
			ended := p.Ended
			r.live = func(now time.Time) meter.State {
				switch {
				case ended:
					return meter.Closed
				case sh != nil && sh.wrote != nil && now.Sub(time.Unix(0, sh.wrote.Load())) < meter.Settle:
					return meter.Active
				}
				return meter.Settled
			}
		}
	}
	// The file work, each under the machine it works on. A finished
	// one keeps its row, saying how it ended, until it is cleared.
	for _, j := range st.Jobs {
		at := slices.IndexFunc(rows, func(r sideItem) bool { return r.key == "machine:"+j.Machine })
		if at < 0 {
			continue
		}
		at++
		for at < len(rows) && !rows[at].heading {
			at++
		}
		item := sideItem{key: "job:" + j.ID, text: j.Title, note: j.Detail, kind: j.Kind,
			click: ShowJobs{}, closes: CancelJob{ID: j.ID}, fill: j.Share, filling: j.Share >= 0 && !j.Done,
			live: func(time.Time) meter.State { return meter.Active }}
		if j.Done {
			item.dim, item.closes = true, DropJob{ID: j.ID}
			item.live = func(time.Time) meter.State { return meter.Closed }
		}
		rows = slices.Insert(rows, at, item)
	}
	return rows
}

// paneKindIcon is the icon for a pane.
func paneKindIcon(p Pane) string {
	switch p.Kind {
	case kindFiles:
		return "files"
	case kindReader:
		return "reader"
	case kindLog:
		return "log"
	case kindSecrets:
		return "secrets"
	case kindJobs:
		return "jobs"
	case kindTunnel:
		return "tunnel"
	}
	if p.Command {
		return "command"
	}
	return "terminal"
}

// anyBreathing reports whether a row's mark is moving now, for the
// window to keep drawing while it does.
func (w *window) anyBreathing(now time.Time) bool {
	for _, k := range w.list.Keys() {
		if row, ok := widget.RowOf[*sideRow](w.list, k); ok && row.marks.live != nil && row.marks.live(now) == meter.Active {
			return true
		}
	}
	return false
}
