package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// knownWindowsFile is where the keys of gridterm windows this one has
// reached are recorded.
//
// Its own file rather than the user's ~/.ssh/known_hosts. A gridterm
// serving is not a machine they reach with ssh, and writing it there
// would put a line in a file other tools read and the user maintains
// for something else.
const knownWindowsFile = "known_windows"

// taken is another machine's gridterm, taken over from this one.
//
// It is not a *machine: a machine is an SSH host this window opens
// shells and tunnels and file transfers on, while this is another
// gridterm that opens those things for itself. The two look alike in
// the panel and are nothing alike underneath.
type taken struct {
	// name is what the window is called here: the name the server list
	// gives it, or the address when it is not saved. It is the key
	// everything else uses, the way a machine's name is.
	name string

	// addr is where it serves, which is what reaching it again needs.
	addr string

	win *serve.Window

	// entry is the panel row for the window itself, so one with nothing
	// open on it is still visible and still closeable.
	entry *conns.Entry

	// log is the account of how the window was taken over, kept after
	// the pane folded it away so "How it was reached" can show it.
	log *connLog
}

// windows are the gridterms on other machines this one has taken over,
// and the panes drawn from them.
//
// One connection per address. The key is the name the server list gives
// that address, or the address itself when the list saves nothing
// there. It is worked out from the list again whenever the list
// changes. So a first save, a rename and a forget each move the key,
// and none of them touches the connection.
//
// Only the goroutine that draws touches any of it.
type windows struct {
	// book is where the names come from, so a change to it moves keys.
	book *remote.Book

	// held is every window taken over, by the name above.
	held map[string]*taken

	// from says which window a pane is drawn from, so letting go of one
	// takes its panes with it. seen says what each of those panes is
	// watching over there, so choosing the same thing again brings the
	// pane forward rather than opening a second one onto one shell. Its
	// keys name the window itself, so a re-key leaves them alone.
	from map[*term.Terminal]*taken
	seen map[*term.Terminal]remoteKey

	// knownAt is where the keys of the windows reached are recorded,
	// empty in the program and set by a test to a file of its own: the
	// real one belongs to whoever is running gridterm.
	knownAt string

	// patience is how long a window being taken over has to get through
	// the handshake. Zero asks the serve package for its own; a test
	// asks for less so it does not wait out the real one.
	patience time.Duration
}

// newWindows builds the record of windows taken over, holding none.
func newWindows(book *remote.Book) *windows {
	return &windows{
		book: book,
		held: make(map[string]*taken),
		from: make(map[*term.Terminal]*taken),
		seen: make(map[*term.Terminal]remoteKey),
	}
}

// named is the window held under a name, or nil.
//
// Exactly, not ignoring case: the key is the server list's own spelling
// of the name, and a rename that changes only capitals is a new key.
func (w *windows) named(name string) *taken { return w.held[name] }

// at is the window already taken over at an address, or nil. Case is
// folded, because an address can be written either way.
func (w *windows) at(addr string) *taken {
	for _, t := range w.held {
		if strings.EqualFold(t.addr, addr) {
			return t
		}
	}
	return nil
}

// holds reports whether a window is still held, by identity: the name
// it is held under moves with the server list and the window does not.
func (w *windows) holds(t *taken) bool {
	for _, have := range w.held {
		if have == t {
			return true
		}
	}
	return false
}

// count is how many windows are held, for a test.
func (w *windows) count() int { return len(w.held) }

// names are the names the windows are held under, in order, for a test
// saying what it found instead.
func (w *windows) names() []string { return slices.Sorted(maps.Keys(w.held)) }

// add records a window taken over, under the name it carries.
func (w *windows) add(t *taken) { w.held[t.name] = t }

// drop lets go of a window, by identity: the name it is held under is
// not always the name the caller asked about.
func (w *windows) drop(t *taken) {
	for name, have := range w.held {
		if have == t {
			delete(w.held, name)
			return
		}
	}
}

