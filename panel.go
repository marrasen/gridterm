package main

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// panelWidth is how wide the connections panel starts.
const panelWidth = 26

// dot is the mark in front of a heading, and behind a connection's icon
// in a sidebar too narrow to draw one.
const dot = '\u2022'

// icon is the little picture that stands for a kind of connection.
func icon(k conns.Kind) grid.Art {
	switch k {
	case conns.Command:
		return grid.Icon(grid.IconCommand)
	case conns.Files:
		return grid.Icon(grid.IconFiles)
	case conns.Tunnel:
		return grid.Icon(grid.IconTunnel)
	}
	return grid.Icon(grid.IconTerminal)
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
	// The row for whatever is in front, marked even while the keys are
	// somewhere else: the sidebar is the list of what is open, so it has
	// to say which one is being looked at.
	l.Style.CurrentFG = a.colours.FG
	// Lifted off the list's own ground rather than the window's
	// selection colour, so it stays darker than the mark drawn on it:
	// the mark is what says whether the connection is open.
	l.Style.CurrentBG = grid.Blend(a.colours.BG, a.colours.FG, 1, 6)
	// A little air around each machine's name, so it reads as a heading
	// for the rows under it rather than as another row. A quarter of a
	// character each way: enough to see, and far less than the blank
	// line it would otherwise take.
	l.Style.HeaderPad = grid.Pad{Before: 1, After: 1}
	l.OnActivate = func(row ui.ListRow) error { return a.revealRow(row) }
	l.OnButton = func(row ui.ListRow) error { return a.openHostMenu(row) }
	return l
}

// stateFG is the colour of the mark in front of a row.
//
// Green for something that is there, brightening and dimming while bytes
// are going past, and grey once it has finished.
func (a *app) stateFG(state meter.State, now time.Time) color.RGBA {
	green := a.colours.ANSI[2]
	switch state {
	case meter.Closed:
		return a.colours.ANSI[8]
	case meter.Active:
		return pulse(green, a.colours.ANSI[10], now)
	}
	return green
}

// pulse moves between two colours in steps, resting on neither.
//
// The step comes from the time passed in rather than from a count, so
// every row pulsing at once is in time with the rest and a row that
// stops being busy simply stops moving. It never reaches either end: a
// mark resting on the steady colour could not be told from a row that is
// only sitting there.
func pulse(from, to color.RGBA, now time.Time) color.RGBA {
	// A triangle: up the steps and back down them.
	const steps = 4
	at := int(now.UnixMilli()/int64(pulseStep/time.Millisecond)) % (steps * 2)
	if at >= steps {
		at = steps*2 - at
	}
	return grid.Blend(from, to, at+1, steps+2)
}

// sidebarTop and sidebarFoot are the two ends of the ground the window's
// frame is drawn on: the sidebar shades between them down its length,
// and the menu bar across its width.
func sidebarTop(p vt.Palette) color.RGBA  { return grid.Blend(p.BG, p.ANSI[4], 1, 20) }
func sidebarFoot(p vt.Palette) color.RGBA { return grid.Blend(p.BG, p.ANSI[4], 1, 8) }

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

// newDock puts the sidebar beside the rest of the window, with a
// colourless divider so the column between the two draws blank and is
// still the drag handle.
func (a *app) newDock(rest ui.Widget) *ui.Dock {
	d := ui.NewDock(panelWidth, a.side, rest)
	d.DividerFG = color.RGBA{}
	d.DividerBG = a.colours.BG
	return d
}

