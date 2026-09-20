package logs

import (
	"bytes"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// budget is how long a test waits for a line to arrive before deciding
// it is not coming.
const budget = 2 * time.Second

// readLine reads one line from a reader, failing if none arrives.
func readLine(t *testing.T, r *Reader) string {
	t.Helper()
	type got struct {
		s   string
		err error
	}
	ch := make(chan got, 1)
	go func() {
		b := make([]byte, 4096)
		n, err := r.Read(b)
		ch <- got{string(b[:n]), err}
	}()
	select {
	case g := <-ch:
		if g.err != nil {
			t.Fatalf("reading a line: %v", g.err)
		}
		return g.s
	case <-time.After(budget):
		t.Fatal("no log line arrived")
		return ""
	}
}

// A pane opened after the window has been running shows what was
// logged before it opened. Debugging starts after the thing went
// wrong, so a log that began when the pane did would be empty.
func TestAReaderStartsWithWhatWasAlreadyLogged(t *testing.T) {
	l := New(0, nil)
	if _, err := l.Write([]byte("the first thing\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := l.Open()
	defer func() { _ = r.Close() }()

	if got, want := readLine(t, r), "the first thing\r\n"; got != want {
		t.Errorf("the reader gave %q, want %q", got, want)
	}
}

// A line logged while the pane is open reaches it.
func TestALineWrittenLaterReachesAnOpenReader(t *testing.T) {
	l := New(0, nil)
	r := l.Open()
	defer func() { _ = r.Close() }()

	go func() {
		_, _ = l.Write([]byte("something happened\n"))
	}()

	if got, want := readLine(t, r), "something happened\r\n"; got != want {
		t.Errorf("the reader gave %q, want %q", got, want)
	}
}

// Every newline goes out with a carriage return in front. A terminal
// moves down a row on the one and back to the first column on the
// other, so without this each line would start further right than the
// last.
func TestEveryLineEndsReadyForATerminal(t *testing.T) {
	l := New(0, nil)
	if _, err := l.Write([]byte("two\nlines\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := l.Open()
	defer func() { _ = r.Close() }()

	if got, want := readLine(t, r), "two\r\nlines\r\n"; got != want {
		t.Errorf("the reader gave %q, want %q", got, want)
	}
}

// A line with no newline of its own still ends as one, so the next
// does not run into it.
func TestALineWithoutANewlineGetsOne(t *testing.T) {
	l := New(0, nil)
	if _, err := l.Write([]byte("no newline here")); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := l.Open()
	defer func() { _ = r.Close() }()

	if got, want := readLine(t, r), "no newline here\r\n"; got != want {
		t.Errorf("the reader gave %q, want %q", got, want)
	}
}

// The lines go to stderr as well, which is where they always went.
// Keeping them here must not take them away from a console.
func TestTheLinesStillGoWhereTheyWent(t *testing.T) {
	var also bytes.Buffer
	l := New(0, &also)

	if _, err := l.Write([]byte("still on stderr\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got, want := also.String(), "still on stderr\n"; got != want {
		t.Errorf("stderr got %q, want %q", got, want)
	}
}

// Only the last few lines are kept, so a window logging all day does
// not hold every line it ever wrote.
func TestOnlyTheLastLinesAreKept(t *testing.T) {
	l := New(3, nil)

	for _, line := range []string{"one\n", "two\n", "three\n", "four\n", "five\n"} {
		if _, err := l.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	if got, want := l.Held(), 3; got != want {
		t.Errorf("it kept %d lines, want %d", got, want)
	}
	r := l.Open()
	defer func() { _ = r.Close() }()
	if got, want := readLine(t, r), "three\r\n"; got != want {
		t.Errorf("the oldest line kept is %q, want %q", got, want)
	}
}

// A reader that fell behind is told how many lines went rather than
// being handed a gap it cannot see.
func TestAReaderThatFellBehindIsToldWhatItMissed(t *testing.T) {
	l := New(2, nil)
	r := l.Open()
	defer func() { _ = r.Close() }()
	for _, line := range []string{"one\n", "two\n", "three\n", "four\n"} {
		if _, err := l.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	got := readLine(t, r)

	if !strings.Contains(got, "2 older log lines are no longer kept") {
		t.Errorf("it said %q, want it to say two lines went", got)
	}
	if next := readLine(t, r); next != "three\r\n" {
		t.Errorf("it carried on with %q, want the oldest line still kept", next)
	}
}

// Writing never waits for whoever is reading. A log line is written
// from wherever the work is, including paths that must not stop for a
// pane nobody is looking at.
func TestWritingDoesNotWaitForAReader(t *testing.T) {
	l := New(4, nil)
	r := l.Open()
	defer func() { _ = r.Close() }()

	done := make(chan struct{})
	go func() {
		for range 1000 {
			_, _ = l.Write([]byte("a line\n"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(budget):
		t.Fatal("writing blocked on a reader that was not reading")
	}
}

// Closing the pane leaves the log running, so the window goes on
// logging and the next pane shows what happened in between.
func TestClosingAReaderLeavesTheLogRunning(t *testing.T) {
	l := New(0, nil)
	r := l.Open()
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := l.Write([]byte("after the pane went\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	again := l.Open()
	defer func() { _ = again.Close() }()
	if got, want := readLine(t, again), "after the pane went\r\n"; got != want {
		t.Errorf("the next reader gave %q, want %q", got, want)
	}
}

// A closed reader says so rather than waiting for a line that is not
// coming, which is the end whatever is reading a session looks for.
func TestAClosedReaderSaysSo(t *testing.T) {
	l := New(0, nil)
	r := l.Open()
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := r.Read(make([]byte, 16)); err != ErrLogClosed {
		t.Errorf("reading a closed reader gave %v, want %v", err, ErrLogClosed)
	}
}

// A read waiting for a line ends when the reader is closed. Without
// this, closing the pane would leave the goroutine reading it stuck for
// as long as the window lived.
func TestClosingWakesAReadThatIsWaiting(t *testing.T) {
	l := New(0, nil)
	r := l.Open()
	ended := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 16))
		ended <- err
	}()
	// Let the read reach the wait before closing under it.
	time.Sleep(20 * time.Millisecond)

	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-ended:
		if err != ErrLogClosed {
			t.Errorf("the read ended with %v, want %v", err, ErrLogClosed)
		}
	case <-time.After(budget):
		t.Fatal("closing left the read waiting")
	}
}

// Wait ends when the reader is closed, which is how whatever holds a
// session learns the pane has finished.
func TestWaitEndsWhenTheReaderIsClosed(t *testing.T) {
	l := New(0, nil)
	r := l.Open()
	ended := make(chan error, 1)
	go func() { ended <- r.Wait() }()
	time.Sleep(20 * time.Millisecond)

	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-ended:
		if err != nil {
			t.Errorf("Wait gave %v, want nothing", err)
		}
	case <-time.After(budget):
		t.Fatal("closing left Wait waiting")
	}
}

// What the pane types goes nowhere. A log is something to read, and
// there is nothing at the far end to take it.
func TestTypingIntoTheLogGoesNowhere(t *testing.T) {
	l := New(0, nil)
	r := l.Open()
	defer func() { _ = r.Close() }()

	n, err := r.Write([]byte("ls -la\n"))

	if err != nil || n != 7 {
		t.Errorf("typing gave %d, %v, want it taken and dropped", n, err)
	}
	if got := l.Held(); got != 0 {
		t.Errorf("typing put %d lines in the log", got)
	}
}

// log.SetOutput points at this, which is the whole reason it is an
// io.Writer. The line arrives with the flags the window sets.
func TestItTakesWhatTheLogPackageWrites(t *testing.T) {
	l := New(0, nil)
	out := log.New(l, "", 0)

	out.Printf("could not reach %s", "margit")

	r := l.Open()
	defer func() { _ = r.Close() }()
	if got, want := readLine(t, r), "could not reach margit\r\n"; got != want {
		t.Errorf("the log line came out as %q, want %q", got, want)
	}
}

// log reuses its buffer between calls, so a kept line has to be a copy.
// Without one every kept line would read as whatever was logged last.
func TestAKeptLineIsACopy(t *testing.T) {
	l := New(0, nil)
	buf := []byte("the first line\n")

	if _, err := l.Write(buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	copy(buf, "OVERWRITTEN!!!")

	r := l.Open()
	defer func() { _ = r.Close() }()
	if got, want := readLine(t, r), "the first line\r\n"; got != want {
		t.Errorf("the kept line reads %q, want %q", got, want)
	}
}

// Several panes can follow the log at once, and each sees every line.
func TestTwoReadersBothSeeALine(t *testing.T) {
	l := New(0, nil)
	one, two := l.Open(), l.Open()
	defer func() { _ = one.Close(); _ = two.Close() }()

	if _, err := l.Write([]byte("both of them\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	for i, r := range []*Reader{one, two} {
		if got, want := readLine(t, r), "both of them\r\n"; got != want {
			t.Errorf("reader %d gave %q, want %q", i, got, want)
		}
	}
}

// Writing from several goroutines at once is safe, which it has to be:
// the window logs from whichever goroutine hit the problem.
func TestWritingFromEverywhereAtOnceIsSafe(t *testing.T) {
	l := New(50, io.Discard)
	r := l.Open()
	defer func() { _ = r.Close() }()
	go func() {
		b := make([]byte, 256)
		for {
			if _, err := r.Read(b); err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_, _ = l.Write([]byte("a line from somewhere\n"))
			}
		}()
	}
	wg.Wait()

	if got := l.Held(); got != 50 {
		t.Errorf("it kept %d lines, want the last 50", got)
	}
}
