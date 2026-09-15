package main

import (
	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
)

// hostKind is what a name stands for.
type hostKind int

const (
	// hostUnknown is a name the window knows nothing about.
	hostUnknown hostKind = iota

	// hostHere is a machine the window has no connection of its own to:
	// this one, or the one -ssh put every pane on.
	hostHere

	// hostWindow is another gridterm, already taken over.
	hostWindow

	// hostSavedWindow is saved as a gridterm window and not taken over
	// yet.
	hostSavedWindow

	// hostMachine is a machine the window holds a connection to.
	hostMachine

	// hostConnecting is a machine being connected to right now.
	hostConnecting

	// hostSavedMachine is in the server list with nothing connected to
	// it.
	hostSavedMachine
)

// String names a kind, for a test that has to say what it found.
func (k hostKind) String() string {
	switch k {
	case hostHere:
		return "here"
	case hostWindow:
		return "window"
	case hostSavedWindow:
		return "saved window"
	case hostMachine:
		return "connected machine"
	case hostConnecting:
		return "connecting"
	case hostSavedMachine:
		return "saved machine"
	}
	return "unknown"
}

// hostFacts holds what a name is, what the server list says about it,
// and whatever this window is holding under it.
type hostFacts struct {
	// a is the window the facts were read from, for record().
	a *app

	// name is the name as it was asked about, which is the name to act
	// on. The server list's own spelling of it is spelling.
	name string

	kind hostKind

	// local says this is the machine gridterm itself is running on
	// rather than the one -ssh put every pane on. Both are hostHere,
	// and only this one has files to read without a connection.
	local bool

	// saved says the server list holds this name, and serves narrows
	// that to one saved as a gridterm window rather than a machine to
	// log in to. Both stay true once the window is taken over, which is
	// why they are flags rather than kinds.
	saved, serves bool

	// spelling is the server list's own spelling of the name, empty
	// unless saved.
	spelling string

	// window, machine and dialling are what stands behind the name.
	// Whichever the kind names is set; the others are nil.
	window   *taken
	machine  *machine
	dialling *dialling

	// heldAt is the window already taken over at this saved window's
	// address, whatever name it is held under. A window taken over by
	// address and saved afterwards is held under the address, so the
	// name alone does not say whether it is held.
	heldAt *taken
}

// held says the window is holding something under this name: a
// connection, another gridterm taken over, or one being reached.
func (f hostFacts) held() bool {
	return f.window != nil || f.machine != nil || f.dialling != nil
}

// toTakeOver says the name is a gridterm window this one is not holding
// yet, so asking for a terminal on it means taking it over.
func (f hostFacts) toTakeOver() bool {
	return f.serves && f.kind != hostWindow && f.heldAt == nil
}

// record clones the server list's entry for the name out of the book,
// which about does not. Empty unless the name is saved.
func (f hostFacts) record() remote.Host {
	if f.a == nil || !f.saved {
		return remote.Host{}
	}
	h, _ := f.a.book.Lookup(f.name)
	return h
}

// about says what kind of host a name is and hands back whatever stands
// behind it.
//
// Called for every row of every frame, so it clones nothing: a caller
// that wants the whole saved record asks record() for it.
//
// Case: the maps the window keys by name are keyed exactly, so they are
// asked with the name exactly as it was given. The book folds case, so
// saved, serves and spelling answer for any capitals.
func (a *app) about(host string) hostFacts {
	f := hostFacts{
		a:        a,
		name:     host,
		window:   a.windows[host],
		machine:  a.machines[host],
		dialling: a.opening[host],
	}
	if k, saved := a.book.Kind(host); saved {
		f.saved, f.serves, f.spelling = true, k.Window, k.Name
		if k.Window {
			f.heldAt = a.windowAt(k.Serve)
		}
	}

	switch {
	case f.window != nil:
		f.kind = hostWindow
	case f.machine == nil && (f.name == conns.Local || f.name == a.localHost):
		// A connection under the same name wins: it is a machine the
		// window reached, not the one it is running on.
		f.kind, f.local = hostHere, f.name == conns.Local
	case f.machine != nil:
		f.kind = hostMachine
	case f.dialling != nil:
		f.kind = hostConnecting
	case f.serves:
		f.kind = hostSavedWindow
	case f.saved:
		f.kind = hostSavedMachine
	}
	return f
}
