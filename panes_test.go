package main

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/gofont/gomono"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// pipeSession is a shell that reads nothing and writes nowhere, with an
// end the test can pull.
type pipeSession struct {
	mu      sync.Mutex
	out     chan []byte
	written []byte
	writes  int
	size    [2]int
	closed  bool

	// closeErr is what Close hands back, for a test about a channel that
	// will not let go.
	closeErr error
}

// failOnClose makes the next Close hand back an error, for a test about
// a channel that will not let go.
func (p *pipeSession) failOnClose(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeErr = err
}

func newPipeSession() *pipeSession {
	return &pipeSession{out: make(chan []byte, 4)}
}

func (p *pipeSession) Read(b []byte) (int, error) {
	got, ok := <-p.out
	if !ok {
		return 0, io.EOF
	}
	return copy(b, got), nil
}

func (p *pipeSession) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.written = append(p.written, b...)
	p.writes++
	return len(b), nil
}

// writeCount is how many writes the terminal has made to this shell,
// for a test about input that has to arrive together.
func (p *pipeSession) writeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes
}

// isClosed reports whether this shell has been closed.
func (p *pipeSession) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// sentText returns what the terminal has written to this shell.
func (p *pipeSession) sentText() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.written)
}
func (p *pipeSession) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.size = [2]int{cols, rows}
	return nil
}

// lastSize returns the size the terminal last told this shell.
func (p *pipeSession) lastSize() [2]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.size
}
func (p *pipeSession) Wait() error { return nil }

func (p *pipeSession) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.out)
	}
	return p.closeErr
}

// reapWhenTold reaps the panes whose shell has gone, once the window has
// actually been told about one.
//
// A terminal sets Exited just before it tells the window, so a reap that
// waited only for Exited can run in the gap and find nothing queued.
// The window itself reaps on every frame, so a late one costs it a
// frame; a test that reaps once has to wait for it.
func reapWhenTold(t *testing.T, a *testApp) {
	t.Helper()
	waitUntil(t, "the window to be told a pane exited", func() bool { return len(a.exits) > 0 })
	a.reapExited()
}

// newTestApp builds an app with one pane and no window, which is enough
// for every tree operation.
type testApp struct {
	*app
	// shells are the fake sessions, in the order they were started, and
	// argvs the argv each was started on. A shell served to another
	// window is started on a goroutine of the server's, so shellsMu
	// guards the appends; a test reads the lists once it has waited for
	// the pane that uses the shell.
	shells   []*pipeSession
	argvs    [][]string
	shellsMu sync.Mutex

	// screen is the image the window draws on, kept between frames: a
	// fresh one arrives blank, and the compositor puts the whole stack
	// back when it does.
	screen *ebiten.Image

	// copied is what the window has put on the clipboard. Its own, not
	// the clipboard of whoever is running the tests: a test run must
	// not reach into that.
	copiedMu sync.Mutex
	copied   []string

	// logged is everything the window reported through logError, from
	// before the first frame. A test that has to see one of those lines
	// reads it; the rest are kept off the test's own output.
	logged *said
}

// copiedText is the last thing the window put on the clipboard.
func (ta *testApp) copiedText() string {
	ta.copiedMu.Lock()
	defer ta.copiedMu.Unlock()
	if len(ta.copied) == 0 {
		return ""
	}
	return ta.copied[len(ta.copied)-1]
}

// testOption is a flag the window was started with, for a test that has
// to build the app the way main does.
type testOption func(*startup)

// startedWith is the -ssh target the window was started with, so a test
// can drive what the flag does.
func startedWith(s startup) testOption {
	return func(into *startup) { *into = s }
}

func newTestApp(t *testing.T, cols, rows int, opts ...testOption) *testApp {
	t.Helper()
	ta := &testApp{app: &app{
		fontSize:   defaultFontSize,
		colours:    vt.DefaultPalette(),
		scrollback: 64,
		panes:      make(map[*term.Terminal]*conns.Entry),
		scaled:     make(map[*term.Terminal]*scaledPane),
		ended:      make(map[*term.Terminal]bool),
		exits:      make(chan struct{}, exitQueue),
		lastSize:   [2]int{cols, rows},
		registry:   conns.New(),
		rates:      make(map[*conns.Entry]*meter.Rate),
		machines:   newMachines(),
		serving:    newServing(),
		agents:     newAgents(),
		kept:       make(map[*term.Terminal]bool),
		tunnels:    make(map[*conns.Entry]*tunnel),
		queue:      jobs.New(1),
		jobs:       make(map[*conns.Entry]*jobs.Job),
		asking:     make(map[chan jobs.Choice]func()),
	}}
	// Set before anything runs, because a window logs from the goroutines
	// it starts and a test that pointed onError at its own collector
	// afterwards would be writing a field those goroutines are reading.
	ta.logged = &said{}
	ta.onError = ta.logged.add
	// A clipboard of its own. Without this every test that copies
	// something would overwrite the clipboard of whoever ran it.
	ta.clip.write = func(s string) error {
		ta.copiedMu.Lock()
		defer ta.copiedMu.Unlock()
		ta.copied = append(ta.copied, s)
		return nil
	}
	// The window's own grid, so markDirty and setGridSize do what they do
	// in the program rather than nothing at all.
	ta.g = grid.New(cols, rows, ta.colours.FG, ta.colours.BG)
	// And a renderer, because the window measures itself in cells and
	// asks the renderer how big one is.
	atlas, err := glyph.NewAtlas(glyph.Fonts{Regular: gomono.TTF}, 12, 96)
	if err != nil {
		t.Fatalf("no atlas: %v", err)
	}
	ta.atlas = atlas
	ta.renderer = render.New(atlas)
	// A server list of its own, in a directory the test owns, so nothing
	// reads or writes the one belonging to whoever is running the tests.
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatalf("server list: %v", err)
	}
	ta.book = book
	// Settings of its own too, so no test reads or writes the ones
	// belonging to whoever is running it.
	set, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	ta.serving.remember(set)
	ta.agents.remember(set)
	// A machine of the test's own: nothing here runs wsl.exe or reads
	// the PATH of whoever is running the tests.
	ta.shellPick = newShellPick()
	ta.shellPick.findShells = func() ([]shells.Shell, error) { return testShells(), nil }
	ta.shellPick.namedShell = func(id string) (shells.Shell, bool) {
		return shells.Lookup(testShells(), id)
	}
	ta.shellPick.remember(set)
	ta.windows = newWindows(book)
	// Long enough to be a handshake and short enough that a test which
	// waits one out is not a test that waits twenty seconds.
	ta.windows.patience = 300 * time.Millisecond
	// The windows it reaches are recorded in a file of the test's own.
	// The real one belongs to whoever is running the tests, and a test
	// that wrote to it would fill it with the loopback ports of servers
	// that existed for a tenth of a second -- and leave keys behind to
	// raise a false alarm if a port ever came round again.
	ta.windows.knownAt = filepath.Join(t.TempDir(), "known_windows")
	ta.newShell = func(argv []string, _, _ int) (session.Session, error) {
		sess := newPipeSession()
		ta.shellsMu.Lock()
		ta.shells = append(ta.shells, sess)
		ta.argvs = append(ta.argvs, argv)
		ta.shellsMu.Unlock()
		return sess, nil
	}
	// The flags, the way main reads them: with -ssh there is no first
	// pane, because the connection opens its own on the first frame.
	var start startup
	for _, opt := range opts {
		opt(&start)
	}
	first, err := ta.openFirst(start)
	if err != nil {
		t.Fatalf("first pane: %v", err)
	}
	// The same shape the window has: everything open sits on the stage,
	// which shows one at a time.
	ta.stage = ta.newTabs(startingPanes(first)...)
	ta.root.SetWidget(ta.stage)
	ta.root.Layout(ui.Rect{Cols: cols, Rows: rows})
	t.Cleanup(func() {
		for pane := range ta.panes {
			_ = pane.Close()
		}
		_ = ta.closeTunnels()
		_ = ta.closeMachines()
		// A pane handed over and not taken back leaves a listener and
		// its accept goroutine running for the rest of the binary.
		_ = ta.agents.stop()
	})
	return ta
}

