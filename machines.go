package main

import (
	"slices"
	"strings"

	"github.com/marrasen/kakel/remote"
)

// Machines, as the program keeps them: by an ID that does not change,
// never by a name. A saved server or window goes by its ID in the
// server list, so renaming it is saving its new name, and nothing
// connected to it moves. A connection made by typing an address, a
// quick connection, gets an ID of its own, noted as quick, and is
// forgotten once it is not connected and nothing is open on it. This
// computer is "". A machine a served window reaches is that window's ID
// and the window's own name for it, joined by farSep.
//
// A name is looked up from the ID wherever one is shown.

// MachineID is what the program knows a machine by, and keys
// everything on it by: a saved server's or window's ID in the server
// list, a quick connection's own, Local, or the key of a machine beyond
// a served window. Never a name: a name is shown, and looked up from
// this, and one is turned into this only where a person or another
// window names a machine, by idOf.
type MachineID string

// Local is this computer.
const Local MachineID = ""

// farID is the key of the machine window reaches, which window knows by
// key.
func farID(window MachineID, key string) MachineID {
	return window + MachineID(farSep+key)
}

// Far splits the key of a machine beyond a window into the window and
// the window's key for it, and reports whether m is one.
func (m MachineID) Far() (window MachineID, key string, ok bool) {
	w, k, ok := strings.Cut(string(m), farSep)
	return MachineID(w), k, ok
}

// Of reports whether m is machine, or a machine beyond it: a window's
// own, or one it reaches.
func (m MachineID) Of(machine MachineID) bool {
	window, _, far := m.Far()
	return m == machine || (far && window == machine)
}

// Machine is a machine the window can show, by its ID, with its name.
type Machine struct {
	ID   MachineID
	Name string
	// Quick says it was connected to by typing its address, Target, and
	// is not saved. Window says it is another kakel window.
	Quick  bool
	Window bool
	Target string
}

// quickConn is a connection made without a saved server: the address
// typed, and whether it is a kakel window.
type quickConn struct {
	target string
	window bool
}

// newQuick notes a quick connection to target, and returns its ID: the
// one it has already while one to that address is kept, for a
// reconnect, and a new one otherwise.
func (a *app) newQuick(target string, window bool) MachineID {
	for id, q := range a.quick {
		if q.target == target && q.window == window {
			return id
		}
	}
	id := MachineID(remote.QuickID())
	a.quick[id] = quickConn{target: target, window: window}
	return id
}

// isQuick reports whether id is a quick connection's.
func (a *app) isQuick(id MachineID) bool {
	_, ok := a.quick[id]
	return ok
}

// nameOf is what machine id is called: a saved server's name now, a
// quick connection's address, and "this computer" for Local.
func (a *app) nameOf(id MachineID) string {
	if window, key, far := id.Far(); far {
		return a.farName(window, key) + " through " + a.nameOf(window)
	}
	switch {
	case id == Local:
		return "this computer"
	case a.quick[id].target != "":
		return a.quick[id].target
	}
	if a.book != nil {
		if name, ok := a.book.NameOf(string(id)); ok {
			return name
		}
	}
	// A saved server removed since, still named in something open.
	if name := a.goneNames[id]; name != "" {
		return name
	}
	return string(id)
}

// farName is what window calls the machine it reaches by key.
func (a *app) farName(window MachineID, key string) string {
	if name := a.farNames[farID(window, key)]; name != "" {
		return name
	}
	return key
}

// idOf is the machine a name says, as one typed, or asked for by an
// agent or another window: this computer for "" or its name, a saved
// server by its name, a quick connection by its address, and one known
// by its ID already. It reports false for none. Names become IDs here,
// and nowhere else.
func (a *app) idOf(name string) (MachineID, bool) {
	id := MachineID(name)
	switch {
	case name == "" || strings.EqualFold(name, "this computer"):
		return Local, true
	case a.isQuick(id), a.conns[id] != nil, a.windows[id] != nil:
		return id, true
	}
	if a.book != nil {
		if h, ok := a.book.LookupID(name); ok {
			return MachineID(h.ID), true
		}
		if h, ok := a.book.Lookup(name); ok {
			return MachineID(h.ID), true
		}
	}
	for id, q := range a.quick {
		if q.target == name {
			return id, true
		}
	}
	return Local, false
}

