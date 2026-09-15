package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/serve"
)

// A real window serving, and a real window working in its shell.
//
// Everything below this is exercised in pieces elsewhere. This is the
// one test that says the pieces are wired to each other: the dialog
// opens a real port, a key in the real file gets in, and what comes
// back is the output of a shell on the machine being served.
func TestOneWindowWorksInAnotherMachinesShell(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	mine, line := aKeyPair(t)
	withServing(t, a, line)
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	w, err := serve.Dial(context.Background(), serve.DialConfig{
		Addr: a.server.Addr(), Keys: []ssh.Signer{mine},
		HostKey: ssh.FixedHostKey(a.server.HostKey()),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer w.Close()

	sess, err := w.Open(80, 24)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	if _, err := sess.Write([]byte("echo taken-over-ok\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		n, err := sess.Read(buf)
		got.Write(buf[:n])
		if strings.Contains(got.String(), "taken-over-ok") {
			return
		}
		if err != nil {
			t.Fatalf("read: %v, having got %q", err, got.String())
		}
	}
	t.Fatalf("no answer from the shell: %q", got.String())
}

// The whole path a user takes: one window serves, another takes it
// over from its own dialog, and a pane here draws a shell there.
func TestTakingOverAWindowOpensAPaneOnIt(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	panes := len(client.panes)

	if err := client.takeOver(host.server.Addr(), keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	// The window has never been reached before, so its key is offered
	// and has to be accepted, the same as any other machine's.
	answer(t, client, "Connect")

	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows[host.server.Addr()] != nil && len(client.panes) > panes
	})
	if got := len(host.server.Clients()); got != 1 {
		t.Errorf("%d clients on the serving window, want one", got)
	}

	// And the panel says so, under the address it was reached at.
	var named bool
	for _, line := range panelText(client, panelNow) {
		if strings.Contains(line, host.server.Addr()) {
			named = true
		}
	}
	if !named {
		t.Errorf("the panel does not name the window taken over: %v", panelText(client, panelNow))
	}
}

// Letting go of a window takes the panes drawn from it, and hangs up.
func TestLettingGoOfATakenWindowTakesItsPanes(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr := host.server.Addr()

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	panes := len(client.panes)
	if err := client.takeOver(addr, keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "a pane on the other window", func() bool {
		return len(client.panes) > panes
	})

	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("drop: %v", err)
	}

	if client.windows[addr] != nil {
		t.Error("the window is still held")
	}
	if got := len(client.panes); got != panes {
		t.Errorf("%d panes are left, want the %d there were before", got, panes)
	}
	waitFor(t, client, "the serving window to see it go", func() bool {
		return len(host.server.Clients()) == 0
	})
}

// A window that is not serving is not taken over, and the failure is
// shown rather than leaving a row that never fills.
func TestTakingOverAWindowThatIsNotThereFails(t *testing.T) {
	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	keyFile, _ := aKeyFile(t)

	// A port nothing is listening on.
	if err := client.takeOver("127.0.0.1:1", keyFile); err != nil {
		t.Fatalf("take over: %v", err)
	}

	f := openDialog(t, client)
	if text := strings.Join(f.Lines, "\n"); !strings.Contains(text, "Could not take over") &&
		!strings.Contains(f.Title, "Could not take over") {
		t.Errorf("the failure was not shown: %q %v", f.Title, f.Lines)
	}
	if client.windows["127.0.0.1:1"] != nil {
		t.Error("a window that answered nothing was held anyway")
	}
}
