package term

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// countingSession counts the reads the terminal made on it, so a test
// can show that nothing is still reading a session that was swapped out.
type countingSession struct {
	*fakeSession
	reads atomic.Int64
}

func newCountingSession() *countingSession {
	return &countingSession{fakeSession: newFakeSession()}
}

func (c *countingSession) Read(p []byte) (int, error) {
	c.reads.Add(1)
	return c.fakeSession.Read(p)
}

// endProgram hangs the session up and waits for the terminal to notice.
func endProgram(t *testing.T, term *Terminal, f *fakeSession) {
	t.Helper()
	_ = f.Close()
	waitFor(t, term.Exited)
}

// restart puts a fresh session under the terminal.
func restart(t *testing.T, term *Terminal) *fakeSession {
	t.Helper()
	next := newFakeSession()
	if err := term.Restart(next); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	return next
}

// TestRestartKeepsTheTranscript is the point of restarting in the pane
// rather than in a new tab: everything the old program printed, and what
// the window said about it going, stays above the new session.
func TestRestartKeepsTheTranscript(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "one\r\ntwo\r\nthree\r\nfour\r\nfive\r\n")
	term.Say("the program has gone")
	endProgram(t, term, f)

	next := restart(t, term)
	next.feed(t, term, "back\r\n")

	text := term.TextLines(40)
	for _, want := range []string{"one", "five", "the program has gone", "back"} {
		if !strings.Contains(text, want) {
			t.Errorf("the transcript is missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "one") > strings.Index(text, "back") {
		t.Errorf("the new session is not below the old transcript:\n%s", text)
	}
	if strings.Index(text, "the program has gone") > strings.Index(text, "back") {
		t.Errorf("the line the window said is not above the new session:\n%s", text)
	}
}

// TestRestartCarriesTheNewSessionBothWays checks the swap moved bytes
// as well as state: output arrives from the new session and typing goes
// to it rather than to the one that ended.
func TestRestartCarriesTheNewSessionBothWays(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	term.HandleKey(input.Event{Kind: input.Text, Rune: 'a', NormalText: true})
	waitFor(t, func() bool { return f.sentText() == "a" })
	endProgram(t, term, f)

	next := restart(t, term)
	next.feed(t, term, "hello")
	term.HandleKey(input.Event{Kind: input.Text, Rune: 'b', NormalText: true})
	waitFor(t, func() bool { return next.sentText() == "b" })

	if got := rowText(draw(term, 20, 4), 0); got != "hello" {
		t.Errorf("row 0 = %q, want the new session's output", got)
	}
	if got := f.sentText(); got != "a" {
		t.Errorf("the old session was written to after the swap: %q", got)
	}
}

// TestRestartMakesTheTerminalLiveAgain checks the flags that say a
// terminal has finished are all put back, and set again next time.
func TestRestartMakesTheTerminalLiveAgain(t *testing.T) {
	var exits atomic.Int32
	term, f := newTestTerm(t, 20, 4, Config{OnExit: func() { exits.Add(1) }})
	endProgram(t, term, f)
	if exits.Load() != 1 {
		t.Fatalf("OnExit called %d times before the restart, want 1", exits.Load())
	}

	next := restart(t, term)
	if term.Exited() {
		t.Error("the terminal still reports itself gone after a restart")
	}

	_ = next.Close()
	waitFor(t, term.Exited)
	waitFor(t, func() bool { return exits.Load() == 2 })
}

// TestCloseAfterARestartClosesTheNewSession checks Close follows the
// swap, and still hands back what the session said on the way out.
func TestCloseAfterARestartClosesTheNewSession(t *testing.T) {
	boom := errors.New("hangup failed")
	term, f := newTestTerm(t, 20, 4, Config{})
	endProgram(t, term, f)

	next := newFakeSession()
	next.closeErr = boom
	if err := term.Restart(next); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	if err := term.Close(); !errors.Is(err, boom) {
		t.Errorf("Close returned %v, want the new session's own error", err)
	}
	if !next.isClosed() {
		t.Error("Close left the new session open")
	}
}

// TestRestartClosesTheSessionThatEnded checks the old session is hung up
// rather than left behind holding a pty.
func TestRestartClosesTheSessionThatEnded(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	endProgram(t, term, f)

	restart(t, term)

	if !f.isClosed() {
		t.Error("the session that ended was left open")
	}
}

// TestRestartRefusesWhenTheOldSessionWillNotHangUp checks the error is
// handed back rather than a second program being started on top of one
// that is still there.
func TestRestartRefusesWhenTheOldSessionWillNotHangUp(t *testing.T) {
	boom := errors.New("hangup failed")
	term, f := newTestTerm(t, 20, 4, Config{})
	f.closeErr = boom
	endProgram(t, term, f)

	next := newFakeSession()
	err := term.Restart(next)

	if !errors.Is(err, boom) {
		t.Errorf("Restart returned %v, want the old session's own error", err)
	}
	if !term.Exited() {
		t.Error("the terminal came back to life on a restart that failed")
	}
}

// TestClosingTheOldSessionDoesNotStopARestartedTerminal checks the
// restarted terminal is on the new session alone.
func TestClosingTheOldSessionDoesNotStopARestartedTerminal(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	endProgram(t, term, f)
	next := restart(t, term)

	_ = f.Close()
	next.feed(t, term, "still here")

	if term.Exited() {
		t.Error("closing the old session ended the restarted terminal")
	}
	if got := rowText(draw(term, 20, 4), 0); got != "still here" {
		t.Errorf("row 0 = %q, want the new session's output", got)
	}
}

// TestRestartIsRefusedWhileTheProgramRuns checks a live terminal is not
// taken away from the session it is on.
func TestRestartIsRefusedWhileTheProgramRuns(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})

	next := newFakeSession()
	if err := term.Restart(next); err == nil {
		t.Fatal("a running terminal was restarted")
	}

	f.feed(t, term, "carrying on")
	term.HandleKey(input.Event{Kind: input.Text, Rune: 'a', NormalText: true})
	waitFor(t, func() bool { return f.sentText() == "a" })

	if got := rowText(draw(term, 20, 4), 0); got != "carrying on" {
		t.Errorf("row 0 = %q, want the terminal still working", got)
	}
	if next.isClosed() {
		t.Error("the refused session was closed, so the caller cannot use it")
	}
}