// setTitle makes one pane's program name the window.
func (ta *testApp) setTitle(t *testing.T, which int, pane *term.Terminal, title string) {
	t.Helper()
	ta.shells[which].out <- []byte("\x1b]0;" + title + "\x07")
	waitFor(t, ta, "the pane to take the title its program set", func() bool { return pane.Title() == title })
}

// checkTree asserts what must always hold: the panes the app knows about
// and the panes in the tree are the same set, and focus is on one of
// them.
func checkTree(t *testing.T, a *testApp) {
	t.Helper()
	inTree := map[*term.Terminal]bool{}
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if leaf == ui.Widget(a.side) || leaf == ui.Widget(a.panel) {
			// The sidebar is a leaf of the dock, not a pane.
			continue
		}
		if p, isFiles := leaf.(*files.Pane); isFiles {
			// A pane of the file manager is not a terminal, and the app
			// keeps those under the manager rather than with the panes.
			if a.files == nil || a.files.rows[p] == nil {
				t.Fatal("a file pane in the tree has no row on the sidebar")
			}
			continue
		}
		pane, ok := leaf.(*term.Terminal)
		if !ok {
			t.Fatalf("a leaf is not a terminal: %T", leaf)
		}
		if _, known := a.panes[pane]; !known {
			t.Fatal("a pane in the tree is not in the app's list, so its shell is never closed")
		}
		inTree[pane] = true
	}
	for pane := range a.panes {
		if !inTree[pane] {
			t.Fatal("a pane in the app's list is not in the tree, so it runs unseen")
		}
	}
	if len(a.panes) == 0 && a.files == nil {
		return
	}
	leaf := ui.FocusedLeaf(a.root.Widget())
	if p, isFiles := leaf.(*files.Pane); isFiles {
		// The manager holds the keys itself and hands them to one of
		// its panes.
		if a.root.Modal() == nil && !p.Focused() {
			t.Fatal("a file pane has the keys and does not know it")
		}
		return
	}
	pane, ok := leaf.(*term.Terminal)
	if !ok || !inTree[pane] {
		t.Fatalf("focus is on %T, which is not a live pane", leaf)
	}
	// Exactly one, or two panes both draw a cursor and both take keys.
	// None at all while a dialog is up: it has the keys and the cursor.
	focused := 0
	for p := range inTree {
		if p.Focused() {
			focused++
		}
	}
	want := 1
	if a.root.Modal() != nil {
		want = 0
	}
	if focused != want {
		t.Fatalf("%d panes believe they have focus, want %d", focused, want)
	}
	if want == 1 && !pane.Focused() {
		t.Fatal("the pane keys reach does not believe it has focus")
	}
}

// checkClosed asserts that panes taken out of the tree really stopped.
// Removing one from the list without closing it leaks a session and two
// goroutines, and nothing else would notice.
func checkClosed(t *testing.T, closed ...*term.Terminal) {
	t.Helper()
	for _, pane := range closed {
		waitUntil(t, "the pane to have exited", pane.Exited)
	}
}

func TestSplitAndClose(t *testing.T) {
	a := newTestApp(t, 40, 10)

	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	checkTree(t, a)
	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want 2", len(a.panes))
	}
	// The new pane takes focus, which is what lets you type into it.
	if len(ui.Leaves(a.root.Widget())) != 2 {
		t.Fatal("the tree does not hold two panes")
	}

	closing := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)
	if err := a.closeFocused(); err != nil {
		t.Fatalf("close: %v", err)
	}
	checkTree(t, a)
	checkClosed(t, closing)
	if len(a.panes) != 1 {
		t.Fatalf("%d panes after closing one, want 1", len(a.panes))
	}
	// The split collapsed: the survivor is the root again.
	if _, isSplit := a.root.Widget().(*ui.Split); isSplit {
		t.Error("the split outlived the pane it was dividing")
	}
	if a.quit.Load() {
		t.Error("closing one pane of two closed the window")
	}
}

func TestClosingTheLastPaneQuits(t *testing.T) {
	a := newTestApp(t, 40, 10)
	only := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)

	if err := a.closeFocused(); err != nil {
		t.Fatalf("close: %v", err)
	}

	checkClosed(t, only)
	if !a.quit.Load() {
		t.Error("closing the only pane did not close the window")
	}
	if len(a.panes) != 0 {
		t.Errorf("%d panes left, want none", len(a.panes))
	}
}

