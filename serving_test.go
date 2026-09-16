package main

import (
	"bytes"
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
// and then keeps both connections open and passes nothing.
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
type blackHole struct {
	host string
	port int

	shut     chan struct{}
	shutOnce sync.Once

	mu    sync.Mutex
	conns []net.Conn

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
		shut: make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = ln.Close()
		b.stop()
		// The copying goroutines are parked in a read, and closing what
		// they are reading is what ends them.
		b.mu.Lock()
		defer b.mu.Unlock()
		b.done = true
		for _, c := range b.conns {
			_ = c.Close()
		}
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
func (b *blackHole) stop() { b.shutOnce.Do(func() { close(b.shut) }) }

// carry passes bytes one way until the relay is stopped. What arrives
// after that is dropped rather than written on.
func (b *blackHole) carry(src, dst net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		select {
		case <-b.shut:
			return
		default:
		}
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// A client that goes while the machine its file session was relayed to
// has stopped answering is still said to have gone.
//
// Closing the subsystem sends that machine a channel close and nothing
// more. One that has dropped off the network never answers it, so the
// copy from it stays where it is: waited for, it would hold the file
// session, the connection it rode on, and the row saying a client this
// window has already lost is still working here.
func TestAClientThatGoesIsSaidToHaveGoneThoughTheMachineIsWedged(t *testing.T) {
	host, client, addr := twoWindows(t)

	// margit reached through a relay the test can stop, so the machine
	// can be made to stop answering without hanging up.
	margit := sshtest.New(t)
	pinServers(t, host, margit)
	relay := newBlackHole(t, margit.Addr())
	cfg := serverConfig(t, margit)
	cfg.Host, cfg.Port = relay.host, relay.port
	connectToMargit(t, host, client, addr, cfg)

	openFilesFromTheFarPlus(t, client, host, addr, "margit")
	if got := margit.SFTPs(); got != 1 {
		t.Fatalf("margit served %d file sessions, want the one being relayed", got)
	}
	// The row is there to go away. Without this a window that never put
	// one up would pass the wait below on its first turn.
	waitFor(t, host, "a row for the client working in this window", func() bool {
		return servingRows(host) == 1
	}, client)

	// What this window logs, so the session it walks away from is not
	// walked away from quietly.
	var logged []string
	host.onError = func(err error) { logged = append(logged, err.Error()) }

	// The machine stops answering, and then the client goes.
	relay.stop()
	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("let go of the window: %v", err)
	}

	waitFor(t, host, "the window serving to say the client has gone", func() bool {
		return servingRows(host) == 0
	})
	waitFor(t, host, "the abandoned file session to be logged", func() bool {
		for _, said := range logged {
			if strings.Contains(said, "abandoned a file session on margit") {
				return true
			}
		}
		return false
	})
	// And the connection to margit is still held: the file session is what
	// went wrong, not the connection carrying it, and a window that closed
	// the whole thing would take every pane and shell on margit with it.
	if host.about("margit").machine == nil {
		t.Errorf("the window let go of margit as well: %v", panelText(host, time.Now()))
	}
}

// A relayed file session that ended blames the connection only when the
// connection really went.
//
// The machine's end of the relay finishing means the far file server
// stopped, which happens when it exits by itself as much as when the
// connection under it goes. A window that said the connection went either
// way sent the user looking for a network fault that was not there.
func TestARelayThatEndedSaysWhetherTheConnectionWent(t *testing.T) {
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

	if got := relayEnded(m.conn, host).Error(); !strings.Contains(got, "ended while it was still in use") {
		t.Errorf("with %s still connected it says %q", host, got)
	}

	if err := a.dropMachine(host); err != nil {
		t.Fatalf("let go of %s: %v", host, err)
	}
	if got := relayEnded(m.conn, host).Error(); !strings.Contains(got, "the connection to "+host+" went") {
		t.Errorf("with the connection closed it says %q", got)
	}
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
