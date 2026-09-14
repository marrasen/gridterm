package serve

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// aKey makes a key pair for a test, with a comment on the public half
// the way authorized_keys carries one.
func aKey(t *testing.T, comment string) (ssh.Signer, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("use the key: %v", err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if comment != "" {
		line += " " + comment
	}
	return signer, line
}

// serving starts a server on this machine only, allowing the keys given.
func serving(t *testing.T, allowed string) *Server {
	t.Helper()
	host, err := HostKey(filepath.Join(t.TempDir(), "host_key"))
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(allowed), "the test")
	if err != nil {
		t.Fatalf("allowed keys: %v", err)
	}
	s, err := Listen(Config{
		Addr:    "127.0.0.1:0",
		HostKey: host,
		Allowed: keys,
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// connect dials the server with a key.
func connect(t *testing.T, s *Server, as ssh.Signer) (*ssh.Client, error) {
	t.Helper()
	return ssh.Dial("tcp", s.Addr(), &ssh.ClientConfig{
		User:            "gridterm",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(as)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
}

// A key in the list gets in.
func TestAListedKeyConnects(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	s := serving(t, line)

	client, err := connect(t, s, mine)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.Clients(); len(got) == 1 {
			if got[0].Name != "marcus@laptop" {
				t.Errorf("the client is called %q, want the name on its key", got[0].Name)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the server never recorded the client")
}

// A key that is not in the list does not.
func TestAnUnlistedKeyIsRefused(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	other, _ := aKey(t, "somebody@else")
	s := serving(t, line)

	client, err := connect(t, s, other)
	if err == nil {
		client.Close()
		t.Fatal("a key nobody listed was let in")
	}
	if got := s.Clients(); len(got) != 0 {
		t.Errorf("%d clients are connected", len(got))
	}
}

// Nothing but a key gets a look in. A window is served to the keys its
// owner listed, and a password would be a second way in that nobody
// asked for.
func TestNothingButAKeyIsOffered(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	s := serving(t, line)

	var offered []string
	_, err := ssh.Dial("tcp", s.Addr(), &ssh.ClientConfig{
		User: "gridterm",
		Auth: []ssh.AuthMethod{
			ssh.Password("hunter2"),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				offered = append(offered, "keyboard-interactive")
				return make([]string, len(qs)), nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Fatal("a password was accepted")
	}
	if len(offered) != 0 {
		t.Errorf("the server asked for %v", offered)
	}
	// The client gave up without trying either, because the server
	// never said they were on offer.
	if !strings.Contains(err.Error(), "no supported methods remain") {
		t.Errorf("the client failed with %v, want it to have nothing to try", err)
	}
}

// A window with nobody listed does not listen at all.
//
// A port that turns every caller away says the window is being served
// when it is not, and it is one forgotten line in a file away from
// being served to whoever adds it.
func TestServingNobodyDoesNotListen(t *testing.T) {
	host, err := HostKey(filepath.Join(t.TempDir(), "host_key"))
	if err != nil {
		t.Fatalf("host key: %v", err)
	}

	s, err := Listen(Config{Addr: "127.0.0.1:0", HostKey: host, Allowed: &Allowed{}})

	if err == nil {
		_ = s.Close()
		t.Fatal("a window with nobody listed started listening")
	}
	if !strings.Contains(err.Error(), "nobody to serve") {
		t.Errorf("it refused with %v", err)
	}
}

// The address is listened on exactly as given. A window told to serve
// this machine only must not end up reachable from the network.
func TestTheAddressIsNotWidened(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	s := serving(t, line)

	host, _, err := net.SplitHostPort(s.Addr())
	if err != nil {
		t.Fatalf("the server is at %q: %v", s.Addr(), err)
	}
	if host != "127.0.0.1" {
		t.Errorf("it is listening on %q, want the loopback it was given", host)
	}
}

// Closing hangs up on whoever is connected.
func TestClosingHangsUpOnEveryone(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	s := serving(t, line)
	client, err := connect(t, s, mine)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- client.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the client was left connected")
	}
	if got := s.Clients(); len(got) != 0 {
		t.Errorf("%d clients are still recorded", len(got))
	}
}

// Closing twice is not an error, and the second one changes nothing.
func TestClosingTwiceIsFine(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	s := serving(t, line)

	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// The host key is the same one every time the window opens.
//
// It is what a client checks this machine by, so a window that made a
// fresh one on each start would look to its own client exactly like a
// machine in the middle.
func TestTheHostKeyIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_key")

	first, err := HostKey(path)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := HostKey(path)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if Fingerprint(first.PublicKey()) != Fingerprint(second.PublicKey()) {
		t.Error("the window came back with a different key")
	}
}

// And it is written where nobody else can read it.
func TestTheHostKeyIsWrittenPrivately(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows has no file modes to set. What keeps the key private
		// there is the configuration directory it lives in, which
		// belongs to the user who owns the window.
		t.Skip("file modes on Windows say nothing about who can read a file")
	}
	path := filepath.Join(t.TempDir(), "host_key")
	if _, err := HostKey(path); err != nil {
		t.Fatalf("host key: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("the key is written %04o, want it readable only by its owner", mode)
	}
}

// A host key file that cannot be read stops the window serving, rather
// than being replaced by a new one.
//
// Writing over it would change the key the client checks this machine
// by, which is the alarm a client is meant to raise.
func TestAnUnreadableHostKeyIsNotReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_key")
	if err := os.WriteFile(path, []byte("this is not a key"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := HostKey(path)

	if err == nil {
		t.Fatal("a file that is not a key was accepted")
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read back: %v", readErr)
	}
	if string(raw) != "this is not a key" {
		t.Error("the file was written over")
	}
}

// A missing authorized_keys is a machine that has never been served
// from, not a fault.
func TestNoAuthorizedKeysFileIsNotAnError(t *testing.T) {
	a, err := LoadAllowed(filepath.Join(t.TempDir(), "nothing_here"))

	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if a.Len() != 0 {
		t.Errorf("it found %d keys", a.Len())
	}
}

// An authorized_keys that cannot be read is. A window that carried on
// would trust nobody while looking like one that trusts the people in a
// file it could not open.
func TestAnUnreadableAuthorizedKeysIsAnError(t *testing.T) {
	at := t.TempDir()
	// A directory where the file should be: readable as a name, not as
	// a file, on every platform.
	path := filepath.Join(at, "authorized_keys")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := LoadAllowed(path)

	if err == nil {
		t.Fatal("a file that could not be read was taken as an empty list")
	}
}

// A line that cannot be read fails the lot, rather than being skipped.
//
// Skipping would leave the user believing a key is trusted when it is
// not, or hide a line somebody else added that they cannot see the
// shape of.
func TestAKeyThatCannotBeReadFailsTheFile(t *testing.T) {
	_, good := aKey(t, "marcus@laptop")

	_, err := ParseAllowed([]byte(good+"\nssh-ed25519 not-a-key nobody\n"), "the test")

	if err == nil {
		t.Fatal("a line that is not a key was skipped")
	}
	if !strings.Contains(err.Error(), "key 2") {
		t.Errorf("it said %v, want which line", err)
	}
}

// Comments and blank lines are what a file people edit looks like.
func TestCommentsAndBlankLinesAreRead(t *testing.T) {
	_, one := aKey(t, "marcus@laptop")
	_, two := aKey(t, "marcus@desktop")
	raw := "# the machines I use\n\n" + one + "\n\n# and this one\n" + two + "\n"

	a, err := ParseAllowed([]byte(raw), "the test")

	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Len() != 2 {
		t.Fatalf("it found %d keys, want 2", a.Len())
	}
	want := []string{"marcus@laptop", "marcus@desktop"}
	for i, got := range a.Names() {
		if got != want[i] {
			t.Errorf("key %d is called %q, want %q", i, got, want[i])
		}
	}
}

// A key written with no comment is named by its fingerprint, so the
// window can still say who is connected.
func TestAKeyWithNoCommentIsNamedByItsFingerprint(t *testing.T) {
	signer, line := aKey(t, "")

	a, err := ParseAllowed([]byte(line), "the test")

	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Fingerprint(signer.PublicKey())
	if got := a.Names(); len(got) != 1 || got[0] != want {
		t.Errorf("it is called %v, want %q", got, want)
	}
}

// An empty set says no to everything, including to nothing at all.
func TestAnEmptySetAllowsNobody(t *testing.T) {
	signer, _ := aKey(t, "marcus@laptop")
	var a *Allowed

	if _, ok := a.Who(signer.PublicKey()); ok {
		t.Error("a nil set let a key in")
	}
	if _, ok := (&Allowed{}).Who(signer.PublicKey()); ok {
		t.Error("an empty set let a key in")
	}
	if _, ok := (&Allowed{}).Who(nil); ok {
		t.Error("an empty set let nothing in")
	}
}

// Listen says what is missing rather than starting half-configured.
func TestListenSaysWhatIsMissing(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	host, err := HostKey(filepath.Join(t.TempDir(), "host_key"))
	if err != nil {
		t.Fatalf("host key: %v", err)
	}

	for _, c := range []struct {
		why string
		cfg Config
	}{
		{"no host key", Config{Addr: "127.0.0.1:0", Allowed: keys}},
		{"no address", Config{HostKey: host, Allowed: keys}},
	} {
		s, err := Listen(c.cfg)
		if err == nil {
			_ = s.Close()
			t.Errorf("%s: it started anyway", c.why)
			continue
		}
		if !strings.Contains(err.Error(), "serve:") {
			t.Errorf("%s: it said %v", c.why, err)
		}
	}
}