// TestCloseAPaneWhoseSiblingIsASplit checks that closing one pane
// promotes a whole subtree, not just a leaf.
func TestCloseAPaneWhoseSiblingIsASplit(t *testing.T) {
	a := newTestApp(t, 40, 10)
	// Three panes: first | (second / third).
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	if err := a.splitHere(ui.Rows); err != nil {
		t.Fatalf("split: %v", err)
	}
	checkTree(t, a)
	panes := ui.Leaves(a.root.Widget())
	if len(panes) != 3 {
		t.Fatalf("%d panes, want 3", len(panes))
	}

	// Close the leftmost, whose sibling is the inner split.
	a.focus(panes[0])
	closing := panes[0].(*term.Terminal)
	if err := a.closeFocused(); err != nil {
		t.Fatalf("close: %v", err)
	}

	checkTree(t, a)
	checkClosed(t, closing)
	if len(a.panes) != 2 {
		t.Errorf("%d panes, want 2", len(a.panes))
	}
	// The stage is always on top; the promoted split is what it shows.
	inner, ok := a.stage.Children()[0].(*ui.Split)
	if !ok || inner.Dir() != ui.Rows {
		t.Errorf("the stage holds %T, want the inner split promoted",
			a.stage.Children()[0])
	}
}

func TestFocusCyclesThroughEveryPane(t *testing.T) {
	a := newTestApp(t, 60, 20)
	for i := 0; i < 2; i++ {
		if err := a.splitHere(ui.Columns); err != nil {
			t.Fatalf("split: %v", err)
		}
	}
	panes := ui.Leaves(a.root.Widget())
	if len(panes) != 3 {
		t.Fatalf("%d panes, want 3", len(panes))
	}
	a.focus(panes[0])

	seen := map[ui.Widget]bool{}
	for i := 0; i < len(panes); i++ {
		seen[ui.FocusedLeaf(a.root.Widget())] = true
		if err := a.focusPane(1); err != nil {
			t.Fatalf("focus next: %v", err)
		}
	}

	if len(seen) != len(panes) {
		t.Errorf("cycling reached %d of %d panes", len(seen), len(panes))
	}
	// A full turn comes back to where it started.
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[0] {
		t.Error("a full cycle did not come back to the first pane")
	}
	// And backwards from the first lands on the last.
	if err := a.focusPane(-1); err != nil {
		t.Fatalf("focus previous: %v", err)
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != panes[len(panes)-1] {
		t.Error("stepping back from the first pane did not reach the last")
	}
}

// TestSplitRefusedWithNoRoom checks that a pane too small to divide is
// left alone, rather than becoming two panes nobody can see with live
// shells in them.
func TestSplitRefusedWithNoRoom(t *testing.T) {
	a := newTestApp(t, 2, 4)

	err := a.splitHere(ui.Columns)

	if err == nil {
		t.Error("splitting a pane two columns wide was allowed")
	}
	if len(a.panes) != 1 {
		t.Errorf("%d panes, want the split refused", len(a.panes))
	}
	checkTree(t, a)
}

// TestReapClosesExitedPanes checks the path a shell takes when it ends
// on its own, with two going at once because map order is random.
func TestReapClosesExitedPanes(t *testing.T) {
	for run := 0; run < 30; run++ {
		a := newTestApp(t, 60, 20)
		for i := 0; i < 2; i++ {
			if err := a.splitHere(ui.Columns); err != nil {
				t.Fatalf("split: %v", err)
			}
		}
		panes := ui.Leaves(a.root.Widget())
		survivor := panes[1].(*term.Terminal)

		// Two shells end at once.
		for _, p := range []ui.Widget{panes[0], panes[2]} {
			pane := p.(*term.Terminal)
			_ = pane.Close()
			waitUntil(t, "the pane to have exited", pane.Exited)
		}
		reapWhenTold(t, a)

		checkTree(t, a)
		if len(a.panes) != 1 {
			t.Fatalf("run %d: %d panes, want 1", run, len(a.panes))
		}
		if _, alive := a.panes[survivor]; !alive {
			t.Fatalf("run %d: the wrong pane survived", run)
		}
		if a.quit.Load() {
			t.Fatalf("run %d: the window closed with a pane still open", run)
		}
	}
}

// TestReapTheLastPaneQuits checks that a shell exiting on its own with
// nothing beside it closes the window.
func TestReapTheLastPaneQuits(t *testing.T) {
	a := newTestApp(t, 40, 10)
	only := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)

	_ = only.Close()
	waitUntil(t, "the pane to have exited", only.Exited)
	reapWhenTold(t, a)

	if !a.quit.Load() {
		t.Error("the last shell exiting did not close the window")
	}
	if len(a.panes) != 0 {
		t.Errorf("%d panes left, want none", len(a.panes))
	}
}

// TestSplitCloseFuzz drives the tree the way a user would and checks the
// invariants after every step. Tree surgery is where a pane gets left
// running unseen, or closed while still drawn.
func TestSplitCloseFuzz(t *testing.T) {
	a := newTestApp(t, 80, 24)
	rng := rand.New(rand.NewSource(1))

	for step := 0; step < 200; step++ {
		switch rng.Intn(4) {
		case 0:
			dir := ui.Columns
			if rng.Intn(2) == 0 {
				dir = ui.Rows
			}
			// Refusing for want of room is an answer, not a failure.
			_ = a.splitHere(dir)
		case 1:
			if len(a.panes) > 1 {
				if err := a.closeFocused(); err != nil {
					t.Fatalf("step %d: close: %v", step, err)
				}
			}
		case 2:
			if err := a.focusPane(1); err != nil {
				t.Fatalf("step %d: focus next: %v", step, err)
			}
		case 3:
			if err := a.focusPane(-1); err != nil {
				t.Fatalf("step %d: focus previous: %v", step, err)
			}
		}
		if a.quit.Load() {
			t.Fatalf("step %d: the window closed with panes still open", step)
		}
		checkTree(t, a)
	}
}

// sendKey gives the window a key, and fails the test when the key is
// refused or nothing takes it.
func sendKey(t *testing.T, a *testApp, ev input.Event) {
	t.Helper()
	took, err := a.root.HandleKey(ev)
	if err != nil {
		t.Fatalf("the window refused %s: %v", keyName(ev), err)
	}
	if !took {
		t.Fatalf("nothing in the window took %s", keyName(ev))
	}
}

