package main

import (
	"fmt"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/vfs"
)

// The whole of what a rename has to follow: the connection or the dial
// under the old name, the panel rows filed under it, the file panes
// reading through it, and the windows taken over, which are keyed by the
// name the server list gives them. Together in one file so that the next
// thing keyed by a name is added here rather than found missing later.

// moves says what a rename moved: the connection under the name, or a
// connection still on its way to it.
//
// The invariant leaves room for one of the two, so at most one of these
// is ever true.
type moves struct{ connection, dial bool }

// rename gives a connection, and any dial still on its way to it, the
// name the server list now uses.
//
// The connection moves only when it is still the same machine: a rename
// that changes the address as well leaves it where it is.
//
// A name this type is already holding is refused. The dialog refuses
// more than that, a window taken over under the name as well, and is the
// guard that fires in practice.
func (ms *machines) rename(was string, to remote.Host) (moved moves, err error) {
	now := to.Name
	if ms.held[now] != nil || ms.opening[now] != nil {
		return moves{}, fmt.Errorf("%s is already the name of a connection", now)
	}
	if m := ms.held[was]; m != nil && m.at.cfg.SameMachine(to.Config()) {
		delete(ms.held, was)
		m.at.name = now
		ms.held[now] = m
		moved.connection = true
	}
	if d := ms.opening[was]; d != nil && d.movesTo(was, to) {
		delete(ms.opening, was)
		ms.opening[now] = d
		d.renamedTo(was, now)
		moved.dial = true
	}
	return moved, nil
}

// renamedMachine follows a rename through everything the window keys by
// a machine's name.
//
// The machine has not changed, only what it is called. What was open
// under the old name would otherwise show as a second machine in the
// sidebar, and closing it would look for one that is not there any more.
func (a *app) renamedMachine(was string, to remote.Host) {
	moved, err := a.machines.rename(was, to)
	if err != nil {
		// Logged rather than shown: the dialog has already refused a
		// rename onto a name the window is holding.
		a.logError(err)
	}
	// The rows follow what moved and nothing else. A connection left
	// under the old name because the address changed keeps its rows
	// there, or the panel would draw them under a name nothing is
	// connected to.
	//
	// A dial still on its way counts as moved for all of this. The
	// machine it is reaching will be held under the new name, and a
	// file pane left under the old one would ask for a machine nothing
	// is connected to and dial the same box a second time under a name
	// the window no longer uses.
	//
	// Finished work needs nothing: it keeps the id of the server it ran
	// on and asks the list what that is called when it is done again.
	switch {
	case moved.connection || moved.dial:
		a.movedFiles(was, to.Name, "")
	case a.droppedOn(was, to.ID):
		// A machine nothing is connected to counts as moved as well. A
		// file pane outlives its connection now, so the likeliest
		// moment to rename a machine is while it is not there: left
		// under the old name, the pane's next click dials the same box
		// under a name the list has stopped using and puts a second
		// group on the sidebar.
		a.movedFiles(was, to.Name, to.ID)
	}
	// refreshServers re-keys a window taken over under that name too.
	a.refreshServers()
	a.markDirty()
}

// movedFiles files what was under one name under another: the file
// panes, the filesystems no pane holds, and the rows.
//
// id narrows the filesystems to the ones on that saved server, and
// empty takes every one under the name. A connection that moved takes
// them all, because they read through it whatever they were opened as.
func (a *app) movedFiles(was, now, id string) {
	if id != "" && a.sharesTheName(was, id) {
		a.movedServer(was, now, id)
		return
	}
	a.renamedFiles(was, now)
	a.renamedTheMachine(was, now, id)
	// The file sessions left parked on it need nothing: they are
	// counted against the connection, which the rename did not touch.
	// The connection's own row, the rows of the panes and the
	// tunnels on it, and the row of a pane watching one being made.
	a.rehostRows(was, now)
}

// sharesTheName reports whether something filed under a name is on a
// machine other than the saved server with an id.
//
// A name can stand for two machines at once: a pane left under it when
// its server was renamed and pointed somewhere else, and a pane on a
// server saved since under that name.
//
// A filesystem already closed does not count. Closing takes it off the
// window's list a frame later, and until then it would count for a pane
// that has gone.
func (a *app) sharesTheName(was, id string) bool {
	for _, r := range a.reopening {
		if r.Host() == was && r.step().id != id && !r.closed() {
			return true
		}
	}
	return false
}

