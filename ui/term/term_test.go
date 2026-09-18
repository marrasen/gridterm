package term

import (
	"errors"
	"image/color"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vt"
)

// fakeSession is a session driven by the test: what it hands to the
// terminal, and what the terminal sent back.
type fakeSession struct {
	mu      sync.Mutex
	out     chan []byte // bytes the terminal will read
	written []byte      // bytes the terminal wrote
	sizes   [][2]int
	closed  bool

	resizeErr error
	writeErr  error
	closeErr  error
}

func newFakeSession() *fakeSession {
	return &fakeSession{out: make(chan []byte, 16)}
}

func (f *fakeSession) Read(p []byte) (int, error) {
	b, ok := <-f.out
	if !ok {
		return 0, io.EOF
	}
	return copy(p, b), nil
}

func (f *fakeSession) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.written = append(f.written, p...)
	return len(p), nil
}

func (f *fakeSession) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, [2]int{cols, rows})
	return f.resizeErr
}

func (f *fakeSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.out)
	}
	return f.closeErr
}

func (f *fakeSession) Wait() error { return nil }

// setWriteErr makes every write from now on fail.
func (f *fakeSession) setWriteErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeErr = err
}

// isClosed reports whether the terminal has hung this session up.
func (f *fakeSession) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// feed hands bytes to the terminal and waits for it to parse them.
//
// The dirty flag is cleared first: Layout sets it too, so waiting on it
// without clearing would return before the bytes had been read.
func (f *fakeSession) feed(t *testing.T, term *Terminal, s string) {
	t.Helper()
	term.pending.Store(false)
	f.out <- []byte(s)
	waitFor(t, term.Dirty)
}

func (f *fakeSession) sentText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.written)
}

func (f *fakeSession) sizeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sizes)
}

func (f *fakeSession) lastSize() [2]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sizes) == 0 {
		return [2]int{}
	}
	return f.sizes[len(f.sizes)-1]
}

// waitFor spins until cond holds. The terminal moves bytes on its own
// goroutines, so a test has to wait for them rather than assume.
func waitFor(t *testing.T, cond func() bool) {
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

// newTestTerm builds a terminal of the given size on a fake session.
func newTestTerm(t *testing.T, cols, rows int, cfg Config) (*Terminal, *fakeSession) {
	t.Helper()
	f := newFakeSession()
	cfg.Session = f
	term, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	term.Layout(ui.Size{Cols: cols, Rows: rows})
	return term, f
}

// draw paints the terminal into a fresh grid and returns it.
func draw(term *Terminal, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	term.Draw(g.View())
	return g
}

func rowText(g *grid.Grid, y int) string {
	cols, _ := g.Size()
	var b strings.Builder
	for x := range cols {
		c := g.At(x, y)
		if c.Width == 0 {
			continue
		}
		b.WriteRune(c.Rune)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestNewRejectsNoSession(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("a terminal with no session was accepted")
	}
}

func TestOutputReachesTheGrid(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})

	f.feed(t, term, "hello")
	g := draw(term, 20, 4)

	if got := rowText(g, 0); got != "hello" {
		t.Errorf("row 0 = %q, want %q", got, "hello")
	}
}

// TestDrawKeepsDamageTracking checks that copying the widget's grid into
// the view does not dirty rows that did not change. Getting this wrong
// makes every frame a full repaint.
func TestDrawKeepsDamageTracking(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "one\r\ntwo")
	g := grid.New(20, 4, color.RGBA{}, color.RGBA{})
	term.Draw(g.View())
	g.ClearDirty()

	// Nothing changed, so nothing may be dirtied.
	term.Draw(g.View())
	if g.AnyDirty() {
		t.Fatal("an unchanged repaint dirtied the grid")
	}

	f.feed(t, term, "\r\nthree")
	term.Draw(g.View())

	if !g.RowDirty(2) {
		t.Error("the changed row was not dirtied")
	}
	if g.RowDirty(0) {
		t.Error("an unchanged row was dirtied, so damage tracking is off")
	}
}

func TestKeysReachTheSession(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})

	handled, err := term.HandleKey(input.Event{
		Kind: input.Text, Rune: 'a', NormalText: true,
	})

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled {
		t.Error("a text event was not consumed")
	}
	waitFor(t, func() bool { return f.sentText() == "a" })
}

// TestKeyWithNoEncodingIsNotConsumed checks that a key the terminal has
// no bytes for travels on, rather than being swallowed.
func TestKeyWithNoEncodingIsNotConsumed(t *testing.T) {
	term, _ := newTestTerm(t, 20, 4, Config{})

	handled, err := term.HandleKey(input.Event{Kind: input.KeyRelease, Key: input.KeyA})

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if handled {
		t.Error("a key release was consumed, so nothing above could ever see one")
	}
}

func TestLayoutResizesTheSession(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	before := f.sizeCount()

	term.Layout(ui.Size{Cols: 40, Rows: 10})

	if got := f.lastSize(); got != [2]int{40, 10} {
		t.Errorf("session size = %v, want 40x10", got)
	}
	if f.sizeCount() != before+1 {
		t.Errorf("resize count = %d, want one more than %d", f.sizeCount(), before)
	}
	if got := term.Size(); got != (ui.Size{Cols: 40, Rows: 10}) {
		t.Errorf("Size() = %+v, want 40x10", got)
	}
}

