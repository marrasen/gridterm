package main

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// panelWidth is how wide the connections panel starts.
const panelWidth = 26

// dot is the mark in front of a heading, and behind a connection's icon
// in a sidebar too narrow to draw one.
const dot = '\u2022'

// clearButton is the button at the end of a finished row, which takes
// the row off the panel. Not named clear, which is a builtin.
const clearButton = '\u00d7'

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

// pulseEvery is how often a busy row pulses, and pulseFor how long one
// pulse takes. Between pulses the row sits still, which is what lets the
// window skip three frames in four while something is busy.
const (
	pulseEvery = time.Second
	pulseFor   = 250 * time.Millisecond
)

// newPanel builds the list of connections, in the window's colours.
//
// ui/list_fill_test.go has a copy of this style as panelStyle(), so a
// change here belongs there too.
func (a *app) newPanel() *ui.List {
	l := ui.NewList()
	l.Style = a.panelStyle()
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
		return a.frameDimFG()
	case meter.Active:
		return pulse(green, a.onFrame(a.colours.ANSI[10]), now)
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
	const of = 1 << 10
	return grid.Blend(from, to, int(pulseAt(now)*of)+1, of+2)
}

// pulseAt is how far into a pulse a moment is: pulseRest between
// pulses, 1 at the top of one, and back again.
//
// It holds exactly still between pulses, so the row is drawn the same as
// the frame before and the window can skip that frame.
func pulseAt(now time.Time) float64 {
	into := now.UnixMilli() % int64(pulseEvery/time.Millisecond)
	fade := int64(pulseFor / time.Millisecond)
	if into >= fade {
		return pulseRest
	}
	return pulseRest + (1-pulseRest)*math.Sin(float64(into)/float64(fade)*math.Pi)
}

// pulseRest is how far towards the busy colour a mark sits between
// pulses. Never nothing: a mark resting on the steady colour could not
// be told from a row that is only sitting there.
const pulseRest = 1.0 / 6.0

// sidebarTop and sidebarFoot are the two ends of the ground the window's
// frame is drawn on: the sidebar shades between them down its length,
// and the menu bar across its width.
//
// A theme that wrote its frame down gets one flat colour instead, so the
// frame can be a ground of its own rather than a shade of the window's.
func (a *app) sidebarTop() color.RGBA {
	if a.look.Set {
		return a.look.BG
	}
	return grid.Blend(a.colours.BG, a.colours.ANSI[4], 1, 20)
}

