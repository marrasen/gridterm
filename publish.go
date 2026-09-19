package main

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
)

// snapshot is what this window has open, in the shape another window
// reads it in.
//
// Built from the registry, which is what the panel is built from, so
// the two windows are looking at one list rather than at two that agree
// today.
func (a *app) snapshot(now time.Time) serve.Snapshot {
	snap := serve.Snapshot{Window: conns.Local}
	for _, g := range a.registry.Groups(now) {
		// Whether the machine these are on is a window this one has
		// taken over. Asked once per machine: a client offers nothing on
		// one, because it is a window rather than a machine.
		window := a.windows.named(g.Host) != nil
		for _, row := range g.Rows {
			// The size of its screen, for something that has one. It
			// is not resized to suit a watcher, so a watcher that
			// wants to know has to be told.
			cols, rows := 0, 0
			if pane := a.paneFor(row.Entry); pane != nil {
				size := pane.Size()
				cols, rows = size.Cols, size.Rows
			}
			snap.Open = append(snap.Open, serve.Open{
				// What the registry calls it, which names it for as
				// long as it is open however the list moves around it.
				ID:    row.ID(),
				Host:  g.Host,
				Kind:  row.Kind.String(),
				Label: row.Label,
				Note:  row.Note,
				// meter.State already spells these the way the
				// panel shows them, so both windows say the same
				// word for the same thing.
				State:  row.State.String(),
				Window: window,
				Cols:   cols,
				Rows:   rows,
			})
		}
	}
	return snap
}

// tellWatchers says what this window has open, when that has changed.
//
// Only when it has changed. A client redrawing its panel from a
// snapshot arriving sixty times a second is a client that never goes
// idle, which is the thing the whole display is built to avoid.
func (a *app) tellWatchers(now time.Time) {
	if !a.serving.on() {
		return
	}
	a.serving.publish(a.snapshot(now))
}

// shared carries the snapshot from the goroutine that draws to the
// goroutines serving clients.
//
// A client's control channel opens a moment after the connection does,
// so it can miss the publish that its arrival caused. It asks for the
// last one instead of waiting for the next, and this is what makes that
// safe to ask from there.
type shared struct {
	mu   sync.Mutex
	snap serve.Snapshot
}

func (s *shared) set(snap serve.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap = snap
}

func (s *shared) get() serve.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

// sameSnapshot reports whether two say the same thing.
func sameSnapshot(was, now serve.Snapshot) bool {
	if was.Window != now.Window || len(was.Open) != len(now.Open) {
		return false
	}
	for i, o := range now.Open {
		if was.Open[i] != o {
			return false
		}
	}
	return true
}