// drain takes every window out and hands them back, for a window that
// is shutting down.
func (w *windows) drain() []*taken {
	out := make([]*taken, 0, len(w.held))
	for name, t := range w.held {
		delete(w.held, name)
		out = append(out, t)
	}
	return out
}

// nameFor is what a window serving at an address is called here.
//
// The name the server list gives it, so everything this window keeps
// about it goes under one name. An address nothing saved is its own
// name.
func (w *windows) nameFor(addr string) string {
	if h, ok := w.savedAt(addr); ok {
		return h.Name
	}
	return addr
}

// savedAt is the saved gridterm window serving at an address.
//
// By address rather than by name, because a connection made from a typed
// target knows nothing about the list. about answers about a name and
// this does not, so the two do not overlap.
func (w *windows) savedAt(addr string) (remote.Host, bool) {
	for _, h := range w.book.Hosts() {
		if h.Window && strings.EqualFold(h.ServeAddr(), addr) {
			return h, true
		}
	}
	return remote.Host{}, false
}

// windowRename is a window whose name the server list has changed, and
// what it was called before.
type windowRename struct {
	window *taken
	was    string
}

// rekey gives every window taken over the name the server list now
// gives its address, and hands back the ones that moved.
//
// A first save, a rename and a forget all land here. Every window ends
// under a key of its own: the name the list gives it, or its address,
// or the key it already had. A window that is not moving keeps its key,
// so nothing else can take it.
//
// Nothing moves at all when a window is left with nowhere free, which a
// list naming one window after another window's address can do. The
// error says so, and the caller reports it.
func (w *windows) rekey() ([]windowRename, error) {
	if len(w.held) == 0 {
		return nil, nil
	}
	saved := w.savedNames()
	// In name order, so which of two windows keeps a contested name is
	// the same answer every time.
	order := slices.Sorted(maps.Keys(w.held))

	// What each window would be called, and which keys are not moving.
	// Worked out before anything moves, so that two windows can trade
	// names.
	want := make([]string, len(order))
	staying := make(map[string]bool, len(order))
	for i, was := range order {
		now := saved[strings.ToLower(w.held[was].addr)]
		if now == "" {
			now = w.held[was].addr
		}
		want[i] = now
		staying[was] = now == was
	}

	// Then the keys, still without writing any of them: a list with no
	// answer leaves every window where it was.
	keys := make([]string, len(order))
	spoken := make(map[string]bool, len(order))
	for i, was := range order {
		t := w.held[was]
		// Free for this window: nothing has taken it, and it is either
		// the key this one already has or one no other window is
		// staying under.
		free := func(key string) bool {
			return !spoken[key] && (key == was || !staying[key])
		}
		switch {
		case free(want[i]):
			keys[i] = want[i]
		case free(t.addr):
			keys[i] = t.addr
		case !spoken[was]:
			keys[i] = was
		default:
			return nil, fmt.Errorf(
				"the server list leaves %s, taken over at %s, no name of its own", was, t.addr)
		}
		spoken[keys[i]] = true
	}

	held := make(map[string]*taken, len(order))
	var moved []windowRename
	for i, was := range order {
		t := w.held[was]
		held[keys[i]] = t
		if keys[i] != was {
			t.name = keys[i]
			moved = append(moved, windowRename{window: t, was: was})
		}
	}
	w.held = held
	return moved, nil
}

// savedNames maps each address the server list saves a window at,
// folded to lower case, to the name the list gives it.
//
// The list holds one name per address, so the map does too.
func (w *windows) savedNames() map[string]string {
	out := map[string]string{}
	for _, h := range w.book.Hosts() {
		if h.Window {
			out[strings.ToLower(h.ServeAddr())] = h.Name
		}
	}
	return out
}

// draws records that a pane is drawn from a window.
func (w *windows) draws(pane *term.Terminal, t *taken) { w.from[pane] = t }

// drawn is how many panes are drawn from windows taken over, for a
// test.
func (w *windows) drawn() int { return len(w.from) }