// TestLayoutIgnoresTheSameSize checks that dragging a window without
// crossing a cell boundary does not resize the shell over and over.
func TestLayoutIgnoresTheSameSize(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	before := f.sizeCount()

	term.Layout(ui.Size{Cols: 20, Rows: 4})
	term.Layout(ui.Size{Cols: 20, Rows: 4})

	if f.sizeCount() != before {
		t.Errorf("resize count = %d, want it unchanged at %d", f.sizeCount(), before)
	}
}

// TestLayoutClampsToOneCell checks the degenerate size a layout produces
// while it settles. An emulator with no columns has nowhere to put the
// cursor.
func TestLayoutClampsToOneCell(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})

	term.Layout(ui.Size{})

	if got := f.lastSize(); got != [2]int{1, 1} {
		t.Errorf("session size = %v, want 1x1", got)
	}
}

func TestResizeFailureIsReported(t *testing.T) {
	boom := errors.New("resize refused")
	f := newFakeSession()
	f.resizeErr = boom
	var got error
	term, err := New(Config{Session: f, OnError: func(e error) { got = e }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	term.Layout(ui.Size{Cols: 10, Rows: 4})

	if !errors.Is(got, boom) {
		t.Errorf("reported %v, want the session's own error", got)
	}
	// A remote session hands back the failure of an earlier drag, so the
	// line must not read as though this drag failed.
	if !strings.Contains(got.Error(), "an earlier resize of this pane failed") {
		t.Errorf("reported %v, want it to say the failure is an earlier one", got)
	}
}

// lateSession reports a resize failure after the drag, the way a remote
// session does: the window-change goes out on a goroutine.
type lateSession struct {
	*fakeSession
	report func(error)
}

func (l *lateSession) ReportLate(report func(error)) { l.report = report }

// A resize that fails after the drag is reported when it fails.
//
// The failure has no caller left to go back to. Kept for the next
// Resize, it waits on a size change that may never come.
func TestALateResizeFailureIsReported(t *testing.T) {
	boom := errors.New("the wire is broken")
	f := newFakeSession()
	late := &lateSession{fakeSession: f}
	var got error
	term, err := New(Config{Session: late, OnError: func(e error) { got = e }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	if late.report == nil {
		t.Fatal("the terminal took no hook for a failure that lands late")
	}
	late.report(boom)

	if !errors.Is(got, boom) {
		t.Errorf("reported %v, want the session's own error", got)
	}
	if !strings.Contains(got.Error(), "an earlier resize of this pane failed") {
		t.Errorf("reported %v, want it to say the failure is an earlier one", got)
	}
}

// TestOnlyTheFocusedTerminalWritesTheCursor checks the rule a shared
// grid needs. One cursor, no owner, so an unfocused terminal must leave
// it alone entirely -- writing a hidden cursor takes it from whoever has
// it just as surely as writing a visible one.
func TestOnlyTheFocusedTerminalWritesTheCursor(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "hi")
	host := grid.New(20, 4, color.RGBA{}, color.RGBA{})

	term.SetFocus(true)
	term.Draw(host.View())
	focused := host.Cursor()
	if !focused.Visible {
		t.Fatal("a focused terminal drew no cursor")
	}

	term.SetFocus(false)
	term.Draw(host.View())

	if host.Cursor() != focused {
		t.Errorf("cursor = %+v, want it untouched at %+v", host.Cursor(), focused)
	}
}

// TestTwoTerminalsOnOneGridInEitherOrder checks what a split pane will
// do: whichever is drawn last must not be able to take the cursor from
// the focused one.
func TestTwoTerminalsOnOneGridInEitherOrder(t *testing.T) {
	for _, tc := range []struct {
		name        string
		focusedLast bool
	}{
		{name: "focused drawn first", focusedLast: false},
		{name: "focused drawn last", focusedLast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left, lf := newTestTerm(t, 10, 4, Config{})
			right, _ := newTestTerm(t, 10, 4, Config{})
			lf.feed(t, left, "hi")
			left.SetFocus(true)
			right.SetFocus(false)

			host := grid.New(20, 4, color.RGBA{}, color.RGBA{})
			// The container clears the cursor once, then draws children.
			host.SetCursor(grid.Cursor{})
			draws := []func(){
				func() { left.Draw(host.View().Sub(0, 0, 10, 4)) },
				func() { right.Draw(host.View().Sub(10, 0, 10, 4)) },
			}
			if tc.focusedLast {
				draws[0], draws[1] = draws[1], draws[0]
			}
			for _, d := range draws {
				d()
			}

			cur := host.Cursor()
			if !cur.Visible {
				t.Fatal("the focused pane's cursor was taken by the unfocused one")
			}
			if cur.X >= 10 {
				t.Errorf("cursor at %d, want it in the left pane", cur.X)
			}
		})
	}
}

// TestExitTellsTheHostGoingAndThenTheStatus is the contract OnExit
// carries: the program going is reported at once, and its status when
// that lands. Twice, and no more.
func TestExitTellsTheHostGoingAndThenTheStatus(t *testing.T) {
	f := newFakeSession()
	// OnExit arrives from the goroutine reading the session, so counting
	// it needs a lock of its own.
	var exits atomic.Int32
	term, err := New(Config{Session: f, OnExit: func() { exits.Add(1) }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_ = f.Close() // the shell goes
	waitFor(t, term.Exited)
	waitFor(t, func() bool { _, over := term.Ending(); return over })
	time.Sleep(20 * time.Millisecond)

	if got := exits.Load(); got != 2 {
		t.Errorf("OnExit called %d times, want the one for the program going"+
			" and the one for its status", got)
	}
	if err := term.Close(); err != nil {
		t.Errorf("Close after exit: %v", err)
	}
}

func TestCloseTwiceIsSafe(t *testing.T) {
	term, _ := newTestTerm(t, 20, 4, Config{})

	if err := term.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	// Sending after Close must not panic on the closed channel.
	term.Paste("ignored")
}

func TestCopyAndPaste(t *testing.T) {
	var copied string
	term, f := newTestTerm(t, 20, 4, Config{
		WriteClipboard: func(s string) { copied = s },
		ReadClipboard:  func() string { return "pasted" },
	})
	f.feed(t, term, "hello")
	// Draw so the widget's grid holds the text the selection covers.
	draw(term, 20, 4)

	term.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft})
	term.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 4})
	term.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 4})

	if !term.Copy() {
		t.Fatal("Copy reported nothing selected")
	}
	if copied != "hello" {
		t.Errorf("copied %q, want %q", copied, "hello")
	}

	term.PasteClipboard()
	waitFor(t, func() bool { return strings.Contains(f.sentText(), "pasted") })
}

