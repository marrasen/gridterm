// Package conns is the list of everything the window has open, grouped
// by the machine it is open on.
//
// It is what the connections panel draws. It knows nothing about SSH:
// an entry is a kind, a label, a meter and two things it can be asked to
// do. That is what keeps the panel from having to learn what a tunnel
// is in order to draw a row for one.
package conns

import (
	"strconv"
	"sync"
	"time"

	"github.com/marrasen/gridterm/meter"
)

// Local is the group a connection on this machine belongs to. It is not
// a host name, so nothing can be saved under it.
const Local = ""

// Kind is what sort of thing is open.
type Kind uint8

const (
	// Terminal is a shell.
	Terminal Kind = iota

	// Command is one program, run and finished with.
	Command

	// Files is one side of a file browser.
	Files

	// Tunnel is a forwarded port.
	Tunnel

	// Server is the connection to a machine itself, which everything
	// else on that machine rides inside.
	Server
)

// String names a kind the way the panel shows it.
func (k Kind) String() string {
	switch k {
	case Terminal:
		return "Terminal"
	case Command:
		return "Command"
	case Files:
		return "Files"
	case Tunnel:
		return "Tunnel"
	case Server:
		return "Server"
	}
	return "Unknown"
}

// Entry is one thing open on one machine.
//
// Label and Note are read on every frame from the goroutine that draws,
// and may be written by it. Meter is written by whichever goroutine is
// moving bytes, which is why it is the only part that has to be safe on
// its own.
type Entry struct {
	// Host names the machine, or Local for this one. It is the name a
	// saved server is called, so the panel and the server list agree.
	Host string

	// Kind is what sort of thing this is.
	Kind Kind

	// Label says which one: a terminal's title, a tunnel's ports, the
	// directory a browser is showing.
	Label string

	// Note is anything else worth a word, such as how a command ended.
	Note string

	// Meter counts what has moved. A nil one means nothing ever moves,
	// which is what a connection that only sits there looks like.
	Meter *meter.Meter

	// Reveal puts this in front of the user: focuses its pane, or its
	// tab. A nil one cannot be revealed.
	Reveal func()

	// Close ends it. A nil one cannot be closed from the panel.
	Close func() error

	// id is what this entry is called for as long as it exists. The
	// registry gives it when the entry is added.
	id uint64
}

// ID names this entry for as long as it is open, for another window
// asking about it.
//
// Given by the registry rather than worked out from where the entry
// sits in the list: the list is built afresh every frame, and something
// closing moves everything after it up one. An empty string means the
// entry is not on a registry, so nothing can ask about it.
func (e *Entry) ID() string {
	if e.id == 0 {
		return ""
	}
	return strconv.FormatUint(e.id, 10)
}

// State is what the entry looks like at a given moment.
func (e *Entry) State(now time.Time) meter.State {
	if e.Meter == nil {
		return meter.Opened
	}
	return e.Meter.StateAt(now)
}

// Row is an entry and what it looks like right now.
type Row struct {
	*Entry
	State meter.State
}

// Group is everything open on one machine.
type Group struct {
	// Host names the machine, or Local for this one.
	Host string

	// Rows are the things open on it, in the order they were opened.
	Rows []Row
}

// Registry is everything the window has open.
//
// A Registry is safe to use from several goroutines, though in practice
// only the one that draws touches it: a connection is opened and closed
// from there.
type Registry struct {
	mu sync.Mutex

	// entries are in the order they were added, which is the order the
	// panel shows them in. A machine's place in the list is where its
	// first connection went.
	entries []*Entry

	// given is the last id handed out. It only goes up, so an id names
	// one thing for the life of the window even after that thing has
	// closed and been dismissed.
	given uint64
}

// New returns an empty registry.
func New() *Registry { return &Registry{} }

// Add puts a connection on the list.
func (r *Registry) Add(e *Entry) {
	if e == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, have := range r.entries {
		if have == e {
			return
		}
	}
	// Named once. An entry dropped and added again -- which is what a
	// connection that died and was dismissed looks like -- keeps the
	// name another window already knows it by.
	if e.id == 0 {
		r.given++
		e.id = r.given
	}
	r.entries = append(r.entries, e)
}

// Drop takes a connection off the list, for one the user has dismissed.
func (r *Registry) Drop(e *Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, have := range r.entries {
		if have == e {
			// The vacated slot at the end is cleared rather than left
			// holding what used to be last: an Entry keeps a whole
			// terminal alive through its Reveal and Close.
			last := len(r.entries) - 1
			copy(r.entries[i:], r.entries[i+1:])
			r.entries[last] = nil
			r.entries = r.entries[:last]
			return
		}
	}
}

// DropFinished takes off every connection that has closed, for the
// "clear finished" command.
func (r *Registry) DropFinished(now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.entries[:0]
	dropped := 0
	for _, e := range r.entries {
		if e.State(now) == meter.Closed {
			dropped++
			continue
		}
		kept = append(kept, e)
	}
	// The tail holds entries that are being thrown away; leaving them
	// there would keep whatever they hold alive.
	for i := len(kept); i < len(r.entries); i++ {
		r.entries[i] = nil
	}
	r.entries = kept
	return dropped
}

// Len returns how many connections are on the list.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// Groups returns everything open, by machine.
//
// The entries it hands back are not the registry's to guard: their
// labels are written by whatever is drawing. Reading one from another
// goroutine is the caller's problem, not this lock's.
//
// This machine comes first, whether or not anything is open on it, so
// the panel always has somewhere to show a local shell. Every other
// machine follows in the order its first connection was opened, so the
// list does not reorder itself while the user is looking at it.
func (r *Registry) Groups(now time.Time) []Group {
	r.mu.Lock()
	defer r.mu.Unlock()

	groups := []Group{{Host: Local}}
	at := map[string]int{Local: 0}
	for _, e := range r.entries {
		i, ok := at[e.Host]
		if !ok {
			i = len(groups)
			at[e.Host] = i
			groups = append(groups, Group{Host: e.Host})
		}
		groups[i].Rows = append(groups[i].Rows, Row{Entry: e, State: e.State(now)})
	}
	return groups
}
