package main

import (
	"errors"
	"fmt"
	"image/color"
	"sort"
	"strconv"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// panelWidth is how wide the connections panel starts.
const panelWidth = 26

// dot is the mark in front of a row, saying what state it is in: a
// bullet rather than a word, because the word was the widest thing on
// most rows and said the least.
const dot = '\u2022'

// The icon in front of a connection, in place of the word for what kind
// it is. "Terminal" and "Files" were the widest thing on most rows and
// said the same thing on every one of them.
const (
	terminalIcon = '\u276f' // a prompt
	commandIcon  = '\u25b8' // something that was run
	filesIcon    = '\u25a4' // a listing
	tunnelIcon   = '\u21c4' // going both ways
)

// icon is the character that stands for a kind of connection.
func icon(k conns.Kind) rune {
	switch k {
	case conns.Command:
		return commandIcon
	case conns.Files:
		return filesIcon
	case conns.Tunnel:
		return tunnelIcon
	}
	return terminalIcon
}

// pulseStep is how long one step of the pulse lasts. A row that changes
// colour every frame is a row that dirties itself every frame, so the
// pulse moves in steps slow enough to see and few enough to cost
// nothing.
const pulseStep = 200 * time.Millisecond

// newPanel builds the list of connections, in the window's colours.
func (a *app) newPanel() *ui.List {
	l := ui.NewList()
	l.Style = ui.ListStyle{
		FG: a.colours.FG,
		BG: a.colours.BG,
		// The selected row is marked the way a selected tab is, so the
		// two read as the same thing.
		SelectedFG: a.colours.BG,
		SelectedBG: a.colours.FG,
		// A machine's name is a heading, not one of its connections.
		HeaderFG: a.colours.ANSI[6],
		// Dimmer than the row: what a connection is doing is a note
		// beside it, not part of its name.
		NoteFG: a.colours.ANSI[8],
		// A ground of its own, shading down the list, so the sidebar
		// reads as part of the window's frame rather than as one more
		// thing running in it.
		BGEnd: sidebarFoot(a.colours),
	}
	l.Style.BG = sidebarTop(a.colours)
	l.OnActivate = func(row ui.ListRow) error { return a.revealRow(row) }
	l.OnButton = func(row ui.ListRow) error { return a.openHostMenu(row) }
	return l
}

// mark is the dot in front of a row and the colour it is drawn in.
//
// Green for something that is there, brightening and dimming while bytes
// are going past, and grey once it has finished.
func (a *app) mark(state meter.State, now time.Time) (rune, color.RGBA) {
	green := a.colours.ANSI[2]
	switch state {
	case meter.Closed:
		return dot, a.colours.ANSI[8]
	case meter.Active:
		return dot, pulse(green, a.colours.ANSI[10], now)
	}
	return dot, green
}

// pulse moves between two colours in steps, resting on neither.
//
// The step comes from the time passed in rather than from a count, so
// every row pulsing at once is in time with the rest and a row that
// stops being busy simply stops moving. It never reaches either end: a
// dot resting on the steady colour could not be told from a row that is
// only sitting there.
func pulse(from, to color.RGBA, now time.Time) color.RGBA {
	// A triangle: up the steps and back down them.
	const steps = 4
	at := int(now.UnixMilli()/int64(pulseStep/time.Millisecond)) % (steps * 2)
	if at >= steps {
		at = steps*2 - at
	}
	return mix(from, to, at+1, steps+2)
}

// sidebarTop and sidebarFoot are the two ends of the ground the window's
// frame is drawn on: the sidebar shades between them down its length,
// and the menu bar across its width.
func sidebarTop(p vt.Palette) color.RGBA  { return mix(p.BG, p.ANSI[4], 1, 20) }
func sidebarFoot(p vt.Palette) color.RGBA { return mix(p.BG, p.ANSI[4], 1, 8) }

// mix blends two colours, at/of the way from the first to the second.
func mix(from, to color.RGBA, at, of int) color.RGBA {
	if of <= 0 {
		return from
	}
	part := func(a, b uint8) uint8 { return uint8(int(a) + (int(b)-int(a))*at/of) }
	return color.RGBA{
		R: part(from.R, to.R),
		G: part(from.G, to.G),
		B: part(from.B, to.B),
		A: part(from.A, to.A),
	}
}

// newSidebar puts the list in the panel, with the way to reach a machine
// that is not open yet pinned under it.
func (a *app) newSidebar() *sidebar {
	s := newSidebar(a.panel, "+ Connect to server…", func() error {
		// Through the registry rather than straight to the function, so
		// a failure reaches the user the way it does from the menu bar
		// and the keys: those go through a wrapper that shows it.
		return a.root.Commands.Run("server.connect")
	})
	s.FG = a.colours.ANSI[6]
	// The foot of the shading the list draws, so the pinned row looks
	// like the bottom of the sidebar rather than something sitting on
	// it.
	s.BG = a.panel.Style.BGEnd
	return s
}

// revealRow puts whatever a row names in front of the user.
func (a *app) revealRow(row ui.ListRow) error {
	e, ok := row.Key.(*conns.Entry)
	if !ok || e.Reveal == nil {
		return nil
	}
	e.Reveal()
	return nil
}

// refreshPanel rebuilds the list from what is open.
//
// Called every frame. The rows are built afresh each time and the list
// keeps the user's place by key, so a connection opening or closing does
// not move the selection out from under them.
//
// Nothing here dirties a layer on its own: a row whose text has not
// changed is written with the same value, and grid.Set leaves a cell
// that did not change alone. A state falls from active to settled by
// itself, because the text is worked out again from the time passed in.
func (a *app) refreshPanel(now time.Time) {
	if a.panel == nil || (a.dock != nil && a.dock.Collapsed) {
		// Nothing to build while nobody can see it. The rows are worked
		// out again the moment the panel opens.
		return
	}
	// A terminal names itself: what the program in it called the window
	// is what the panel shows. Read rather than pushed, the way the
	// window title is. A program that has named nothing leaves the row
	// saying what it was started as, which is what a remote command has
	// instead of a title.
	for t, e := range a.panes {
		if title := t.Title(); title != "" {
			e.Label = title
		}
	}

	// A tunnel says what it is carrying, which is the one thing about it
	// the meter cannot say.
	for e, open := range a.tunnels {
		e.Note = open.note()
	}

	// What is still open, so a rate belonging to something that has gone
	// is not kept for the life of the window.
	live := make(map[*conns.Entry]bool, len(a.rates))

	open := map[string][]conns.Row{}
	for _, group := range a.registry.Groups(now) {
		open[group.Host] = group.Rows
	}

	var rows []ui.ListRow
	for _, host := range a.hosts(open) {
		rows = append(rows, a.hostRow(host, now))
		for _, row := range open[host] {
			live[row.Entry] = true
			if row.Kind == conns.Server {
				// The connection itself is the machine, and the machine
				// is the heading above these rows. A row for it as well
				// says the same thing twice.
				continue
			}
			rows = append(rows, a.panelRow(row, now))
		}
	}
	for e := range a.rates {
		if !live[e] {
			delete(a.rates, e)
		}
	}
	a.panel.SetRows(rows)
	a.followTheStage()
}

// hosts is every machine the sidebar shows, in the order it shows them:
// this one, then the saved servers, then anything else the window has
// open.
//
// A saved server is listed before anything is connected to it. That is
// how it is reached: the plus beside its name opens the connection.
func (a *app) hosts(open map[string][]conns.Row) []string {
	out := []string{conns.Local}
	seen := map[string]bool{conns.Local: true}
	// Names rather than Hosts: this runs every frame, and cloning every
	// saved machine and its key files to read the names off them is work
	// for nothing.
	for _, name := range a.book.Names() {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	// Then whatever is open that the book does not name. Sorted, because
	// they come out of a map and an order that changed every frame would
	// shuffle the sidebar under the user.
	var rest []string
	for host := range open {
		if seen[host] {
			continue
		}
		seen[host] = true
		rest = append(rest, host)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// allHosts is every machine the window knows about, in the order the
// sidebar shows them.
func (a *app) allHosts() []string {
	open := map[string][]conns.Row{}
	for _, group := range a.registry.Groups(time.Now()) {
		open[group.Host] = group.Rows
	}
	return a.hosts(open)
}

// hostRow is the heading for one machine.
//
// It carries the dot the connection's own row used to, so a machine with
// nothing open on it still says whether it is connected and what it was
// reached through.
func (a *app) hostRow(host string, now time.Time) ui.ListRow {
	row := ui.ListRow{
		Text:   groupName(host),
		Header: true,
		Key:    hostKey(host),
		// Indented like the rows under it, so its own dot sits in the
		// column theirs do. A heading hard against the left edge has
		// nowhere to put one.
		Depth: 1,
		// What can be opened on this machine, since the name itself is
		// not something to act on.
		Button: '+',
		// A blank where the dot goes while nothing is connected, so the
		// name does not shift sideways when something is.
		Mark: ' ',
	}
	m := a.machines[host]
	if m == nil || m.entry == nil {
		return row
	}
	state := m.entry.State(now)
	row.Mark, row.MarkFG = a.mark(state, now)
	row.Note = a.note(conns.Row{Entry: m.entry, State: state}, now)
	return row
}

// followTheStage puts the bar on the row for whatever the stage is
// showing, when that has changed.
//
// The sidebar is how a pane is chosen, so it has to say which one is in
// front. Only on a change: in between, the bar is the user's, and
// snapping it back every frame would stop them looking anywhere else.
func (a *app) followTheStage() {
	e := a.showing()
	if e == a.shown {
		return
	}
	a.shown = e
	if e == nil {
		return
	}
	if a.panel.Select(e) {
		a.panel.Reveal()
	}
}

// showing is the sidebar row for whatever the stage has in front,
// whether or not the keys are in it.
func (a *app) showing() *conns.Entry {
	if a.stage == nil {
		return nil
	}
	switch w := ui.FocusedLeaf(a.stage).(type) {
	case *term.Terminal:
		return a.panes[w]
	case *files.Pane:
		if a.files == nil {
			return nil
		}
		return a.files.rows[w]
	}
	return nil
}

// panelRow turns one connection into a line.
func (a *app) panelRow(row conns.Row, now time.Time) ui.ListRow {
	text := string(icon(row.Kind))
	if row.Label != "" {
		text += " " + row.Label
	}
	out := ui.ListRow{Text: text, Depth: 1, Key: row.Entry, Note: a.note(row, now)}
	out.Mark, out.MarkFG = a.mark(row.State, now)
	out.Art = a.graph(row.Entry)
	if row.State == meter.Closed {
		// A finished connection reads as finished rather than as one
		// more thing running.
		out.FG = a.colours.ANSI[8]
	}
	return out
}

// note is what a row says at its end: how fast, or what it is carrying.
//
// What state it is in is the dot's business now. The note is for things
// the dot cannot say: a speed, a count, the machine a connection is
// reached through.
//
// A speed comes first while one is worth showing, because that is the
// answer to "is this working": a tunnel carrying two streams and moving
// nothing is stuck, and the row has to be able to say so.
func (a *app) note(row conns.Row, now time.Time) string {
	if row.Meter == nil {
		return row.Note
	}
	// Sampled whatever the state, so the window it measures is always
	// the one just gone. Sampling only while active would measure the
	// first busy second against however long the quiet spell before it
	// lasted, and report a fraction of the real speed.
	rate := a.rates[row.Entry]
	if rate == nil {
		rate = &meter.Rate{}
		a.rates[row.Entry] = rate
	}
	in, out := rate.Sample(row.Meter, now)
	if row.State == meter.Active {
		if speed := meter.Speed(max(in, out)); speed != "" {
			return speed
		}
	}
	return row.Note
}

// graph is the last few seconds of traffic on a connection, drawn in one
// cell.
//
// A picture rather than a number: what a row is asked is usually "is
// this going", and the shape of the last twelve seconds answers that
// where one speed does not. The number stays beside it for the times
// when how fast is the question.
func (a *app) graph(e *conns.Entry) grid.Art {
	rate := a.rates[e]
	if rate == nil {
		return grid.Art{}
	}
	past := rate.Past()
	if len(past) == 0 {
		return grid.Art{}
	}
	var moved bool
	for _, speed := range past {
		if speed > 0 {
			moved = true
		}
	}
	if !moved {
		// Nothing has gone past in the whole run, so there is no shape
		// to show. A row of nothing but zeroes is noise.
		return grid.Art{}
	}
	return grid.Graph(meter.Bars(past, grid.ArtGraphMax))
}

// streams is what a tunnel's row says when nothing is moving through it:
// how many connections are going through it at all.
func streams(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 stream"
	}
	return strconv.Itoa(n) + " streams"
}

// groupName is what a machine is called on the panel.
func groupName(host string) string {
	if host == conns.Local {
		return "Local"
	}
	return host
}

// showPanel opens or closes the connections panel.
func (a *app) showPanel(on bool) error {
	if a.dock == nil {
		return nil
	}
	a.dock.ShowPanel(on)
	a.relayout()
	return nil
}

// togglePanel shows the panel, or hides it when it is already showing.
func (a *app) togglePanel() error {
	if a.dock == nil {
		return nil
	}
	return a.showPanel(a.dock.Collapsed)
}

// focusPanel puts the keys on the panel, opening it if it is hidden.
func (a *app) focusPanel() error {
	if a.dock == nil {
		return nil
	}
	if err := a.showPanel(true); err != nil {
		return err
	}
	if !a.dock.Focus(a.side) {
		return errors.New("there is no room for the panel")
	}
	a.markDirty()
	return nil
}

// closeSelectedConnection ends whatever the panel has selected.
func (a *app) closeSelectedConnection() error {
	e, ok := a.selectedConnection()
	if !ok {
		return nil
	}
	if e.Close == nil {
		return fmt.Errorf("%s cannot be closed from here", e.Kind)
	}
	return e.Close()
}

// clearFinished takes every connection that has ended off the panel.
//
// A command that stopped keeps its pane, so that what it printed can
// still be read. Clearing its row is the user saying they have read it,
// so the pane goes too: a pane with no row is one the sidebar cannot
// reach, and the sidebar is the only way to choose what is showing.
func (a *app) clearFinished() error {
	var err error
	now := time.Now()
	for t := range a.ended {
		// Only the ones that really have finished. A pane whose channel
		// would not close is still open, and its row still says so.
		if e := a.panes[t]; e == nil || e.State(now) != meter.Closed {
			continue
		}
		if cerr := a.removePane(t, false); cerr != nil && err == nil {
			err = cerr
		}
	}
	if a.registry.DropFinished(now) > 0 {
		a.markDirty()
	}
	return err
}

// selectedConnection returns what the panel has selected.
//
// Nothing is selected while the panel is hidden. Its rows are only
// rebuilt while it can be seen, so the selection of a hidden panel is
// whatever was there when it was last looked at -- possibly a connection
// that has since been closed and taken off the list.
func (a *app) selectedConnection() (*conns.Entry, bool) {
	if a.panel == nil || (a.dock != nil && a.dock.Collapsed) {
		return nil, false
	}
	row, ok := a.panel.Selected()
	if !ok {
		return nil, false
	}
	e, ok := row.Key.(*conns.Entry)
	return e, ok
}
