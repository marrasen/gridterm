package machines

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/marrasen/kakel/logs"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/vfs"
)

// Machine is everything kept on one machine's connection. The zero
// Machine is one with nothing kept: not connected, not being connected
// to, and with no log.
type Machine struct {
	// Conn is its SSH connection, and SavedID the saved server it was
	// reached by, "" for one typed. Reached is the address the dial
	// reached, to tell whether a pane's machine is the same one when it
	// connects again.
	Conn    *remote.Conn
	SavedID ID
	Reached string

	// Window is its connection when it is another kakel window.
	Window *Window

	// Files are its files, once opened, for all its file panes.
	Files vfs.FS

	// Log is its connection log, from the first time it was connected
	// to.
	Log *logs.Lines

	// Dialing is set while a connection to it is on its way, and gives
	// that connection up. Waiters hear how it went.
	Dialing context.CancelFunc
	Waiters []func(error)

	// Dropped says its connection went by itself, and it stays on the
	// sidebar until cleared. LetGo says it is being let go of on
	// purpose, so its going is not a drop. Lost withdraws the question
	// offering to connect to a dropped window again.
	Dropped bool
	LetGo   bool
	Lost    context.CancelFunc
}

// empty reports whether nothing is kept on the machine any more.
func (m *Machine) empty() bool {
	return m.Conn == nil && m.Window == nil && m.Files == nil && m.Log == nil && m.Dialing == nil &&
		len(m.Waiters) == 0 && !m.Dropped && !m.LetGo && m.Lost == nil && m.Reached == ""
}

// Window is a connection to another kakel window.
type Window struct {
	Serve         *serve.Window
	Addr, KeyFile string
	// Bound is the pane here showing each thing it has open, by its ID
	// there.
	Bound map[string]string
	// Seen is its list as last told, to publish only a change, and
	// Leaving says the user let go of it.
	Seen    []serve.Open
	Leaving bool
}

// quick is a connection made without a saved server: the address typed,
// and whether it is a kakel window.
type quick struct {
	target string
	window bool
}

// Registry is every machine kakel knows: the saved ones, by the server
// list, and the rest by what is kept on them. It belongs to the
// program's goroutine.
type Registry struct {
	book func() *remote.Book
	all  map[ID]*Machine
	// quick are the quick connections, gone the names of saved servers
	// removed while something still named them, and far what windows
	// call the machines beyond them.
	quick map[ID]quick
	gone  map[ID]string
	far   map[ID]string
	// hops are the connections to jump hosts made only to go through,
	// users how many connections go through each connection, and routes
	// the route each connection was reached by.
	hops   map[ID]*remote.Conn
	users  map[*remote.Conn]int
	routes map[*remote.Conn]string
}

// New is a registry that finds the saved machines in what book gives,
// which may be nil.
func New(book func() *remote.Book) *Registry {
	return &Registry{
		book: book, all: map[ID]*Machine{},
		quick: map[ID]quick{}, gone: map[ID]string{}, far: map[ID]string{},
		hops: map[ID]*remote.Conn{}, users: map[*remote.Conn]int{}, routes: map[*remote.Conn]string{},
	}
}

// Get is what is kept on id, the zero Machine when nothing is.
func (r *Registry) Get(id ID) Machine {
	if m := r.all[id]; m != nil {
		return *m
	}
	return Machine{}
}

// At is the record of id, to change, made on first use. One left with
// nothing kept goes at the next Forget.
func (r *Registry) At(id ID) *Machine {
	m := r.all[id]
	if m == nil {
		m = &Machine{}
		r.all[id] = m
	}
	return m
}

// IDs are the machines for which keep reports true, in order.
func (r *Registry) IDs(keep func(Machine) bool) []ID {
	var out []ID
	for _, id := range slices.Sorted(maps.Keys(r.all)) {
		if keep(*r.all[id]) {
			out = append(out, id)
		}
	}
	return out
}

// Each runs f on every record, in order. f may change them, and may
// add or remove records.
func (r *Registry) Each(f func(ID, *Machine)) {
	for _, id := range slices.Sorted(maps.Keys(r.all)) {
		if m := r.all[id]; m != nil {
			f(id, m)
		}
	}
}

// Name is what machine id is called: a saved server's name now, a quick
// connection's address, and "this computer" for Local.
func (r *Registry) Name(id ID) string {
	if window, key, far := id.Far(); far {
		return r.FarName(window, key) + " through " + r.Name(window)
	}
	switch {
	case id == Local:
		return "this computer"
	case r.quick[id].target != "":
		return r.quick[id].target
	}
	if b := r.book(); b != nil {
		if name, ok := b.NameOf(string(id)); ok {
			return name
		}
	}
	// A saved server removed since, still named in something open.
	if name := r.gone[id]; name != "" {
		return name
	}
	return string(id)
}

// FarName is what window calls the machine it reaches by key.
func (r *Registry) FarName(window ID, key string) string {
	if name := r.far[FarID(window, key)]; name != "" {
		return name
	}
	return key
}

// NameFar notes what window calls the machine it reaches by key.
func (r *Registry) NameFar(window ID, key, name string) {
	r.far[FarID(window, key)] = name
}

