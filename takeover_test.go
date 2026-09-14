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
		Addr: a.server.Addr(), Key: mine,
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