// TestCopyWithNoSelection checks that copy is a no-op rather than
// clearing the clipboard.
func TestCopyWithNoSelection(t *testing.T) {
	calls := 0
	term, _ := newTestTerm(t, 20, 4, Config{
		WriteClipboard: func(string) { calls++ },
	})

	if term.Copy() {
		t.Error("Copy reported success with nothing selected")
	}
	if calls != 0 {
		t.Error("Copy wrote to the clipboard with nothing selected")
	}
}

// TestClickWithoutDraggingClearsTheSelection checks that a plain click
// is a click, not a one-cell highlight left behind.
func TestClickWithoutDraggingClearsTheSelection(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "hello")
	draw(term, 20, 4)

	term.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 2})
	term.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 2})

	if term.Copy() {
		t.Error("a click left a selection behind")
	}
}

// TestMouseDragNeedsAPressFirst checks that motion arriving without a
// press does not start a selection, which is what stops the mouse
// painting a selection as it crosses the window.
func TestMouseDragNeedsAPressFirst(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "hello")
	draw(term, 20, 4)

	handled, err := term.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: 3,
	})

	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if handled {
		t.Error("motion with no press behind it was consumed")
	}
	if term.Copy() {
		t.Error("motion with no press behind it started a selection")
	}
}

func TestTitleReachesTheHost(t *testing.T) {
	var got string
	var mu sync.Mutex
	term, f := newTestTerm(t, 20, 4, Config{
		OnTitle: func(s string) { mu.Lock(); got = s; mu.Unlock() },
	})

	f.feed(t, term, "\x1b]0;my title\x07")

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return got == "my title"
	})
	_ = term
}

// TestScrollView moves through the scrollback and back.
func TestScrollView(t *testing.T) {
	term, f := newTestTerm(t, 20, 3, Config{})
	f.feed(t, term, "one\r\ntwo\r\nthree\r\nfour\r\nfive")

	term.ScrollView(2)
	g := draw(term, 20, 3)
	if got := rowText(g, 0); got != "one" {
		t.Errorf("after scrolling back, row 0 = %q, want %q", got, "one")
	}

	// Typing jumps back to the live screen.
	term.HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true})
	g = draw(term, 20, 3)
	if got := rowText(g, 2); got != "five" {
		t.Errorf("after typing, row 2 = %q, want %q", got, "five")
	}
}

// TestWriteFailureIsReportedAndEndsTheTerminal checks that a broken pipe
// is not swallowed: the host is told and the terminal reports itself
// gone rather than silently dropping everything typed into it.
func TestWriteFailureIsReportedAndEndsTheTerminal(t *testing.T) {
	boom := errors.New("broken pipe")
	f := newFakeSession()
	f.writeErr = boom
	var mu sync.Mutex
	var got error
	term, err := New(Config{
		Session: f,
		OnError: func(e error) { mu.Lock(); got = e; mu.Unlock() },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	term.Layout(ui.Size{Cols: 20, Rows: 4})

	term.HandleKey(input.Event{Kind: input.Text, Rune: 'a', NormalText: true})

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return got != nil
	})
	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(got, boom) {
		t.Errorf("reported %v, want the session's own error", got)
	}
	if !term.Exited() {
		t.Error("the terminal did not report itself gone after a write failure")
	}
}

