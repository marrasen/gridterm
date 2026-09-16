package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"golang.org/x/crypto/ssh"
)

// withServing points a test app's serving files at a directory of its
// own, writes the keys given there, and says where they went.
func withServing(t *testing.T, a *testApp, allowed string) servePaths {
	t.Helper()
	at := t.TempDir()
	paths := servePaths{
		hostKey: filepath.Join(at, "serve_host_key"),
		allowed: filepath.Join(at, "authorized_keys"),
	}
	a.serving.usePaths(paths)
	if allowed != "" {
		if err := os.WriteFile(paths.allowed, []byte(allowed), 0o600); err != nil {
			t.Fatalf("write the keys: %v", err)
		}
	}
	t.Cleanup(func() { _ = a.stopServing() })
	return paths
}

// withSettings points a test app's settings at a file of its own and
// hands back what was loaded, along with why it could not be read.
func withSettings(t *testing.T, a *testApp, path string) (*settings.Settings, error) {
	t.Helper()
	set, err := settings.Load(path)
	a.useSettings(set)
	return set, err
}

// openServeDialog opens the serve dialog the way a user does, from the
// palette.
func openServeDialog(t *testing.T, a *testApp) *ui.Form {
	t.Helper()
	runFromPalette(t, a, "serve.window")
	return awaitModal[*ui.Form](t, a, "the serve dialog",
		byTitle[*ui.Form]("Serve this window"))
}

// aFreePort is a port nothing is listening on, for a test that has to
// name one rather than ask for whichever is free.
func aFreePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("the listener is at %q: %v", l.Addr(), err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("let the port go again: %v", err)
	}
	return port
}

