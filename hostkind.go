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

	// hostHere is the machine gridterm itself is running on, which it
	// needs no connection to.
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

	// hostFar is a machine of a window taken over, reached through that
	// window. about never gives this out: a name cannot say it, and
	// a.current is the only thing that hands it over.
	hostFar
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
	case hostFar:
		return "machine of a window taken over"
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
	//
	// A saved name is also looked for under the list's own spelling of
	// it, because the list folds case and the maps do not.
	window   *taken
	machine  *machine
	dialling *dialling

	// far is the machine of a window taken over, set only on hostFar. Its
	// window is never nil there.
	far remoteHostKey
}

// held says the window is holding something under this name: a
// connection, another gridterm taken over, or one being reached.
// runsCommands reports whether a command can be run on a machine, which
// needs a connection and a shell.
func (f hostFacts) runsCommands() bool {
	return f.kind != hostHere && f.kind != hostWindow && !f.serves
}

func (f hostFacts) held() bool {
	return f.window != nil || f.machine != nil || f.dialling != nil
}

// toTakeOver says the name is a gridterm window this one is not holding
// yet, so asking for a terminal on it means taking it over.
func (f hostFacts) toTakeOver() bool {
	return f.serves && f.window == nil
}

// log is the account of how the name was reached, or is being reached,
// and nil when the window is holding nothing under it that kept one.
func (f hostFacts) log() *connLog {
	switch {
	case f.window != nil:
		return f.window.log
	case f.machine != nil:
		return f.machine.log
	case f.dialling != nil:
		return f.dialling.log
	}
	return nil
}

// headingRow is the row the machine's heading on the panel draws itself
// from: the connection the name is holding, or nil when it holds none.
func (f hostFacts) headingRow() *conns.Entry {
	switch {
	case f.window != nil:
		return f.window.entry
	case f.machine != nil:
		return f.machine.entry
	}
	return nil
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
		window:   a.windows.named(host),
		machine:  a.machines.named(host),
		dialling: a.machines.connecting(host),
	}
	if k, saved := a.book.Kind(host); saved {
		f.saved, f.serves, f.spelling = true, k.Window, k.Name
		if k.Window && f.window == nil {
			f.window = a.windows.named(k.Name)
		}
	}

	switch {
	case f.window != nil:
		f.kind = hostWindow
	case f.machine == nil && f.name == conns.Local:
		// A connection under the same name wins: it is a machine the
		// window reached, not the one it is running on.
		f.kind = hostHere
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