// TestTerminalIsAWidget checks the interfaces the toolkit routes on.
func TestTerminalIsAWidget(t *testing.T) {
	term, _ := newTestTerm(t, 20, 4, Config{})

	var w ui.Widget = term
	if _, ok := w.(ui.KeyHandler); !ok {
		t.Error("the terminal does not take keys")
	}
	if _, ok := w.(ui.MouseHandler); !ok {
		t.Error("the terminal does not take mouse events")
	}
	if _, ok := w.(ui.Focusable); !ok {
		t.Error("the terminal does not track focus")
	}
}

// TestSelectionIsVisibleInTheDrawnGrid checks the highlight reaches the
// grid being drawn into. The selection lives on the widget's own grid,
// and the grid the layout hands it knows nothing about it, so Draw has
// to resolve the colour rather than carry the selection over.
func TestSelectionIsVisibleInTheDrawnGrid(t *testing.T) {
	pal := vt.DefaultPalette()
	term, f := newTestTerm(t, 20, 4, Config{Palette: &pal})
	f.feed(t, term, "hello")
	host := grid.New(20, 4, pal.FG, pal.BG)
	term.Draw(host.View())

	term.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft})
	term.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 4})
	term.Draw(host.View())

	if got := host.BGOf(2, 0); got != pal.Selection {
		t.Errorf("cell 2,0 background = %v, want the selection colour %v", got, pal.Selection)
	}
	if got := host.BGOf(10, 0); got == pal.Selection {
		t.Error("a cell outside the selection was highlighted")
	}
}

// TestReverseVideoSurvivesTheCopy checks SGR 7 still shows after the
// copy into the layout's grid, both halves of it. The colours are
// resolved during the copy and the attribute cleared, so leaving either
// step out swaps them twice or not at all.
func TestReverseVideoSurvivesTheCopy(t *testing.T) {
	pal := vt.DefaultPalette()
	term, f := newTestTerm(t, 20, 4, Config{Palette: &pal})

	f.feed(t, term, "\x1b[7mX\x1b[0m")
	host := grid.New(20, 4, pal.FG, pal.BG)
	term.Draw(host.View())

	if got := host.BGOf(0, 0); got != pal.FG {
		t.Errorf("reversed cell background = %v, want the foreground %v", got, pal.FG)
	}
	// Both halves, or reversed text ends up drawn on itself.
	if got := host.FGOf(0, 0); got != pal.BG {
		t.Errorf("reversed cell foreground = %v, want the background %v", got, pal.BG)
	}
}

// TestTypingClearsTheSelection checks that typing replaces a selection,
// as it does everywhere else.
func TestTypingClearsTheSelection(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{WriteClipboard: func(string) {}})
	f.feed(t, term, "hello")
	draw(term, 20, 4)
	term.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft})
	term.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 4})
	if !term.Copy() {
		t.Fatal("the drag selected nothing")
	}

	term.HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true})

	if term.Copy() {
		t.Error("typing left the selection behind")
	}
}

// TestShiftOverridesMouseReporting checks the xterm convention: a
// program that has taken the mouse still lets you select text while
// Shift is held.
func TestShiftOverridesMouseReporting(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{WriteClipboard: func(string) {}})
	// Turn on mouse click reporting, then print something to select.
	f.feed(t, term, "\x1b[?1000hhello")
	draw(term, 20, 4)
	before := len(f.sentText())

	// Without shift the program gets the report and no selection starts.
	term.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft})
	term.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 4})
	waitFor(t, func() bool { return len(f.sentText()) > before })
	if term.Copy() {
		t.Error("a drag started a selection while the program owned the mouse")
	}

	// With shift held the selection works and the program hears nothing.
	sent := len(f.sentText())
	term.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Mods: input.ModShift,
	})
	term.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: 4, Mods: input.ModShift,
	})

	if !term.Copy() {
		t.Error("shift did not override mouse reporting, so text cannot be selected")
	}
	if len(f.sentText()) != sent {
		t.Error("a shift-drag was also reported to the program")
	}
}

// TestMouseReportingReachesTheProgram checks the other half: without
// shift, the program is told about the click.
func TestMouseReportingReachesTheProgram(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "\x1b[?1000h")

	term.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 3, Row: 1,
	})

	waitFor(t, func() bool { return strings.Contains(f.sentText(), "\x1b[M") })
}

// TestPasteIsBracketedWhenAsked checks that a program which turned
// bracketed paste on gets the markers, so it can tell pasted text from
// typing.
func TestPasteIsBracketedWhenAsked(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})

	term.Paste("plain")
	waitFor(t, func() bool { return strings.Contains(f.sentText(), "plain") })
	if strings.Contains(f.sentText(), "\x1b[200~") {
		t.Error("paste was bracketed before the program asked for it")
	}

	f.feed(t, term, "\x1b[?2004h")
	term.Paste("wrapped")

	waitFor(t, func() bool {
		return strings.Contains(f.sentText(), "\x1b[200~wrapped\x1b[201~")
	})
}