// fieldSays checks what a dialog field is showing.
func fieldSays(t *testing.T, f *ui.Form, label, want string) {
	t.Helper()
	if got := f.Field(label).Text(); got != want {
		t.Errorf("the %q field says %q, want %q", label, got, want)
	}
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
	paths := withServing(t, a, "")

	err := a.openServing()

	if err == nil {
		t.Fatal("it offered to serve nobody")
	}
	if !strings.Contains(err.Error(), paths.allowed) {
		t.Errorf("it said %v, without saying where the keys go", err)
	}
	if a.serving.on() {
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

	host, _, err := net.SplitHostPort(a.serving.addr())
	if err != nil {
		t.Fatalf("the server is at %q: %v", a.serving.addr(), err)
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
		if a.serving.on() {
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
	addr := a.serving.addr()

	if err := a.stopServing(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if a.serving.on() {
		t.Fatal("the window still holds a server")
	}
	c, err := net.Dial("tcp", addr)
	if err == nil {
		_ = c.Close()
		t.Error("the port is still open")
	}
}

// Serving is never on until it is turned on. Opening a window does not
// open a port.
func TestAWindowDoesNotServeUntilItIsTold(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))

	if a.serving.on() {
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
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)

	pressButton(t, a, f, "Serve")

	if !a.serving.on() {
		t.Fatal("pressing Serve did not open a port")
	}
	if _, port, _ := net.SplitHostPort(a.serving.addr()); port != strconv.Itoa(servePort) {
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
	pressButton(t, a, awaitModal[*ui.Form](t, a, "a dialog", nil), "Serve")

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)

	text := strings.Join(f.Lines, "\n")
	if !strings.Contains(text, "SHA256:") {
		t.Errorf("the dialog does not show a fingerprint:\n%s", text)
	}
	if !strings.Contains(text, a.serving.addr()) {
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
	paths := withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	want := serve.Fingerprint(a.serving.server.HostKey())

	// The file goes, the way a cleanup tool or a tidy-up would take it.
	if err := os.Remove(paths.hostKey); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := a.showServing(); err != nil {
		t.Fatalf("show: %v", err)
	}

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
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
	first := a.serving.addr()

	err := a.startServing("0", whereHere)

	if err == nil {
		t.Fatal("it started serving twice")
	}
	if a.serving.addr() != first {
		t.Errorf("the window now holds %s, having let go of %s", a.serving.addr(), first)
	}
	if err := a.stopServing(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	c, dialErr := net.Dial("tcp", first)
	if dialErr == nil {
		_ = c.Close()
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

	if a.serving.on() {
		t.Error("the window still says it is being served")
	}
	n := awaitModal[*ui.Notice](t, a, "a notice", nil)
	if !strings.Contains(n.Message(), "gave up") {
		t.Errorf("the user was not told why:\n%s", n.Message())
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

	host, _, err := net.SplitHostPort(a.serving.addr())
	if err != nil {
		t.Fatalf("the server is at %q: %v", a.serving.addr(), err)
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

// The serve dialog opens on whatever was last served with, rather than
// asking for the port again every time.
func TestTheServeDialogOpensOnWhatWasLastServedWith(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	set, err := withSettings(t, a, filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}

	want := aFreePort(t)

	f := openServeDialog(t, a)
	retypeField(t, a, f, "Port", want)
	pressButton(t, a, f, "Serve")
	pressButton(t, a, awaitModal[*ui.Form](t, a, "the serving dialog", nil), "Stop serving")

	if port, saved := set.ServePort(); !saved || strconv.Itoa(port) != want {
		t.Errorf("the settings hold port %d, %v; want %s saved", port, saved, want)
	}
	if reach, saved := set.ServeReach(); !saved || reach != settings.ReachHere {
		t.Errorf("the settings hold the reach %q, %v; want %q saved",
			reach, saved, settings.ReachHere)
	}
	again := openServeDialog(t, a)
	fieldSays(t, again, "Port", want)
	fieldSays(t, again, "Reachable from", whereHere)
}

// And the next window opens on it too: the settings are a file, not a
// memory of this run.
func TestAnotherWindowOpensOnTheSameSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	set, err := settings.Load(path)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	// Written rather than served, because serving anywhere opens a port
	// to the network and a test has no business doing that.
	if err := set.PutServe(2300, settings.ReachAnywhere); err != nil {
		t.Fatalf("save the settings: %v", err)
	}

	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
	}

	f := openServeDialog(t, a)

	fieldSays(t, f, "Port", "2300")
	fieldSays(t, f, "Reachable from", whereAnywhere)
}

// A port of 0 means whichever one is free, and it is remembered as that
// rather than as the port it happened to get.
func TestNoPortAskedForIsRememberedAsNoPortAskedFor(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	set, err := withSettings(t, a, filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}

	f := openServeDialog(t, a)
	retypeField(t, a, f, "Port", "0")
	pressButton(t, a, f, "Serve")

	if port, saved := set.ServePort(); !saved || port != 0 {
		t.Errorf("the settings hold port %d, %v; want 0 saved", port, saved)
	}
	pressButton(t, a, awaitModal[*ui.Form](t, a, "the serving dialog", nil), "Stop serving")
	fieldSays(t, openServeDialog(t, a), "Port", "0")
}

// Settings that cannot be written are said, and the window goes on
// serving: the port is open whatever the file did.
func TestASettingsFileThatWillNotTakeAWriteIsSaid(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	// Nothing is there to read, and nothing can be written either: what
	// the directory would be called is taken by a file.
	blocked := filepath.Join(t.TempDir(), "gridterm")
	if _, err := withSettings(t, a, filepath.Join(blocked, "settings.json")); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block the directory: %v", err)
	}

	f := openServeDialog(t, a)
	pressButton(t, a, f, "Serve")

	// On top of the dialog that says what is being served, not under it:
	// a notice nobody sees is a failure that was swallowed.
	n := awaitModal[*ui.Notice](t, a, "the settings notice", nil)
	if !strings.Contains(n.Title, "remember") {
		t.Errorf("the user was told %q, which does not say the settings were not kept", n.Title)
	}
	if !a.serving.on() {
		t.Fatal("a settings file that would not take a write stopped the window serving")
	}
}

// Settings nobody could read are said at the start, the dialog opens on
// the defaults, and serving does not write over the file.
func TestUnreadableSettingsAreSaidAndNotWrittenOver(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"version": 1, "servePort": 70000}`), 0o600); err != nil {
		t.Fatalf("write the settings: %v", err)
	}
	was, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	if _, err := withSettings(t, a, path); err == nil {
		t.Fatal("a port of 70000 loaded clean")
	}

	n := awaitModal[*ui.Notice](t, a, "the settings notice", nil)
	if !strings.Contains(n.Message(), "repaired") {
		t.Errorf("the user was not told to repair the file:\n%s", n.Message())
	}
	sendKey(t, a, press(input.KeyEscape, 0))

	f := openServeDialog(t, a)
	fieldSays(t, f, "Port", strconv.Itoa(servePort))
	fieldSays(t, f, "Reachable from", whereHere)
	pressButton(t, a, f, "Serve")

	if !a.serving.on() {
		t.Fatal("settings that could not be read stopped the window serving")
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings again: %v", err)
	}
	if !bytes.Equal(was, now) {
		t.Errorf("the settings were written over:\n%s\nwant\n%s", now, was)
	}
}

// The dialog says what a port of 0 means, so the 0 it opens on after a
// run that asked for one is a choice rather than a puzzle.
func TestTheDialogSaysWhatPortZeroMeans(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))

	f := openServeDialog(t, a)

	text := strings.Join(f.Lines, "\n")
	if !strings.Contains(text, "0 asks for whichever port is free") {
		t.Errorf("the dialog does not say what a port of 0 means:\n%s", text)
	}
}

// blackHole carries TCP to another address until it is told to stop,
// and then keeps both connections open and passes nothing until it is
// started again.
//
// What a machine that has dropped off the network looks like from here:
// the socket is still there, and nothing crosses it in either direction.
// A machine that hung up would be a different thing, and would end
// everything waiting on it by itself.
//
// A whole connection rather than sshtest.Freeze: that freezes one SFTP
// session, and x/crypto answers a channel close from the connection's own
// mux loop, so the close would be answered and the copy from the machine
// would end. Here nothing crosses the socket at all, which is what leaves
// that copy where it is.
//
// What arrives while it is stopped is held rather than thrown away, so a
// machine that comes back answers everything that was said to it in the
// meantime.
type blackHole struct {
	host string
	port int

	// server is the machine it carries to, so a test holding the relay can
	// ask that machine what it is serving. Set by whoever built the pair.
	server *sshtest.Server

	mu    sync.Mutex
	conns []net.Conn

	// stopped says nothing is being carried, and again is closed when it
	// starts again, which is what lets a held reader go on.
	stopped bool
	again   chan struct{}

	// done says the test has finished and has closed what was held, so a
	// connection accepted after that closes itself.
	done bool
}

// newBlackHole listens on a port of its own and carries what arrives to
// an address.
func newBlackHole(t *testing.T, to string) *blackHole {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	b := &blackHole{
		host: hostOf(t, ln.Addr().String()),
		port: portOf(t, ln.Addr().String()),
	}
	t.Cleanup(func() {
		_ = ln.Close()
		b.shutDown()
	})
	go func() {
		for {
			in, err := ln.Accept()
			if err != nil {
				return
			}
			out, err := net.Dial("tcp", to)
			if err != nil {
				// One connection that could not be carried is not the end
				// of the listener.
				_ = in.Close()
				continue
			}
			if !b.keep(in, out) {
				continue
			}
			go b.carry(in, out)
			go b.carry(out, in)
		}
	}()
	return b
}

// keep remembers a pair of connections for the test to close on its way
// out, and says whether it took them. One accepted after the test has
// already closed what it held is closed here instead, because nothing
// else ever would.
func (b *blackHole) keep(in, out net.Conn) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		_ = in.Close()
		_ = out.Close()
		return false
	}
	b.conns = append(b.conns, in, out)
	return true
}

// stop leaves both sides connected and stops carrying anything.
func (b *blackHole) stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped || b.done {
		return
	}
	b.stopped, b.again = true, make(chan struct{})
}

// resume starts carrying again, and what was held back crosses in the
// order it arrived. A machine that has come back on the network.
func (b *blackHole) resume() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.release()
}

// shutDown ends the relay for good and closes both sides, for a test on
// its way out.
func (b *blackHole) shutDown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.done = true
	b.release()
	// The copying goroutines are parked in a read, and closing what they
	// are reading is what ends them.
	for _, c := range b.conns {
		_ = c.Close()
	}
}

// release lets go of whatever is being held back, with the lock held.
func (b *blackHole) release() {
	if !b.stopped {
		return
	}
	b.stopped = false
	close(b.again)
	b.again = nil
}

// carry passes bytes one way, holding them back while the relay is
// stopped.
func (b *blackHole) carry(src, dst net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if !b.pass() {
				return
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// pass holds a reader up while the relay is stopped, and reports whether
// it may go on: one released by the test going away never carries again.
func (b *blackHole) pass() bool {
	b.mu.Lock()
	if b.done {
		b.mu.Unlock()
		return false
	}
	if !b.stopped {
		b.mu.Unlock()
		return true
	}
	again := b.again
	b.mu.Unlock()

	<-again
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.done
}

// said collects what a window logged, whichever goroutine reported it.
// Every test window has one from the moment it is built, as testApp.logged.
//
// logError runs wherever the failure was noticed, so the list is guarded:
// a test reading it while a client's goroutine appends to it would be a
// race whether or not it ever showed up as a wrong answer.
type said struct {
	mu    sync.Mutex
	lines []string
}

// add is what a window's onError is pointed at.
func (s *said) add(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, err.Error())
}

// holds reports whether anything logged so far has a phrase in it.
func (s *said) holds(what string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range s.lines {
		if strings.Contains(line, what) {
			return true
		}
	}
	return false
}

// all is everything logged so far, for a failure that has to say what it
// found instead.
func (s *said) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

// aRelayedWindow is a window that has taken another over through a relay
// the test can stop, with a file pane open on the window over there.
//
// What comes back is the window doing the taking over, the address it
// reached the other one at, and the relay, still carrying: it is the
// test's to stop and start.
func aRelayedWindow(t *testing.T) (client *testApp, addr string, relay *blackHole) {
	t.Helper()
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	withPanel(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	relay = newBlackHole(t, host.serving.addr())
	addr = net.JoinHostPort(relay.host, strconv.Itoa(relay.port))

	client = newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	panes := len(client.panes)
	if err := client.takeOver(addr, keyFile, nil); err != nil {
		t.Fatalf("take over: %v", err)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "a pane on the other window", func() bool {
		return client.windows.named(addr) != nil && len(client.panes) > panes
	}, host)
	openFilesFromThePlus(t, client, addr)
	return client, addr, relay
}

// aWedgedRelay is a window serving a client that has file panes on
// margit, with margit made to stop answering without hanging up.
//
// panes is how many file panes the client opens on margit first. What
// comes back is the two windows, the address the client reached the first
// one at, the relay carrying margit so a test can start it again, and
// what the serving window has logged.
func aWedgedRelay(t *testing.T, panes int) (
	host, client *testApp, addr string, relay *blackHole, logged *said) {

	t.Helper()
	host, client, addr = twoWindows(t)

	// margit reached through a relay the test can stop, so the machine
	// can be made to stop answering without hanging up.
	margit := sshtest.New(t)
	pinServers(t, host, margit)
	relay = newBlackHole(t, margit.Addr())
	relay.server = margit
	cfg := serverConfig(t, margit)
	cfg.Host, cfg.Port = relay.host, relay.port
	connectToMargit(t, host, client, addr, cfg)

	for i := 0; i < panes; i++ {
		openFilesFromTheFarPlus(t, client, host, addr, "margit")
	}
	waitFor(t, host, "margit to be serving every relayed file session", func() bool {
		return margit.SFTPs() == panes
	}, client)
	// The row is there to go away. Without this a window that never put
	// one up would pass a wait for it to go on its first turn.
	waitFor(t, host, "a row for the client working in this window", func() bool {
		return servingRows(host) == 1
	}, client)

	relay.stop()
	return host, client, addr, relay, host.logged
}

// A client that goes while the machine its file session was relayed to
// has stopped answering is still said to have gone, and is said to have
// gone at once.
//
// Closing the subsystem sends that machine a channel close and nothing
// more. One that has dropped off the network never answers it, so the
// copy from it stays where it is: waited for, it would hold the file
// session, the connection it rode on, and the row saying a client this
// window has already lost is still working here.
//
// The grace is for reading the machine's answer back to the client. A
// client whose whole connection has gone cannot read it, so there is
// nothing to wait for and the row goes now.
func TestAClientThatGoesIsSaidToHaveGoneThoughTheMachineIsWedged(t *testing.T) {
	host, client, addr, _, logged := aWedgedRelay(t, 1)

	started := time.Now()
	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("let go of the window: %v", err)
	}

	waitFor(t, host, "the window serving to say the client has gone", func() bool {
		return servingRows(host) == 0
	})
	if took := time.Since(started); took > relayGrace/2 {
		t.Errorf("the row for the client stayed %v, want it gone well inside the %v grace:"+
			" the client's own connection had gone, so nothing was waiting for margit", took, relayGrace)
	}
	// The copy from margit is left where it is, and counted while it is.
	waitFor(t, host, "the file session to be counted as parked on margit", func() bool {
		return parkedOn(host, "margit") == 1
	})
	// Not called an abandonment, though. The client walked away, and
	// margit was never given a grace to answer in.
	if logged.holds("abandoned a file session on margit") {
		t.Errorf("it blamed margit for a session the client walked away from: %v", logged.all())
	}
	// And the connection to margit is still held: the file session is what
	// went wrong, not the connection carrying it, and a window that closed
	// the whole thing would take every pane and shell on margit with it.
	if host.about("margit").machine == nil {
		t.Errorf("the window let go of margit as well: %v", panelText(host, time.Now()))
	}
}

// A relay whose client goes while the machine is answering is not left
// counted against that machine.
//
// The client going and the machine answering the close are a race, and
// the copy from the machine ends a round trip later whichever way it
// falls. A count that only ever went up turned four slow closes into a
// machine this window would open no more file sessions on.
func TestARelayWhoseClientGoesWhileTheMachineAnswersIsNotCounted(t *testing.T) {
	a, relay := aRelayedMachine(t)

	gone, cancel := context.WithCancel(context.Background())
	ours, done := relayToMargit(t, a, gone)
	waitFor(t, a, "margit to be serving the relay", func() bool {
		return relay.server.SFTPs() == 1
	})

	// The client's whole connection goes first and margit answers the
	// close a moment later, which is the order that counted it.
	cancel()
	if err := ours.Close(); err != nil {
		t.Fatalf("close the client end: %v", err)
	}
	waitFor(t, a, "the relay to finish", func() bool { return finished(done) })

	waitFor(t, a, "nothing to be counted as parked on margit", func() bool {
		return len(a.serving.abandoned) == 0
	})
}

// A relay parked past the grace is counted while it is parked, and
// uncounted once the machine finally answers.
//
// The count is what turns the next file session away, so it has to say
// how many are parked now. A machine that came back on the network and
// answered every close is one to ask again.
func TestARelayParkedPastTheGraceIsUncountedWhenTheMachineAnswers(t *testing.T) {
	a, relay := aRelayedMachine(t)

	gone, cancel := context.WithCancel(context.Background())
	defer cancel()
	ours, done := relayToMargit(t, a, gone)
	waitFor(t, a, "margit to be serving the relay", func() bool {
		return relay.server.SFTPs() == 1
	})

	// margit drops off the network, and the file channel closes with the
	// client still there: the grace is waited out and the copy from margit
	// is parked.
	relay.stop()
	if err := ours.Close(); err != nil {
		t.Fatalf("close the client end: %v", err)
	}
	waitFor(t, a, "the file session to be counted as parked on margit", func() bool {
		return parkedOn(a, "margit") == 1
	})
	if !a.logged.holds("abandoned a file session on margit") {
		t.Errorf("nothing said the session was abandoned: %v", a.logged.all())
	}
	waitFor(t, a, "the relay to finish", func() bool { return finished(done) })

	// margit comes back and answers the close it was sent.
	relay.resume()

	waitFor(t, a, "the parked file session to be uncounted", func() bool {
		return parkedOn(a, "margit") == 0
	})
	if !a.logged.holds("the file session left parked on margit has ended") {
		t.Errorf("nothing said the parked session ended: %v", a.logged.all())
	}
}

// A machine answering again under the same name is not still counted
// against.
//
// The relays were parked on the connection that went, and the new one is
// carrying none of them. A count left behind would turn away the first
// file session on a machine that is answering perfectly well.
func TestAMachineThatConnectsAgainStartsFromNothing(t *testing.T) {
	a, relay := aRelayedMachine(t)

	gone, cancel := context.WithCancel(context.Background())
	defer cancel()
	ours, _ := relayToMargit(t, a, gone)
	waitFor(t, a, "margit to be serving the relay", func() bool {
		return relay.server.SFTPs() == 1
	})
	relay.stop()
	if err := ours.Close(); err != nil {
		t.Fatalf("close the client end: %v", err)
	}
	waitFor(t, a, "the file session to be counted as parked on margit", func() bool {
		return parkedOn(a, "margit") == 1
	})

	// The window loses the connection with nothing clearing the count,
	// which is the state a machine answering again has to survive.
	a.machines.drop(a.machines.named("margit"))

	second := sshtest.New(t)
	pinServers(t, a, second)
	a.connectAs("margit", serverConfig(t, second))
	waitFor(t, a, "margit to answer again", func() bool { return a.machines.named("margit") != nil })

	if got := parkedOn(a, "margit"); got != 0 {
		t.Errorf("%d file sessions are counted against margit, want none:"+
			" nothing is parked on the connection it has now", got)
	}
}

// finished reports whether a relay started by relayToMargit has returned.
func finished(done chan error) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// A file channel that closes on its own, with the client still there,
// does wait the grace out.
//
// The client is still on the other end of the error stream, which is
// where the machine's account of the session arrives. Walking away at
// once would throw that away while somebody was waiting to read it.
func TestAFileChannelThatClosesWaitsForTheWedgedMachine(t *testing.T) {
	host, client, addr, _, logged := aWedgedRelay(t, 1)
	held := windowAt(t, client, addr)

	// The pane goes and the window stays, so the file channel closes and
	// nothing else does.
	started := time.Now()
	if err := client.closeFilesOn(held.name); err != nil {
		t.Fatalf("closing the pane on margit: %v", err)
	}

	waitFor(t, host, "the abandoned file session to be logged", func() bool {
		return logged.holds("abandoned a file session on margit")
	}, client)
	took := time.Since(started)
	if took < relayGrace {
		t.Errorf("the relay was abandoned after %v, want the %v grace waited out:"+
			" the client is still there to read what margit says", took, relayGrace)
	}
	// And bounded at the other end: the wait is one grace, not a wait for
	// a machine that is never going to answer.
	if took > 3*relayGrace {
		t.Errorf("the relay was abandoned after %v, want it inside three times the %v grace",
			took, relayGrace)
	}
	// And the client is still working in this window, so nothing here
	// mistook one file channel for the whole connection.
	if got := servingRows(host); got != 1 {
		t.Errorf("the window has %d rows for clients, want the one still connected: %v",
			got, logged.all())
	}
}

// A machine holding as many parked file sessions as this window will
// allow is refused the next one, by name and by count, and the client
// asking is told why.
//
// Each parked relay is a goroutine and an SSH channel waiting on a
// machine that has already stopped answering. They end when that machine
// answers after all or when the connection to it closes, so without a
// bound a client whose panes keep landing on a dead machine piles them
// up.
func TestAMachineWithTooManyParkedFileSessionsIsRefusedTheNext(t *testing.T) {
	host, client, addr, relay, _ := aWedgedRelay(t, mostAbandonedRelays)
	held := windowAt(t, client, addr)

	// The panes go and the window stays, so each file channel closes with
	// the client still there. Every one of them waits its grace out on a
	// machine that has stopped answering and is parked.
	if err := client.closeFilesOn(held.name); err != nil {
		t.Fatalf("closing the panes on margit: %v", err)
	}
	waitFor(t, host, "every relay to be parked on margit", func() bool {
		return parkedOn(host, "margit") == mostAbandonedRelays
	}, client)

	// The next one is turned away before anything is opened, and what
	// this window said travels back to the client on the file session's
	// error stream and into the notice the user reads.
	chooseMenuItemOver(t, host, clickPlusFar(t, client, addr, "margit"), "conn.files")
	n := awaitModal[*ui.Notice](t, client, "the refusal", nil)
	want := "margit has stopped answering; four file sessions to it are still waiting to end"
	if !strings.Contains(n.Message(), want) {
		t.Fatalf("the client was told %q, want it to say %q", n.Message(), want)
	}
	// And nothing was opened for it: no pane, and no file manager to hold
	// one.
	if client.files != nil {
		t.Errorf("a file pane was opened anyway: %v", filesRows(client))
	}

	// A rename takes the count with it: the relays are still parked, so
	// the machine under its new name is still not one to ask.
	was := host.machines.named("margit")
	if was == nil {
		t.Fatal("margit is no longer connected")
	}
	host.renamedMachine("margit", remote.Host{Name: "margit2",
		Address: was.at.cfg.Host, Port: was.at.cfg.Port, User: was.at.cfg.User})
	if got := parkedOn(host, "margit2"); got != mostAbandonedRelays {
		t.Errorf("after the rename %d file sessions are counted against it, want %d",
			got, mostAbandonedRelays)
	}

	// And the machine comes back under the name it has now. Every parked
	// relay ends, the count goes to nothing, and the machine is one to ask
	// again: the relays are counted against the connection, which the
	// rename did not touch, so ending them takes the count off the machine
	// the user is looking at.
	relay.resume()
	waitFor(t, host, "the parked file sessions on margit2 to end", func() bool {
		return parkedOn(host, "margit2") == 0
	}, client)
	if host.machines.named("margit2") == nil {
		t.Error("margit2 is no longer connected")
	}

	// And the next file session is allowed rather than turned away, which
	// is what the count falling is for. Asked the way a goroutine serving
	// a client asks, on one of its own, because the answer comes from the
	// goroutine that draws.
	back := make(chan error, 1)
	go func() {
		_, err := host.connectionTo("margit2")
		back <- err
	}()
	var asked error
	waitFor(t, host, "the window to say whether margit2 is one to ask again", func() bool {
		select {
		case asked = <-back:
			return true
		default:
			return false
		}
	}, client)
	if asked != nil {
		t.Errorf("a file session on margit2 was still refused: %v", asked)
	}
}

// A file session parked on a connection that is gone leaves the count of
// the connection holding that name now alone.
//
// The window can hold a machine under a name, lose it, and answer under
// that name again while the first connection's relays are still parked.
// Counted by name, the old one ending took the new one's count with it,
// and a machine answering perfectly well was refused the file session
// after that.
func TestAParkedRelayFromAnOldConnectionLeavesTheNewCountAlone(t *testing.T) {
	a, first := aRelayedMachine(t)
	old := a.machines.named("margit")
	if old == nil {
		t.Fatal("margit is not connected")
	}

	// One parked on the connection this window has now: margit stops
	// answering and the client's end closes, so the grace runs out.
	gone, cancel := context.WithCancel(context.Background())
	defer cancel()
	ours, _ := relayToMargit(t, a, gone)
	waitFor(t, a, "margit to be serving the relay", func() bool {
		return first.server.SFTPs() == 1
	})
	first.stop()
	if err := ours.Close(); err != nil {
		t.Fatalf("close the client end: %v", err)
	}
	waitFor(t, a, "the file session to be counted as parked on margit", func() bool {
		return parkedOn(a, "margit") == 1
	})

	// The window loses the connection while that relay is still parked on
	// it, and another machine answers under the same name.
	a.machines.drop(old)
	second := sshtest.New(t)
	other := newBlackHole(t, second.Addr())
	other.server = second
	cfg := serverConfig(t, second)
	cfg.Host, cfg.Port = other.host, other.port
	a.connectAs("margit", cfg)
	waitFor(t, a, "margit to answer again", func() bool {
		m := a.machines.named("margit")
		return m != nil && m != old
	})
	now := a.machines.named("margit")

	// One parked on the new connection, the same way.
	theirs, _ := relayToMargit(t, a, gone)
	waitFor(t, a, "the new margit to be serving the relay", func() bool {
		return second.SFTPs() == 1
	})
	other.stop()
	if err := theirs.Close(); err != nil {
		t.Fatalf("close the client end: %v", err)
	}
	waitFor(t, a, "the file session to be counted as parked on the new margit", func() bool {
		return a.serving.abandonedRelays(now.conn) == 1
	})

	// The old connection's relay ends at last. It was never the new
	// connection's, so the new connection is still holding the one it has.
	first.resume()
	waitFor(t, a, "the old connection's parked relay to end", func() bool {
		return a.serving.abandonedRelays(old.conn) == 0
	})
	if got := a.serving.abandonedRelays(now.conn); got != 1 {
		t.Errorf("%d file sessions are counted against the connection margit has now, want the one"+
			" parked on it: a relay parked on the connection before it took the count with it", got)
	}

	// And closing that connection ends the one parked on it, so the count
	// goes with it.
	if err := a.dropMachine("margit"); err != nil {
		t.Fatalf("let go of margit: %v", err)
	}
	if got := a.serving.abandonedRelays(now.conn); got != 0 {
		t.Errorf("%d file sessions are still counted against a connection that has been closed", got)
	}
}

// parkedOn is how many relayed file sessions are parked on the connection
// a window holds a machine under now.
//
// It is -1 when the window is not holding that machine at all. A count of
// zero would read as a machine with nothing parked on it, and a test
// waiting for the count to fall would pass on a machine that had gone.
func parkedOn(a *testApp, host string) int {
	m := a.machines.named(host)
	if m == nil {
		return -1
	}
	return a.serving.abandonedRelays(m.conn)
}

// aRelayedMachine is a window connected to a test machine through a relay
// the test can stop, so the machine can be made to stop answering without
// hanging up.
func aRelayedMachine(t *testing.T) (*testApp, *blackHole) {
	t.Helper()
	machine := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	relay := newBlackHole(t, machine.Addr())
	relay.server = machine
	cfg := serverConfig(t, machine)
	cfg.Host, cfg.Port = relay.host, relay.port
	a.connectAs("margit", cfg)
	waitFor(t, a, "margit to answer", func() bool { return a.machines.named("margit") != nil })
	return a, relay
}

// relayToMargit starts a relayed file session on margit from a goroutine
// of its own, the way a client's does.
//
// It hands back the end a client would hold and what the relay reported
// once it finished. gone stands for the client's connection.
func relayToMargit(t *testing.T, a *testApp, gone context.Context) (net.Conn, chan error) {
	t.Helper()
	ours, theirs := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- a.relayFiles(gone, "margit", theirs) }()
	return ours, done
}

// A relayed file session that ended blames the connection only when the
// connection really went.
//
// The machine's end of the relay finishing means the far file server
// stopped, which happens when it exits by itself as much as when the
// connection under it goes. A window that said the connection went either
// way sent the user looking for a network fault that was not there.
func TestARelayThatEndedSaysWhetherTheConnectionWent(t *testing.T) {
	t.Run("still connected", func(t *testing.T) {
		_, _, host, m := aMachineToRelayTo(t)
		if got := relayEnded(m.conn, host).Error(); !strings.Contains(got, "ended while it was still in use") {
			t.Errorf("with %s still connected it says %q", host, got)
		}
	})

	t.Run("closed from this window", func(t *testing.T) {
		a, _, host, m := aMachineToRelayTo(t)
		if err := a.dropMachine(host); err != nil {
			t.Fatalf("let go of %s: %v", host, err)
		}
		if got := relayEnded(m.conn, host).Error(); !strings.Contains(got, "the connection to "+host+" went") {
			t.Errorf("with the connection closed it says %q", got)
		}
	})

	// The machine drops off the network instead. Nothing here closed the
	// connection, and the wording about the file server ending by itself
	// is for exactly the case this is not.
	t.Run("the machine dropped", func(t *testing.T) {
		_, s, host, m := aMachineToRelayTo(t)
		s.CloseClients()
		if err := m.conn.Wait(); err == nil {
			t.Fatal("the connection to the machine ended cleanly")
		}
		if got := relayEnded(m.conn, host).Error(); !strings.Contains(got, "the connection to "+host+" went") {
			t.Errorf("with the machine gone it says %q", got)
		}
	})
}

// aMachineToRelayTo is a window with one connection open, the machine at
// the far end of it, the name the window holds it under, and what it is
// holding.
//
// The server is handed back as well, so a test can cut the connection
// from the far end.
func aMachineToRelayTo(t *testing.T) (*testApp, *sshtest.Server, string, *machine) {
	t.Helper()
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	m := a.machines.named(host)
	if m == nil {
		t.Fatalf("nothing is connected to %s", host)
	}
	return a, s, host, m
}

// servingRows counts the rows a window has for the clients working in it.
func servingRows(a *testApp) int {
	n := 0
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if strings.HasPrefix(row.Label, "serving ") {
				n++
			}
		}
	}
	return n
}
