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
	for x := 0; x < cols; x++ {
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
func TestExitCallsOnExitOnce(t *testing.T) {
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
	time.Sleep(20 * time.Millisecond)

	if got := exits.Load(); got != 1 {
		t.Errorf("OnExit called %d times, want 1", got)
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
// the host must still be told once.
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
	time.Sleep(50 * time.Millisecond)
	if got := exits.Load(); got != 1 {
		t.Errorf("OnExit called %d times, want 1", got)
	}
}

// TestSendAfterCloseDoesNotPanic checks the shutdown path: a device
// report can be produced by the reader at any moment, including while
// the window is closing.
func TestSendAfterCloseDoesNotPanic(t *testing.T) {
	for i := 0; i < 200; i++ {
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
