//go:build windows

package session

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// budget is how long a test waits for a shell on this machine. Generous
// on purpose: a passing test never waits this long.
const budget = 10 * time.Second

// readUntil pumps the session until want appears or the deadline passes.
//
// A ConPTY delivers output in arbitrarily sized chunks and repaints the
// screen with escape sequences around it, so no single Read can be
// assumed to hold a whole line.
func readUntil(t *testing.T, s Session, want string, timeout time.Duration) string {
	t.Helper()
	// The buffer is read again on the timeout path while the reader is
	// still writing to it, so it needs a lock.
	var (
		mu   sync.Mutex
		buf  bytes.Buffer
		done = make(chan struct{})
	)
	go func() {
		defer close(done)
		b := make([]byte, 4096)
		for {
			n, err := s.Read(b)
			mu.Lock()
			buf.Write(b[:n])
			found := strings.Contains(buf.String(), want)
			mu.Unlock()
			if found || err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

// shell starts a shell for a test and makes sure it is let go of.
func shell(t *testing.T, argv ...string) Session {
	t.Helper()
	s, err := StartLocal(LocalConfig{Command: argv, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// A shell runs, takes what is typed at it, and answers.
func TestStartLocalRunsACommandOnWindows(t *testing.T) {
	s := shell(t, "cmd.exe")
	readUntil(t, s, ">", budget)

	if _, err := s.Write([]byte("echo hello-from-the-pty\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := readUntil(t, s, "hello-from-the-pty", budget)
	if !strings.Contains(got, "hello-from-the-pty") {
		t.Fatalf("output = %q, want it to contain hello-from-the-pty", got)
	}
}

// The end of a session arrives as a broken pipe on Windows rather than
// as an end of file, and the caller is given one case.
func TestReadReturnsEOFWhenTheChildExitsOnWindows(t *testing.T) {
	s := shell(t, "cmd.exe", "/c", "echo done")

	read := make(chan error, 1)
	go func() {
		b := make([]byte, 4096)
		for {
			if _, err := s.Read(b); err != nil {
				read <- err
				return
			}
		}
	}()
	select {
	case err := <-read:
		if err != io.EOF {
			t.Fatalf("Read = %v, want io.EOF", err)
		}
	case <-time.After(budget):
		t.Fatal("the read never ended after the child exited")
	}
}

// Wait reports what the child exited with, so a window can say a shell
// failed rather than that it closed.
func TestWaitReportsTheExitStatusOnWindows(t *testing.T) {
	s := shell(t, "cmd.exe", "/c", "exit 3")
	go func() { _, _ = io.Copy(io.Discard, s) }()

	if err := s.Wait(); err == nil {
		t.Fatal("Wait reported a clean exit for a child that exited 3")
	}
}

// Close is reachable from more than one path -- the window closing, the
// shell exiting, a read error -- so it has to tolerate being called
// twice.
func TestCloseIsIdempotentOnWindows(t *testing.T) {
	s := shell(t, "cmd.exe")
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// Close is reachable from several goroutines at once -- the window
// closing while the reaper is also finishing -- which is what the
// sync.Once is there for.
func TestCloseIsSafeConcurrentlyOnWindows(t *testing.T) {
	s := shell(t, "cmd.exe")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Close()
		}()
	}
	wg.Wait()
}

// A child that has been reaped is reported as gone straight away.
//
// Windows has no slave handle to hand back, so the reaper lets go of the
// pty itself, and that takes the same lock a Close from the window holds.
// Saying the child had gone only after that returned meant the window's
// Close could not be told, and it waited out the hangup grace for a child
// that had already exited.
func TestTheChildIsReportedGoneBeforeTheReaperCloses(t *testing.T) {
	s := shell(t, "cmd.exe", "/c", "exit 0")
	l := s.(*local)

	// Nothing reads here: Read may not be called concurrently with
	// itself, and the reaper's own path through Close is what this is
	// about.
	//
	// The clock starts before the wait, because the wait is part of what
	// is being timed: the reaper waits, then closes, and the close is
	// where the grace used to be spent.
	start := time.Now()

	// Wait is idempotent, so this returns once the child has been
	// reaped, whichever goroutine did the reaping.
	_ = s.Wait()

	select {
	case <-l.done:
	case <-time.After(budget):
		t.Fatal("the child was never reported gone")
	}
	if took := time.Since(start); took >= hangupGrace {
		t.Fatalf("the child was reported gone %v after it was started, "+
			"which is the hangup grace being waited out", took)
	}
}

// Closing a tab does not wait out the hangup grace for a shell that
// takes the hint.
func TestClosingATabDoesNotWaitOutTheHangupGrace(t *testing.T) {
	s := shell(t, "cmd.exe")
	go func() { _, _ = io.Copy(io.Discard, s) }()
	// Up and running, so the close is the one being timed rather than
	// the start.
	readUntil(t, s, ">", budget)

	start := time.Now()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if took := time.Since(start); took >= hangupGrace {
		t.Fatalf("Close took %v, and the grace is %v: it waited for a child "+
			"that had already gone and then killed it", took, hangupGrace)
	}
}

// A child that writes and exits in the same instant keeps its output.
//
// A ConPTY repaints on a clock of its own, so the child's last line
// reaches the pipe after the child has already been reaped.
func TestOutputSurvivesAChildThatExitsImmediatelyOnWindows(t *testing.T) {
	const want = "hello-from-a-child-that-exits"
	s := shell(t, "cmd.exe", "/c", "echo "+want)

	got := readUntil(t, s, want, budget)
	if !strings.Contains(got, want) {
		t.Fatalf("output = %q, want it to contain %q: the pseudoconsole was "+
			"closed before it had written the child's last line", got, want)
	}
}

// A child that exits without writing anything ends the session rather
// than leaving the reader waiting for a flush that will never come.
func TestASilentChildEndsTheSessionOnWindows(t *testing.T) {
	// drainCap is the longest the end of a session may take once the
	// child has gone. Nothing waits on a clock, so this is slack.
	const drainCap = 2 * time.Second

	s := shell(t, "cmd.exe", "/c", "exit 0")

	read := make(chan error, 1)
	go func() {
		b := make([]byte, 4096)
		for {
			if _, err := s.Read(b); err != nil {
				read <- err
				return
			}
		}
	}()

	start := time.Now()
	select {
	case err := <-read:
		if err != io.EOF {
			t.Fatalf("Read = %v, want io.EOF", err)
		}
	case <-time.After(budget):
		t.Fatal("the session never ended for a child that wrote nothing")
	}
	if took := time.Since(start); took > drainCap {
		t.Fatalf("the session took %v to end, and the cap is %v", took, drainCap)
	}
}

// TERM is set for the child unless the caller set it.
func TestTermIsSetByDefaultOnWindows(t *testing.T) {
	s := shell(t, "cmd.exe")
	readUntil(t, s, ">", budget)

	if _, err := s.Write([]byte("echo TERM is %TERM%\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := readUntil(t, s, "TERM is xterm-256color", budget)
	if !strings.Contains(got, "TERM is xterm-256color") {
		t.Fatalf("output = %q, want TERM to be set", got)
	}
}

// A command that is not there is reported rather than leaving a window
// with a pane attached to nothing.
func TestStartLocalReportsAMissingCommandOnWindows(t *testing.T) {
	_, err := StartLocal(LocalConfig{
		Command: []string{"no-such-program-anywhere.exe"},
		Cols:    80, Rows: 24,
	})
	if err == nil {
		t.Fatal("a command that is not there started")
	}
}
