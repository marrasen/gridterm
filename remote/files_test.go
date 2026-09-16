package remote

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// An SFTP session rides on the connection, so closing the machine closes
// the browser panes on it.
func TestFilesRideOnTheConnection(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.Files(t.Context())
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if got := c.riderCount(); got != 1 {
		t.Fatalf("the connection holds %d riders, want the session", got)
	}

	// Closing the session lets go of the connection's record of it, or
	// every browser pane ever opened would be held until the window
	// closed.
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := c.riderCount(); got != 0 {
		t.Fatalf("the connection still holds %d riders", got)
	}
	// Twice is the same answer, not a second failure.
	if err := f.Close(); err != nil {
		t.Fatalf("close again: %v", err)
	}
}

// Closing the connection closes the session, and the session knows it.
func TestClosingTheConnectionClosesItsFiles(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.Files(t.Context())
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close the connection: %v", err)
	}
	if _, err := f.Client().ReadDir("."); err == nil {
		t.Fatal("the session still listed a directory after its connection closed")
	}
	// And closing it afterwards is not a second failure.
	if err := f.Close(); err != nil {
		t.Fatalf("close the session: %v", err)
	}
}

// A machine that will not do SFTP says so, and leaves nothing behind.
//
// sftp's own NewClient opens a session and does not close it when the
// subsystem is refused, so a user retrying a browser pane would use up
// the machine's sessions until the connection itself could open nothing.
func TestRefusedSFTPLeavesNoSessionBehind(t *testing.T) {
	s := sshtest.New(t)
	s.RefuseSFTP()
	c := connectTest(t, s)

	const tries = 8
	for i := 0; i < tries; i++ {
		if f, err := c.Files(t.Context()); err == nil {
			_ = f.Close()
			t.Fatal("SFTP started on a machine that refuses it")
		}
	}
	if got := c.riderCount(); got != 0 {
		t.Fatalf("the connection is holding %d sessions that never started", got)
	}
	// Nothing is left open on the machine either. A real sshd allows ten
	// sessions at once by default, and one left behind for every attempt
	// would use them up.
	deadline := time.Now().Add(10 * time.Second)
	for s.Sessions() > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := s.Sessions(); got != 0 {
		t.Fatalf("the machine still has %d sessions open after %d refusals", got, tries)
	}
	// A shell still opens, which is what the machine would have run out
	// of room for.
	sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("a shell after %d refused SFTP starts: %v", tries, err)
	}
	if err := sh.Close(); err != nil {
		t.Fatalf("close the shell: %v", err)
	}
}

// Closing a session whose far end has stopped answering does not hold
// the window. Closing SFTP sends an end of file and waits for the far
// end to close the channel, and a machine that has gone never will.
func TestClosingFilesDoesNotWaitForeverOnADeadMachine(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)

	f, err := c.Files(t.Context())
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	// The machine stops answering without closing anything, which is
	// what dropping off the network looks like from here. A connection
	// that was closed would not do: the read fails at once then, and
	// this waits for a read that never returns.
	s.Freeze()

	done := make(chan error, 1)
	go func() { done <- f.Close() }()
	select {
	case err := <-done:
		// Whether it closed cleanly or reported the dead transport, it
		// came back.
		_ = err
	case <-time.After(drainGrace * 20):
		t.Fatal("closing the session never came back")
	}
	// And the connection it rides on closes too, which is the thing that
	// would otherwise hold the window: this runs on the goroutine that
	// draws.
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case <-closed:
	case <-time.After(drainGrace * 20):
		t.Fatal("closing the machine never came back")
	}
}

// Closing a session whose transport is dead in both directions comes
// back, and closing the machine after it comes back too.
//
// Closing the channel only sends a message. A machine that is still on
// the network and carrying nothing never answers it, so the SFTP
// client's own close stays parked on a read that nothing will wake.
// Waited for, that read holds the goroutine that draws, and it holds
// Conn.Close as well: closing the transport is the only thing that ends
// it, and Conn.Close does that after its riders.
func TestClosingFilesOnADeadTransportComesBack(t *testing.T) {
	s := sshtest.New(t)
	c, wire := connectDeaf(t, s)

	f, err := c.Files(t.Context())
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	// Nothing crosses in either direction from here on, and it never
	// starts again.
	wire.goDeaf()

	done := make(chan error, 1)
	go func() { done <- f.Close() }()
	select {
	case err := <-done:
		// And it says which way it ended: the close was left behind
		// rather than finishing, which is logged rather than shown.
		if !errors.Is(err, ErrCloseAbandoned) {
			t.Errorf("close gave %v, want the close to have been left behind", err)
		}
	case <-time.After(drainGrace * 20):
		t.Fatal("closing the session never came back")
	}

	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case <-closed:
	case <-time.After(drainGrace * 20):
		t.Fatal("closing the machine never came back")
	}
	// And the close that was left behind has finished: the transport
	// going is what wakes the read it was parked on. Asked again, it
	// waits for that read the same way, so an answer now means the read
	// has ended.
	again := make(chan error, 1)
	go func() { again <- f.Client().Close() }()
	select {
	case <-again:
	case <-time.After(drainGrace * 20):
		t.Fatal("the close that was left behind is still waiting on the dead transport")
	}
}

// deafWire is a connection that carries nothing in either direction
// without hanging up.
//
// What a machine that has dropped off the network looks like from here:
// the socket is still there, what is written to it goes nowhere, and
// nothing comes back. A machine that hung up would be a different
// thing, and would end everything waiting on it by itself.
type deafWire struct {
	net.Conn

	mu   sync.Mutex
	deaf bool
}

// goDeaf drops everything written from now on, so the machine hears no
// more and answers nothing.
func (w *deafWire) goDeaf() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deaf = true
}

// Write throws the bytes away once the wire has gone deaf, and says they
// were sent: a socket into a network that has gone takes them just the
// same.
func (w *deafWire) Write(p []byte) (int, error) {
	w.mu.Lock()
	deaf := w.deaf
	w.mu.Unlock()
	if deaf {
		return len(p), nil
	}
	return w.Conn.Write(p)
}

// connectDeaf connects to the test server over a wire the test can make
// deaf.
func connectDeaf(t *testing.T, s *sshtest.Server) (*Conn, *deafWire) {
	t.Helper()
	w := &deafWire{}
	to := func(ctx context.Context, addr string) (net.Conn, error) {
		nc, err := overTCP(ctx, addr)
		if err != nil {
			return nil, err
		}
		w.Conn = nc
		return w, nil
	}
	c, err := connect(t.Context(), to, nil, testConfig(t, s))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, w
}

// A session opened on a connection that has closed is refused, or it
// would be held by nothing and closed by nothing.
func TestFilesOnAClosedConnectionIsRefused(t *testing.T) {
	s := sshtest.New(t)
	c := connectTest(t, s)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := c.Files(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("error = %v, want ErrClosed", err)
	}
}