// TestRestartIsRefusedWithoutASession checks the nil the window would
// pass after failing to dial.
func TestRestartIsRefusedWithoutASession(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	endProgram(t, term, f)

	if err := term.Restart(nil); err == nil {
		t.Error("a restart with no session was accepted")
	}
}

// TestRestartIsRefusedAfterClose checks a pane the window has finished
// with is not brought back.
func TestRestartIsRefusedAfterClose(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	endProgram(t, term, f)
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := term.Restart(newFakeSession()); err == nil {
		t.Error("a closed terminal was restarted")
	}
}

// TestTwoRestartsInARow checks the second swap is as good as the first:
// a pane the user reconnects, loses again and reconnects.
func TestTwoRestartsInARow(t *testing.T) {
	term, f := newTestTerm(t, 20, 6, Config{})
	f.feed(t, term, "first\r\n")
	endProgram(t, term, f)

	second := restart(t, term)
	second.feed(t, term, "second\r\n")
	endProgram(t, term, second)

	third := restart(t, term)
	third.feed(t, term, "third\r\n")
	term.HandleKey(input.Event{Kind: input.Text, Rune: 'c', NormalText: true})
	waitFor(t, func() bool { return third.sentText() == "c" })

	text := term.TextLines(40)
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(text, want) {
			t.Errorf("the transcript is missing %q:\n%s", want, text)
		}
	}
	if term.Exited() {
		t.Error("the twice restarted terminal reports itself gone")
	}
}

// TestRestartTakesTheRoomThePaneHasNow checks the size a dead pane was
// given while it was dead reaches the emulator and the new session. A
// shell told the wrong width lays its prompt out for it.
func TestRestartTakesTheRoomThePaneHasNow(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	endProgram(t, term, f)

	// The room the layout gives a dead pane, which the emulator does not
	// take until there is a program to take it for.
	term.Layout(ui.Size{Cols: 40, Rows: 10})
	if got := term.Size(); got != (ui.Size{Cols: 20, Rows: 4}) {
		t.Fatalf("a dead terminal resized itself to %+v", got)
	}

	next := restart(t, term)

	if got := term.Size(); got != (ui.Size{Cols: 40, Rows: 10}) {
		t.Errorf("Size() = %+v, want the room the pane has now", got)
	}
	if got := next.lastSize(); got != [2]int{40, 10} {
		t.Errorf("the new session was told %v, want 40x10", got)
	}
	next.feed(t, term, "0123456789012345678901234567890123456789")
	if got := rowText(draw(term, 40, 10), 0); len(got) != 40 {
		t.Errorf("row 0 = %q, want 40 columns of output", got)
	}
}

// TestRestartTellsTheNewSessionASizeThatHasNotChanged checks the size
// reaches a session that was started at some size of its own, even when
// the pane is the size it always was.
func TestRestartTellsTheNewSessionASizeThatHasNotChanged(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	endProgram(t, term, f)

	next := restart(t, term)

	if got := next.lastSize(); got != [2]int{20, 4} {
		t.Errorf("the new session was told %v, want 20x4", got)
	}
}

