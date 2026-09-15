package main

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
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
	snap := serve.Snapshot{Window: a.localHost}
	for _, g := range a.registry.Groups(now) {
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
				State: row.State.String(),
				Cols:  cols,
				Rows:  rows,
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

// openRows are the rows for what a window taken over has open.
//
// They are shown under that window, and are what it says they are: a
// client that worked them out for itself would be a client disagreeing
// with the machine it is looking at.
func (a *app) openRows(t *taken) []serve.Open { return t.win.Opens() }

// remoteRows are the screens a window taken over has open.
//
// Shown under the window itself, one line each, saying which machine
// over there it is on. They cannot be closed from here: the pane
// drawing them is that window's, not this one's, and a row that offered
// to close something it cannot reach would be a row that lies.
//
// Only what can be opened here. That window's own connections, tunnels
// and clients are rows on its panel and nothing this one can do
// anything with, and a list of rows that do nothing is a list nobody
// can read.
func (a *app) remoteRows(on hostFacts) []ui.ListRow {
	t := on.window
	if t == nil {
		return nil
	}
	host := on.name
	// Grouped by the machine over there they run on, the way that
	// window groups them itself. A flat list with the machine's name
	// repeated on every line says the same thing many times and hides
	// what each row actually is.
	var order []string
	under := map[string][]serve.Open{}
	for _, open := range t.win.Opens() {
		if !open.HasScreen() {
			continue
		}
		if a.windows.watcher(remoteKeyFor(host, open)) != nil {
			// There is a pane of this window watching it, with a row of
			// its own. One thing open should be one row, and the row
			// that can be put in front and closed is the better one.
			continue
		}
		if _, seen := under[open.Host]; !seen {
			order = append(order, open.Host)
		}
		under[open.Host] = append(under[open.Host], open)
	}

	dim := grid.Blend(a.colours.FG, a.colours.BG, 1, 2)
	var rows []ui.ListRow
	for _, on := range order {
		if len(order) > 1 || !isTheirOwn(on) {
			// A heading only when there is something to tell apart.
			// One machine's worth of panes under a window whose name is
			// right above them needs no line saying so twice.
			rows = append(rows, ui.ListRow{
				Text:   theirName(on),
				Header: true,
				Depth:  1,
				Key:    remoteHostKey{window: host, host: on},
				Mark:   ' ',
				FG:     dim,
			})
		}
		for _, open := range under[on] {
			rows = append(rows, ui.ListRow{
				Text:  open.Label,
				Note:  open.Note,
				Depth: 2,
				Key:   remoteKeyFor(host, open),
				Mark:  remoteMark,
				// Dimmed, because it is running somewhere else: what
				// this window can do with it is open a pane to watch it
				// in, not close it or put it in front.
				FG: dim,
			})
		}
	}
	return rows
}

// remoteHostKey names a machine of a window taken over, for the heading
// over the screens open on it.
//
// Its own type so it is never mistaken for a machine of this window or
// for one of that window's screens: nothing can be done with it, and a
// key that compared equal to something that can would act on the wrong
// thing.
type remoteHostKey struct {
	window, host string
}

// isTheirOwn reports whether a machine name is the window's own, which
// is what its Local means.
func isTheirOwn(host string) bool { return host == conns.Local || host == "" }

// theirName is what to call a machine of a window taken over.
//
// Its "Local" is not this machine, and a heading saying so plainly
// would be read as this one.
func theirName(host string) string {
	if isTheirOwn(host) {
		return "that machine"
	}
	return host
}

// remoteMark is the dot in front of a row belonging to another window.
// Hollow, because what it stands for is not running here.
const remoteMark = '◦'

// remoteKey names a row belonging to a window taken over, so choosing
// it can say which thing on which window.
//
// Only what names the thing: the window it is on and what that window
// calls it. The list keeps the user's place by comparing keys, so a key
// carrying what the row is doing -- its note, its state, the size of
// its screen, the title the program gave it a moment ago -- would move
// the selection out from under them every time any of that changed.
type remoteKey struct {
	window, id string
}

// remoteKeyFor names one of the things a window taken over has open.
func remoteKeyFor(window string, open serve.Open) remoteKey {
	return remoteKey{window: window, id: open.ID}
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
