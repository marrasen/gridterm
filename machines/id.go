// Package machines keeps what kakel knows about each machine it works
// on: what it is called, how it is connected, and what hangs off that
// connection, in one record per machine.
//
// Machines go by an ID that does not change, never by a name. A saved
// server or window goes by its ID in the server list, so renaming it is
// saving its new name, and nothing connected to it moves. A connection
// made by typing an address, a quick connection, gets an ID of its own,
// and is forgotten once it is not connected and nothing is open on it.
// This computer is Local. A machine a served window reaches is that
// window's ID and the window's own key for it, joined by FarID.
//
// A name is looked up from the ID wherever one is shown.
package machines

import (
	"strings"
	"time"
)

// ID is what the program knows a machine by, and keys everything on it
// by: a saved server's or window's ID in the server list, a quick
// connection's own, Local, or the key of a machine beyond a served
// window. Never a name: a name is shown, and looked up from this, and
// one is turned into this only where a person or another window names a
// machine, by Registry.Find.
type ID string

// Local is this computer.
const Local ID = ""

// sep joins a window's ID and its key for a machine it reaches. No ID
// or key holds it.
const sep = "\x00"

// FarID is the ID of the machine window reaches, which window knows by
// key.
func FarID(window ID, key string) ID {
	return window + ID(sep+key)
}

// Far splits the ID of a machine beyond a window into the window and
// the window's key for it, and reports whether id is one.
func (id ID) Far() (window ID, key string, ok bool) {
	w, k, ok := strings.Cut(string(id), sep)
	return ID(w), k, ok
}

// Of reports whether id is machine, or a machine beyond it: a window's
// own, or one it reaches.
func (id ID) Of(machine ID) bool {
	window, _, far := id.Far()
	return id == machine || (far && window == machine)
}

// Info is a machine the window can show, by its ID, with its name.
type Info struct {
	ID   ID
	Name string
	// Quick says it was connected to by typing its address, Target, and
	// is not saved. Window says it is another kakel window.
	Quick  bool
	Window bool
	Target string
	// Since is when it connected, and RTT the round trip its last ping
	// took, for a machine connected to; zero otherwise.
	Since time.Time
	RTT   time.Duration
	// Silent says its last ping went unanswered.
	Silent bool
}