// dismiss presses Escape on a widget, the way a user drops a menu, a
// chooser or a dialog.
func dismiss(t *testing.T, w ui.KeyHandler) {
	t.Helper()
	took, err := w.HandleKey(press(input.KeyEscape, 0))
	if err != nil {
		t.Fatalf("Escape was refused: %v", err)
	}
	if !took {
		t.Fatalf("%T did not take Escape", w)
	}
}

// keyName spells a key event the way a failure should name it.
func keyName(ev input.Event) string {
	if ev.Kind == input.Text {
		return strconv.QuoteRune(ev.Rune)
	}
	if ev.Mods == 0 {
		return ev.Key.String()
	}
	return ev.Mods.String() + "+" + ev.Key.String()
}

// press builds a key press, the way the ui tests do.
func press(k input.Key, mods input.Mods) input.Event {
	return input.Event{Kind: input.KeyPress, Key: k, Mods: mods}
}

// waitFor runs the pump of every window it is given until something is
// true, or fails the test.
func waitFor(t *testing.T, a *testApp, what any, cond func() bool, also ...*testApp) {
	t.Helper()
	// A window taken over answers its client from the goroutine that
	// draws, so a test that pumped only one of two would wait for an
	// answer the other was never going to give.
	waitLoop(t, what, cond, append([]*testApp{a}, also...))
}

// waitUntil waits for something to become true where there is no window
// to pump, which is a test driving one part of the program on its own.
func waitUntil(t *testing.T, what any, cond func() bool) {
	t.Helper()
	waitLoop(t, what, cond, nil)
}

// waitLoop is the one waiting loop: one budget, and one failure that says
// what was waited for.
func waitLoop(t *testing.T, what any, cond func() bool, windows []*testApp) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		for _, a := range windows {
			a.pump.run()
		}
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %v", what)
}

// awaitModal runs the pump until the modal on top is a T the test is
// after, and returns it. A nil want takes the first T of any title.
func awaitModal[T ui.Widget](t *testing.T, a *testApp, what string, want func(T) bool) T {
	t.Helper()
	var found T
	waitFor(t, a, modalWanted{what: what, a: a}, func() bool {
		got, is := a.root.Modal().(T)
		if !is || (want != nil && !want(got)) {
			return false
		}
		found = got
		return true
	})
	return found
}

// modalWanted names what a test waited for and what was on top instead,
// read when the wait gives up rather than when it starts.
type modalWanted struct {
	what string
	a    *testApp
}

func (m modalWanted) String() string {
	return fmt.Sprintf("%s; the modal on top is %T titled %q",
		m.what, m.a.root.Modal(), titleOf(m.a.root.Modal()))
}

// byTitle matches a modal by the whole title it draws at its top.
func byTitle[T ui.Widget](want string) func(T) bool {
	return func(w T) bool { return titleOf(w) == want }
}

// byTitlePrefix matches a modal whose title starts with a prefix, for a
// title that goes on to name the thing it is about.
func byTitlePrefix[T ui.Widget](prefix string) func(T) bool {
	return func(w T) bool { return strings.HasPrefix(titleOf(w), prefix) }
}

// titleOf returns the title a modal draws at its top, and empty for a
// modal that draws none.
func titleOf(w ui.Widget) string {
	switch m := w.(type) {
	case *jobDialog:
		return m.Title
	case *ui.Form:
		return m.Title
	case *ui.Notice:
		return m.Title
	}
	return ""
}

// offWindow runs something that talks to a window from another goroutine
// and waits for it. The window is pumped meanwhile, because it answers
// from the goroutine that draws.
func offWindow(t *testing.T, a *testApp, what string, do func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- do() }()
	var err error
	waitFor(t, a, what, func() bool {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	})
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// TestSplitRoomBoundary pins where splitting stops being possible: one
// cell each side and one for the divider.
func TestSplitRoomBoundary(t *testing.T) {
	for _, tc := range []struct {
		cols  int
		allow bool
	}{{cols: 2, allow: false}, {cols: 3, allow: true}} {
		a := newTestApp(t, tc.cols, 4)

		err := a.splitHere(ui.Columns)

		if tc.allow && err != nil {
			t.Errorf("%d columns: %v, want the split allowed", tc.cols, err)
		}
		if !tc.allow && err == nil {
			t.Errorf("%d columns: the split was allowed", tc.cols)
		}
	}
}

// TestSplitRefusedWhenThePaneIsNotShown checks the pane a narrow window
// squeezed out. It keeps the size it was last given on purpose, so the
// answer has to come from the tree rather than from the pane.
func TestSplitRefusedWhenThePaneIsNotShown(t *testing.T) {
	a := newTestApp(t, 40, 10)
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	hidden := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)
	if got := hidden.Size().Cols; got < 3 {
		t.Fatalf("the pane starts %d columns wide, too narrow for the test", got)
	}

	// Narrow the window until there is no room for the second pane.
	a.lastSize = [2]int{2, 10}
	a.root.Layout(ui.Rect{Cols: 2, Rows: 10})

	if err := a.splitHere(ui.Columns); err == nil {
		t.Error("a pane with nowhere to be drawn was split")
	}
	if len(a.panes) != 2 {
		t.Errorf("%d panes, want the split refused", len(a.panes))
	}
	if got := hidden.Size().Cols; got < 3 {
		t.Errorf("the squeezed pane was resized to %d columns, reflowing its scrollback", got)
	}
}

// TestTitleFollowsFocus checks that a background pane cannot rename the
// window.
func TestTitleFollowsFocus(t *testing.T) {
	a := newTestApp(t, 40, 10)
	first := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	second := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)

	a.setTitle(t, 0, first, "background")
	a.setTitle(t, 1, second, "foreground")

	if got := a.focusedTerminal().Title(); got != "foreground" {
		t.Errorf("title = %q, want the focused pane's", got)
	}
	a.focus(first)
	if got := a.focusedTerminal().Title(); got != "background" {
		t.Errorf("after moving focus, title = %q, want the other pane's", got)
	}
}

