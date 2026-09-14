package main

import (
	"io"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/gofont/gomono"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// pipeSession is a shell that reads nothing and writes nowhere, with an
// end the test can pull.
type pipeSession struct {
	mu      sync.Mutex
	out     chan []byte
	written []byte
	size    [2]int
	closed  bool
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
	return len(b), nil
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
	return nil
}

// newTestApp builds an app with one pane and no window, which is enough
// for every tree operation.
type testApp struct {
	*app
	// shells are the fake sessions, in the order the panes were made.
	shells []*pipeSession
}

func newTestApp(t *testing.T, cols, rows int) *testApp {
	t.Helper()
	ta := &testApp{app: &app{
		fontSize:   defaultFontSize,
		colours:    vt.DefaultPalette(),
		scrollback: 64,
		panes:      make(map[*term.Terminal]struct{}),
		exits:      make(chan struct{}, exitQueue),
		lastSize:   [2]int{cols, rows},
	}}
	// The window's own grid, so markDirty and setGridSize do what they do
	// in the program rather than nothing at all.
	ta.g = grid.New(cols, rows, ta.colours.FG, ta.colours.BG)
	ta.newSession = func(int, int) (session.Session, error) {
		sess := newPipeSession()
		ta.shells = append(ta.shells, sess)
		return sess, nil
	}
	first, err := ta.newTerminal()
	if err != nil {
		t.Fatalf("first pane: %v", err)
	}
	ta.root.SetWidget(first)
	ta.root.Layout(ui.Rect{Cols: cols, Rows: rows})
	t.Cleanup(func() {
		for pane := range ta.panes {
			_ = pane.Close()
		}
	})
	return ta
}

// setTitle makes one pane's program name the window.
func (ta *testApp) setTitle(t *testing.T, which int, pane *term.Terminal, title string) {
	t.Helper()
	ta.shells[which].out <- []byte("\x1b]0;" + title + "\x07")
	waitUntil(t, func() bool { return pane.Title() == title })
}

// checkTree asserts what must always hold: the panes the app knows about
// and the panes in the tree are the same set, and focus is on one of
// them.
func checkTree(t *testing.T, a *testApp) {
	t.Helper()
	inTree := map[*term.Terminal]bool{}
	for _, leaf := range ui.Leaves(a.root.Widget()) {
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
	if len(a.panes) == 0 {
		return
	}
	leaf := ui.FocusedLeaf(a.root.Widget())
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
		waitUntil(t, pane.Exited)
	}
}

func TestSplitAndClose(t *testing.T) {
	a := newTestApp(t, 40, 10)

	if err := a.splitFocused(ui.Columns); err != nil {
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
	if err := a.splitFocused(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	if err := a.splitFocused(ui.Rows); err != nil {
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
	inner, ok := a.root.Widget().(*ui.Split)
	if !ok || inner.Dir() != ui.Rows {
		t.Errorf("root = %T, want the inner split promoted", a.root.Widget())
	}
}

func TestFocusCyclesThroughEveryPane(t *testing.T) {
	a := newTestApp(t, 60, 20)
	for i := 0; i < 2; i++ {
		if err := a.splitFocused(ui.Columns); err != nil {
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

	err := a.splitFocused(ui.Columns)

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
			if err := a.splitFocused(ui.Columns); err != nil {
				t.Fatalf("split: %v", err)
			}
		}
		panes := ui.Leaves(a.root.Widget())
		survivor := panes[1].(*term.Terminal)

		// Two shells end at once.
		for _, p := range []ui.Widget{panes[0], panes[2]} {
			pane := p.(*term.Terminal)
			_ = pane.Close()
			waitUntil(t, pane.Exited)
		}
		a.reapExited()

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
	waitUntil(t, only.Exited)
	a.reapExited()

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
			_ = a.splitFocused(dir)
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

// press builds a key press, the way the ui tests do.
func press(k input.Key, mods input.Mods) input.Event {
	return input.Event{Kind: input.KeyPress, Key: k, Mods: mods}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting")
}

// TestSplitRoomBoundary pins where splitting stops being possible: one
// cell each side and one for the divider.
func TestSplitRoomBoundary(t *testing.T) {
	for _, tc := range []struct {
		cols  int
		allow bool
	}{{cols: 2, allow: false}, {cols: 3, allow: true}} {
		a := newTestApp(t, tc.cols, 4)

		err := a.splitFocused(ui.Columns)

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
	if err := a.splitFocused(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	hidden := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)
	if got := hidden.Size().Cols; got < 3 {
		t.Fatalf("the pane starts %d columns wide, too narrow for the test", got)
	}

	// Narrow the window until there is no room for the second pane.
	a.lastSize = [2]int{2, 10}
	a.root.Layout(ui.Rect{Cols: 2, Rows: 10})

	if err := a.splitFocused(ui.Columns); err == nil {
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
	if err := a.splitFocused(ui.Columns); err != nil {
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
	case <-time.After(2 * time.Second):
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
	strip, ok := a.root.Widget().(*ui.Tabs)
	if !ok {
		t.Fatalf("root = %T, want one tab strip", a.root.Widget())
	}
	if got := len(strip.Children()); got != 3 {
		t.Errorf("%d tabs, want 3", got)
	}
}

// TestTabsAndSplitsNest checks the two containers working together: a
// tab strip inside one half of a split.
func TestTabsAndSplitsNest(t *testing.T) {
	a := newTestApp(t, 60, 20)
	if err := a.splitFocused(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	if err := a.openTab(); err != nil {
		t.Fatalf("open tab: %v", err)
	}

	checkTree(t, a)
	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want 3", len(a.panes))
	}
	split, ok := a.root.Widget().(*ui.Split)
	if !ok {
		t.Fatalf("root = %T, want the split still on top", a.root.Widget())
	}
	if _, ok := split.Children()[1].(*ui.Tabs); !ok {
		t.Errorf("the second half is %T, want a tab strip", split.Children()[1])
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

// TestClosingTabsCollapsesTheStrip checks the whole life of a strip:
// carries on, collapses to its last tab, then goes with it.
func TestClosingTabsCollapsesTheStrip(t *testing.T) {
	a := newTestApp(t, 40, 10)
	for i := 0; i < 2; i++ {
		if err := a.openTab(); err != nil {
			t.Fatalf("open tab: %v", err)
		}
	}

	if err := a.closeFocused(); err != nil {
		t.Fatalf("close: %v", err)
	}
	checkTree(t, a)
	if _, ok := a.root.Widget().(*ui.Tabs); !ok {
		t.Errorf("root = %T, want the strip carrying on with two tabs", a.root.Widget())
	}

	if err := a.closeFocused(); err != nil {
		t.Fatalf("close: %v", err)
	}
	checkTree(t, a)
	if _, ok := a.root.Widget().(*term.Terminal); !ok {
		t.Errorf("root = %T, want the strip collapsed into its last tab", a.root.Widget())
	}

	if err := a.closeFocused(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !a.quit.Load() {
		t.Error("closing the last tab did not close the window")
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
			_ = a.splitFocused(dir)
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
	if err := a.splitFocused(ui.Columns); err != nil {
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
	if err := a.splitFocused(ui.Columns); err != nil {
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

	waitUntil(t, func() bool { return a.shells[0].sentText() == "x" })
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

	if len(a.panes) != 2 {
		t.Errorf("%d panes, want the accelerator to have reached past the dialog", len(a.panes))
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

	if _, err := a.root.HandleKey(press(input.KeyK, input.ModCtrl)); err != nil {
		t.Fatalf("ctrl+K: %v", err)
	}
	if a.palette == nil {
		t.Fatal("ctrl+K did not open the dialog")
	}

	if _, err := a.root.HandleKey(press(input.KeyK, input.ModCtrl)); err != nil {
		t.Fatalf("ctrl+K again: %v", err)
	}
	if a.palette != nil {
		t.Error("ctrl+K again did not close the dialog")
	}
}

// TestPaletteSurvivesAPaneExiting checks a shell ending on its own while
// the dialog is open. The tree changes underneath it, and the dialog is
// no part of that.
func TestPaletteSurvivesAPaneExiting(t *testing.T) {
	a := newTestApp(t, 40, 10)
	a.comp = render.NewCompositor(nil)
	a.commands()
	if err := a.splitFocused(ui.Columns); err != nil {
		t.Fatalf("split: %v", err)
	}
	if err := a.openPalette(); err != nil {
		t.Fatalf("open palette: %v", err)
	}

	// One of the two shells ends on its own.
	pane := ui.Leaves(a.root.Widget())[0].(*term.Terminal)
	_ = pane.Close()
	waitUntil(t, pane.Exited)
	a.reapExited()

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
		t.Skipf("no atlas: %v", err)
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