// TestCloseReportsTheSessionFailure checks the repo rule: an error from
// the session on the way out is handed back, not dropped.
func TestCloseReportsTheSessionFailure(t *testing.T) {
	boom := errors.New("hangup failed")
	f := newFakeSession()
	f.closeErr = boom
	term, err := New(Config{Session: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := term.Close(); !errors.Is(err, boom) {
		t.Errorf("Close returned %v, want the session's own error", err)
	}
}

// TestExitIsReportedOnceAcrossBothGoroutines checks the claim the swap
// makes: the reader and the writer can both find the session gone, and
// the host must still be told once about it going and once about the
// status, however many goroutines noticed.
func TestExitIsReportedOnceAcrossBothGoroutines(t *testing.T) {
	boom := errors.New("broken pipe")
	f := newFakeSession()
	f.writeErr = boom
	var exits atomic.Int32
	term, err := New(Config{
		Session: f,
		OnExit:  func() { exits.Add(1) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	// The writer fails on this, and the reader sees EOF at the same time.
	term.Paste("x")
	_ = f.Close()

	waitFor(t, term.Exited)
	waitFor(t, func() bool { _, over := term.Ending(); return over })
	time.Sleep(50 * time.Millisecond)
	if got := exits.Load(); got != 2 {
		t.Errorf("OnExit called %d times, want the one for the program going"+
			" and the one for its status", got)
	}
}

// TestSendAfterCloseDoesNotPanic checks the shutdown path: a device
// report can be produced by the reader at any moment, including while
// the window is closing.
func TestSendAfterCloseDoesNotPanic(t *testing.T) {
	for range 200 {
		f := newFakeSession()
		term, err := New(Config{Session: f})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); term.Paste("report") }()
		go func() { defer wg.Done(); _ = term.Close() }()
		wg.Wait()
	}
}

// TestOutputBeforeLayoutIsNotDestroyed checks that a shell banner
// arriving before the first Layout is parsed at the real width. Parsed
// one column wide it would be reflowed into nonsense.
func TestOutputBeforeLayoutIsNotDestroyed(t *testing.T) {
	f := newFakeSession()
	term, err := New(Config{Session: f, Size: ui.Size{Cols: 40, Rows: 4}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	f.feed(t, term, "user@host:~$ ")
	term.Layout(ui.Size{Cols: 40, Rows: 4})
	g := draw(term, 40, 4)

	if got := rowText(g, 0); got != "user@host:~$" {
		t.Errorf("row 0 = %q, want the banner at its real width", got)
	}
}

// TestLayoutToZeroStillResizes checks a genuine zero-sized layout is
// acted on rather than mistaken for never having been laid out.
func TestLayoutToZeroStillResizes(t *testing.T) {
	f := newFakeSession()
	term, err := New(Config{Session: f, Size: ui.Size{Cols: 10, Rows: 4}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	term.Layout(ui.Size{})

	if got := f.lastSize(); got != [2]int{1, 1} {
		t.Errorf("session size = %v, want 1x1", got)
	}
}

// TestTwoTerminalsInASplit is the whole point of splitting: two shells
// on one grid, each drawn in its own half, each typed into separately.
func TestTwoTerminalsInASplit(t *testing.T) {
	pal := vt.DefaultPalette()
	left, lf := newTestTerm(t, 1, 1, Config{Palette: &pal})
	right, rf := newTestTerm(t, 1, 1, Config{Palette: &pal})

	split := ui.NewSplit(ui.Columns, left, right)
	split.DividerFG = pal.FG
	var root ui.Root
	root.SetWidget(split)
	root.Layout(ui.Rect{Cols: 11, Rows: 2})

	lf.feed(t, left, "LL")
	rf.feed(t, right, "RR")
	host := grid.New(11, 2, pal.FG, pal.BG)
	root.Draw(host.View())

	if got := rowText(host, 0); got != "LL   │RR" {
		t.Errorf("row 0 = %q, want the two shells either side of the divider", got)
	}
	// Each shell was told its own width, not the window's.
	if got := left.Size(); got != (ui.Size{Cols: 5, Rows: 2}) {
		t.Errorf("left pane size = %+v, want 5x2", got)
	}
	if got := right.Size(); got != (ui.Size{Cols: 5, Rows: 2}) {
		t.Errorf("right pane size = %+v, want 5x2", got)
	}

	// Typing reaches the focused pane only.
	root.HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true})
	waitFor(t, func() bool { return lf.sentText() == "x" })
	if got := rf.sentText(); got != "" {
		t.Errorf("the unfocused pane received %q", got)
	}

	// Clicking the other pane moves focus, and then typing follows.
	root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 8, Row: 1,
	})
	root.HandleMouse(input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseLeft, Col: 8, Row: 1,
	})
	root.HandleKey(input.Event{Kind: input.Text, Rune: 'y', NormalText: true})

	waitFor(t, func() bool { return rf.sentText() == "y" })
	if got := lf.sentText(); got != "x" {
		t.Errorf("the first pane received %q after focus moved away", got)
	}
}

// TestSplitCursorBelongsToTheFocusedPane checks on real terminals what
// the split tests check on fakes: one cursor, and the focused pane has
// it whichever way round the panes are drawn.
func TestSplitCursorBelongsToTheFocusedPane(t *testing.T) {
	pal := vt.DefaultPalette()
	left, lf := newTestTerm(t, 1, 1, Config{Palette: &pal})
	right, rf := newTestTerm(t, 1, 1, Config{Palette: &pal})
	split := ui.NewSplit(ui.Columns, left, right)
	var root ui.Root
	root.SetWidget(split)
	root.Layout(ui.Rect{Cols: 11, Rows: 2})
	lf.feed(t, left, "LL")
	rf.feed(t, right, "RR")
	host := grid.New(11, 2, pal.FG, pal.BG)

	root.Draw(host.View())
	cur := host.Cursor()
	if !cur.Visible || cur.X >= 6 {
		t.Errorf("cursor = %+v, want it visible in the left pane", cur)
	}

	split.Focus(right)
	root.Draw(host.View())

	cur = host.Cursor()
	if !cur.Visible || cur.X < 6 {
		t.Errorf("cursor = %+v, want it visible in the right pane", cur)
	}
}

// TestSplitResizesBothShells checks that changing the window tells both
// programs their new width, not just the focused one.
func TestSplitResizesBothShells(t *testing.T) {
	left, lf := newTestTerm(t, 1, 1, Config{})
	right, rf := newTestTerm(t, 1, 1, Config{})
	var root ui.Root
	root.SetWidget(ui.NewSplit(ui.Columns, left, right))

	root.Layout(ui.Rect{Cols: 41, Rows: 10})

	if got := lf.lastSize(); got != [2]int{20, 10} {
		t.Errorf("left shell told %v, want 20x10", got)
	}
	if got := rf.lastSize(); got != [2]int{20, 10} {
		t.Errorf("right shell told %v, want 20x10", got)
	}
}

// TestSplitKeepsDamageTrackingAcrossPanes checks that output in one pane
// does not repaint the other. Two shells on one grid is where a lazy
// copy would cost twice as much as it should.
func TestSplitKeepsDamageTrackingAcrossPanes(t *testing.T) {
	left, lf := newTestTerm(t, 1, 1, Config{})
	right, rf := newTestTerm(t, 1, 1, Config{})
	var root ui.Root
	root.SetWidget(ui.NewSplit(ui.Rows, left, right))
	root.Layout(ui.Rect{Cols: 10, Rows: 5})
	lf.feed(t, left, "top")
	rf.feed(t, right, "bottom")
	host := grid.New(10, 5, fgOf(), bgOf())
	root.Draw(host.View())
	host.ClearDirty()

	root.Draw(host.View())
	if host.AnyDirty() {
		t.Fatal("an unchanged repaint dirtied the grid")
	}

	lf.feed(t, left, "\r\nmore")
	root.Draw(host.View())

	if !host.RowDirty(1) {
		t.Error("the row that changed was not dirtied")
	}
	for _, y := range []int{3, 4} {
		if host.RowDirty(y) {
			t.Errorf("row %d of the other pane was dirtied by output in the first", y)
		}
	}
}

func fgOf() color.RGBA { return color.RGBA{0xff, 0xff, 0xff, 0xff} }
func bgOf() color.RGBA { return color.RGBA{0x00, 0x00, 0x00, 0xff} }

// TestTerminalsInADeck is the whole point of it: two shells, one shown
// at a time, each told the whole of the room.
func TestTerminalsInADeck(t *testing.T) {
	pal := vt.DefaultPalette()
	first, ff := newTestTerm(t, 1, 1, Config{Palette: &pal})
	second, sf := newTestTerm(t, 1, 1, Config{Palette: &pal})

	deck := ui.NewDeck(first, second)
	var root ui.Root
	root.SetWidget(deck)
	root.Layout(ui.Rect{Cols: 14, Rows: 3})

	ff.feed(t, first, "FIRST")
	sf.feed(t, second, "SECOND")
	host := grid.New(14, 3, pal.FG, pal.BG)
	root.Draw(host.View())

	if got := rowText(host, 0); got != "FIRST" {
		t.Errorf("the top row = %q, want the first shell", got)
	}

	// Each shell was told the whole of the room, not all but a row.
	if got := first.Size(); got != (ui.Size{Cols: 14, Rows: 3}) {
		t.Errorf("the shown shell has %+v, want 14x3", got)
	}

	// Typing reaches the shell being shown, and only that one.
	root.HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true})
	waitFor(t, func() bool { return ff.sentText() == "x" })
	if got := sf.sentText(); got != "" {
		t.Errorf("the hidden shell received %q", got)
	}

	// Bringing the other one forward gives it the screen and the keys.
	deck.Focus(second)
	root.Draw(host.View())
	if got := rowText(host, 0); got != "SECOND" {
		t.Errorf("the top row = %q, want the second shell once it is in front", got)
	}
	root.HandleKey(input.Event{Kind: input.Text, Rune: 'y', NormalText: true})
	waitFor(t, func() bool { return sf.sentText() == "y" })
}

