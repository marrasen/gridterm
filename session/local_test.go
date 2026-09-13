//go:build !windows

package session

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// readUntil pumps the session until want appears or the deadline passes.
// A pty delivers output in arbitrarily sized chunks, so no single Read
// can be assumed to hold a whole line.
func readUntil(t *testing.T, s Session, want string, timeout time.Duration) string {
	t.Helper()
	// The buffer is shared with the reader goroutine and is read again
	// on the timeout path, so it needs a lock. Without one the failure
	// message races with the reader and -race turns a timeout into a
	// data-race report instead.
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
			if n > 0 {
				mu.Lock()
				buf.Write(b[:n])
				got := strings.Contains(buf.String(), want)
				mu.Unlock()
				if got {
					return
				}
			}
			if err != nil {
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

func TestStartLocalRunsACommand(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "echo hello-from-the-pty"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	got := readUntil(t, s, "hello-from-the-pty", 5*time.Second)
	if !strings.Contains(got, "hello-from-the-pty") {
		t.Fatalf("output = %q, want it to contain hello-from-the-pty", got)
	}
}

// The shell must see the size before it starts, or it lays out its
// prompt for the wrong width.
func TestInitialSizeIsVisibleToTheChild(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "stty size"},
		Cols:    123, Rows: 45,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	got := readUntil(t, s, "45 123", 5*time.Second)
	if !strings.Contains(got, "45 123") {
		t.Fatalf("stty size reported %q, want 45 123", strings.TrimSpace(got))
	}
}

func TestResizeReachesTheChild(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "read _; stty size"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	if err := s.Resize(101, 37); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if _, err := s.Write([]byte("\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := readUntil(t, s, "37 101", 5*time.Second)
	if !strings.Contains(got, "37 101") {
		t.Fatalf("stty size after resize = %q, want 37 101", strings.TrimSpace(got))
	}
}

func TestWriteReachesTheChild(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "read line; echo got:$line"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	if _, err := s.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := readUntil(t, s, "got:ping", 5*time.Second)
	if !strings.Contains(got, "got:ping") {
		t.Fatalf("output = %q", got)
	}
}

// A pty master reports the child's exit differently on each platform.
// Read must normalise that to io.EOF or the caller has three cases.
func TestReadReturnsEOFWhenTheChildExits(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "exit 0"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	done := make(chan error, 1)
	go func() {
		b := make([]byte, 256)
		for {
			_, err := s.Read(b)
			if err != nil {
				done <- err
				return
			}
		}
	}()

	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("Read returned %v, want io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read never returned after the child exited")
	}
}

func TestWaitReportsTheExitStatus(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "exit 3"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	// Drain, or the child can block writing and never exit.
	go io.Copy(io.Discard, s)

	err = s.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Wait returned %v (%T), want an *exec.ExitError", err, err)
	}
	if got := exit.ExitCode(); got != 3 {
		t.Fatalf("exit code = %d, want 3", got)
	}
}

// Close is reachable from more than one path — the window closing, the
// shell exiting, a read error — so it has to tolerate being called
// twice.
func TestCloseIsIdempotent(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "sleep 30"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// Close is reachable from several goroutines at once — the window
// closing while the reaper is also finishing — which is what the
// sync.Once is there for.
func TestCloseIsSafeConcurrently(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "sleep 30"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
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

// Closing the pty master discards whatever the kernel still had
// buffered, so a short command's entire output could vanish before the
// pump ever saw it.
func TestOutputSurvivesAChildThatExitsImmediately(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "printf 'x%.0s' $(seq 1 4000); exit 0"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	// Start reading late, as a terminal busy with a frame would.
	time.Sleep(300 * time.Millisecond)

	var got []byte
	b := make([]byte, 4096)
	for {
		n, err := s.Read(b)
		got = append(got, b[:n]...)
		if err != nil {
			break
		}
	}
	if len(got) < 4000 {
		t.Fatalf("read %d bytes, want 4000: output was discarded when the child exited",
			len(got))
	}
}

// A child that ignores SIGHUP would otherwise outlive the window that
// started it, holding the pty open.
func TestCloseKillsAChildThatIgnoresHangup(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "trap '' HUP; while :; do sleep 1; done"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	go io.Copy(io.Discard, s)

	start := time.Now()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close only returns once the child is gone or has been killed.
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Close took %v; it should give up on the hangup and kill", d)
	}

	done := make(chan struct{})
	go func() { _ = s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the child survived Close")
	}
}

func TestWaitIsIdempotent(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "exit 0"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()
	go io.Copy(io.Discard, s)

	first := s.Wait()
	if second := s.Wait(); second != first {
		t.Fatalf("second Wait returned %v, first returned %v", second, first)
	}
}

func TestEnvIsPassedThrough(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "echo v=$GRIDTERM_TEST"},
		Env:     []string{"GRIDTERM_TEST=marker"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	got := readUntil(t, s, "v=marker", 5*time.Second)
	if !strings.Contains(got, "v=marker") {
		t.Fatalf("output = %q", got)
	}
}

func TestTermIsSetByDefault(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "echo term=$TERM"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	got := readUntil(t, s, "term=", 5*time.Second)
	if !strings.Contains(got, "term=xterm-256color") {
		t.Fatalf("TERM = %q, want xterm-256color", strings.TrimSpace(got))
	}
}

func TestTermCanBeOverridden(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "echo term=$TERM"},
		Env:     []string{"TERM=dumb"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	got := readUntil(t, s, "term=", 5*time.Second)
	if !strings.Contains(got, "term=dumb") {
		t.Fatalf("TERM = %q, want dumb", strings.TrimSpace(got))
	}
}

func TestStartLocalReportsAMissingCommand(t *testing.T) {
	_, err := StartLocal(LocalConfig{
		Command: []string{"/nonexistent/definitely-not-a-shell"},
		Cols:    80, Rows: 24,
	})
	if err == nil {
		t.Fatal("StartLocal succeeded for a command that does not exist")
	}
}

func TestDirIsHonoured(t *testing.T) {
	dir := t.TempDir()
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "pwd"},
		Dir:     dir,
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	// macOS puts temp dirs under /var, which is a symlink to
	// /private/var, and pwd prints the physical path. Resolve both ends
	// rather than comparing the symlinked one.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		want = dir
	}
	// Wait for the newline, not the first slash: a pty can split the
	// write and leave a prefix of the path in the buffer.
	got := readUntil(t, s, "\n", 5*time.Second)
	if !strings.Contains(got, want) {
		t.Fatalf("pwd = %q, want it to contain %q", strings.TrimSpace(got), want)
	}
}