// TestRestartLeavesNothingRunning checks the goroutines on the old
// session are gone: nothing reading it, nothing writing to it.
//
// The read count is what proves it, because a reader left on a closed
// session spins on the end of it. The goroutine count is a second check
// that restarting repeatedly does not pile them up.
func TestRestartLeavesNothingRunning(t *testing.T) {
	old := newCountingSession()
	term, err := New(Config{Session: old, Size: ui.Size{Cols: 20, Rows: 4}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	term.HandleKey(input.Event{Kind: input.Text, Rune: 'a', NormalText: true})
	waitFor(t, func() bool { return old.sentText() == "a" })
	endProgram(t, term, old.fakeSession)

	next := restart(t, term)
	reads, written := old.reads.Load(), old.sentText()
	settled := runtime.NumGoroutine()

	// Anything the restarted terminal does must reach the new session
	// alone.
	next.feed(t, term, "hello")
	term.HandleKey(input.Event{Kind: input.Text, Rune: 'b', NormalText: true})
	waitFor(t, func() bool { return next.sentText() == "b" })
	time.Sleep(20 * time.Millisecond)

	if got := old.reads.Load(); got != reads {
		t.Errorf("the old session was read %d more times after the swap", got-reads)
	}
	if got := old.sentText(); got != written {
		t.Errorf("the old session was written to after the swap: %q", got)
	}

	for i := 0; i < 4; i++ {
		endProgram(t, term, next)
		next = restart(t, term)
	}
	waitFor(t, func() bool { return runtime.NumGoroutine() <= settled })
}

// TestRestartDropsInputTypedAtTheProgramThatWent checks the queue is
// emptied over the swap. Those bytes were aimed at a program that has
// gone, and a new shell would run what is left of a half typed line at
// its own prompt.
func TestRestartDropsInputTypedAtTheProgramThatWent(t *testing.T) {
	var mu sync.Mutex
	var failed error
	term, f := newTestTerm(t, 20, 4, Config{
		OnError: func(e error) { mu.Lock(); failed = e; mu.Unlock() },
	})
	endProgram(t, term, f)

	// The writer has to be gone before anything can be left queued, so
	// the first key after the program went is the one that breaks it.
	f.setWriteErr(errors.New("broken pipe"))
	term.Paste("rm -rf ")
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return failed != nil
	})
	term.Paste("everything")

	next := restart(t, term)
	next.feed(t, term, "$ ")
	term.HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true})
	waitFor(t, func() bool { return next.sentText() == "x" })

	if got := next.sentText(); got != "x" {
		t.Errorf("the new session was sent %q, want only what was typed at it", got)
	}
}

// TestRestartWithOutputArriving is the swap under -race: the new
// session's reader starts on output that is already waiting, while
// another goroutine reads the screen.
func TestRestartWithOutputArriving(t *testing.T) {
	term, f := newTestTerm(t, 20, 6, Config{})
	f.feed(t, term, "before\r\n")
	endProgram(t, term, f)

	next := newFakeSession()
	const chunks = 8
	for i := 0; i < chunks; i++ {
		next.out <- []byte("after\r\n")
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = term.TextLines(10)
			_ = term.Said()
		}
	}()

	err := term.Restart(next)
	waitFor(t, func() bool {
		return strings.Count(term.TextLines(40), "after") == chunks
	})
	close(stop)
	<-done

	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !strings.Contains(term.TextLines(40), "before") {
		t.Error("the transcript was lost under a restart with output arriving")
	}
}

// TestWatchingWorksAgainAfterARestart checks a watcher arriving at a
// restarted pane is shown it rather than told the program has gone.
func TestWatchingWorksAgainAfterARestart(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	endProgram(t, term, f)
	next := restart(t, term)
	next.feed(t, term, "hello")

	w := &fakeWatcher{}
	unwatch, err := term.Watch(w)
	if err != nil {
		t.Fatalf("Watch after a restart: %v", err)
	}
	defer unwatch()

	if !strings.Contains(w.screenText(), "hello") {
		t.Errorf("the watcher was given %q, want the restarted screen", w.screenText())
	}
}

// fakeWatcher records what a watcher was given.
type fakeWatcher struct {
	mu     sync.Mutex
	screen []byte
	writes []byte
	ended  bool
}

func (w *fakeWatcher) Screen(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.screen = append([]byte(nil), p...)
	return nil
}

func (w *fakeWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, p...)
	return len(p), nil
}

func (w *fakeWatcher) Ended() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ended = true
}

func (w *fakeWatcher) screenText() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.screen)
}
