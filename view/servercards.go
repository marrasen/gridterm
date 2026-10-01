package view

import (
	"hash/fnv"
	"image/color"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// The Servers pane shows each machine as a card: a badge in its own
// colour, its name, how its connection is doing, who and where it is,
// buttons for what is opened there most, and under them what is open
// on it in every window. The cards stand side by side as the pane is
// wide, under three headings: this computer, the machines connected,
// and the servers saved.
//
// A card's header is the machine's heading row, drawn as a card's: its
// ⋯ opens the machine's menu, as the heading's plus did.

// cardState is how a card's machine is doing, for its pill.
type cardState int

const (
	cardOff cardState = iota
	cardLocal
	cardConnected
	cardDialing
	cardLost
)

// The sections the cards stand in, in order.
const (
	sectionHere = iota
	sectionConnected
	sectionSaved
	sections
)

var sectionTitles = [sections]string{"THIS COMPUTER", "CONNECTED", "SAVED"}

// cardAction is a button on a card, and what it asks the program.
type cardAction struct {
	label string
	ic    *icon.Icon
	in    gunim.Intent
}

// do asks what the button does.
func (a cardAction) do(w *Window, u *gunim.UI) { u.Send(w, a.in) }

// cardInfo is what a card's header shows of its machine.
type cardInfo struct {
	name, sub string
	state     cardState
	// since is when it connected, and rtt the round trip its last ping
	// took, said on its pill.
	since time.Time
	rtt   time.Duration
	// silent says its last ping went unanswered.
	silent  bool
	badge   string
	hue     color.NRGBA
	actions []cardAction
	section int
}

// cardHues are the badges' colours, a machine's picked by its name, so
// it keeps its colour from one run to the next.
var cardHues = []color.NRGBA{
	{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff},
	{R: 0x4c, G: 0xc3, B: 0x8a, A: 0xff},
	{R: 0xe8, G: 0xb3, B: 0x4a, A: 0xff},
	{R: 0xd9, G: 0x6c, B: 0xc8, A: 0xff},
	{R: 0x9b, G: 0x7b, B: 0xff, A: 0xff},
	{R: 0x3f, G: 0xc1, B: 0xc9, A: 0xff},
	{R: 0xe5, G: 0x7a, B: 0x5a, A: 0xff},
	{R: 0x8f, G: 0xb5, B: 0x4a, A: 0xff},
}

// hueOf is name's badge colour.
func hueOf(name string) color.NRGBA {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return cardHues[h.Sum32()%uint32(len(cardHues))]
}

// badgeOf is the two letters on name's badge: the first letters of its
// first two words, or its first two letters.
func badgeOf(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var b []rune
	switch {
	case len(words) >= 2:
		b = []rune{[]rune(words[0])[0], []rune(words[1])[0]}
	case len(words) == 1:
		b = []rune(words[0])
		if len(b) > 2 {
			b = b[:2]
		}
	default:
		b = []rune("?")
	}
	return strings.ToUpper(string(b))
}

// localWho is this computer's user@host, as a card says it.
func localWho() string {
	host, _ := os.Hostname()
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
		if i := strings.LastIndexAny(name, `\`); i >= 0 {
			name = name[i+1:]
		}
	}
	switch {
	case name != "" && host != "":
		return name + "@" + host
	case host != "":
		return host
	}
	return name
}

// cardFor is what m's card shows.
func (w *Window) cardFor(m machines.ID, name string) cardInfo {
	open := []cardAction{
		{"Terminal", icon.SquareTerminal, app.OpenOn{Machine: m}},
		{"Files", icon.Folder, app.FilesOn{Machine: m}},
	}
	c := cardInfo{name: name, badge: badgeOf(name), hue: hueOf(name)}
	if m == machines.Local {
		c.name, c.state, c.section, c.actions = "This computer", cardLocal, sectionHere, open
		c.badge = "⌂"
		c.sub = localWho()
		for _, sh := range w.shellChoices {
			if sh.ID == w.chosenShell && w.chosenShell != "" {
				c.sub += " · " + sh.Title
			}
		}
		return c
	}
	c.section = sectionConnected
	switch {
	case slices.Contains(w.dialing, m):
		c.state = cardDialing
	case slices.Contains(w.dropped, m):
		c.state = cardLost
	case slices.Contains(w.connected, m) || slices.ContainsFunc(w.remoteWindows, func(rw app.RemoteWindow) bool { return rw.Name == m }):
		c.state = cardConnected
	default:
		if _, _, far := m.Far(); far {
			// Reached by another window, which says it is connected.
			c.state = cardConnected
		} else {
			c.section = sectionSaved
		}
	}
	if win, _, far := m.Far(); far {
		c.sub = "through " + w.nameOf(win)
	}
	for _, info := range w.machineList {
		if info.ID != m {
			continue
		}
		if info.Quick {
			c.sub = info.Target + " · not saved"
		}
		c.since, c.rtt, c.silent = info.Since, info.RTT, info.Silent
	}
	for _, h := range w.saved {
		if machines.ID(h.ID) != m {
			continue
		}
		c.sub = hostWho(h)
		if h.Window {
			c.sub = "kakel window · " + h.ServeAddr()
		}
		if h.Via != "" {
			c.sub += " · via " + w.nameOf(machines.ID(h.Via))
		}
	}
	c.actions = open
	if c.state == cardOff || c.state == cardLost {
		c.actions = []cardAction{{"Connect", icon.Plug, app.ConnectTo{Server: m}}}
	}
	return c
}

// hostWho is a saved server's user@address, with its port when it has
// one.
func hostWho(h remote.Host) string {
	s := h.Address
	if h.User != "" {
		s = h.User + "@" + s
	}
	if h.Port != 0 && h.Port != 22 {
		s += ":" + strconv.Itoa(h.Port)
	}
	return s
}

// serverCards holds the cards, by machine, in the order the rows gave.
type serverCards struct {
	w     *Window
	cards map[machines.ID]*serverCard
	order []machines.ID
	grid  *cardGrid
	// rows are the rows last shown, and filter what the search field
	// holds: only the machines it is found in show.
	rows   []sideItem
	filter string
	// compact says the pane is too narrow for cards side by side: each
	// card is a line, its buttons icons at its end.
	compact bool
}

func newServerCards(w *Window) *serverCards {
	s := &serverCards{w: w, cards: map[machines.ID]*serverCard{}}
	s.grid = &cardGrid{s: s, hint: widget.NewLabel("")}
	s.grid.hint.Color = look.Faint
	for i := range s.grid.titles {
		l := widget.NewLabel(sectionTitles[i])
		l.Size, l.Color = smallText, look.Faint
		s.grid.titles[i] = l
	}
	return s
}

// serverCard is one machine's card: its header row, and a list of
// what is open on it.
type serverCard struct {
	id    machines.ID
	head  *sideRow
	items []*sideRow
	info  cardInfo
}

// Children implements [gunim.Composite]: the header, then the rows.
func (c *serverCard) Children() []gunim.Node {
	out := []gunim.Node{c.head}
	for _, r := range c.items {
		out = append(out, r)
	}
	return out
}

// cardPad is the room inside a card's edges, and cardGap the room
// between cards.
const (
	cardPad   = 10
	cardGap   = 12
	cardWidth = 300
)

// Layout implements [gunim.Node]: the header, then the rows.
func (c *serverCard) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	w := cs.Max.W
	at := childrenByNode(kids)
	head := at[c.head]
	hs := head.Layout(gunim.Constraints{Max: geom.Sz(w, cs.Max.H)})
	head.Place(geom.Pt(0, 0))
	y := hs.H + 4
	for _, r := range c.items {
		k, ok := at[r]
		if !ok {
			continue
		}
		delete(at, r)
		rs := k.Layout(gunim.Constraints{Max: geom.Sz(w-2*cardPad+12, 1e6)})
		k.Place(geom.Pt(cardPad-6, y))
		y += rs.H
	}
	delete(at, c.head)
	// Rows on their way out take no room.
	for _, k := range at {
		k.Layout(gunim.Constraints{})
	}
	if len(c.items) > 0 {
		y += cardPad - 4
	}
	return geom.Sz(w, y)
}

// Paint implements [gunim.Node].
func (c *serverCard) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	r := geom.Rect{Max: box.Point()}
	radius := float32(10)
	p.RRect(r, radius, paint.Solid(widget.CardFill.Get(f.Theme)))
	p.RRectStroke(r, radius, paint.Fill{}, paint.Stroke{Width: 1, Color: widget.DialogBorder.Get(f.Theme)})
	at := childrenByNode(kids)
	head := at[c.head]
	head.Paint(p)
	if len(c.items) > 0 {
		// A line between the header and what is open.
		line := widget.DialogBorder.Get(f.Theme)
		y := head.Size().H - 1
		p.RRect(geom.Rc(cardPad, y, box.W-2*cardPad, 1), 0, paint.Solid(line))
		for _, r := range c.items {
			if k, ok := at[r]; ok {
				k.Paint(p)
			}
		}
	}
}

// cardGrid lays the cards out in columns, under their sections'
// titles.
type cardGrid struct {
	s      *serverCards
	titles [sections]*widget.Label
	// hint says what to do where there is nothing to show: no machine
	// found, or no server saved yet.
	hint *widget.Label
	// shown are the sections with a card, and laidOut the cards in each.
	shown [sections]bool
}

// Children implements [gunim.Composite]: the titles, then the cards.
func (g *cardGrid) Children() []gunim.Node {
	out := make([]gunim.Node, 0, sections+len(g.s.order))
	for _, t := range g.titles {
		out = append(out, t)
	}
	out = append(out, g.hint)
	for _, m := range g.s.order {
		out = append(out, g.s.cards[m])
	}
	return out
}

// Layout implements [gunim.Node]: each section's title, then its cards
// in as many columns as fit, each row of cards as tall as its tallest.
func (g *cardGrid) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const pad = 16
	width := max(c.Max.W, 1)
	g.s.compact = width < compactBelow
	cols := max(1, int((width-2*pad+cardGap)/(cardWidth+cardGap)))
	cw := (width - 2*pad - float32(cols-1)*cardGap) / float32(cols)
	at := childrenByNode(kids)
	placed := map[gunim.Node]bool{}
	y := float32(4)
	for sec := range sections {
		var in []gunim.Child
		for _, m := range g.s.order {
			c := g.s.cards[m]
			if k, ok := at[c]; ok && c.info.section == sec {
				in = append(in, k)
				placed[c] = true
			}
		}
		title := at[g.titles[sec]]
		placed[g.titles[sec]] = true
		g.shown[sec] = len(in) > 0
		if len(in) == 0 {
			title.Layout(gunim.Constraints{})
			continue
		}
		ts := title.Layout(gunim.Constraints{Max: geom.Sz(width-2*pad, 40)})
		title.Place(geom.Pt(pad+2, y+6))
		y += ts.H + 14
		for row := 0; row < len(in); row += cols {
			tallest := float32(0)
			for col := 0; col < cols && row+col < len(in); col++ {
				k := in[row+col]
				s := k.Layout(gunim.Constraints{Max: geom.Sz(cw, 1e6)})
				k.Place(geom.Pt(pad+float32(col)*(cw+cardGap), y))
				tallest = max(tallest, s.H)
			}
			y += tallest + cardGap
		}
		y += 8
	}
	placed[g.hint] = true
	if hint := at[g.hint]; g.hint.Text != "" {
		hs := hint.Layout(gunim.Constraints{Max: geom.Sz(width-2*pad, 200)})
		hint.Place(geom.Pt(pad+2, y))
		y += hs.H + 16
	} else {
		hint.Layout(gunim.Constraints{})
	}
	// Cards on their way out take no room.
	for n, k := range at {
		if !placed[n] {
			k.Layout(gunim.Constraints{})
		}
	}
	return geom.Sz(width, y+8)
}

// childrenByNode maps each of kids to its node, as the engine's order
// of them need not be the one shown.
func childrenByNode(kids gunim.Children) map[gunim.Node]gunim.Child {
	at := make(map[gunim.Node]gunim.Child, kids.Len())
	for k := range kids.All {
		at[k.Node()] = k
	}
	return at
}

// Paint implements [gunim.Node].
func (g *cardGrid) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	at := childrenByNode(kids)
	for i, t := range g.titles {
		if k, ok := at[t]; ok && g.shown[i] {
			k.Paint(p)
		}
	}
	if g.hint.Text != "" {
		at[g.hint].Paint(p)
	}
	for _, m := range g.s.order {
		if k, ok := at[g.s.cards[m]]; ok {
			k.Paint(p)
		}
	}
}

// sync brings the cards up to date with rows, the machines' headings
// each followed by what is open on it.
func (s *serverCards) sync(rows []sideItem, u *gunim.UI) {
	type group struct {
		head  sideItem
		items []sideItem
	}
	s.rows = rows
	// One group a machine: a machine a window reaches, which a tunnel
	// through it also names at the top, comes together on one card.
	var groups []group
	at := map[string]int{}
	cur := -1
	for _, r := range rows {
		if r.heading {
			i, ok := at[r.key]
			if !ok {
				i = len(groups)
				at[r.key] = i
				groups = append(groups, group{head: r})
			}
			cur = i
			continue
		}
		if cur >= 0 {
			groups[cur].items = append(groups[cur].items, r)
		}
	}
	seen := map[machines.ID]bool{}
	s.order = s.order[:0]
	for _, g := range groups {
		m := machines.ID(strings.TrimPrefix(g.head.key, "machine:"))
		info := s.w.cardFor(m, strings.TrimSuffix(g.head.text, " (quick)"))
		if !s.found(info, g.items) {
			continue
		}
		seen[m] = true
		s.order = append(s.order, m)
		c := s.cards[m]
		fresh := c == nil
		if fresh {
			c = &serverCard{id: m}
			c.head = s.w.newSideRow(g.head)
			s.cards[m] = c
		} else {
			c.head.set(g.head)
		}
		c.info = info
		c.head.card = &c.info
		c.head.title.SetText(c.info.name)
		c.head.title.Size, c.head.title.Color = cardTitleSize, widget.Ink
		c.head.note.Size, c.head.note.Color = smallText, look.Faint
		c.head.said = ""
		c.head.note.SetText(c.info.sub)
		// The rows it had, kept by key, so a row keeps its state.
		had := map[string]*sideRow{}
		for _, r := range c.items {
			had[r.key] = r
		}
		c.items = c.items[:0]
		for _, it := range g.items {
			r := had[it.key]
			if r == nil {
				r = s.w.newSideRow(it)
				if !fresh {
					u.Insert(c, r)
				}
			} else {
				r.set(it)
				delete(had, it.key)
			}
			c.items = append(c.items, r)
		}
		for _, r := range had {
			u.Remove(r)
		}
		if fresh {
			// Inserted whole: its header and rows come with it.
			u.Insert(s.grid, c)
		}
	}
	for m, c := range s.cards {
		if !seen[m] {
			c.head.closeMenu(u)
			u.Remove(c)
			delete(s.cards, m)
		}
	}
	// The sections in their order: this computer, connected, saved.
	slices.SortStableFunc(s.order, func(a, b machines.ID) int { return s.cards[a].info.section - s.cards[b].info.section })
	hint := ""
	switch {
	case s.filter != "" && len(s.order) == 0 && looksLikeHost(s.filter):
		hint = "No machine is called that. Press Enter to connect to " + strings.TrimSpace(s.filter) + "."
	case s.filter != "" && len(s.order) == 0:
		hint = "No machine is called that."
	case s.filter == "" && len(s.w.saved) == 0:
		hint = "No servers saved yet. Add one, or import the ones your SSH config knows, from Add."
	}
	s.grid.hint.SetText(hint)
}

// found reports whether the search field's text is in a card: its
// name, who and where, or what is open on it.
func (s *serverCards) found(info cardInfo, items []sideItem) bool {
	q := strings.ToLower(strings.TrimSpace(s.filter))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(info.name), q) || strings.Contains(strings.ToLower(info.sub), q) {
		return true
	}
	return slices.ContainsFunc(items, func(it sideItem) bool { return strings.Contains(strings.ToLower(it.text), q) })
}

// find shows only the machines text is found in.
func (s *serverCards) find(text string, u *gunim.UI) {
	s.filter = text
	if s.w.listShown {
		s.sync(s.rows, u)
		u.Invalidate()
	}
}

// submit is what Enter in the search field asks: the first button of
// the one machine found, or a connection to what was typed, when it
// names a host.
func (s *serverCards) submit(text string) gunim.Intent {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if len(s.order) == 1 {
		if c := s.cards[s.order[0]]; len(c.info.actions) > 0 {
			return c.info.actions[0].in
		}
	}
	if len(s.order) == 0 && looksLikeHost(text) {
		return app.ConnectTo{Target: strings.TrimSpace(text)}
	}
	return nil
}

// looksLikeHost reports whether text could be an address to connect
// to: user@host, or a host name with a dot or a port.
func looksLikeHost(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && !strings.ContainsAny(text, " \t") && strings.ContainsAny(text, "@.:")
}

// keys are every row's key, headers and what is open under them, in the
// order the cards show them.
func (s *serverCards) keys() []widget.Key {
	var out []widget.Key
	for _, m := range s.order {
		c := s.cards[m]
		out = append(out, widget.Key(c.head.key))
		for _, r := range c.items {
			out = append(out, widget.Key(r.key))
		}
	}
	return out
}

// row is the row with key, a header or one under it.
func (s *serverCards) row(key widget.Key) (*sideRow, bool) {
	for _, m := range s.order {
		c := s.cards[m]
		if widget.Key(c.head.key) == key {
			return c.head, true
		}
		for _, r := range c.items {
			if widget.Key(r.key) == key {
				return r, true
			}
		}
	}
	return nil, false
}

// cardTitleSize is a card's name's size.
var cardTitleSize = theme.Length("kakel.card.title", 15)

// cardHeadHeight is a card's header's height: the name, who and where,
// and the buttons; compactHeadHeight a compact card's, a line.
const (
	cardHeadHeight    = 92
	compactHeadHeight = 50
)

// compactBelow is how narrow the pane is when its cards go compact.
const compactBelow = 420

// The header's buttons: their height, and the room between them.
const (
	cardChipHeight = 28
	cardChipGap    = 6
	moreWidth      = 32
)

// cardChips are the header's buttons, its actions and then ⋯, where
// they are in a header box wide.
func (r *sideRow) cardChips(box geom.Size, th *theme.Live) []geom.Rect {
	if r.w.cards.compact {
		// Icons alone, at the end: the actions, then ⋯.
		y := (box.H - cardChipHeight) / 2
		x := box.W - cardPad - moreWidth
		out := make([]geom.Rect, len(r.card.actions)+1)
		out[len(out)-1] = geom.Rc(x, y, moreWidth, cardChipHeight)
		for i := len(r.card.actions) - 1; i >= 0; i-- {
			x -= cardChipGap + cardChipHeight
			out[i] = geom.Rc(x, y, cardChipHeight, cardChipHeight)
		}
		return out
	}
	y := box.H - cardPad - cardChipHeight
	x := float32(cardPad)
	var out []geom.Rect
	for _, a := range r.card.actions {
		run := text.Default().Shape(a.label, smallText.Get(th)+1)
		w := 10 + 16 + 6 + run.Advance + 12
		out = append(out, geom.Rc(x, y, w, cardChipHeight))
		x += w + cardChipGap
	}
	out = append(out, geom.Rc(box.W-cardPad-moreWidth, y, moreWidth, cardChipHeight))
	return out
}

// layoutCard lays a header out: the badge and the name on the first
// line, who and where under it, the buttons along the bottom.
func (r *sideRow) layoutCard(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	if r.w.cards.compact {
		const badge = 30
		x := float32(cardPad + badge + 10)
		buttons := float32(len(r.card.actions)+1)*(cardChipHeight+cardChipGap) + cardPad
		room := max(0, c.Max.W-x-buttons)
		ts := kids.At(0).Layout(gunim.Constraints{Max: geom.Sz(room, 22)})
		ns := kids.At(1).Layout(gunim.Constraints{Max: geom.Sz(room, 18)})
		top := (compactHeadHeight - ts.H - ns.H - 1) / 2
		kids.At(0).Place(geom.Pt(x, top))
		kids.At(1).Place(geom.Pt(x, top+ts.H+1))
		return geom.Sz(c.Max.W, compactHeadHeight)
	}
	const badge = 36
	x := float32(cardPad + badge + 10)
	// The name stops short of the pill, as wide as its words are now.
	pill := pillWidth(*r.card, f) + 8
	ts := kids.At(0).Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-x-cardPad-pill), 24)})
	kids.At(0).Place(geom.Pt(x, cardPad+1))
	kids.At(1).Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-x-cardPad), 20)})
	kids.At(1).Place(geom.Pt(x, cardPad+ts.H+3))
	return geom.Sz(c.Max.W, cardHeadHeight)
}

// paintCard draws a header: the badge, the status pill, the buttons.
func (r *sideRow) paintCard(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	badge := float32(36)
	info := r.card
	compact := r.w.cards.compact
	b := geom.Rc(cardPad, cardPad, badge, badge)
	if compact {
		badge = 30
		b = geom.Rc(cardPad, (box.H-badge)/2, badge, badge)
	}
	// The badge: the machine's letters on its colour.
	fill := info.hue
	fill.A = 0x38
	p.RRect(b, 9, paint.Solid(fill))
	run := text.GoSans(true, false).Shape(info.badge, 14)
	run.Paint(p, geom.Pt(b.Center().X-run.Advance/2, b.Center().Y-run.Height()/2), info.hue)
	kids.At(0).Paint(p)
	kids.At(1).Paint(p)
	if compact {
		// The state as a dot on the badge's corner, in place of the pill.
		_, c := pillOf(info.state, f.Theme)
		dot := geom.Rc(b.Max.X-8, b.Max.Y-8, 11, 11)
		p.RRect(dot, 5.5, paint.Solid(widget.CardFill.Get(f.Theme)))
		p.RRect(dot.Inset(geom.Uniform(2)), 3.5, paint.Solid(c))
	} else {
		r.paintPill(p, f, box)
	}
	r.paintChips(p, f, box)
	if t := r.ring.Value(); t > 0.01 {
		c := widget.Accent.Get(f.Theme)
		c.A = uint8(float32(c.A) * min(t, 1))
		p.RRectStroke(geom.Rect{Max: box.Point()}.Inset(geom.Uniform(1)), 9, paint.Fill{}, paint.Stroke{Width: 1.5, Color: c})
	}
}

// pillOf is a state's words and colour.
func pillOf(s cardState, th *theme.Live) (string, color.NRGBA) {
	return pillFor(cardInfo{state: s}, time.Time{}, th)
}

// pillFor is a card's pill's words and colour at now: a connected
// machine's round trip and how long it has been connected, once pinged.
func pillFor(c cardInfo, now time.Time, th *theme.Live) (string, color.NRGBA) {
	switch c.state {
	case cardLocal:
		return "Local", widget.ToastSuccessInk.Get(th)
	case cardConnected:
		if c.silent {
			return "Not answering", widget.ToastWarningInk.Get(th)
		}
		if c.rtt > 0 && !c.since.IsZero() {
			return roundTrip(c.rtt) + " · " + connectedFor(now.Sub(c.since)), widget.ToastSuccessInk.Get(th)
		}
		return "Connected", widget.ToastSuccessInk.Get(th)
	case cardDialing:
		return "Connecting…", widget.ToastWarningInk.Get(th)
	case cardLost:
		return "Lost", widget.DialogDangerInk.Get(th)
	}
	return "Not connected", look.Faint.Get(th)
}

// roundTrip says a ping's round trip: in milliseconds, or seconds once
// it takes one.
func roundTrip(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1 ms"
	case d < time.Second:
		return strconv.Itoa(int(d.Milliseconds())) + " ms"
	}
	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + " s"
}

// connectedFor says how long a connection has stood, to the minute.
func connectedFor(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m < 1:
		return "just now"
	case m < 60:
		return strconv.Itoa(m) + " min"
	case m < 24*60:
		s := strconv.Itoa(m/60) + " h"
		if m%60 != 0 {
			s += " " + strconv.Itoa(m%60) + " min"
		}
		return s
	}
	s := strconv.Itoa(m/(24*60)) + " d"
	if h := m % (24 * 60) / 60; h != 0 {
		s += " " + strconv.Itoa(h) + " h"
	}
	return s
}

// pillWidth is how wide a card's pill is at f's time.
func pillWidth(c cardInfo, f gunim.Frame) float32 {
	words, _ := pillFor(c, f.Now, f.Theme)
	return 8 + 6 + 6 + text.Default().Shape(words, smallText.Get(f.Theme)).Advance + 10
}

// paintPill draws the state's pill at the header's top right. A
// connected machine's says how long it has been connected, so it is
// drawn again each minute.
func (r *sideRow) paintPill(p *paint.Painter, f gunim.Frame, box geom.Size) {
	words, c := pillFor(*r.card, f.Now, f.Theme)
	run := text.Default().Shape(words, smallText.Get(f.Theme))
	w := pillWidth(*r.card, f)
	if r.card.state == cardConnected && !r.card.since.IsZero() {
		f.RedrawAt(f.Now.Add(time.Minute))
	}
	pr := geom.Rc(box.W-cardPad-w, cardPad+4, w, 22)
	fill := c
	fill.A = 0x22
	p.RRect(pr, 11, paint.Solid(fill))
	dot := geom.Rc(pr.Min.X+9, pr.Center().Y-3, 6, 6)
	p.RRect(dot, 3, paint.Solid(c))
	run.Paint(p, geom.Pt(dot.Max.X+6, pr.Center().Y-run.Height()/2), c)
}

// paintChips draws the header's buttons, the one under the pointer lit.
func (r *sideRow) paintChips(p *paint.Painter, f gunim.Frame, box geom.Size) {
	chips := r.cardChips(box, f.Theme)
	ink, faint := widget.Ink.Get(f.Theme), look.Faint.Get(f.Theme)
	for i, cr := range chips {
		fill := widget.ChipFill.Get(f.Theme)
		if i == r.chipHot {
			fill = widget.ChipHover.Get(f.Theme)
		}
		p.RRect(cr, 7, paint.Solid(fill))
		if i == len(chips)-1 {
			// ⋯: three dots.
			for d := range 3 {
				dot := geom.Rc(cr.Center().X-7+float32(d)*6, cr.Center().Y-1.5, 3, 3)
				p.RRect(dot, 1.5, paint.Solid(faint))
			}
			continue
		}
		a := r.card.actions[i]
		if r.w.cards.compact {
			drawIcon(p, a.ic, geom.Rc(cr.Center().X-8, cr.Center().Y-8, 16, 16), ink, 1.5)
			continue
		}
		drawIcon(p, a.ic, geom.Rc(cr.Min.X+10, cr.Center().Y-8, 16, 16), ink, 1.5)
		run := text.Default().Shape(a.label, smallText.Get(f.Theme)+1)
		run.Paint(p, geom.Pt(cr.Min.X+10+16+6, cr.Center().Y-run.Height()/2), ink)
	}
}

// chipAt is the header button at pt, -1 for none.
func (r *sideRow) chipAt(pt geom.Point, box geom.Size, th *theme.Live) int {
	for i, cr := range r.cardChips(box, th) {
		if cr.Contains(pt) {
			return i
		}
	}
	return -1
}

// handleCard is a header's answer to the pointer: a button under it is
// lit, and a press on one does what it says; ⋯ opens the machine's
// menu.
func (r *sideRow) handleCard(e input.Event, u *gunim.UI) bool {
	box, _ := u.Bounds(r)
	size := box.Size()
	switch e := e.(type) {
	case input.PointerMove:
		if hot := r.chipAt(e.Pos, size, u.Theme()); hot != r.chipHot {
			r.chipHot = hot
			u.Invalidate()
		}
		return true
	case input.PointerLeave:
		r.chipHot = -1
		u.Invalidate()
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		i := r.chipAt(e.Pos, size, u.Theme())
		switch {
		case i < 0:
			return true
		case i == len(r.card.actions):
			r.w.openMachineMenu(r, u)
		default:
			r.card.actions[i].do(r.w, u)
		}
		return true
	case input.FocusGained:
		r.ring.Animate(1, widget.Quick.Get(u.Theme()))
		return false
	case input.FocusLost:
		r.ring.Animate(0, widget.Settle.Get(u.Theme()))
		return false
	case input.KeyPress:
		if r.menu != nil {
			if e.Key == input.KeyEscape || e.Key == input.KeyTab {
				r.closeMenu(u)
				return true
			}
			return r.menu.Key(e, u)
		}
		return r.cardKey(e, u)
	}
	return false
}

// cardKey is a header's answer to a key: the arrows move along the
// cards and what is open on them, Enter does the first button, F opens
// the files, E edits the machine, Delete removes it, and the menu key or
// Shift+F10 opens its menu.
func (r *sideRow) cardKey(e input.KeyPress, u *gunim.UI) bool {
	w, m := r.w, r.machine
	plain := e.Mods == 0
	switch {
	case e.Key == input.KeyUp, e.Key == input.KeyDown:
		step := 1
		if e.Key == input.KeyUp {
			step = -1
		}
		w.focusRow(r.key, step, u)
	case e.Key == input.KeyLeft, e.Key == input.KeyRight:
		w.cards.stepCard(r.key, e.Key == input.KeyRight, u)
	case e.Key == input.KeyHome:
		w.focusRow("", 1, u)
	case e.Key == input.KeyEnd:
		w.focusRowsAway(r.key, len(w.cards.keys()), u)
	case e.Key == input.KeyPageUp, e.Key == input.KeyPageDown:
		step := sidebarPage
		if e.Key == input.KeyPageUp {
			step = -step
		}
		w.focusRowsAway(r.key, step, u)
	case e.Key == input.KeyEnter || e.Key == input.KeyKPEnter, e.Key == input.KeySpace && plain:
		if len(r.card.actions) > 0 {
			r.card.actions[0].do(w, u)
		}
	case e.Key == input.KeyF && plain:
		u.Send(w, app.FilesOn{Machine: m})
	case e.Key == input.KeyE && plain:
		if m == machines.Local {
			w.thisComputerDialog(u)
			break
		}
		for _, h := range w.saved {
			if machines.ID(h.ID) == m {
				saved := h
				w.serverForm(&saved, u)
			}
		}
	case e.Key == input.KeyDelete && m != machines.Local:
		if slices.ContainsFunc(w.saved, func(h remote.Host) bool { return machines.ID(h.ID) == m }) {
			w.confirmRemove(m, u)
		}
	case e.Key == input.KeyMenu, e.Key == input.KeyF10 && e.Mods == input.ModShift:
		w.openMachineMenu(r, u)
	case e.Key == input.KeyEscape:
		// Back to the pane last worked in, wherever it is.
		if id := w.lastWorked; id != "" && id != w.focused {
			u.Send(r, app.FocusPane{Pane: id})
		} else if n := w.focusNode(w.focused, u); n != nil {
			u.Focus(n)
		}
	default:
		return false
	}
	return true
}

// stepCard gives the keyboard to the header of the card after the one
// the row keyed from is on, or before it.
func (s *serverCards) stepCard(from string, forward bool, u *gunim.UI) {
	at := -1
	for i, m := range s.order {
		c := s.cards[m]
		if c.head.key == from || slices.ContainsFunc(c.items, func(r *sideRow) bool { return r.key == from }) {
			at = i
		}
	}
	next := at + 1
	if !forward {
		next = at - 1
	}
	if at >= 0 && next >= 0 && next < len(s.order) {
		u.Focus(s.cards[s.order[next]].head)
	}
}

// at is the header of the card at p, in the window's space, or nil.
func (s *serverCards) at(p geom.Point, u *gunim.UI) gunim.Node {
	for _, m := range s.order {
		c := s.cards[m]
		if box, ok := u.Bounds(c); ok && box.Contains(p) {
			return c.head
		}
	}
	return nil
}

// first is the first card's header, for the keyboard to come to, or nil
// with no card.
func (s *serverCards) first() gunim.Node {
	if len(s.order) == 0 {
		return nil
	}
	return s.cards[s.order[0]].head
}

// menuAnchor is where a heading's menu opens from: its ⋯ on a card, its
// plus otherwise.
func (r *sideRow) menuAnchor(box geom.Size, th *theme.Live) geom.Rect {
	if r.card != nil {
		chips := r.cardChips(box, th)
		return chips[len(chips)-1]
	}
	return geom.Rect{Min: geom.Pt(box.W-plusWidth-6, 0), Max: box.Point()}
}
