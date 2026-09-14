package remote

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
)

func TestShellStarts(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)

	got := readUntil(t, sess, "READY", 5*time.Second)
	if !strings.Contains(got, "READY 80x24") {
		t.Fatalf("greeting = %q, want READY 80x24", strings.TrimSpace(got))
	}
}

// The pty has to be requested with the right size before the shell
// starts, or the remote lays out its first prompt for the wrong width.
func TestShellRequestsThePtyWithTheInitialSize(t *testing.T) {
	s := sshtest.New(t)
	startTest(t, s, func(sh *ShellConfig) { sh.Cols, sh.Rows = 133, 42 })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cols, rows := s.Size(); cols == 133 && rows == 42 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cols, rows := s.Size()
	t.Fatalf("server saw %dx%d, want 133x42", cols, rows)
}

func TestShellResizeSendsWindowChange(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)
	readUntil(t, sess, "READY", 5*time.Second)

	if err := sess.Resize(101, 37); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	got := readUntil(t, sess, "SIZE", 5*time.Second)
	if !strings.Contains(got, "SIZE 101x37") {
		t.Fatalf("server reported %q, want SIZE 101x37", strings.TrimSpace(got))
	}
}

func TestShellResizeWithNonsenseSizeIsIgnored(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)
	readUntil(t, sess, "READY", 5*time.Second)

	if err := sess.Resize(0, 0); err != nil {
		t.Errorf("Resize(0,0) = %v, want nil", err)
	}
	if cols, rows := s.Size(); cols != 80 || rows != 24 {
		t.Fatalf("server saw %dx%d after a nonsense resize, want the original 80x24",
			cols, rows)
	}
}

func TestShellWriteReachesTheRemote(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)
	readUntil(t, sess, "READY", 5*time.Second)

	if _, err := sess.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := readUntil(t, sess, "ping", 5*time.Second)
	if !strings.Contains(got, "ping") {
		t.Fatalf("echo = %q", got)
	}
}

// A remote shell exiting non-zero is an ordinary end of session, not a
// read error: the reader must see io.EOF.
func TestShellReadReturnsEOFWhenTheRemoteExits(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)
	readUntil(t, sess, "READY", 5*time.Second)

	if _, err := sess.Write([]byte("bye\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		b := make([]byte, 256)
		for {
			if _, err := sess.Read(b); err != nil {
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
		t.Fatal("Read never returned after the remote exited")
	}
}

func TestShellRunsACommand(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, func(sh *ShellConfig) {
		sh.Command = []string{"echo", "hello world"}
	})

	got := readUntil(t, sess, "RAN", 5*time.Second)
	if !strings.Contains(got, "RAN 'echo' 'hello world'") {
		t.Fatalf("server ran %q, want the quoted argv", strings.TrimSpace(got))
	}
}

// A dropped connection is not a clean logout. Reporting it as one hides
// the difference between closing a window and losing the network.
func TestShellDroppedConnectionIsNotReportedAsACleanExit(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)
	readUntil(t, sess, "READY", 5*time.Second)

	// Cut the connection underneath the session.
	s.CloseClients()

	done := make(chan error, 1)
	go func() {
		b := make([]byte, 256)
		for {
			if _, err := sess.Read(b); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err == nil || err == io.EOF {
			t.Fatalf("Read returned %v for a dropped connection, want a real error", err)
		}
		if !strings.Contains(err.Error(), "closed by the remote host") {
			t.Fatalf("Read returned %v, want it to say the connection was closed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read never returned after the connection dropped")
	}
}

// The remote exits 7, which must still read as an ordinary end of
// session rather than an error.
func TestShellWaitReportsTheRemoteExitStatus(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)
	readUntil(t, sess, "READY", 5*time.Second)
	_, _ = sess.Write([]byte("bye\n"))
	drain(sess)

	err := sess.Wait()
	var exit *ssh.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Wait returned %v (%T), want an *ssh.ExitError", err, err)
	}
	if got := exit.ExitStatus(); got != 7 {
		t.Fatalf("exit status = %d, want 7", got)
	}
	if second := sess.Wait(); !sameErr(err, second) {
		t.Fatalf("second Wait = %v, first = %v", second, err)
	}
}

// Closing the window with a keystroke still in flight used to race:
// closing the session stdin writes a flag that Write reads.
func TestShellCloseRacingWriteIsSafe(t *testing.T) {
	s := sshtest.New(t)
	sess := startTest(t, s, nil)
	readUntil(t, sess, "READY", 5*time.Second)
	drain(sess)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if _, err := sess.Write([]byte("x")); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		_ = sess.Close()
	}()
	wg.Wait()
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"ls"}, `'ls'`},
		{[]string{"echo", "a b"}, `'echo' 'a b'`},
		{[]string{"echo", "it's"}, `'echo' 'it'\''s'`},
		{[]string{"echo", "$HOME"}, `'echo' '$HOME'`},
		{nil, ``},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.argv); got != tc.want {
			t.Errorf("shellQuote(%q) = %s, want %s", tc.argv, got, tc.want)
		}
	}
}

func TestSessionEnd(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantEOF bool
	}{
		{"clean exit", nil, true},
		{"non-zero exit", &ssh.ExitError{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionEnd(tc.err)
			if (got == io.EOF) != tc.wantEOF {
				t.Errorf("sessionEnd(%v) = %v, want EOF=%v", tc.err, got, tc.wantEOF)
			}
		})
	}

	// A session that ends with no exit status is a dropped connection,
	// not a logout.
	got := sessionEnd(&ssh.ExitMissingError{})
	if got == io.EOF || got == nil {
		t.Fatalf("sessionEnd(ExitMissingError) = %v, want a real error", got)
	}
}

func sameErr(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Error() == b.Error()
}