// "Ignored" has to mean the child's size is unchanged, not merely that
// no error came back.
func TestResizeWithNonsenseSizeLeavesTheSizeAlone(t *testing.T) {
	s, err := StartLocal(LocalConfig{
		Command: []string{"/bin/sh", "-c", "read _; stty size"},
		Cols:    77, Rows: 21,
	})
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer s.Close()

	if err := s.Resize(0, 0); err != nil {
		t.Errorf("Resize(0,0) returned %v, want nil", err)
	}
	if err := s.Resize(-5, -5); err != nil {
		t.Errorf("Resize(-5,-5) returned %v, want nil", err)
	}
	if _, err := s.Write([]byte("\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := readUntil(t, s, "21 77", 5*time.Second)
	if !strings.Contains(got, "21 77") {
		t.Fatalf("stty size = %q, want the original 21 77", strings.TrimSpace(got))
	}
}

func TestHasEnv(t *testing.T) {
	cases := []struct {
		env  []string
		key  string
		want bool
	}{
		{[]string{"TERM=xterm"}, "TERM", true},
		{[]string{"TERMINAL=x"}, "TERM", false},
		{[]string{"TERM="}, "TERM", true},
		{nil, "TERM", false},
		{[]string{"A=1", "TERM=x"}, "TERM", true},
		// Windows environment names are case-insensitive, and os/exec
		// keeps the last duplicate there, so a caller's "Term=dumb"
		// would lose to an appended "TERM=...".
		{[]string{"Term=dumb"}, "TERM", true},
		{[]string{"term=dumb"}, "TERM", true},
		{[]string{"TERMINFO=/x"}, "TERM", false},
	}
	for _, tc := range cases {
		if got := hasEnv(tc.env, tc.key); got != tc.want {
			t.Errorf("hasEnv(%q, %q) = %v, want %v", tc.env, tc.key, got, tc.want)
		}
	}
}