// TestPaneExitedNeverBlocks checks the notice a shell sends when it
// ends. Nothing drains the queue once the window is closing, so waiting
// on it would hang the goroutine reading that shell.
func TestPaneExitedNeverBlocks(t *testing.T) {
	a := newTestApp(t, 40, 10)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < exitQueue*3; i++ {
			a.paneExited()
		}
	}()

	select {
	case <-done:
	case <-time.After(waitBudget):
		t.Fatal("paneExited blocked once the queue was full")
	}
}

// TestClosePaneEndsTheShellEvenWhenTheTreeSurgeryFails checks that a
// pane the tree does not know about is still shut down. Leaving a shell
// running is worse than a crooked tree, and the pane could never be
// reaped: every attempt would fail the same way.
func TestClosePaneEndsTheShellEvenWhenTheTreeSurgeryFails(t *testing.T) {
	a := newTestApp(t, 40, 10)
	stray, err := a.newTerminal()
	if err != nil {
		t.Fatalf("second pane: %v", err)
	}
	// It is in the app's list but was never put in the tree.
	if _, known := a.panes[stray]; !known {
		t.Fatal("the new pane was not recorded")
	}

	if err := a.closePane(stray); err == nil {
		t.Error("closing a pane that is not in the tree reported success")
	}

	checkClosed(t, stray)
	if _, known := a.panes[stray]; known {
		t.Error("the pane is still in the list, so it would be reaped for ever")
	}
}

// TestOpenTabStartsAStrip checks the first tab opened beside a plain
// pane, which has to become a strip holding both.
func TestOpenTabStartsAStrip(t *testing.T) {
	a := newTestApp(t, 40, 10)
	first := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)

	if err := a.openTab(); err != nil {
		t.Fatalf("open tab: %v", err)
	}

	checkTree(t, a)
	strip, ok := a.root.Widget().(*ui.Tabs)
	if !ok {
		t.Fatalf("root = %T, want a tab strip", a.root.Widget())
	}
	if got := strip.Children(); len(got) != 2 || got[0] != ui.Widget(first) {
		t.Errorf("tabs = %v, want the original first", got)
	}
	if ui.FocusedLeaf(a.root.Widget()) == ui.Widget(first) {
		t.Error("the new tab is not the one being shown")
	}
}

// TestOpenTabAddsToAnExistingStrip checks that a second tab joins the
// strip rather than nesting another one inside it.
func TestOpenTabAddsToAnExistingStrip(t *testing.T) {
	a := newTestApp(t, 40, 10)
	for i := 0; i < 2; i++ {
		if err := a.openTab(); err != nil {
			t.Fatalf("open tab: %v", err)
		}
	}

	checkTree(t, a)
	if a.root.Widget() != ui.Widget(a.stage) {
		t.Fatalf("root = %T, want the one stage", a.root.Widget())
	}
	if got := len(a.stage.Children()); got != 3 {
		t.Errorf("the stage holds %d panes, want 3", got)
	}
}

// The stage draws no row of labels. Which pane is showing is chosen from
// the sidebar, which has room to say what each one is and which machine
// it is on.
func TestTheStageHasNoTabStrip(t *testing.T) {
	a := newTestApp(t, 40, 10)
	for i := 0; i < 2; i++ {
		if err := a.openTab(); err != nil {
			t.Fatalf("open tab: %v", err)
		}
	}
	if !a.stage.HideStrip {
		t.Fatal("the stage draws a strip of labels")
	}
	// The pane being shown gets every row, rather than all but one.
	area, shown := a.root.AreaOf(ui.FocusedLeaf(a.root.Widget()))
	if !shown {
		t.Fatal("the pane being shown is not on screen")
	}
	if area.Y != 0 || area.Rows != 10 {
		t.Fatalf("the pane sits at row %d and is %d tall, want all 10", area.Y, area.Rows)
	}
}

// TestTabsAndSplitsNest checks the two containers working together: a
// split is one of the things the stage holds, beside a pane of its own.
func TestTabsAndSplitsNest(t *testing.T) {
	a := newTestApp(t, 60, 20)
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	if err := a.openTab(); err != nil {
		t.Fatalf("open tab: %v", err)
	}

	checkTree(t, a)
	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want 3", len(a.panes))
	}
	if a.root.Widget() != ui.Widget(a.stage) {
		t.Fatalf("root = %T, want the stage on top", a.root.Widget())
	}
	kids := a.stage.Children()
	if len(kids) != 2 {
		t.Fatalf("the stage holds %d things, want the split and the new pane", len(kids))
	}
	if _, ok := kids[0].(*ui.Split); !ok {
		t.Errorf("the first is %T, want the split", kids[0])
	}
	if _, ok := kids[1].(*term.Terminal); !ok {
		t.Errorf("the second is %T, want the new pane", kids[1])
	}
	// And the new one is what is being shown.
	if ui.FocusedLeaf(a.root.Widget()) != kids[1] {
		t.Error("the new pane is not the one being shown")
	}
}