// The cursor belongs to the shell in front: a hidden one must not draw
// its own into the shared grid.
func TestTheCursorBelongsToTheShellInFront(t *testing.T) {
	pal := vt.DefaultPalette()
	first, ff := newTestTerm(t, 1, 1, Config{Palette: &pal})
	second, sf := newTestTerm(t, 1, 1, Config{Palette: &pal})
	deck := ui.NewDeck(first, second)
	var root ui.Root
	root.SetWidget(deck)
	root.Layout(ui.Rect{Cols: 14, Rows: 3})
	ff.feed(t, first, "AB")
	sf.feed(t, second, "CDEF")
	host := grid.New(14, 3, pal.FG, pal.BG)

	root.Draw(host.View())
	cur := host.Cursor()
	if !cur.Visible || cur.Y != 0 || cur.X != 2 {
		t.Errorf("cursor = %+v, want it after the first shell's text", cur)
	}

	deck.Focus(second)
	root.Draw(host.View())

	cur = host.Cursor()
	if !cur.Visible || cur.X != 4 {
		t.Errorf("cursor = %+v, want it after the second shell's text", cur)
	}
}

// TestTerminalCancelGestureEndsADrag checks the case that leaves a
// terminal selecting for ever. A dialog opening between a press and its
// release ends the drag without one, and a terminal still selecting
// would extend its selection on the next plain hover: motion is reported
// with no button down.
func TestTerminalCancelGestureEndsADrag(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{WriteClipboard: func(string) {}})
	f.feed(t, term, "hello")
	draw(term, 20, 4)

	term.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft})
	term.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 2})
	if !term.Copy() {
		t.Fatal("the drag selected nothing")
	}

	// The release never comes: a dialog opened over it.
	term.CancelGesture()
	term.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: 4})

	if got := term.SelectionText(); got != "hel" {
		t.Errorf("selection = %q, want it left where the drag ended: a hover extended it", got)
	}
}

