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

// Machine is a machine the window can show, by its ID, with its name.
type Machine struct {
	ID, Name string
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
func (a *app) newQuick(target string, window bool) string {
	for id, q := range a.quick {
		if q.target == target && q.window == window {
			return id
		}
	}
	id := remote.QuickID()
	a.quick[id] = quickConn{target: target, window: window}
	return id
}

// isQuick reports whether id is a quick connection's.
func (a *app) isQuick(id string) bool {
	_, ok := a.quick[id]
	return ok
}

// nameOf is what machine id is called: a saved server's name now, a
// quick connection's address, and "this computer" for "".
func (a *app) nameOf(id string) string {
	if window, host, far := strings.Cut(id, farSep); far {
		return a.farName(window, host) + " through " + a.nameOf(window)
	}
	switch {
	case id == "":
		return "this computer"
	case a.quick[id].target != "":
		return a.quick[id].target
	}
	if a.book != nil {
		if name, ok := a.book.NameOf(id); ok {
			return name
		}
	}
	// A saved server removed since, still named in something open.
	if name := a.goneNames[id]; name != "" {
		return name
	}
	return id
}

// farName is what window calls the machine it reaches by key.
func (a *app) farName(window, key string) string {
	if name := a.farNames[window+farSep+key]; name != "" {
		return name
	}
	return key
}

// idOf is the machine a name says, as one typed, or asked for by an
// agent or another window: this computer for "" or its name, a saved
// server by its name, a quick connection by its address, and one known
// by its ID already. It reports false for none.
func (a *app) idOf(name string) (string, bool) {
	switch {
	case name == "" || strings.EqualFold(name, "this computer"):
		return "", true
	case a.isQuick(name), a.conns[name] != nil, a.windows[name] != nil:
		return name, true
	}
	if a.book != nil {
		if h, ok := a.book.LookupID(name); ok {
			return h.ID, true
		}
		if h, ok := a.book.Lookup(name); ok {
			return h.ID, true
		}
	}
	for id, q := range a.quick {
		if q.target == name {
			return id, true
		}
	}
	return "", false
}

// savedHost is the saved server or window with ID id.
func (a *app) savedHost(id string) (remote.Host, bool) {
	if a.book == nil {
		return remote.Host{}, false
	}
	return a.book.LookupID(id)
}

// isWindow reports whether machine id is a kakel window: one connected
// to, a saved one, or a quick one.
func (a *app) isWindow(id string) bool {
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
			out = append(out, Machine{ID: h.ID, Name: h.Name, Window: h.Window})
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
	slices.SortFunc(out, func(x, y Machine) int { return strings.Compare(x.ID, y.ID) })
	return out
}

// used reports whether anything is kept on machine id: a connection, a
// dial, a pane, a tunnel, a job or a dropped row.
func (a *app) used(id string) bool {
	if a.conns[id] != nil || a.windows[id] != nil || a.dialing[id] || a.dropped[id] {
		return true
	}
	on := func(m string) bool { return m == id || strings.HasPrefix(m, id+farSep) }
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
		delete(a.accounts, id)
		a.st.Accounts = slices.DeleteFunc(a.st.Accounts, func(n string) bool { return n == id })
		a.forgetFar(id)
	}
	// Names of machines beyond windows go once their window is not
	// connected and nothing open is on them.
	for key := range a.farNames {
		window, _, _ := strings.Cut(key, farSep)
		if a.windows[window] == nil && !slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.On != "" && p.Machine+farSep+p.On == key }) {
			delete(a.farNames, key)
		}
	}
	// Names kept for removed servers go once nothing names them, and
	// their logs with them.
	for id := range a.goneNames {
		if !a.used(id) {
			delete(a.goneNames, id)
			delete(a.accounts, id)
			a.st.Accounts = slices.DeleteFunc(a.st.Accounts, func(n string) bool { return n == id })
		}
	}
}
