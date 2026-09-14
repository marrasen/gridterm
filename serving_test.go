package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// withServing points a test app's serving files at a directory of its
// own and writes the keys given there.
func withServing(t *testing.T, a *testApp, allowed string) {
	t.Helper()
	at := t.TempDir()
	a.servePaths = servePaths{
		hostKey: filepath.Join(at, "serve_host_key"),
		allowed: filepath.Join(at, "authorized_keys"),
	}
	if allowed != "" {
		if err := os.WriteFile(a.servePaths.allowed, []byte(allowed), 0o600); err != nil {
			t.Fatalf("write the keys: %v", err)
		}
	}
	t.Cleanup(func() { _ = a.stopServing() })
}

// aPublicKey is one line of an authorized_keys file.
func aPublicKey(t *testing.T, comment string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("use the key: %v", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + comment
}

// A window nobody is allowed to connect to says so, and says where to
// put a key, rather than offering to open a port that serves nobody.
func TestServingNobodySaysWhereToPutAKey(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, "")

	err := a.openServing()

	if err == nil {
		t.Fatal("it offered to serve nobody")
	}
	if !strings.Contains(err.Error(), a.servePaths.allowed) {
		t.Errorf("it said %v, without saying where the keys go", err)
	}
	if a.server != nil {
		t.Error("a port was opened")
	}
}

// Serving this machine only listens on the loopback, whatever else the
// machine can be reached on.
func TestServingThisMachineOnlyStaysOnTheLoopback(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))

	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	host, _, err := net.SplitHostPort(a.server.Addr())
	if err != nil {
		t.Fatalf("the server is at %q: %v", a.server.Addr(), err)
	}
	if host != "127.0.0.1" {
		t.Errorf("it is listening on %q, want the loopback", host)
	}
}

// A port that is not a port is refused, and nothing is opened.
func TestARefusedPortOpensNothing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))

	for _, port := range []string{"", "no", "-1", "70000", "1.5"} {
		if err := a.startServing(port, whereHere); err == nil {
			t.Errorf("%q was taken as a port", port)
		}
		if a.server != nil {
			t.Fatalf("%q opened a port", port)
		}
	}
}

// Stopping closes the port. The window is not being served afterwards,
// and says so when asked again.
func TestStoppingClosesThePort(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr := a.server.Addr()

	if err := a.stopServing(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if a.server != nil {
		t.Fatal("the window still holds a server")
	}
	c, err := net.Dial("tcp", addr)
	if err == nil {
		c.Close()
		t.Error("the port is still open")
	}
}

// Serving is never on until it is turned on. Opening a window does not
// open a port.
func TestAWindowDoesNotServeUntilItIsTold(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))

	if a.server != nil {
		t.Fatal("a window opened a port by itself")
	}
}

// The dialog shows the fingerprint, which is the only thing a client
// has to check this machine by on a first connection.
func TestTheServingDialogShowsTheFingerprint(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	f := openDialog(t, a)

	text := strings.Join(f.Lines, "\n")
	if !strings.Contains(text, "SHA256:") {
		t.Errorf("the dialog does not show a fingerprint:\n%s", text)
	}
	if !strings.Contains(text, a.server.Addr()) {
		t.Errorf("the dialog does not say where it is serving:\n%s", text)
	}
}
