package remote

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// One machine reached through another, with no local port opened for it.
func TestThroughReachesTheFarMachine(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)

	bastion := connectTest(t, near)
	c, err := bastion.Through(t.Context(), testConfig(t, far))
	if err != nil {
		t.Fatalf("Through: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// It is a connection like any other: a shell runs on it.
	sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Shell on the far machine: %v", err)
	}
	if got := readUntil(t, sh, "READY", 5*time.Second); !strings.Contains(got, "READY") {
		t.Fatalf("the far shell said %q", strings.TrimSpace(got))
	}

	// It went through the near machine rather than from here, and no
	// local port was opened for it.
	if n := near.Forwards(); n != 1 {
		t.Fatalf("the near machine carried %d streams, want the one hop", n)
	}
	farHost, farPort := far.Host()
	want := net.JoinHostPort(farHost, strconv.Itoa(farPort))
	if got := near.Forwarded(); len(got) != 1 || got[0] != want {
		t.Fatalf("the near machine was asked to reach %v, want %q", got, want)
	}
	if n := far.Conns(); n != 1 {
		t.Fatalf("the far machine saw %d connections, want 1", n)
	}
	if got := c.Via(); got != bastion {
		t.Fatalf("the far connection says it came through %v, want the bastion", got)
	}
}

// Closing the machine in the middle closes what was reached through it.
// A connection whose carrier has gone reads nothing and never ends.
func TestClosingTheBastionClosesWhatRidesOnIt(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)

	bastion := connectTest(t, near)
	c, err := bastion.Through(t.Context(), testConfig(t, far))
	if err != nil {
		t.Fatalf("Through: %v", err)
	}
	sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	readUntil(t, sh, "READY", 5*time.Second)

	if err := bastion.Close(); err != nil {
		t.Fatalf("close the bastion: %v", err)
	}
	if _, err := c.Shell(ShellConfig{Cols: 80, Rows: 24}); err == nil {
		t.Fatal("the far connection still opened a shell after its carrier closed")
	}
	if _, err := sh.Write([]byte("ping\n")); err == nil {
		t.Fatal("the far shell still took input after its carrier closed")
	}
}

// A far connection that closes on its own must not leave the machine in
// the middle holding a record of it.
func TestTheBastionForgetsAClosedHop(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)

	bastion := connectTest(t, near)
	c, err := bastion.Through(t.Context(), testConfig(t, far))
	if err != nil {
		t.Fatalf("Through: %v", err)
	}
	if bastion.riderCount() != 1 {
		t.Fatalf("the bastion holds %d riders, want the hop", bastion.riderCount())
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close the hop: %v", err)
	}
	if n := bastion.riderCount(); n != 0 {
		t.Fatalf("the bastion still holds %d riders after the hop closed", n)
	}
}

// A hop through a machine that has already gone is held by nothing and
// closed by nothing.
func TestThroughAClosedConnectionIsRefused(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)

	bastion := connectTest(t, near)
	if err := bastion.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err := bastion.Through(t.Context(), testConfig(t, far))
	if err == nil {
		t.Fatal("a hop was made through a connection that had closed")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("error = %v, want ErrClosed", err)
	}
}

// Three machines in a line, each reached through the one before it.
func TestThroughChainsAsFarAsItIsAsked(t *testing.T) {
	first := sshtest.New(t)
	second := sshtest.New(t)
	third := sshtest.New(t)

	a := connectTest(t, first)
	b, err := a.Through(t.Context(), testConfig(t, second))
	if err != nil {
		t.Fatalf("the second hop: %v", err)
	}
	c, err := b.Through(t.Context(), testConfig(t, third))
	if err != nil {
		t.Fatalf("the third hop: %v", err)
	}

	sh, err := c.Shell(ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Shell at the far end: %v", err)
	}
	if got := readUntil(t, sh, "READY", 5*time.Second); !strings.Contains(got, "READY") {
		t.Fatalf("the far shell said %q", strings.TrimSpace(got))
	}

	// Closing the first takes the whole chain with it.
	if err := a.Close(); err != nil {
		t.Fatalf("close the first: %v", err)
	}
	if _, err := c.Shell(ShellConfig{Cols: 80, Rows: 24}); err == nil {
		t.Fatal("the far end survived the first machine closing")
	}
}

// The host key of the far machine is checked, not the near one's. A hop
// that trusted whatever it found would be a way past the whole check.
func TestThroughChecksTheFarMachinesHostKey(t *testing.T) {
	near := sshtest.New(t)
	far := sshtest.New(t)
	other := sshtest.New(t)

	bastion := connectTest(t, near)
	cfg := checkedConfig(t, far, sshtest.WriteKnownHosts(t, far.LineFor(other.HostKey())))
	cfg.Ask = nil

	_, err := bastion.Through(t.Context(), cfg)
	if err == nil {
		t.Fatal("a hop accepted a host key that did not match known_hosts")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want it to say the key does not match", err)
	}
}

// Cancelling reaches a hop as surely as it reaches a connection made
// from here.
func TestThroughIsCancelled(t *testing.T) {
	near := sshtest.New(t)
	host, port := sshtest.Deaf(t)

	bastion := connectTest(t, near)
	cfg := testConfig(t, sshtest.New(t))
	cfg.Host, cfg.Port = host, port

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		c, err := bastion.Through(ctx, cfg)
		if c != nil {
			_ = c.Close()
		}
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the hop succeeded against a machine that never spoke")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not reach the hop")
	}
}