func TestFocusTabCycles(t *testing.T) {
	a := newTestApp(t, 40, 10)
	for i := 0; i < 2; i++ {
		if err := a.openTab(); err != nil {
			t.Fatalf("open tab: %v", err)
		}
	}
	tabs := ui.Leaves(a.root.Widget())
	if len(tabs) != 3 {
		t.Fatalf("%d tabs, want 3", len(tabs))
	}
	a.focus(tabs[0])

	seen := map[ui.Widget]bool{}
	for i := 0; i < len(tabs); i++ {
		seen[ui.FocusedLeaf(a.root.Widget())] = true
		if err := a.focusTab(1); err != nil {
			t.Fatalf("next tab: %v", err)
		}
	}

	if len(seen) != len(tabs) {
		t.Errorf("cycling reached %d of %d tabs", len(seen), len(tabs))
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != tabs[0] {
		t.Error("a full cycle did not come back to the first tab")
	}
	if err := a.focusTab(-1); err != nil {
		t.Fatalf("previous tab: %v", err)
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != tabs[len(tabs)-1] {
		t.Error("stepping back from the first tab did not reach the last")
	}
}

// TestFocusTabDoesNothingOutsideAStrip checks that the tab keys are
// harmless in a window that has no tabs.
func TestFocusTabDoesNothingOutsideAStrip(t *testing.T) {
	a := newTestApp(t, 40, 10)
	before := ui.FocusedLeaf(a.root.Widget())

	if err := a.focusTab(1); err != nil {
		t.Fatalf("next tab: %v", err)
	}

	if ui.FocusedLeaf(a.root.Widget()) != before {
		t.Error("moving between tabs moved focus with no tabs open")
	}
	checkTree(t, a)
}

// TestTheStageOutlivesItsPanes checks the whole life of the stage: it
// carries on as panes close, it is still there with one left, and the
// window goes with the last one.
//
// It does not stand aside for its last pane the way a plain strip does.
// Every pane the window opens goes in it, so a stage replaced by a
// terminal is a window with nowhere to put the next one.
func TestTheStageOutlivesItsPanes(t *testing.T) {
	a := newTestApp(t, 40, 10)
	for i := 0; i < 2; i++ {
		if err := a.openTab(); err != nil {
			t.Fatalf("open tab: %v", err)
		}
	}

	for left := 2; left >= 1; left-- {
		if err := a.closeFocused(); err != nil {
			t.Fatalf("close: %v", err)
		}
		checkTree(t, a)
		if got := a.root.Widget(); got != ui.Widget(a.stage) {
			t.Fatalf("with %d panes left the root is %T, want the stage", left, got)
		}
		if got := len(a.stage.Children()); got != left {
			t.Fatalf("the stage holds %d panes, want %d", got, left)
		}
	}

	if err := a.closeFocused(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !a.quit.Load() {
		t.Error("closing the last pane did not close the window")
	}
}

// TestTabsAndSplitsFuzz drives both containers together and checks the
// invariants after every step. Mixing them is where a pane gets left
// running unseen.
func TestTabsAndSplitsFuzz(t *testing.T) {
	a := newTestApp(t, 80, 24)
	rng := rand.New(rand.NewSource(7))

	for step := 0; step < 200; step++ {
		switch rng.Intn(6) {
		case 0:
			dir := ui.Columns
			if rng.Intn(2) == 0 {
				dir = ui.Rows
			}
			// Refusing for want of room is an answer, not a failure.
			_ = a.splitHere(dir)
		case 1:
			if err := a.openTab(); err != nil {
				t.Fatalf("step %d: open tab: %v", step, err)
			}
		case 2:
			if len(a.panes) > 1 {
				if err := a.closeFocused(); err != nil {
					t.Fatalf("step %d: close: %v", step, err)
				}
			}
		case 3:
			if err := a.focusPane(1); err != nil {
				t.Fatalf("step %d: next pane: %v", step, err)
			}
		case 4:
			before := ui.FocusedLeaf(a.root.Widget())
			if err := a.focusTab(1); err != nil {
				t.Fatalf("step %d: next tab: %v", step, err)
			}
			// A strip with more than one tab must actually move, or a
			// command that quietly does nothing looks like success.
			if strip, _ := a.stripAbove(before); strip != nil && len(strip.Children()) > 1 {
				if ui.FocusedLeaf(a.root.Widget()) == before {
					t.Fatalf("step %d: the next tab command did nothing", step)
				}
			}
		case 5:
			if err := a.focusTab(-1); err != nil {
				t.Fatalf("step %d: previous tab: %v", step, err)
			}
		}
		if a.quit.Load() {
			t.Fatalf("step %d: the window closed with panes still open", step)
		}
		checkTree(t, a)
	}
}

// TestFocusTabWorksAfterSplittingATab checks the tab keys in a tree
// where the focused pane's own parent is a split, not the strip. Asking
// for the direct parent finds no strip and the tab keys go dead.
func TestFocusTabWorksAfterSplittingATab(t *testing.T) {
	a := newTestApp(t, 60, 20)
	if err := a.openTab(); err != nil {
		t.Fatalf("open tab: %v", err)
	}
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	before := ui.FocusedLeaf(a.root.Widget())

	if err := a.focusTab(1); err != nil {
		t.Fatalf("next tab: %v", err)
	}

	checkTree(t, a)
	if ui.FocusedLeaf(a.root.Widget()) == before {
		t.Error("moving to the next tab did nothing: the strip above the split was not found")
	}
	// And back again lands inside the split, on one of its panes.
	if err := a.focusTab(-1); err != nil {
		t.Fatalf("previous tab: %v", err)
	}
	strip, ok := a.root.Widget().(*ui.Tabs)
	if !ok {
		t.Fatalf("root = %T, want the strip", a.root.Widget())
	}
	if _, isSplit := strip.Children()[1].(*ui.Split); !isSplit {
		t.Fatalf("the second tab is %T, want the split", strip.Children()[1])
	}
}

// TestOpenTabFromInsideASplitJoinsTheStripAbove checks that a new tab
// joins the strip the pane is already under, rather than starting a
// second strip nested inside the split.
func TestOpenTabFromInsideASplitJoinsTheStripAbove(t *testing.T) {
	a := newTestApp(t, 60, 20)
	if err := a.openTab(); err != nil {
		t.Fatalf("open tab: %v", err)
	}
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}

	if err := a.openTab(); err != nil {
		t.Fatalf("open tab: %v", err)
	}

	checkTree(t, a)
	strip, ok := a.root.Widget().(*ui.Tabs)
	if !ok {
		t.Fatalf("root = %T, want one strip", a.root.Widget())
	}
	if got := len(strip.Children()); got != 3 {
		t.Errorf("%d tabs, want 3 in the one strip", got)
	}
	for _, tab := range strip.Children() {
		if _, nested := tab.(*ui.Tabs); nested {
			t.Error("a second strip was started inside the first")
		}
	}
}

// TestPaletteOpensAndCloses checks the dialog's whole life: it goes on
// the modal stack, gets a layer of its own, and takes both away again.
func TestPaletteOpensAndCloses(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	layersBefore := len(a.comp.Layers())

	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	if a.palette == nil {
		t.Fatal("the dialog was not made")
	}
	if a.root.Modal() != ui.Widget(a.palette) {
		t.Errorf("top modal = %v, want the dialog", a.root.Modal())
	}
	if got := len(a.comp.Layers()); got != layersBefore+1 {
		t.Errorf("%d layers, want one more than %d", got, layersBefore)
	}

	a.closePalette()

	if a.palette != nil || a.dismissPalette != nil || len(a.modals) != 0 {
		t.Error("the dialog left something behind")
	}
	if a.root.Modal() != nil {
		t.Error("the dialog is still on the modal stack")
	}
	if got := len(a.comp.Layers()); got != layersBefore {
		t.Errorf("%d layers, want it back to %d", got, layersBefore)
	}
}

// TestPaletteOpeningWhileOpenCloses checks the toggle: the command
// that shows the dialog hides it again, so its key is never dead.
func TestPaletteOpeningWhileOpenCloses(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette again: %v", err)
	}

	if a.palette != nil {
		t.Error("asking again left the dialog open")
	}
	if got := len(a.comp.Layers()); got != 0 {
		t.Errorf("%d layers, want none", got)
	}
	// And closing when it is already closed is harmless.
	a.closePalette()
}

// TestPaletteTakesKeysFromTheTerminal checks the routing the dialog
// needs: while it is open the shell must not receive what is typed.
func TestPaletteTakesKeysFromTheTerminal(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	handled, err := a.root.HandleKey(input.Event{Kind: input.Text, Rune: 'c', NormalText: true})

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled {
		t.Error("the dialog did not take the key")
	}
	if got := a.palette.Query(); got != "c" {
		t.Errorf("query = %q, want the key to have reached the dialog", got)
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the shell received %q while the dialog was open", got)
	}
}

// TestPaletteRunsACommandFromTheRegistry checks the point of the whole
// thing: every command the window registered is reachable by typing.
func TestPaletteRunsACommandFromTheRegistry(t *testing.T) {
	a := newTestApp(t, 60, 20)
	a.comp = render.NewCompositor(nil)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	// "Split right" by its word starts.
	for _, r := range "sr" {
		if _, err := a.root.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true}); err != nil {
			t.Fatalf("typing: %v", err)
		}
	}
	if got, ok := a.palette.Selected(); !ok || got.ID != "pane.splitRight" {
		t.Fatalf("selected %+v, want the split command", got)
	}
	if _, err := a.root.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	if a.palette != nil {
		t.Error("the dialog is still open after running a command")
	}
	// Splitting asks what goes in the half that opens up, and the line
	// it opens on is a shell here.
	takeFirstChoice(t, a)
	if len(a.panes) != 2 {
		t.Errorf("%d panes, want the split to have happened", len(a.panes))
	}
	checkTree(t, a)
}