// revealRow puts whatever a row names in front of the user.
func (a *app) revealRow(row ui.ListRow) error {
	if remote, ok := row.Key.(remoteKey); ok {
		// Something running on a window this one took over. There is no
		// pane here to put in front, so one is opened to watch it in.
		return a.attachHere(remote, nil)
	}
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

	// And a pane says when somebody elsewhere is reading it. Two people
	// looking at one terminal is what taking over a window means, and
	// the one sitting at it has to be able to tell.
	for pane, e := range a.panes {
		want := ""
		if h := a.agents.of(pane); h != nil {
			// Ahead of the far end's size: the user can see a size, and
			// cannot otherwise see that something else is typing here.
			want = h.note()
		} else if what, ok := a.windows.watching(pane); ok {
			// A pane showing a screen that is not its size, which is
			// the one thing about it the user cannot otherwise work
			// out from what it draws. Asked for afresh, because the
			// far end is redrawn at its own size whenever it changes.
			cols, rows := a.farSize(what)
			want = farNote(pane.Size(), cols, rows)
		} else if n := pane.Watched(); n > 0 {
			if pane.Held() && pane.Size() != pane.Box() {
				// Somebody watching set the size, and this window draws
				// that screen in whatever room it has: the size is the
				// only thing that explains what is on it.
				want = heldNote(pane.Size(), n)
			} else {
				want = watchedNote(n)
			}
		}
		// Only over a note of our own. The one other note a pane can
		// carry says its channel could not be let go of, and that is
		// the only place the user can read it.
		if e.Note == "" || isOurNote(e.Note) || isAgentNote(e.Note) {
			e.Note = want
		}
	}

	// Whoever is working in this window from elsewhere is told what it
	// has open, once the rows say what they are going to say. Told
	// whether or not anybody here is looking at the panel: what another
	// window sees has nothing to do with whether this one's sidebar
	// happens to be open.
	a.tellWatchers(now)
	if a.panel == nil || (a.dock != nil && a.dock.Collapsed) {
		// Nothing to build while nobody can see it. The rows are worked
		// out again the moment the panel opens.
		return
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
		// Once per heading, and handed down: every row of the heading
		// asks the same questions about the same name.
		on := a.about(host)
		rows = append(rows, a.hostRow(on, now))
		heading := on.headingRow()
		for _, row := range open[host] {
			live[row.Entry] = true
			if row.Kind == conns.Server && row.Entry == heading {
				// The connection itself is the machine, and the machine
				// is the heading above these rows. A row for it as well
				// says the same thing twice. The row of a connection
				// that has dropped is drawn: the heading no longer
				// carries it, and greyRow left it to be read and
				// cleared.
				continue
			}
			rows = append(rows, a.panelRow(row, now))
		}
		// And what the window taken over says it has open, under it.
		// Its list, not one worked out here: what a window has open is
		// that window's business, and a client that guessed would
		// disagree with the machine it is looking at.
		rows = append(rows, a.remoteRows(on)...)
	}
	for e := range a.rates {
		if !live[e] {
			delete(a.rates, e)
		}
	}
	a.panel.SetRows(rows)
	// Which row is in front, told to the list rather than left to the
	// bar: the bar is the user's and moves where they put it.
	a.panel.SetCurrent(a.showing())
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
func (a *app) hostRow(on hostFacts, now time.Time) ui.ListRow {
	row := ui.ListRow{
		Text:   groupName(on.name),
		Header: true,
		Key:    hostKey(on.name),
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
	if on.kind == hostWindow || on.serves {
		// A window of its own colour. It is a different thing to a
		// machine -- another gridterm, with panes rather than a shell --
		// and the sidebar should not make the user read the name to
		// tell which is which.
		row.FG = a.colours.ANSI[5]
	}
	e := on.headingRow()
	if e == nil {
		return row
	}
	state := e.State(now)
	row.Mark, row.MarkFG = dot, a.stateFG(state, now)
	row.Note = a.note(conns.Row{Entry: e, State: state}, now)
	return row
}

// greyRow leaves the row of a connection that has gone on the panel,
// closed and with nothing left to reveal.
//
// A machine or a window whose far end goes by itself keeps its row until
// the user clears it, with "Close this connection" or "clear finished",
// because what a connection did before it went is worth reading. What
// the row says it was is the caller's to set.
//
// why is what the connection ended with, and goes on the row's note: a
// user told only that a machine went has nothing to act on, while "the
// host closed the connection" and "connection reset by peer" send them
// to different places.
//
// A clean end says nothing the greying does not, so it is left off: a
// nil reason, and an end of file, which is what a far end hanging up
// politely looks like.
func (a *app) greyRow(e *conns.Entry, why error) {
	if why != nil && !errors.Is(why, io.EOF) {
		said := serve.Plain(why.Error())
		if e.Note != "" {
			said = e.Note + ": " + said
		}
		e.Note = said
	}
	dead := meter.New()
	dead.Close()
	e.Meter = dead
	e.Reveal = nil
	e.Close = func() error {
		a.registry.Drop(e)
		a.refreshServers()
		a.markDirty()
		return nil
	}
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
	// The kind icon in the state colour, with the dot behind it for a
	// sidebar too narrow to draw the icon.
	state := a.stateFG(row.State, now)
	out := ui.ListRow{
		Text: row.Label, Depth: 1, Key: row.Entry,
		Note: a.note(row, now), Icon: icon(row.Kind), IconFG: state,
		Mark: dot, MarkFG: state,
	}
	out.Art = a.graph(row.Entry)
	if row.State == meter.Closed {
		// A finished connection reads as finished rather than as one
		// more thing running.
		out.FG = a.colours.ANSI[8]
	}
	return out
}

// note is what a row says at its end.
//
// What state it is in is the mark's business, and how busy it is the
// graph's. The note is for what neither can say: a count of streams, the
// machine a connection is reached through, how far a job has got.
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
	// Sampled and thrown away: the sampling is what keeps the run the
	// graph draws, and the graph is what the speed used to be for. The
	// number itself took the widest part of the row and pushed out the
	// one thing that says which connection this is.
	rate.Sample(row.Meter, now)
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