// savedHost is the saved server or window with ID id.
func (a *app) savedHost(id MachineID) (remote.Host, bool) {
	if a.book == nil || id == Local {
		return remote.Host{}, false
	}
	return a.book.LookupID(string(id))
}

// isWindow reports whether machine id is a kakel window: one connected
// to, a saved one, or a quick one.
func (a *app) isWindow(id MachineID) bool {
	if a.windows[id] != nil || a.quick[id].window {
		return true
	}
	h, ok := a.savedHost(id)
	return ok && h.Window
}

// machines lists what the window can show a name for: the saved
// servers and windows, and the quick connections.
func (a *app) machines() []Machine {
	var out []Machine
	if a.book != nil {
		for _, h := range a.book.Hosts() {
			out = append(out, Machine{ID: MachineID(h.ID), Name: h.Name, Window: h.Window})
		}
	}
	for id, q := range a.quick {
		out = append(out, Machine{ID: id, Name: q.target, Quick: true, Window: q.window, Target: q.target})
	}
	// The machines windows reach, by the window's key for each, named
	// as the window calls them.
	for key, name := range a.farNames {
		out = append(out, Machine{ID: key, Name: name})
	}
	for id, name := range a.goneNames {
		if !slices.ContainsFunc(out, func(m Machine) bool { return m.ID == id }) {
			out = append(out, Machine{ID: id, Name: name})
		}
	}
	slices.SortFunc(out, func(x, y Machine) int { return strings.Compare(string(x.ID), string(y.ID)) })
	return out
}

// used reports whether anything is kept on machine id: a connection, a
// dial, a pane, a tunnel, a job or a dropped row, on it or beyond it.
func (a *app) used(id MachineID) bool {
	if a.conns[id] != nil || a.windows[id] != nil || a.dialing[id] || a.dropped[id] {
		return true
	}
	on := func(m MachineID) bool { return m.Of(id) }
	if c := a.clip; c != nil && on(c.machine) {
		return true
	}
	if slices.ContainsFunc(a.running, func(r *running) bool { return on(r.from) || on(r.to) }) {
		return true
	}
	return slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return on(p.Machine) }) ||
		slices.ContainsFunc(a.st.Tunnels, func(t Tunnel) bool { return on(t.Machine) }) ||
		slices.ContainsFunc(a.st.Jobs, func(j Job) bool { return on(j.Machine) })
}

// forgetQuick forgets the quick connections nothing is kept on any
// more: not connected, and nothing open on them. Their logs go too.
func (a *app) forgetQuick() {
	for id := range a.quick {
		if a.used(id) {
			continue
		}
		delete(a.quick, id)
		a.forgetAccount(id)
		a.forgetFar(id)
	}
	// Names of machines beyond windows go once their window is not
	// connected and nothing open is on them.
	for key := range a.farNames {
		window, _, _ := key.Far()
		if a.windows[window] == nil && !slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.On != "" && farID(p.Machine, p.On) == key }) {
			delete(a.farNames, key)
		}
	}
	// Names kept for removed servers go once nothing names them, and
	// their logs with them.
	for id := range a.goneNames {
		if !a.used(id) {
			delete(a.goneNames, id)
			a.forgetAccount(id)
		}
	}
}

// forgetAccount lets go of machine id's connection log.
func (a *app) forgetAccount(id MachineID) {
	delete(a.accounts, id)
	a.st.Accounts = slices.DeleteFunc(a.st.Accounts, func(n MachineID) bool { return n == id })
}