// TestPaletteEscapeGivesFocusBack checks that closing the dialog puts
// typing back where it was.
func TestPaletteEscapeGivesFocusBack(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	if _, err := a.root.HandleKey(press(input.KeyEscape, 0)); err != nil {
		t.Fatalf("Escape: %v", err)
	}
	if _, err := a.root.HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true}); err != nil {
		t.Fatalf("typing after Escape: %v", err)
	}

	waitFor(t, a, "the shell to be sent what was typed", func() bool { return a.shells[0].sentText() == "x" })
	checkTree(t, a)
}

// TestAcceleratorReachesPastThePalette checks that a shortcut the dialog
// has no use for still works, so the window can be closed while it is
// open.
func TestAcceleratorReachesPastThePalette(t *testing.T) {
	a := newTestApp(t, 60, 20)
	a.comp = render.NewCompositor(nil)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	// Ctrl+Shift+D splits, and the dialog does not want it.
	if _, err := a.root.HandleKey(press(input.KeyD, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("accelerator: %v", err)
	}

	// Splitting asks what goes in the half that opens up.
	takeFirstChoice(t, a)
	if len(a.panes) != 2 {
		t.Errorf("%d panes, want the accelerator to have reached past the dialog", len(a.panes))
	}
}

// takeFirstChoice answers the chooser on the modal stack with its first
// line, which is what Enter on a freshly opened one does.
func takeFirstChoice(t *testing.T, a *testApp) {
	t.Helper()
	c, ok := a.root.Modal().(*ui.Chooser)
	if !ok {
		t.Fatalf("nothing is asking: the top modal is %T", a.root.Modal())
	}
	if _, err := c.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("taking the first line: %v", err)
	}
}

// TestPaletteLayerIsSeeThrough checks that the window is still visible
// behind the dialog. The layer is composited over the widget tree, and
// a background with any alpha would blank everything under it.
func TestPaletteLayerIsSeeThrough(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)

	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	if !a.modals[0].layer.Transparent {
		t.Error("the dialog's layer is not marked see-through")
	}
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			if got := a.modals[0].g.At(x, y); got.BG.A != 0 {
				t.Fatalf("cell %d,%d = %+v, want the layer clear before anything is drawn",
					x, y, got)
			}
		}
	}
}