// TestTerminalCancelGestureThroughTheRoot checks the same thing where it
// actually happens: a dialog opening while a drag is in progress.
func TestTerminalCancelGestureThroughTheRoot(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{WriteClipboard: func(string) {}})
	f.feed(t, term, "hello")
	var root ui.Root
	root.SetWidget(term)
	root.Layout(ui.Rect{Cols: 20, Rows: 4})
	root.Draw(grid.New(20, 4, fgOf(), bgOf()).View())

	root.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft})
	root.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 2})
	root.PushModal(&nothing{})
	root.PopModal()
	root.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: 8})

	if got := term.SelectionText(); got != "hel" {
		t.Errorf("selection = %q, want the dialog to have ended the drag", got)
	}
}

// nothing is a widget that draws nothing, for standing in as a dialog.
type nothing struct{}

func (*nothing) Layout(ui.Size) {}
func (*nothing) Draw(grid.View) {}

// A terminal the host draws elsewhere leaves the room the layout gave it
// blank, and still paints its whole screen when the host asks for it.
//
// The host is drawing a held screen that does not fit that room, shrunk
// to fit, on a layer of its own. Whatever the tree last painted there
// would otherwise show around the picture.
func TestATerminalDrawnElsewhereBlanksItsRoom(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "hello")

	term.SetElsewhere(true)
	g := draw(term, 20, 4)

	if got := rowText(g, 0); got != "" {
		t.Errorf("row 0 = %q, want nothing: the host is drawing this screen", got)
	}
	if !term.Elsewhere() {
		t.Error("the terminal does not say the host is drawing it")
	}

	// The host's own draw is the one that paints it.
	own := grid.New(20, 4, color.RGBA{}, color.RGBA{})
	term.DrawScreen(own.View())
	if got := rowText(own, 0); got != "hello" {
		t.Errorf("the host's copy holds %q, want %q", got, "hello")
	}

	term.SetElsewhere(false)
	if got := rowText(draw(term, 20, 4), 0); got != "hello" {
		t.Errorf("back in the tree the row is %q, want %q", got, "hello")
	}
}

// A reading carries what the shell said about the command line, from the
// same moment as the screen it came with.
//
// Read separately, an exit status can be from after the screen: the
// agent is then told a command finished that the screen it was given
// still shows running.
func TestAReadingCarriesTheCommandTheShellMarked(t *testing.T) {
	term, f := newTestTerm(t, 40, 6, Config{})

	// A shell with no marks says nothing about its commands.
	f.feed(t, term, "$ ")
	if read := term.ReadLines(0); read.Cmd.Integrated || read.Cmd.Done != 0 {
		t.Errorf("a shell that marked nothing reads as %+v", read.Cmd)
	}

	// The prompt, the command, and the command running.
	f.feed(t, term, "\x1b]133;A\a$ \x1b]133;B\amake\r\n"+"\x1b]133;C\a")
	read := term.ReadLines(0)
	if !read.Cmd.Integrated || !read.Cmd.Running {
		t.Errorf("while the command runs the reading says %+v", read.Cmd)
	}

	// And the command finishing, with the status it gave.
	f.feed(t, term, "built\r\n"+"\x1b]133;D;2\a$ ")
	read = term.ReadLines(0)
	if read.Cmd.Running || read.Cmd.Done != 1 {
		t.Errorf("after the command finished the reading says %+v", read.Cmd)
	}
	if status, ok := read.Cmd.Exit(); !ok || status != 2 {
		t.Errorf("it says exit %d, known %v; want 2", status, ok)
	}
	if !strings.Contains(read.Text, "built") {
		t.Errorf("the screen that came with it reads %q", read.Text)
	}
}