// Find is the machine a name says, as one typed, or asked for by an
// agent or another window: this computer for "" or its name, a saved
// server by its name, a quick connection by its address, and one known
// by its ID already. It reports false for none. Names become IDs here,
// and nowhere else.
func (r *Registry) Find(name string) (ID, bool) {
	id := ID(name)
	switch {
	case name == "" || strings.EqualFold(name, "this computer"):
		return Local, true
	case r.IsQuick(id), r.Get(id).Conn != nil, r.Get(id).Window != nil:
		return id, true
	}
	if b := r.book(); b != nil {
		if h, ok := b.LookupID(name); ok {
			return ID(h.ID), true
		}
		if h, ok := b.Lookup(name); ok {
			return ID(h.ID), true
		}
	}
	for id, q := range r.quick {
		if q.target == name {
			return id, true
		}
	}
	return Local, false
}

// NewQuick notes a quick connection to target, and returns its ID: the
// one it has already while one to that address is kept, for a
// reconnect, and a new one otherwise.
func (r *Registry) NewQuick(target string, window bool) ID {
	for id, q := range r.quick {
		if q.target == target && q.window == window {
			return id
		}
	}
	id := ID(remote.QuickID())
	r.quick[id] = quick{target: target, window: window}
	return id
}

// KeepQuick notes id as a quick connection to target again, having been
// forgotten while a question about it was up.
func (r *Registry) KeepQuick(id ID, target string, window bool) {
	r.quick[id] = quick{target: target, window: window}
}

// IsQuick reports whether id is a quick connection's.
func (r *Registry) IsQuick(id ID) bool {
	_, ok := r.quick[id]
	return ok
}

// Quick is the address quick connection id was made to, and whether it
// is a kakel window. It reports false for one that is not quick.
func (r *Registry) Quick(id ID) (target string, window, ok bool) {
	q, ok := r.quick[id]
	return q.target, q.window, ok
}

// Saved is the saved server or window with ID id.
func (r *Registry) Saved(id ID) (remote.Host, bool) {
	b := r.book()
	if b == nil || id == Local {
		return remote.Host{}, false
	}
	return b.LookupID(string(id))
}

// IsWindow reports whether machine id is a kakel window: one connected
// to, a saved one, or a quick one.
func (r *Registry) IsWindow(id ID) bool {
	if r.Get(id).Window != nil || r.quick[id].window {
		return true
	}
	h, ok := r.Saved(id)
	return ok && h.Window
}

// Removed notes that saved server id, called name, was removed from the
// list, to name it by while something is still open on it.
func (r *Registry) Removed(id ID, name string) {
	r.gone[id] = name
}

// Infos lists what the window can show a name for: the saved servers
// and windows, the quick connections, the machines windows reach and
// the removed ones still named.
func (r *Registry) Infos() []Info {
	var out []Info
	if b := r.book(); b != nil {
		for _, h := range b.Hosts() {
			out = append(out, Info{ID: ID(h.ID), Name: h.Name, Window: h.Window})
		}
	}
	for id, q := range r.quick {
		out = append(out, Info{ID: id, Name: q.target, Quick: true, Window: q.window, Target: q.target})
	}
	for id, name := range r.far {
		out = append(out, Info{ID: id, Name: name})
	}
	for id, name := range r.gone {
		if !slices.ContainsFunc(out, func(m Info) bool { return m.ID == id }) {
			out = append(out, Info{ID: id, Name: name})
		}
	}
	slices.SortFunc(out, func(x, y Info) int { return strings.Compare(string(x.ID), string(y.ID)) })
	return out
}

// Forget forgets what nothing is kept on any more, by used, which says
// whether something open is on a machine or beyond it: quick
// connections, removed servers' names, the names of machines beyond
// windows not connected, and records left empty. It returns the
// machines forgotten whose logs went with them.
func (r *Registry) Forget(used func(ID) bool) []ID {
	var gone []ID
	busy := func(id ID) bool {
		m := r.Get(id)
		return m.Conn != nil || m.Window != nil || m.Dialing != nil || m.Dropped || used(id)
	}
	forget := func(id ID) {
		if m := r.all[id]; m != nil {
			m.Log = nil
		}
		gone = append(gone, id)
	}
	for id := range r.quick {
		if !busy(id) {
			delete(r.quick, id)
			forget(id)
		}
	}
	for id := range r.far {
		window, _, _ := id.Far()
		if r.Get(window).Window == nil && !used(id) {
			delete(r.far, id)
		}
	}
	for id := range r.gone {
		if !busy(id) {
			delete(r.gone, id)
			forget(id)
		}
	}
	for id, m := range r.all {
		if m.empty() {
			delete(r.all, id)
		}
	}
	slices.Sort(gone)
	return gone
}

// Connected are the servers with an SSH connection, in order.
func (r *Registry) Connected() []ID { return r.IDs(func(m Machine) bool { return m.Conn != nil }) }

// Windows are the kakel windows connected to, in order.
func (r *Registry) Windows() []ID { return r.IDs(func(m Machine) bool { return m.Window != nil }) }

// Dialing are the machines a connection is on its way to, in order.
func (r *Registry) Dialing() []ID { return r.IDs(func(m Machine) bool { return m.Dialing != nil }) }

// Dropped are the machines whose connection went by itself, in order.
func (r *Registry) Dropped() []ID { return r.IDs(func(m Machine) bool { return m.Dropped }) }