// TestPaletteIsDrawnOntoItsLayer checks the dialog actually reaches a
// grid. Nothing else in the window would notice if it did not.
func TestPaletteIsDrawnOntoItsLayer(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	// What app.Draw does with the dialog.
	a.drawModals()

	found := false
	for y := 0; y < 10 && !found; y++ {
		for x := 0; x < 40; x++ {
			if a.modals[0].g.At(x, y).Rune == '>' {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("the dialog drew nothing onto its layer")
	}
}

// TestPaletteFollowsAResize checks that widening the window moves the
// dialog with it rather than leaving it measured for the old one.
func TestPaletteFollowsAResize(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	a.g = grid.New(40, 10, a.colours.FG, a.colours.BG)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	a.setGridSize(80, 20)

	cols, rows := a.modals[0].g.Size()
	if cols != a.lastSize[0] || rows != a.lastSize[1] {
		t.Errorf("the dialog's layer is %dx%d, want the window's %v", cols, rows, a.lastSize)
	}
}

// TestPaletteShortcutToggles checks that the key which opens the dialog
// closes it too, rather than being dead while it is up.
func TestPaletteShortcutToggles(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	a.commands()

	if _, err := a.root.HandleKey(press(input.KeyK, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("ctrl+shift+K: %v", err)
	}
	if a.palette == nil {
		t.Fatal("ctrl+shift+K did not open the dialog")
	}

	if _, err := a.root.HandleKey(press(input.KeyK, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("ctrl+shift+K again: %v", err)
	}
	if a.palette != nil {
		t.Error("ctrl+shift+K again did not close the dialog")
	}
}

// TestPaletteSurvivesAPaneExiting checks a shell ending on its own while
// the dialog is open. The tree changes underneath it, and the dialog is
// no part of that.
func TestPaletteSurvivesAPaneExiting(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	a.commands()
	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	// One of the two shells ends on its own.
	pane := ui.Leaves(a.root.Widget())[0].(*term.Terminal)
	_ = pane.Close()
	waitUntil(t, "the pane to have exited", pane.Exited)
	reapWhenTold(t, a)

	checkTree(t, a)
	if a.palette == nil {
		t.Error("the dialog closed when a pane it was not part of went away")
	}
	if a.root.Modal() != ui.Widget(a.palette) {
		t.Error("the dialog is no longer the top modal")
	}
}

// TestAppDrawPutsTheDialogOnItsLayer checks the window's own draw path,
// not just the dialog's. Nothing else notices if the modal is left out
// of it: the dialog is on a layer nothing else touches.
func TestAppDrawPutsTheDialogOnItsLayer(t *testing.T) {
	atlas, err := glyph.NewAtlas(glyph.Fonts{Regular: gomono.TTF}, 12, 96)
	if err != nil {
		t.Fatalf("no atlas: %v", err)
	}
	a := newTestApp(t, 40, 10)
	a.renderer = render.New(atlas)
	a.comp = render.NewCompositor(a.renderer)
	a.g = grid.New(40, 10, a.colours.FG, a.colours.BG)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.commands()
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	a.Draw(ebiten.NewImage(320, 160))

	found := false
	for y := 0; y < 10 && !found; y++ {
		for x := 0; x < 40; x++ {
			if a.modals[0].g.At(x, y).Rune == '>' {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("the window drew a frame without the dialog on it")
	}
}

// TestResizeReachesTheWidgetTree checks that a new window size tells the
// panes, not just the grids. Without it every shell keeps the size it
// had and wraps its output at the wrong column.
func TestResizeReachesTheWidgetTree(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.g = grid.New(40, 10, a.colours.FG, a.colours.BG)
	pane := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)
	if got := pane.Size(); got != (ui.Size{Cols: 40, Rows: 10}) {
		t.Fatalf("the pane starts at %+v, want 40x10", got)
	}

	a.setGridSize(80, 20)

	if got := pane.Size(); got != (ui.Size{Cols: 80, Rows: 20}) {
		t.Errorf("the pane has %+v after the window changed, want 80x20", got)
	}
	if got := a.shells[0].lastSize(); got != [2]int{80, 20} {
		t.Errorf("the shell was told %v, want 80x20", got)
	}
}

// A container that will not take a split leaves no shell behind.
//
// A file pane's parent is the file manager, which holds panes and
// nothing else. Splitting one used to build the terminal, fail to put it
// in the tree, and leave it running where nobody could see or close it.
func TestASplitThatCannotBePlacedLeavesNoShell(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("a file pane: %v", err)
	}
	a.focus(a.files.view.Panes()[0])
	was := len(a.panes)

	err := a.splitHere(ui.Columns)
	if err == nil {
		t.Fatal("splitting a file pane reported nothing")
	}
	if got := len(a.panes); got != was {
		t.Fatalf("%d panes after the refusal, want the %d there were", got, was)
	}
	checkTree(t, a)
}

// The sidebar forgets the pane in front when it goes, even while nobody
// is looking at the sidebar.
//
// The rows are only rebuilt while the sidebar is open, so a pane closed
// while it is hidden would be held by the window until it was opened
// again.
func TestClosingAPaneForgetsItEvenWithTheSidebarHidden(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.openTab(); err != nil {
		t.Fatalf("openTab: %v", err)
	}
	a.refreshPanel(panelNow)
	if a.shown == nil {
		t.Fatal("the sidebar is not following the stage, so this proves nothing")
	}

	if err := a.showPanel(false); err != nil {
		t.Fatalf("hide the sidebar: %v", err)
	}
	if err := a.closeFocused(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if a.shown != nil {
		t.Fatal("the window is still holding the pane that was closed")
	}
}

// Ctrl+K reaches the shell, because readline's kill-to-end-of-line is
// in a lot of people's fingers.
//
// An accelerator runs before any widget sees the key, so a window that
// took Ctrl+K for itself took it away from every shell in it.
func TestCtrlKReachesTheShell(t *testing.T) {
	a := newTestApp(t, 60, 20)
	a.comp = render.NewCompositor(nil)
	a.commands()

	if _, err := a.root.HandleKey(press(input.KeyK, input.ModCtrl)); err != nil {
		t.Fatalf("ctrl+K: %v", err)
	}
	if a.palette != nil {
		t.Fatal("ctrl+K opened the palette instead of reaching the shell")
	}
	waitFor(t, a, "the shell to be sent the chord", func() bool { return a.shells[0].sentText() == "\x0b" })
}

// A clipboard that cannot be read says so, rather than pasting nothing.
//
// A paste that quietly does nothing looks exactly like an empty
// clipboard: the user tries again, and again, and is never told that
// xclip is not installed.
func TestAClipboardThatCannotBeReadSaysSo(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withDialogs(t, a)
	a.commands()

	boom := errors.New("xclip is not installed")
	a.readClip = func() (string, error) { return "", boom }

	if _, err := a.root.HandleKey(press(input.KeyV, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("the paste chord: %v", err)
	}

	n, ok := a.root.Modal().(*ui.Notice)
	if !ok {
		t.Fatalf("the failure showed %T, want a dialog", a.root.Modal())
	}
	if !strings.Contains(n.Message(), boom.Error()) {
		t.Fatalf("the dialog says %q", n.Message())
	}
	// And nothing was typed into the shell.
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the shell was sent %q", got)
	}
}

// And one that can be read is pasted.
func TestAClipboardThatCanBeReadIsPasted(t *testing.T) {
	a := newTestApp(t, 60, 20)
	withDialogs(t, a)
	a.commands()
	a.readClip = func() (string, error) { return "uptime", nil }

	if _, err := a.root.HandleKey(press(input.KeyV, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("the paste chord: %v", err)
	}
	if a.root.Modal() != nil {
		t.Fatalf("a paste that worked showed %T", a.root.Modal())
	}
	waitFor(t, a, "the shell to be sent what was typed", func() bool { return a.shells[0].sentText() == "uptime" })
}
