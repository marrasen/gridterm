package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/serve"
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

// aKeyPair makes a key and the authorized_keys line for it.
func aKeyPair(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("use the key: %v", err)
	}
	return signer, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " marcus@laptop"
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

// Pressing Serve on the dialog as it opens works.
//
// It did not: the default port was the field's placeholder rather than
// its text, so the first press sent an empty string to be read as a
// number. Every test called startServing directly, so nothing noticed.
func TestPressingServeOnTheDialogAsItOpensWorks(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.openServing(); err != nil {
		t.Fatalf("open: %v", err)
	}
	f := openDialog(t, a)

	pressButton(t, a, f, "Serve")

	if a.server == nil {
		t.Fatal("pressing Serve did not open a port")
	}
	if _, port, _ := net.SplitHostPort(a.server.Addr()); port != strconv.Itoa(servePort) {
		t.Errorf("it is serving on port %s, want the %d the dialog offered", port, servePort)
	}
}

// And the dialog that says what is being served survives being opened.
//
// A form closes as soon as a button's action returns, and closing a
// dialog takes anything stacked on top of it. Opened from inside the
// action, the dialog with the port and the fingerprint in it was
// destroyed the moment it appeared.
func TestTheServingDialogSurvivesTheButtonThatOpensIt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.openServing(); err != nil {
		t.Fatalf("open: %v", err)
	}
	pressButton(t, a, openDialog(t, a), "Serve")

	f := openDialog(t, a)

	text := strings.Join(f.Lines, "\n")
	if !strings.Contains(text, "SHA256:") {
		t.Errorf("the dialog does not show a fingerprint:\n%s", text)
	}
	if !strings.Contains(text, a.server.Addr()) {
		t.Errorf("the dialog does not say where it is serving:\n%s", text)
	}
}

// The fingerprint shown is the one the running server presents.
//
// Read back from disk, it was whatever the file said now -- and if the
// file had gone, serve.HostKey quietly made a new one, so the dialog
// handed the user a fingerprint to check that could never match. That
// trains somebody to click past the one warning that matters.
func TestTheFingerprintShownIsTheOneBeingServed(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	want := serve.Fingerprint(a.server.HostKey())

	// The file goes, the way a cleanup tool or a tidy-up would take it.
	if err := os.Remove(a.servePaths.hostKey); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := a.showServing(); err != nil {
		t.Fatalf("show: %v", err)
	}

	f := openDialog(t, a)
	if text := strings.Join(f.Lines, "\n"); !strings.Contains(text, want) {
		t.Errorf("the dialog shows a fingerprint the server does not present:\n%s\nwant %s",
			text, want)
	}
}

// Starting twice is refused rather than leaving the first listener with
// nothing able to reach it or close it.
func TestServingTwiceIsRefused(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	first := a.server.Addr()

	err := a.startServing("0", whereHere)

	if err == nil {
		t.Fatal("it started serving twice")
	}
	if a.server.Addr() != first {
		t.Errorf("the window now holds %s, having let go of %s", a.server.Addr(), first)
	}
	if err := a.stopServing(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	c, dialErr := net.Dial("tcp", first)
	if dialErr == nil {
		c.Close()
		t.Error("the first port is still open after stopping")
	}
}

// A window that stops serving because its listener failed says so, and
// stops claiming to be served.
//
// It used to report the failure to a log nobody is reading and leave
// the dialog offering to stop a listener that had already gone.
func TestALostListenerIsSaidAndNotClaimed(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	a.servingStopped(errors.New("the listener gave up"))

	if a.server != nil {
		t.Error("the window still says it is being served")
	}
	f := openDialog(t, a)
	if text := strings.Join(f.Lines, "\n"); !strings.Contains(text, "gave up") {
		t.Errorf("the user was not told why:\n%s", text)
	}
}

// Which addresses a choice means.
//
// "Anywhere" is every address the machine has, which is what reaching
// it over Tailscale needs. Everything else is this machine only: the
// wider one has to be asked for by name, so a dialog that changed its
// wording cannot quietly open the port to the network.
func TestWhichAddressesAChoiceMeans(t *testing.T) {
	if got := listenHost(whereAnywhere); got != "" {
		t.Errorf("anywhere means %q, want every address", got)
	}
	for _, where := range []string{whereHere, "", "anywhere", "0.0.0.0", "::", "Anywhere"} {
		if got := listenHost(where); got != "127.0.0.1" {
			t.Errorf("%q means %q, want this machine only", where, got)
		}
	}
}

// Serving anywhere really does listen on every address.
//
// Off by default, and asked for by name. Listening on every address is
// the one thing a test can do that reaches outside the machine running
// it: on Windows it makes the firewall ask whoever is at the keyboard
// whether the test binary may accept connections, every run. Set
// GRIDTERM_TEST_NETWORK=1 to have it.
func TestServingAnywhereListensOnEveryAddress(t *testing.T) {
	if os.Getenv("GRIDTERM_TEST_NETWORK") != "1" {
		t.Skip("set GRIDTERM_TEST_NETWORK=1 to let a test open a port to the network")
	}
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))

	if err := a.startServing("0", whereAnywhere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	host, _, err := net.SplitHostPort(a.server.Addr())
	if err != nil {
		t.Fatalf("the server is at %q: %v", a.server.Addr(), err)
	}
	if host != "::" && host != "0.0.0.0" {
		t.Errorf("it is listening on %q, want every address", host)
	}
}

// aKeyFile writes a private key for a test and returns its path and the
// authorized_keys line for it.
func aKeyFile(t *testing.T) (path, line string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "a test")
	if err != nil {
		t.Fatalf("encode the key: %v", err)
	}
	path = filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write the key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("use the key: %v", err)
	}
	return path, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) +
		" marcus@laptop"
}