// remoteRows are the screens a window taken over has open, the machines
// over there, and this window's own rows on those machines.
//
// All of it under the window itself, one line each. A screen over there
// cannot be closed from here: the pane drawing it is that window's, not
// this one's, and a row that offered to close something it cannot reach
// would be a row that lies.
//
// Only what can be opened here. That window's tunnels and clients are
// rows on its panel and nothing this one can do anything with, so they
// are left out. Its connections become the headings, and one to a
// machine it is connected to carries a plus, since a pane here can read
// that machine's files through it. A machine still being connected to
// carries none, and neither does a window that window has itself taken
// over: neither has files this one can ask for. A screen that has
// finished is left out too: there is nothing left on it to watch.
//
// mine are this window's own rows filed under that window, so the ones
// on a machine over there go under that machine's heading rather than
// under the window's name. far is what farRows worked out for this
// frame.
func (a *app) remoteRows(on hostFacts, mine []conns.Row,
	far map[*conns.Entry]remoteHostKey, now time.Time) []ui.ListRow {

	t := on.window
	if t == nil {
		return nil
	}
	// Grouped by the machine over there they run on, the way that
	// window groups them itself. The window's own machine has no
	// heading: the window's name is right above its rows. Every other
	// machine that window is connected to has one, whether or not
	// anything is open on it, so a heading does not come and go with
	// what happens to be under it.
	var order []string
	under := map[string][]serve.Open{}
	// The connection to each machine over there, which is what says the
	// window is connected to it rather than still on its way, and what
	// says whether it is a machine at all.
	held := map[string]serve.Open{}
	group := func(host string) {
		if _, seen := under[host]; !seen {
			order = append(order, host)
			under[host] = nil
		}
	}
	for _, open := range t.win.Opens() {
		if open.State == meter.Closed.String() {
			continue
		}
		if open.Kind == conns.Server.String() && !isTheirOwn(open.Host) {
			// The connection to a machine over there, which is what
			// makes the machine worth a heading.
			held[open.Host] = open
			group(open.Host)
			continue
		}
		if !open.HasScreen() {
			continue
		}
		if a.watchingPane(remoteKeyFor(t, open)) != nil {
			// There is a pane of this window watching it, with a row of
			// its own. One thing open should be one row, and the row
			// that can be put in front and closed is the better one.
			continue
		}
		group(open.Host)
		under[open.Host] = append(under[open.Host], open)
	}

	// And this window's own rows on those machines. A machine the window
	// over there has let go of still gets a heading, so a pane still
	// reading it has somewhere to be shown.
	ours := map[string][]conns.Row{}
	for _, row := range mine {
		key, yes := far[row.Entry]
		if !yes || key.window != t {
			continue
		}
		group(key.host)
		ours[key.host] = append(ours[key.host], row)
	}

	dim := grid.Blend(a.frameFG(), a.sidebarTop(), 1, 2)
	screens := func(host string) []ui.ListRow {
		var rows []ui.ListRow
		for _, open := range under[host] {
			rows = append(rows, ui.ListRow{
				Text:  open.Label,
				Note:  open.Note,
				Depth: 2,
				Key:   remoteKeyFor(t, open),
				Mark:  remoteMark,
				// Dimmed, because it is running somewhere else: what
				// this window can do with it is open a pane to watch it
				// in, not close it or put it in front.
				FG: dim,
			})
		}
		return rows
	}

	// The window's own machine first, so its screens sit under the
	// window's name where the rest of its rows are.
	var rows []ui.ListRow
	for _, host := range order {
		if isTheirOwn(host) {
			rows = append(rows, screens(host)...)
		}
	}
	// Then each machine over there: its name, this window's rows on it,
	// and the screens over there nobody here is watching.
	for _, host := range order {
		if isTheirOwn(host) {
			continue
		}
		// A machine heading, a step in from the window's own: the same
		// vocabulary the sidebar uses at the top level, so it reads as a
		// machine rather than as a server nobody is connected to.
		head := ui.ListRow{
			Text:   host,
			Header: true,
			Depth:  2,
			Key:    remoteHostKey{window: t, host: host},
			// A blank where the dot goes while that window holds nothing
			// under the name, so the name does not shift sideways.
			Mark: ' ',
		}
		if open, connected := held[host]; connected {
			head.Mark, head.MarkFG = dot, a.stateFG(stateNamed(open.State), now)
			if open.Window {
				// A window that window took over, in the colour a window
				// has. It has panes of its own and no files to serve
				// this one, so there is nothing to open on it.
				head.FG = a.onSidebar(a.colours.ANSI[5])
			} else {
				// What can be opened on it, which is a pane reading its
				// files through the window.
				head.Button = '+'
			}
		}
		rows = append(rows, head)
		for _, row := range ours[host] {
			mine := a.panelRow(row, now)
			// A step in, the way the screens over there are: this
			// heading is itself inside the window's.
			mine.Depth = 2
			rows = append(rows, mine)
		}
		rows = append(rows, screens(host)...)
	}
	return rows
}

// stateNamed reads a state back from the word another window published
// it as, so a row here is marked the colour that window marks it.
//
// Anything else is a state this build has no word for, which is drawn
// the way a connection that is simply there is.
func stateNamed(said string) meter.State {
	for _, state := range []meter.State{meter.Opened, meter.Active, meter.Settled, meter.Closed} {
		if said == state.String() {
			return state
		}
	}
	return meter.Settled
}

