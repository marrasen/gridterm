package main

import (
	"fmt"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
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
	if d := ms.opening[was]; d != nil {
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
	switch {
	case moved.connection:
		a.renamedFiles(was, to.Name)
		// The file sessions left parked on it need nothing: they are
		// counted against the connection, which the rename did not touch.

		// The connection's own row, and the rows of the panes and the
		// tunnels on it.
		a.rehostRows(was, to.Name)
	case moved.dial:
		// The row of the pane watching the connection being made.
		a.rehostRows(was, to.Name)
	}
	// refreshServers re-keys a window taken over under that name too.
	a.refreshServers()
	a.markDirty()
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