// A reading says which line the cursor is on and what is in front of it,
// which is what a shell that marks nothing has instead of a mark.
func TestAReadingSaysWhatIsInFrontOfTheCursor(t *testing.T) {
	term, f := newTestTerm(t, 40, 3, Config{})

	f.feed(t, term, "$ ")
	read := term.ReadLines(0)
	if read.Before != "$" {
		// The prompt with its trailing space cut, which is what the text
		// in front of the cursor is worth comparing on.
		t.Errorf("in front of the cursor is %q, want %q", read.Before, "$")
	}
	if read.Line != 0 {
		t.Errorf("the cursor is on line %d, want 0", read.Line)
	}

	// A command and its output, and the prompt comes back further down.
	f.feed(t, term, "whoami\r\nmarcus\r\n$ ")
	read = term.ReadLines(0)
	if read.Before != "$" {
		t.Errorf("in front of the cursor is %q, want the prompt", read.Before)
	}
	if read.Line != 2 {
		t.Errorf("the cursor is on line %d, want 2", read.Line)
	}

	// Only what is in front of the cursor. A shell that draws a
	// suggestion after it, or a cursor moved back along the line, must
	// not put the rest of the row into what the prompt is taken to be.
	f.feed(t, term, "ls -la\x1b[3D")
	read = term.ReadLines(0)
	// With its trailing space cut, as every reading of a row is.
	if read.Before != "$ ls" {
		t.Errorf("in front of the cursor is %q, want %q", read.Before, "$ ls")
	}
	f.feed(t, term, "\x1b[3C\r\n$ ")

	// Enough output to scroll, and the line number goes on counting
	// rather than starting again at the top of the screen.
	f.feed(t, term, "one\r\ntwo\r\nthree\r\n$ ")
	read = term.ReadLines(0)
	// Four lines written on a three row screen: the cursor is on the
	// bottom row of the three showing, and the lines before them have
	// gone off the top.
	if read.Line != 6 {
		t.Errorf("after scrolling the cursor is on line %d, want 6", read.Line)
	}
}

// Reading from a line gives what the pane has said since that line,
// however far it has scrolled in the meantime.
func TestReadingFromALine(t *testing.T) {
	term, f := newTestTerm(t, 40, 4, Config{})
	f.feed(t, term, "$ ls\r\n")

	// The line the output starts on, taken the way a caller would.
	from := term.ReadLines(0).Line
	f.feed(t, term, "one\r\ntwo\r\nthree\r\nfour\r\n$ ")

	read, there := term.ReadFrom(from, 500)
	for _, want := range []string{"one", "two", "three", "four"} {
		if !strings.Contains(read.Text, want) {
			t.Errorf("reading from line %d misses %q:\n%s", from, want, read.Text)
		}
	}
	if strings.Contains(read.Text, "$ ls") {
		t.Errorf("reading from line %d reaches back above it:\n%s", from, read.Text)
	}
	// Four lines of output and the row the prompt came back on.
	if there != 5 {
		t.Errorf("it says there are %d lines from line %d, want 5", there, from)
	}

	// At most what was asked for, counted from the bottom, and it still
	// says how many there were.
	short, there := term.ReadFrom(from, 2)
	if strings.Contains(short.Text, "one") {
		t.Errorf("a read of two lines gave %q", short.Text)
	}
	if !strings.HasSuffix(short.Text, "$") {
		t.Errorf("a read of two lines gave %q, want the last of it", short.Text)
	}
	if there != 5 {
		t.Errorf("a read of two lines says there are %d, want 5", there)
	}

	// A line the screen has not reached gives the row the cursor is on
	// rather than nothing at all.
	if ahead, there := term.ReadFrom(from+10_000, 500); strings.Count(ahead.Text, "\n") != 0 || there != 1 {
		t.Errorf("reading from a line that has not happened gave %q, %d lines", ahead.Text, there)
	}

	// Zero lines is one line, not the whole of history: this reads
	// output, and output has no bound.
	if none, _ := term.ReadFrom(from, 0); strings.Count(none.Text, "\n") != 0 {
		t.Errorf("a read of no lines gave %q", none.Text)
	}
}

// A boundary written down before the screen scrolled still names the
// same line afterwards, which is the whole point of numbering lines.
func TestReadingFromALineThatHasScrolledAway(t *testing.T) {
	term, f := newTestTerm(t, 40, 4, Config{})
	// Fill the screen and push it along, so the boundary is written down
	// on a screen that has already scrolled.
	f.feed(t, term, "old one\r\nold two\r\nold three\r\nold four\r\nold five\r\n$ ls\r\n")
	from := term.ReadLines(0).Line
	if from < 4 {
		t.Fatalf("the boundary is line %d, and the screen has not scrolled far enough", from)
	}

	// Enough output to push the boundary off the screen and into history.
	f.feed(t, term, "one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\n$ ")

	read, there := term.ReadFrom(from, 500)
	for _, want := range []string{"one", "two", "three", "four", "five", "six"} {
		if !strings.Contains(read.Text, want) {
			t.Errorf("reading from line %d misses %q:\n%s", from, want, read.Text)
		}
	}
	if strings.Contains(read.Text, "old five") || strings.Contains(read.Text, "$ ls") {
		t.Errorf("reading from line %d reaches back above it:\n%s", from, read.Text)
	}
	if there != 7 {
		t.Errorf("it says there are %d lines from line %d, want 7", there, from)
	}
}