// farRows says which machine of a window taken over each of this
// window's rows is on.
//
// A file pane reading through that window, and a pane watching a screen
// over there: both are filed under the window's name, and both belong
// under the name of the machine they really touch.
//
// Worked out once a frame and handed down. The sidebar asks about every
// row it draws, and asking a window what it has open takes a copy under
// a lock.
//
// It fills a map the caller keeps rather than making one: the sidebar
// asks once a frame, and a map a frame is a map a frame for nothing.
func (a *app) farRows(out map[*conns.Entry]remoteHostKey) map[*conns.Entry]remoteHostKey {
	clear(out)
	if b := a.files; b != nil {
		for p, key := range b.far {
			if row := b.rows[p]; row != nil && !isTheirOwn(key.host) {
				out[row] = key
			}
		}
	}
	// What each window has open, asked for once per window rather than
	// once per pane watching one.
	opens := map[*taken][]serve.Open{}
	for pane, what := range a.windows.watched() {
		e := a.panes[pane]
		if e == nil || what.window == nil || !a.windows.holds(what.window) {
			continue
		}
		list, asked := opens[what.window]
		if !asked {
			list = what.window.win.Opens()
			opens[what.window] = list
		}
		for _, open := range list {
			if open.ID != what.id {
				continue
			}
			if !isTheirOwn(open.Host) {
				out[e] = remoteHostKey{window: what.window, host: open.Host}
			}
			break
		}
	}
	return out
}

// remoteHostKey names a machine of a window taken over, for the heading
// over the screens open on it.
//
// Its own type so it is never mistaken for a machine of this window or
// for one of that window's screens: nothing can be done with it, and a
// key that compared equal to something that can would act on the wrong
// thing.
type remoteHostKey struct {
	window *taken
	host   string
}

// isTheirOwn reports whether a machine name is the window's own, which
// is what its Local means.
func isTheirOwn(host string) bool { return host == conns.Local || host == "" }

// remoteMark is the dot in front of a row belonging to another window.
// Hollow, because what it stands for is not running here.
const remoteMark = '◦'

// remoteKey names a row belonging to a window taken over, so choosing
// it can say which thing on which window.
//
// The window itself rather than the name it is held under, because the
// server list moves that name under a row already drawn.
//
// Only what names the thing: the window it is on and what that window
// calls it. The list keeps the user's place by comparing keys, so a key
// carrying what the row is doing -- its note, its state, the size of
// its screen, the title the program gave it a moment ago -- would move
// the selection out from under them every time any of that changed.
type remoteKey struct {
	window *taken
	id     string
}

// remoteKeyFor names one of the things a window taken over has open.
func remoteKeyFor(t *taken, open serve.Open) remoteKey {
	return remoteKey{window: t, id: open.ID}
}

// farNote is what a watching pane's row says about the screen it is
// showing, when that screen is not the size of the pane.
//
// Only when it differs. The far end is not resized to suit the pane, so
// a wider screen wraps and a taller one runs off the bottom, and the
// size is the only thing that explains it.
func farNote(pane ui.Size, cols, rows int) string {
	if cols <= 0 || rows <= 0 || (pane.Cols == cols && pane.Rows == rows) {
		return ""
	}
	return farSize + sizeText(cols, rows)
}

// heldNote is what the row of a pane says when somebody watching it has
// taken its size.
//
// The size they set, and how many of them are reading it.
func heldNote(size ui.Size, n int) string {
	return farSize + sizeText(size.Cols, size.Rows) + ", " + watchedNote(n)
}

// sizeText writes a screen size the way both notes say it.
func sizeText(cols, rows int) string {
	return strconv.Itoa(cols) + "x" + strconv.Itoa(rows)
}

// isFarNote reports whether a note is one of ours.
func isFarNote(note string) bool { return strings.HasPrefix(note, farSize) }

// farSize begins the note on a pane showing a screen of another size.
//
// Short, because the note sits on the same line as the title and the
// sidebar is narrow: a longer one left nothing of the title to read.
const farSize = "at "

// watchedNote is what a pane's row says when somebody elsewhere is
// reading it.
func watchedNote(n int) string { return watchedBy + " " + strconv.Itoa(n) }

// isOurNote reports whether a note is one the panel wrote about
// watching, so it can be taken away again without writing over a note
// something else put there.
func isOurNote(note string) bool {
	return strings.HasPrefix(note, watchedBy) || isFarNote(note)
}

// watchedBy begins the note on a pane somebody elsewhere is reading.
const watchedBy = "watched by"