// drawnFrom are the panes drawn from one window.
func (w *windows) drawnFrom(t *taken) []*term.Terminal {
	var out []*term.Terminal
	for pane, on := range w.from {
		if on == t {
			out = append(out, pane)
		}
	}
	return out
}

// drawsFromAWindow reports whether a pane is drawn from a window taken
// over.
func (w *windows) drawsFromAWindow(pane *term.Terminal) bool {
	_, ok := w.from[pane]
	return ok
}

// forget takes a pane off the record, for one that has been closed.
func (w *windows) forget(pane *term.Terminal) {
	delete(w.from, pane)
	delete(w.seen, pane)
}

// watch records what a pane is watching on the window it is drawn from.
func (w *windows) watch(pane *term.Terminal, what remoteKey) { w.seen[pane] = what }

// watching is what a pane is watching over there, and whether it is
// watching anything at all.
func (w *windows) watching(pane *term.Terminal) (remoteKey, bool) {
	what, ok := w.seen[pane]
	return what, ok
}

// watcher is the pane already watching something on a window taken
// over, or nil when nothing is.
func (w *windows) watcher(what remoteKey) *term.Terminal {
	for pane, have := range w.seen {
		if have == what {
			return pane
		}
	}
	return nil
}

// openTakeOver asks which window to take over.
func (a *app) openTakeOver() error {
	f := a.newForm("Take over a window")
	// Broken into short lines by hand. A dialog is as wide as its
	// longest line and stops there, so a sentence written as one line
	// is a sentence with its end cut off.
	f.Lines = []string{
		"Work in a gridterm running on another machine.",
		"",
		"That window has to be serving, and has to have",
		"this machine's public key in its authorized_keys.",
	}
	addr := f.AddField("Machine", a.newField(
		fmt.Sprintf("host[:port], port %d unless given", servePort), 0))
	key := f.AddField("Key file", a.newField("optional, or the agent's keys", 0))

	f.AddButton(ui.Button{Title: "Take over", Do: func() error {
		// Through workOnWindow, so typing the address of a window this
		// one already holds gives a terminal on it rather than the
		// complaint that it has been taken over.
		return a.workOnWindow(strings.TrimSpace(addr.Text()), strings.TrimSpace(key.Text()), nil)
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// takeOver reaches another window and puts it on the panel.
//
// The work happens on a goroutine of its own: reaching a machine can
// stop to ask for a passphrase, or to ask whether its key is the one
// expected, and neither can be answered by the goroutine that draws.
func (a *app) takeOver(addr, keyFile string, at *spot) error {
	if addr == "" {
		return errors.New("no machine to take over")
	}
	addr = serveAddr(addr)
	// Already held, whatever it is called. By address rather than by
	// name: saving, renaming or forgetting a window changes its name
	// and changes nothing about the connection, and a guard that went
	// by name would let a second one be made to the same far window.
	if held := a.windows.at(addr); held != nil {
		return fmt.Errorf("this window has already taken over %s", held.name)
	}
	// What this window is called here. A saved one goes under the name
	// the user gave it, so the sidebar has one heading for it rather
	// than one for the name and another for the address.
	name := a.windows.nameFor(addr)
	// Already on its way. Asked about rather than refused: waiting for
	// it is usually what the user wants.
	if d := a.about(name).dialling; d != nil {
		a.askAboutTheOneOnItsWay(d, name, func() { a.workOnWindowOrSay(addr, keyFile, at) })
		return nil
	}

	ctx, cancel := context.WithCancel(a.ctx)

	// A pane rather than a row that only says "opening". It is somewhere
	// to watch from: every step as it is tried, and whatever the far end
	// says, in full and there to copy. The same pane carries the shell
	// when there is one, and the account folds away to one line that says
	// where to read the rest of it.
	//
	// Letting go of the address is done here rather than by the closure
	// that finishes the dial: a dial that has not come back yet still
	// has to stop holding it, or nothing can try again.
	held := &dialling{cancel: cancel, names: []string{name}}
	// Before the pane opens, so a name a machine is already connected
	// under is refused with no pane left behind saying otherwise.
	if err := a.machines.holdNames(held); err != nil {
		cancel()
		return err
	}
	log := newConnLog(func() { a.pump.post(func() { a.machines.giveUp(held) }) })
	held.log = log
	pane, err := a.openSessionTab(log, name, conns.Terminal, "connecting", at)
	if err != nil {
		cancel()
		a.machines.release(held)
		return err
	}
	a.machines.dialStarted()
	log.Say("taking over " + addr)

	ask := &askUser{app: a, log: log, stop: func() { a.machines.giveUp(held) }}
	// Read here rather than in the goroutine below: what the window
	// holds belongs to the goroutine that draws.
	knownAt, patience := a.windows.knownAt, a.windows.patience
	go func() {
		win, err := remote.ReachWindow(ctx, remote.Reach{
			Addr: addr, KeyFile: keyFile, Ring: a.keys, Ask: ask,
			Known:    func() (string, error) { return knownWindows(knownAt) },
			Patience: patience,
			Saying:   log.Say,
		})
		a.pump.post(func() {
			a.machines.dialEnded()
			// Read before the context is let go of on the next line,
			// which would otherwise make every window look like one the
			// user gave up on.
			gaveUp := ctx.Err()
			cancel()
			if err != nil {
				a.machines.settle(held, false)
				if gaveUp != nil {
					a.endedAs(pane, "given up on")
					log.GaveUp()
					return
				}
				a.endedAs(pane, "not taken over")
				log.Failed(err)
				return
			}
			if gaveUp != nil {
				// Given up on while the last of the handshake was
				// finishing. Nothing else knows about it, and a failure
				// to hang up is the user's to see: it is a socket to a
				// machine that thinks somebody is working in it.
				a.machines.settle(held, false)
				a.endedAs(pane, "given up on")
				log.GaveUp()
				if err := win.Close(); err != nil {
					a.reportError("Could not let go of "+addr, err)
				}
				return
			}
			a.becomeWindowPane(a.holdWindow(name, addr, win), pane, log)
			a.machines.settle(held, true)
		})
	}()
	return nil
}

// workOnWindow opens a terminal on the window serving at an address,
// taking it over first when this one has not already.
//
// The one way in for every request to work on a window, however it was
// named: taking over one already taken over would only report that it
// has been, which is not what the user asked for.
func (a *app) workOnWindow(addr, keyFile string, at *spot) error {
	addr = serveAddr(addr)
	// By address rather than by name: a window taken over before it was
	// saved is held under its address, and a name that was never asked
	// about is held under nothing.
	if held := a.windows.at(addr); held != nil {
		return a.openOnWindow(held.name, at)
	}
	return a.takeOver(addr, keyFile, at)
}

// workOnWindowOrSay is workOnWindow for a caller with nowhere to return
// an error to, and says which of the two failed.
func (a *app) workOnWindowOrSay(addr, keyFile string, at *spot) {
	where := serveAddr(addr)
	title := "Could not take over " + a.windows.nameFor(where)
	if held := a.windows.at(where); held != nil {
		title = "Could not open a terminal on " + held.name
	}
	if err := a.workOnWindow(addr, keyFile, at); err != nil {
		a.reportError(title, err)
	}
}

// serveAddr is an address as the user typed it, with the serve port
// filled in when none was given.
func serveAddr(addr string) string {
	if addr == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		// No port, or a bare IPv6 literal, which has colons of its own
		// and is not an address with a port on the end.
		return net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(servePort))
	}
	return addr
}

// becomeWindowPane hands the pane that was watching a window being taken
// over to a shell on that window.
//
// The window itself rather than its name, which the server list can
// have changed while the connection was being made.
func (a *app) becomeWindowPane(t *taken, pane *term.Terminal, log *connLog) {
	t.log = log
	size := pane.Size()
	sess, err := t.win.Open(size.Cols, size.Rows)
	if err != nil {
		a.endedAs(pane, "no terminal")
		log.Failed(err)
		return
	}
	log.Say("connected")
	a.windows.draws(pane, t)
	log.Became(t.name, sess)
}

// holdWindow remembers a window and puts a row on the panel for it.
func (a *app) holdWindow(name, addr string, win *serve.Window) *taken {
	t := &taken{name: name, addr: addr, win: win}
	t.entry = &conns.Entry{
		Host: name,
		// A Server rather than a Terminal: it is the connection itself,
		// which the heading stands for, and the panel leaves those out
		// from under their own name rather than saying it twice.
		Kind:   conns.Server,
		Label:  "taken over",
		Meter:  &meter.Meter{},
		Reveal: func() { a.revealWindow(t) },
		// Closed by the window itself rather than by the name it is
		// held under now: the server list can give it another at any
		// time.
		Close: func() error { return a.dropWindow(t.name) },
	}
	a.windows.add(t)
	a.registry.Add(t.entry)
	a.refreshServers()

	// A window that quits at the far end is still held here, saying it
	// is taken over, until something notices. Nothing else here would.
	go func() {
		// Why it ended, not only that it did: it goes on the greyed row,
		// which is all the user has to work from afterwards.
		why := win.Wait()
		a.pump.post(func() { a.windowDied(t, why) })
	}()
	return t
}

// windowDied is called when the other window has gone by itself.
func (a *app) windowDied(t *taken, why error) {
	// By identity: the name may hold another window by now.
	if a.windows.named(t.name) != t {
		// Already let go of from here, and its row with it.
		return
	}
	if err := a.letGoOfWindow(t); err != nil {
		a.reportError("Trouble letting go of "+t.name, err)
	}
	// The row stays, the way greyRow says a dropped connection's row
	// does. It says what became of the window, under the name it was
	// held by.
	t.entry.Label = "no longer serving"
	a.greyRow(t.entry, why)
	a.refreshServers()
	a.markDirty()
}

// revealWindow puts one of a window's panes in front of the user.
//
// A window with nothing open on it has nothing to show, so nothing
// happens: there is no window for a connection itself.
func (a *app) revealWindow(t *taken) {
	for _, pane := range a.windows.drawnFrom(t) {
		a.focus(pane)
		return
	}
}

// openOnWindow opens a terminal in the other window, drawn in a pane
// here.
func (a *app) openOnWindow(addr string, at *spot) error {
	t := a.about(addr).window
	if t == nil {
		return fmt.Errorf("this window has not taken over %s", addr)
	}
	sess, err := t.win.Open(a.lastSize[0], a.lastSize[1])
	if err != nil {
		return err
	}
	// Under the name holding the window, which is not always the name
	// asked about, so the sidebar keeps one heading for it.
	pane, err := a.openSessionTab(sess, t.name, conns.Terminal, "terminal", at)
	if err != nil {
		// The session is ours and nothing else knows about it.
		_ = sess.Close()
		return err
	}
	a.windows.draws(pane, t)
	return nil
}

// farSize is how big the screen a pane is watching is now, or zero when
// the window over there no longer says it has it open.
//
// Asked for afresh rather than remembered: the far end is redrawn at
// its own size whenever that changes, and a size kept from the moment
// the pane opened would go on being shown long after it stopped being
// true.
func (a *app) farSize(what remoteKey) (cols, rows int) {
	open, ok := a.openOver(what)
	if !ok {
		return 0, 0
	}
	return open.Cols, open.Rows
}

// dropWindow lets go of another window, taking the panes drawn from it.
func (a *app) dropWindow(addr string) error {
	on := a.about(addr)
	t := on.window
	if t == nil {
		if d := on.dialling; d != nil {
			// Still on its way. Cancelling closes the connection under
			// the handshake, and the goroutine takes the row away. The
			// address is let go of here rather than there, so another
			// attempt can have it at once.
			a.machines.giveUp(d)
			return nil
		}
		return nil
	}
	a.registry.Drop(t.entry)
	err := a.letGoOfWindow(t)
	a.refreshServers()
	return err
}

// letGoOfWindow hangs up on a window and takes away what was drawn from
// it, leaving its row on the panel.
//
// Whether the row goes with it is the caller's: one the user let go of
// has nothing left to say, and one that quit at the far end keeps a
// greyed row.
func (a *app) letGoOfWindow(t *taken) error {
	// Under the name holding it, which is not always the name asked
	// about: the list can save a second name at one window's address,
	// and that name reaches the same connection.
	name := t.name
	a.windows.drop(t)

	// A file pane reading through this window is reading through a
	// connection that is about to go. It is taken away here, because
	// nothing else would: a pane does not end by itself the way a shell
	// does.
	errs := []error{a.closeFilesOn(name)}

	// Then the terminal panes, each closed while the connection
	// carrying it is still there to hang up politely.
	for _, pane := range a.windows.drawnFrom(t) {
		a.windows.forget(pane)
		errs = append(errs, a.closePane(pane))
	}
	errs = append(errs, t.win.Close())
	return errors.Join(errs...)
}

// closeWindows hangs up on every window this one took over, for a
// window that is shutting down.
//
// The connections and nothing else. The panes have gone by then, and a
// window on its way out has no tree left to take one out of and nobody
// to show a rebuilt menu to.
func (a *app) closeWindows() error {
	var errs []error
	for _, t := range a.windows.drain() {
		errs = append(errs, t.win.Close())
	}
	return errors.Join(errs...)
}

// knownWindows is where the keys of the windows reached are recorded:
// the file a test named, or the usual one.
func knownWindows(named string) (string, error) {
	if named != "" {
		return named, nil
	}
	return knownWindowsPath()
}

// knownWindowsPath is where the keys of windows this one has reached
// are recorded.
func knownWindowsPath() (string, error) {
	at, err := serve.Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(at, 0o700); err != nil {
		return "", fmt.Errorf("could not make %s: %w", at, err)
	}
	return filepath.Join(at, knownWindowsFile), nil
}

// attachHere opens a pane on what the window taken over already has
// running, rather than starting something new there.
func (a *app) attachHere(what remoteKey, at *spot) error {
	t := what.window
	if t == nil {
		// A key with no window behind it. Nothing builds one, so this is
		// a mistake in this window rather than anything the user did.
		return errors.New("that row names no window to watch it on")
	}
	if !a.windows.holds(t) {
		return fmt.Errorf("this window has let go of %s", t.name)
	}
	// Already watching it: the pane comes forward rather than a second
	// one opening on the same program, which would be two panes typing
	// into one shell.
	if pane := a.windows.watcher(what); pane != nil {
		a.focus(pane)
		return nil
	}
	open, ok := a.openOver(what)
	if !ok {
		return fmt.Errorf("%s no longer has that open", t.name)
	}
	if !open.HasScreen() {
		// Asking anyway opened a pane that showed the refusal, and the
		// pane was a row of its own: every attempt left another one
		// behind.
		return fmt.Errorf("%q on %s has no screen to watch: it is %s, not a terminal",
			open.Label, t.name, strings.ToLower(open.Kind))
	}
	sess, err := t.win.Attach(open, a.lastSize[0], a.lastSize[1])
	if err != nil {
		return err
	}
	pane, err := a.openSessionTab(sess, t.name, conns.Terminal, open.Label, at)
	if err != nil {
		// The session is ours and nothing else knows about it. Failing
		// to let go of it leaves the pane over there being watched by
		// nobody, which is worth saying along with why this failed.
		return errors.Join(err, sess.Close())
	}
	a.windows.draws(pane, t)
	a.windows.watch(pane, what)
	return nil
}

// openOver is what a window taken over says about one of the things it
// has open, and whether it still has it.
func (a *app) openOver(what remoteKey) (serve.Open, bool) {
	t := what.window
	if t == nil || what.id == "" || !a.windows.holds(t) {
		return serve.Open{}, false
	}
	for _, open := range t.win.Opens() {
		if open.ID == what.id {
			return open, true
		}
	}
	return serve.Open{}, false
}
