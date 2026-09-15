package main

import (
	"strconv"
	"sync"
	"time"

	"github.com/marrasen/gridterm/conns"
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
		for i, row := range g.Rows {
			snap.Open = append(snap.Open, serve.Open{
				// The machine and the place in its list, which together
				// name a row for as long as it is there. Nothing here
				// has an identity of its own to send.
				ID:    openID(g.Host, i),
				Host:  g.Host,
				Kind:  row.Kind.String(),
				Label: row.Label,
				Note:  row.Note,
				// meter.State already spells these the way the
				// panel shows them, so both windows say the same
				// word for the same thing.
				State: row.State.String(),
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
	if a.server == nil {
		return
	}
	snap := a.snapshot(now)
	if same := sameSnapshot(a.lastSnapshot, snap); same {
		return
	}
	a.lastSnapshot = snap
	a.openNow.set(snap)
	a.server.Publish(snap)
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

// remoteRows are the rows for what a window taken over has open.
//
// Shown under the window itself, one line each, saying which machine
// over there it is on. They cannot be revealed or closed from here: the
// pane drawing them is that window's, not this one's, and a row that
// offered to close something it cannot reach would be a row that lies.
func (a *app) remoteRows(host string) []ui.ListRow {
	t := a.windows[host]
	if t == nil {
		return nil
	}
	var rows []ui.ListRow
	for _, open := range t.win.Opens() {
		text := open.Label
		if open.Host != conns.Local && open.Host != "" {
			// Which machine over there, because "Local" on that window
			// is not this machine and saying so plainly would be wrong.
			text = open.Host + ": " + open.Label
		}
		rows = append(rows, ui.ListRow{
			Text:  text,
			Note:  open.Note,
			Depth: 1,
			Key:   remoteKey{window: host, id: open.ID, label: open.Label},
			Mark:  remoteMark,
			// Dimmed, because it is running somewhere else: what this
			// window can do with it is open a pane to watch it in, not
			// close it or put it in front.
			FG: mix(a.colours.FG, a.colours.BG, 1, 2),
		})
	}
	return rows
}

// remoteMark is the dot in front of a row belonging to another window.
// Hollow, because what it stands for is not running here.
const remoteMark = '◦'

// remoteKey names a row belonging to a window taken over, so choosing
// it can say which thing on which window.
type remoteKey struct {
	window string
	id     string
	label  string
}

// openID names one of the things a window has open, for as long as it
// is open.
//
// The machine and the place in its list, which is all there is: nothing
// in the registry has an identity of its own, and one made up here
// would have to be kept in step with a list built afresh every frame.
func openID(host string, at int) string { return host + "#" + strconv.Itoa(at) }
