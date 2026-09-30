package view

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/words"

	"github.com/marrasen/kakel/look"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/meter"
)

// What a sidebar row shows besides its words: a mark in front saying how
// the thing is doing, green while it is there, breathing while bytes go
// past, grey once it has finished; a little image of what kind of
// thing it is; how far a piece of work has got, filling the row; the
// last seconds of a tunnel's traffic, as a graph; and, while the pointer
// is on a row that can close, a cross that closes it.

// rowMarks are a row's marks and where they were laid out.
type rowMarks struct {
	kind     string
	live     func(now time.Time) meter.State
	fill     float32
	filling  bool
	traffic  *meter.Meter
	shared   func() (agent, watched bool)
	hues     look.Marks
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
	m.shared, m.hues = it.shared, it.hues
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
	if m.moved() {
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
	if m.moved() {
		m.graph = geom.Rc(right-graphRoom+4, (height-12)/2, graphRoom-6, 12)
	}
	_ = end
}

// moved reports whether the row carries traffic that has ever moved:
// only then is room kept for its graph, and the name keeps it before.
func (m *rowMarks) moved() bool {
	if m.traffic == nil {
		return false
	}
	in, out := m.traffic.Totals()
	return in+out > 0
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
		return look.Faint.Get(th.Theme)
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
	m.paintStripe(p, now)
	if m.live != nil {
		colour := markColour(m.live(now), now, f)
		p.RRect(geom.Rc(m.markX-3.5, mid-3.5, 7, 7), 3.5, paint.Solid(colour))
	}
	if m.kind != "" && !m.heading {
		ink := widget.Ink.Get(f.Theme)
		ink.A = 0xb0
		paintIcon(p, m.kind, geom.Rc(m.iconX, mid-iconSize/2, iconSize, iconSize), ink)
	}
	if m.traffic != nil && !m.graph.Empty() {
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

// paintStripe draws, at the row's start, a stripe for each of an agent
// working in the pane and another window watching it, glowing in their
// colours as the pane's rings do.
func (m *rowMarks) paintStripe(p *paint.Painter, now time.Time) {
	if m.shared == nil {
		return
	}
	agent, watched := m.shared()
	var hues []color.NRGBA
	if agent {
		hues = append(hues, m.hues.Agent)
	}
	if watched {
		hues = append(hues, m.hues.Watched)
	}
	const dim, bright = 0x90, 0xe0
	a := uint8(dim + int(float64(bright-dim)*glowAt(now)+0.5))
	for i, c := range hues {
		c.A = a
		p.RRect(geom.Rc(float32(2+3*i), 5, 2, m.height-10), 1, paint.Solid(c))
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
	drawIcon(p, icon.X, r, c, 1.5)
}

// kindIcons are the images for the kinds of row: a terminal, a
// command, a folder, a page, a log, a tunnel, a lock, a window, and one
// for each kind of file work.
var kindIcons = map[string]*icon.Icon{
	"files":    icon.Folder,
	"reader":   icon.FileText,
	"log":      icon.ScrollText,
	"tunnel":   icon.Cable,
	"secrets":  icon.Lock,
	"jobs":     icon.Files,
	"copy":     icon.Copy,
	"move":     icon.FileInput,
	"delete":   icon.Trash2,
	"window":   icon.AppWindow,
	"served":   icon.ScreenShare,
	"command":  icon.SquareChevronRight,
	"terminal": icon.SquareTerminal,
	"split":    icon.Columns2,
	"servers":  icon.Server,
}

// paintIcon draws the little image for a kind of row in r.
func paintIcon(p *paint.Painter, kind string, r geom.Rect, c color.NRGBA) {
	ic, ok := kindIcons[kind]
	if !ok {
		ic = icon.SquareTerminal
	}
	drawIcon(p, ic, r, c, 1.3)
}

// drawIcon draws ic into r tinted c, its strokes thick pixels wide
// whatever r's size.
func drawIcon(p *paint.Painter, ic *icon.Icon, r geom.Rect, c color.NRGBA, thick float32) {
	size := min(r.Size().W, r.Size().H)
	if size <= 0 {
		return
	}
	p.Mask(icon.Stroke{Icon: ic, Width: thick * 24 / size, Progress: 1}, r, c)
}

// markRows gives the sidebar's rows their marks, and puts the file work
// under way under the machine it works on.
func (w *Window) markRows(rows []sideItem, st app.State) []sideItem {
	panes := map[string]app.Pane{}
	all := st.AllPanes
	if all == nil {
		all = st.Panes
	}
	for _, p := range all {
		panes[p.ID] = p
	}
	tunnels := map[string]app.Tunnel{}
	for _, t := range st.Tunnels {
		tunnels[t.ID] = t
	}
	connected := func(m machines.ID) bool {
		return m == "" || slices.Contains(st.Connected, m) ||
			slices.ContainsFunc(st.Windows, func(rw app.RemoteWindow) bool { return rw.Name == m })
	}
	for i := range rows {
		r := &rows[i]
		switch {
		case r.heading && strings.HasPrefix(r.key, "machine:"):
			m := machines.ID(strings.TrimPrefix(r.key, "machine:"))
			if _, _, far := m.Far(); far {
				// A machine a window reached is there while the window is.
				r.live = func(time.Time) meter.State { return meter.Opened }
				continue
			}
			switch {
			case slices.Contains(st.Dialing, m):
				r.live = func(time.Time) meter.State { return meter.Active }
			case connected(m):
				r.live = func(time.Time) meter.State { return meter.Settled }
			case slices.Contains(st.Dropped, m), slices.ContainsFunc(st.Panes, func(p app.Pane) bool { return p.Machine == m }):
				// Its connection went, and its panes stay to be read.
				r.live = func(time.Time) meter.State { return meter.Closed }
			}
			if slices.ContainsFunc(st.Windows, func(rw app.RemoteWindow) bool { return rw.Name == m }) {
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
			if rd, ok := st.Readers[p.ID]; ok && r.note == "" {
				r.note = readerNote(rd)
			}
			sh := w.shells.Get(p.ID)
			if sh != nil {
				// What goes past, as a graph, as for a tunnel.
				r.traffic = sh.Traffic()
			}
			if t, ok := w.terms[p.ID]; ok {
				// The colours of this update, which the terminal takes
				// only after the rows.
				r.shared, r.hues = t.shared, st.Marks
			}
			ended := p.Ended
			r.live = func(now time.Time) meter.State {
				switch {
				case ended:
					return meter.Closed
				case sh != nil && now.Sub(sh.Wrote()) < meter.Settle:
					return meter.Active
				}
				return meter.Settled
			}
		}
	}
	// The file work, each under the machine it works on. A finished
	// one keeps its row, saying how it ended, until it is cleared.
	for _, j := range st.Jobs {
		at := slices.IndexFunc(rows, func(r sideItem) bool { return r.key == "machine:"+string(j.Machine) })
		// A machine a window reached has a heading only while something
		// is open on it; otherwise the work goes under the window.
		if window, _, far := j.Machine.Far(); at < 0 && far {
			at = slices.IndexFunc(rows, func(r sideItem) bool { return r.key == "machine:"+string(window) })
		}
		if at < 0 {
			continue
		}
		at++
		for at < len(rows) && !rows[at].heading {
			at++
		}
		item := sideItem{key: "job:" + j.ID, text: j.Title, note: j.Detail, kind: j.Kind,
			click: app.ShowJobs{}, closes: app.CancelJob{ID: j.ID}, fill: j.Share, filling: j.Share >= 0 && !j.Done,
			live: func(time.Time) meter.State { return meter.Active }}
		if j.Done {
			item.dim, item.closes = true, app.DropJob{ID: j.ID}
			item.live = func(time.Time) meter.State { return meter.Closed }
		}
		rows = slices.Insert(rows, at, item)
	}
	return rows
}

// paneKindIcon is the icon for a pane.
func paneKindIcon(p app.Pane) string {
	switch p.Kind {
	case app.KindFiles:
		return "files"
	case app.KindReader:
		return "reader"
	case app.KindLog:
		return "log"
	case app.KindSecrets:
		return "secrets"
	case app.KindJobs:
		return "jobs"
	case app.KindTunnel:
		return "tunnel"
	case app.KindChooser:
		return "split"
	}
	if p.Command {
		return "command"
	}
	return "terminal"
}

// anyBreathing reports whether a row's mark is moving now, for the
// window to keep drawing while it does.
func (w *Window) anyBreathing(now time.Time) bool {
	if !w.listShown {
		// Out of the tree, the list keeps the rows it had last.
		return false
	}
	for _, k := range w.list.Keys() {
		if row, ok := widget.RowOf[*sideRow](w.list, k); ok && row.marks.live != nil && row.marks.live(now) == meter.Active {
			return true
		}
	}
	return false
}

// readerNote is what a reader's row says beside its name: why the file
// would not read, that it is reading, how big an image is, or how many
// lines the file has, with a plus when there is more than was read.
func readerNote(rd app.Reader) string {
	switch {
	case rd.Err != "":
		return rd.Err
	case rd.Seq == 0:
		return "reading"
	case rd.Pic != nil:
		return strings.TrimSpace(fmt.Sprintf("%d×%d %s", rd.Pic.Was.X, rd.Pic.Was.Y, rd.Pic.Kind))
	case len(rd.Lines) > 0 && rd.Cut:
		return words.Count(len(rd.Lines), "line") + "+"
	case len(rd.Lines) > 0:
		return words.Count(len(rd.Lines), "line")
	}
	return ""
}