// movedServer files what is on one saved server under the name it has
// now, and leaves everything else under the old name where it is.
//
// For a name that stands for two machines, where moving by the name
// would take the other machine's panes and rows along. The rows that
// can be told apart are the ones a file pane, a reader or a piece of
// file work holds, because each knows the filesystem it reads through.
func (a *app) movedServer(was, now, id string) {
	on := func(f vfs.FS) bool {
		r, is := f.(*reopening)
		return is && r.Host() == was && r.step().id == id
	}
	if b := a.files; b != nil {
		for _, p := range b.view.Panes() {
			if !on(p.FS()) {
				continue
			}
			p.FS().(*reopening).Renamed(now)
			if row := b.rows[p]; row != nil && row.Host == was {
				row.Host = now
			}
		}
	}
	for _, r := range a.readers {
		if r.on != nil && on(r.on) && r.row.Host == was {
			r.row.Host = now
		}
	}
	// Work is filed under the machine it read from, which its end says,
	// finished or not.
	for e, from := range a.jobFrom {
		if e.Host == was && from.far.window == nil && from.at.id == id {
			e.Host = now
		}
	}
	a.renamedTheMachine(was, now, id)
}

// renamedFiles tells the file panes on a machine that it is called
// something else now.
//
// The name is frozen into the filesystem when the pane opens, and the
// window finds a pane's machine by matching it. A pane left under the
// old name would not be closed with its connection, and would sit on a
// session that had gone.
func (a *app) renamedFiles(was, now string) {
	for _, f := range a.filesystemsOn(was) {
		f.fs.Renamed(f.named(now))
	}
}

// droppedOn reports whether a file pane is filed under a name that
// nothing is connected to, and nothing is on its way to, and is on the
// saved server with an id.
//
// By the id and not by the address. The address says where the machine
// was, which is not which server the user edited: one renamed and
// pointed somewhere else in the same edit is still the entry the pane
// was opened from, and one saved since under the name it gave up is
// not, wherever it is.
func (a *app) droppedOn(was, id string) bool {
	if id == "" || a.machines.named(was) != nil || a.machines.connecting(was) != nil {
		return false
	}
	for _, r := range a.reopening {
		if r.Host() == was && r.step().id == id {
			return true
		}
	}
	return false
}

// followSaved files a filesystem's machine under the name its saved
// server goes by now, for a pane that is about to open it again.
//
// A rename reaches a pane when it happens, but not one reading through a
// connection left under the old name: a rename that points the entry
// somewhere else leaves the connection where it is, because it is to the
// old address. Once that goes, the pane is on the server the user
// edited, and that server has another name.
//
// Left alone while something is connected or connecting under the old
// name, because what is under it is what the pane reads through.
func (a *app) followSaved(was, now, id string) {
	if a.machines.named(was) != nil || a.machines.connecting(was) != nil {
		return
	}
	a.movedFiles(was, now, id)
	a.markDirty()
}

// rekeyWindows follows a change to the server list through the windows
// taken over.
//
// Called from refreshServers, which is where the list may have changed.
// The key a window is held under comes from the list, so whatever else
// goes by the old name follows here.
func (a *app) rekeyWindows() {
	moved, err := a.windows.rekey()
	if err != nil {
		// Logged rather than shown: this runs whenever a connection is
		// made or lost, and a dialog a frame would be unusable.
		a.logError(err)
		return
	}
	if len(moved) == 0 {
		return
	}
	// What goes by each old name is gathered before any of it is
	// renamed, because two windows can trade names.
	rows := make([][]*conns.Entry, len(moved))
	reading := make([][]renamedPane, len(moved))
	for i, r := range moved {
		rows[i] = a.rowsUnder(r.was)
		reading[i] = a.filesystemsOn(r.was)
	}
	for i, r := range moved {
		for _, e := range rows[i] {
			e.Host = r.window.name
		}
		for _, f := range reading[i] {
			f.fs.Renamed(f.named(r.window.name))
		}
	}
}

// rowsUnder are the panel rows filed under a machine's name: its panes,
// its tunnels and the connection itself.
func (a *app) rowsUnder(host string) []*conns.Entry {
	var out []*conns.Entry
	for _, group := range a.registry.Groups(time.Now()) {
		if group.Host != host {
			continue
		}
		for _, row := range group.Rows {
			out = append(out, row.Entry)
		}
	}
	return out
}

// rehostRows moves every panel row under one name to another, for a
// machine or a window the user has called something else.
func (a *app) rehostRows(was, now string) {
	for _, e := range a.rowsUnder(was) {
		e.Host = now
	}
}
