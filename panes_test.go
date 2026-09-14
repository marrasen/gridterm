package main

import (
	"io"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// pipeSession is a shell that reads nothing and writes nowhere, with an
// end the test can pull.
type pipeSession struct {
	mu     sync.Mutex
	out    chan []byte
	closed bool
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

func (p *pipeSession) Write(b []byte) (int, error) { return len(b), nil }
func (p *pipeSession) Resize(int, int) error       { return nil }
func (p *pipeSession) Wait() error                 { return nil }

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
		palette:    vt.DefaultPalette(),
		scrollback: 64,
		panes:      make(map[*term.Terminal]struct{}),
		exits:      make(chan struct{}, exitQueue),
		lastSize:   [2]int{cols, rows},
	}}
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
	focused := 0
	for p := range inTree {
		if p.Focused() {
			focused++
		}
	}
	if focused != 1 {
		t.Fatalf("%d panes believe they have focus, want 1", focused)
	}
	if !pane.Focused() {
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