func (a *app) sidebarFoot() color.RGBA {
	if a.look.Set {
		return a.look.BG
	}
	return grid.Blend(a.colours.BG, a.colours.ANSI[4], 1, 8)
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
	s.FG = a.headingFG()
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
	// instead of a title. A pane whose program has gone keeps the label
	// it ended with, which is how "connection lost" stays on the row of
	// one whose transport went.
	if a.paneRows == nil {
		a.paneRows = make(map[*conns.Entry]*term.Terminal, len(a.panes))
	}
	clear(a.paneRows)
	for t, e := range a.panes {
		a.paneRows[e] = t
		if a.ended[t] {
			continue
		}
		// A shell that calls its window by the path of the program it is
		// running says nothing the row does not already say, so the row
		// keeps the name this machine has for that shell.
		if title := t.Title(); title != "" && !a.namesItself(t, title) {
			e.Label = title
			continue
		}
		if name := a.shellName(t); name != "" {
			e.Label = name
		}
	}
	if a.files != nil {
		for _, e := range a.files.rows {
			// A file browser's pane has no terminal, so its row maps to
			// nil: on the list, with nothing to share.
			a.paneRows[e] = nil
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
		// Only over a note of our own. The one other note a pane can
		// carry says its channel could not be let go of, and that is
		// the only place the user can read it.
		if e.Note == "" || isOurNote(e.Note) || isAgentNote(e.Note) {
			e.Note = a.paneNote(pane)
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
		// out again the moment the panel opens, and until then they
		// hold whatever they named -- a window let go of among it.
		// Acting on one is refused: attachHere asks whether the window
		// is still held.
		return
	}

	a.panel.SetHover(a.hoverRow())

	// What is still open, so a rate belonging to something that has gone
	// is not kept for the life of the window.
	live := make(map[*conns.Entry]bool, len(a.rates))

	open := map[string][]conns.Row{}
	for _, group := range a.registry.Groups(now) {
		open[group.Host] = group.Rows
	}

	// Which of this window's rows are on a machine of a window taken
	// over, worked out once for the whole sidebar.
	far := a.farRows()

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
			// A row on a machine of the window taken over goes under
			// that machine's heading further down.
			if key, over := far[row.Entry]; over && key.window == on.window {
				continue
			}
			rows = append(rows, a.panelRow(row, now))
		}
		// And what the window taken over says it has open, under it.
		// Its list, not one worked out here: what a window has open is
		// that window's business, and a client that guessed would
		// disagree with the machine it is looking at.
		rows = append(rows, a.remoteRows(on, open[host], far, now)...)
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
		row.FG = a.onFrame(a.colours.ANSI[5])
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
	// Nothing is left to end, so closing the row and clearing it are the
	// same act: the row goes off the panel.
	drop := a.dropRow(e)
	e.Close, e.Clear = drop, drop
}

// dropRow takes one row off the panel, for a row whose connection has
// already gone.
func (a *app) dropRow(e *conns.Entry) func() error {
	return func() error {
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
	return a.entryOf(ui.FocusedLeaf(a.stage))
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
	if pane := a.paneRows[row.Entry]; pane != nil {
		out.Edge = a.sharedEdge(pane, now)
	}
	// How far the job on this row has got, asked of the job itself: one
	// that has finished is off the list, so its row fills nothing.
	if j := a.jobs[row.Entry]; j != nil {
		out.Fill = jobFill(j.Progress())
	}
	if row.State == meter.Closed {
		// A finished connection reads as finished rather than as one
		// more thing running.
		out.FG = a.frameDimFG()
		if row.Entry.Clear != nil {
			// Clearing the row is the one thing left to do with it, so
			// it is offered on the row itself rather than through a
			// menu. Asked of Clear rather than of Close: a pane keeps
			// what it printed after its program has gone, and its
			// Close would throw the transcript away.
			out.Button = clearButton
		}
	}
	_, isPane := a.paneRows[row.Entry]
	if out.Button == 0 && isPane && row.Entry.Close != nil {
		// A pane's row closes the pane and the row together, so its
		// cross is offered only while the pointer is on the row.
		out.HoverButton = clearButton
	}
	return out
}

// paneNote is what a pane's row says about who else is in it: an agent,
// somebody reading it from another window, or both at once.
func (a *app) paneNote(pane *term.Terminal) string {
	var say []string
	// Ahead of the far end's size: the user can see a size, and cannot
	// otherwise see that something else is typing here. Nothing is
	// worked in once the program has gone, whether or not the hand-over
	// is still in force.
	if h := a.agents.of(pane); h != nil && !a.ended[pane] {
		say = append(say, h.note())
	}
	if what, ok := a.windows.watching(pane); ok {
		// A pane showing a screen that is not its size, which is the one
		// thing about it the user cannot otherwise work out from what it
		// draws. Asked for afresh, because the far end is redrawn at its
		// own size whenever it changes.
		cols, rows := a.farSize(what)
		if note := farNote(pane.Size(), cols, rows); note != "" {
			say = append(say, note)
		}
	} else if n := pane.Watched(); n > 0 {
		if pane.Held() && pane.Size() != pane.ScreenRoom() {
			// Somebody watching set the size, and this window draws that
			// screen in whatever room it has: the size is the only thing
			// that explains what is on it.
			say = append(say, heldNote(pane.Size(), n))
		} else {
			say = append(say, watchedNote(n))
		}
	}
	return strings.Join(say, ", ")
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
// A pane whose program stopped is kept, so that what it printed can
// still be read. Clearing its row is the user saying they have read it,
// so the pane goes too: a pane with no row is one the sidebar cannot
// reach, and the sidebar is the only way to choose what is showing.
func (a *app) clearFinished() error {
	var err error
	now := time.Now()
	// Every pane whose meter has closed, not only the ones the window has
	// reaped: the meter closes on the goroutine reading the session and
	// the reap happens on this one, and DropFinished below goes by the
	// meter, so a pane left open here would lose its row.
	var doomed []*term.Terminal
	for t, e := range a.panes {
		if e.State(now) == meter.Closed {
			doomed = append(doomed, t)
		}
	}
	// Listed first: closing a pane takes it out of the map being walked.
	for _, t := range doomed {
		if cerr := a.closePane(t); cerr != nil && err == nil {
			err = cerr
		}
	}
	if a.registry.DropFinished(now) > 0 {
		a.markDirty()
	}
	// The same as the cross on one row does: what is open has changed,
	// so the menus and the windows held by name are worked out again.
	a.refreshServers()
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

// panelStyle is the sidebar's colours, built afresh whenever the window
// changes theme.
func (a *app) panelStyle() ui.ListStyle {
	st := ui.ListStyle{
		FG: a.frameFG(),
		BG: a.colours.BG,
		// The selected row is marked the way a selected tab is, so the
		// two read as the same thing.
		SelectedFG: a.activeFG(),
		SelectedBG: a.activeBG(),
		// A machine's name is a heading, not one of its connections.
		HeaderFG: a.headingFG(),
		// Dimmer than the row: what a connection is doing is a note
		// beside it, not part of its name.
		NoteFG: a.frameDimFG(),
		// How far a copy has got, further along the line the sidebar's
		// own ground is shaded on: a row filling up reads as part of the
		// frame rather than as a colour from somewhere else.
		FillBG: a.fillBG(),
		// A ground of its own, shading down the list, so the sidebar
		// reads as part of the window's frame rather than as one more
		// thing running in it.
		BGEnd: a.sidebarFoot(),
	}
	st.BG = a.sidebarTop()
	// The row for whatever is in front, marked even while the keys are
	// somewhere else: the sidebar is the list of what is open, so it has
	// to say which one is being looked at.
	st.CurrentFG = a.frameFG()
	// Lifted off the list's own ground rather than the window's
	// selection colour, so it stays darker than the mark drawn on it:
	// the mark is what says whether the connection is open.
	st.CurrentBG = a.currentBG()
	// A little air around each machine's name, so it reads as a heading
	// for the rows under it rather than as another row. A quarter of a
	// character each way: enough to see, and far less than the blank
	// line it would otherwise take.
	st.HeaderPad = grid.Pad{Before: 1, After: 1}
	return st
}
