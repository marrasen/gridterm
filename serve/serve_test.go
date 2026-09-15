package serve

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

	_, after := aKey(t, "somebody@else")

	// The bad line in the middle, which is where skipping it would go
	// unnoticed: a parser that runs off the end of a file gives an
	// error whether or not it fails the line that caused it.
	_, err := ParseAllowed([]byte(good+"\nssh-ed25519 not-a-key nobody\n"+after+"\n"), "the test")

	if err == nil {
		t.Fatal("a line that is not a key was skipped")
	}
	if !strings.Contains(err.Error(), "line 2") {
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

// A key carrying restrictions is refused, rather than admitted with
// them quietly dropped.
//
// gridterm does not honour from=, command= or any of the rest. A user
// who copied a line they deliberately restricted would otherwise be
// handing out a full takeover of their window from anywhere.
func TestARestrictedKeyIsRefused(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")

	for _, option := range []string{
		`from="10.0.0.1"`,
		`command="/bin/false"`,
		"restrict",
		`expiry-time="19990101"`,
		"cert-authority",
		"no-pty",
	} {
		a, err := ParseAllowed([]byte(option+" "+line), "the test")
		if err == nil {
			t.Errorf("%s was accepted, admitting %d keys with it dropped", option, a.Len())
			continue
		}
		if !strings.Contains(err.Error(), "line 1") {
			t.Errorf("%s: it said %v, want which line", option, err)
		}
	}
}

// A certificate is refused too. It has an expiry, principals and a
// revocation list behind it and none of that is checked here, so taking
// one as a plain key would honour it for ever.
func TestACertificateIsRefused(t *testing.T) {
	signer, _ := aKey(t, "")
	ca, _ := aKey(t, "")
	cert := &ssh.Certificate{
		Key:         signer.PublicKey(),
		CertType:    ssh.UserCert,
		ValidBefore: ssh.CertTimeInfinity,
	}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatalf("sign: %v", err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(cert)))

	_, err := ParseAllowed([]byte(line), "the test")

	if err == nil {
		t.Fatal("a certificate was taken as a key")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("it said %v", err)
	}
}

// Two windows starting together end up known by one key.
//
// They did not: both read, both saw nothing, and both wrote. The one on
// disk belonged to neither reliably, and every client that had pinned a
// fingerprint saw a host key change -- the very alarm the key is for.
func TestWindowsStartingTogetherAgreeOnTheHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_key")

	const at = 8
	start := make(chan struct{})
	got := make(chan string, at)
	for i := 0; i < at; i++ {
		go func() {
			<-start
			signer, err := HostKey(path)
			if err != nil {
				got <- "failed: " + err.Error()
				return
			}
			got <- Fingerprint(signer.PublicKey())
		}()
	}
	close(start)

	seen := map[string]int{}
	for i := 0; i < at; i++ {
		seen[<-got]++
	}
	if len(seen) != 1 {
		t.Errorf("the windows came up with %d different keys: %v", len(seen), seen)
	}
}

// A key anybody can read is refused rather than served with.
func TestAHostKeyOthersCanReadIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes on Windows say nothing about who can read a file")
	}
	path := filepath.Join(t.TempDir(), "host_key")
	if _, err := HostKey(path); err != nil {
		t.Fatalf("make: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	_, err := HostKey(path)

	if err == nil {
		t.Fatal("a key others can read was used anyway")
	}
	if !strings.Contains(err.Error(), "readable by others") {
		t.Errorf("it said %v", err)
	}
}

// Only so many callers may be part way through connecting.
//
// Nobody has authenticated at that point, so without a cap a stranger
// who can reach the port pins a goroutine and a socket per connection
// for the whole handshake window -- while the window is holding the
// user's shells.
func TestOnlySoManyMayBeConnectingAtOnce(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	s := serving(t, line)

	// Callers that connect and then say nothing at all.
	var quiet []net.Conn
	t.Cleanup(func() {
		for _, c := range quiet {
			_ = c.Close()
		}
	})
	for i := 0; i < handshakesAtOnce; i++ {
		c, err := net.Dial("tcp", s.Addr())
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		quiet = append(quiet, c)
	}
	waitFor(t, "the server to take them all", func() bool { return s.connecting() == handshakesAtOnce })

	// One more is hung up on rather than queued.
	extra, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("dial the extra one: %v", err)
	}
	defer extra.Close()

	_ = extra.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := extra.Read(make([]byte, 1)); err == nil {
		t.Error("the extra caller was served a banner rather than hung up on")
	}
	if got := s.connecting(); got != handshakesAtOnce {
		t.Errorf("%d callers are connecting, want no more than %d", got, handshakesAtOnce)
	}
}

// Closing hangs up on someone part way through connecting, too.
//
// Close says it hangs up on everyone. A caller that has been accepted
// and has not authenticated is in no list, so nothing reached it: it
// kept its socket and its goroutine for the rest of the handshake
// window after the user pressed Stop serving.
func TestClosingHangsUpOnSomeoneStillConnecting(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	s := serving(t, line)

	c, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	// Read the banner, so the handshake is known to have started.
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err != nil {
		t.Fatalf("read the banner: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(c); err != nil {
		t.Errorf("the caller was left connected: %v", err)
	}
}

// A caller that arrives after the server has closed is turned away.
func TestNobodyArrivesAfterClosing(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	s := serving(t, line)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if got := s.arrived(nil); got != arrivedClosed {
		t.Errorf("a caller arriving after closing got %v, want to be turned away", got)
	}
	if s.add(&Client{Name: "late"}) {
		t.Error("a client was recorded after closing")
	}
	if got := s.Clients(); len(got) != 0 {
		t.Errorf("%d clients are recorded", len(got))
	}
}

// A client that goes is forgotten, so the window stops saying it is
// there.
func TestAClientThatGoesIsForgotten(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	s := serving(t, line)
	client, err := connect(t, s, mine)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	waitFor(t, "the client to arrive", func() bool { return len(s.Clients()) == 1 })

	_ = client.Close()

	waitFor(t, "the client to be forgotten", func() bool { return len(s.Clients()) == 0 })
}

// Arriving and going are both told, and going says why.
//
// A client lost to a network fault and one that hung up are different
// things to be told about.
func TestArrivingAndGoingAreBothTold(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	host, err := HostKey(filepath.Join(t.TempDir(), "host_key"))
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	joined := make(chan string, 1)
	gone := make(chan string, 1)
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys,
		OnJoin:  func(c *Client) { joined <- c.Name },
		OnGone:  func(c *Client, why error) { gone <- c.Name },
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	client, err := connect(t, s, mine)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	select {
	case name := <-joined:
		if name != "marcus@laptop" {
			t.Errorf("the arrival was called %q", name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nobody was told the client arrived")
	}

	_ = client.Close()

	select {
	case name := <-gone:
		if name != "marcus@laptop" {
			t.Errorf("the departure was called %q", name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nobody was told the client went")
	}
}

// Refusals are told about, but counted rather than told one at a time:
// a stranger with a loop makes thousands, down a queue that has no
// depth to overflow.
func TestRefusalsAreCountedRatherThanToldOneAtATime(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	other, _ := aKey(t, "somebody@else")
	host, err := HostKey(filepath.Join(t.TempDir(), "host_key"))
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	var mu sync.Mutex
	var told []string
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys,
		OnError: func(err error) {
			mu.Lock()
			told = append(told, err.Error())
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	const tries = 20
	for i := 0; i < tries; i++ {
		if c, err := connect(t, s, other); err == nil {
			c.Close()
			t.Fatal("a key nobody listed was let in")
		}
	}

	// Most of them, rather than all: a dial that gives up before it
	// reaches the server is a refusal the server never saw, and this
	// runs alongside every other package's tests.
	waitFor(t, "the refusals to land", func() bool { return s.turnedAwaySoFar() >= tries/2 })

	mu.Lock()
	defer mu.Unlock()
	refused := s.turnedAwaySoFar()
	if len(told) == 0 {
		t.Fatal("the user was told nothing about any refused connection")
	}
	if len(told) >= refused {
		t.Errorf("the user was told %d times about %d refusals", len(told), refused)
	}
}

// waitFor waits for something to become true.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// connecting is how many callers are part way through authenticating.
func (s *Server) connecting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.arriving)
}

// A caller that says nothing is hung up on when its time runs out.
//
// Without that it holds a goroutine and a socket for as long as it
// likes, and nobody has authenticated at that point.
func TestACallerThatSaysNothingIsDropped(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	host, err := HostKey(filepath.Join(t.TempDir(), "host_key"))
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	keys, err := ParseAllowed([]byte(line), "the test")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	s, err := Listen(Config{
		Addr: "127.0.0.1:0", HostKey: host, Allowed: keys,
		Handshake: 50 * time.Millisecond,
		OnError:   func(error) {},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	c, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(c); err != nil {
		t.Errorf("the caller was left connected: %v", err)
	}
	waitFor(t, "the server to let go of it", func() bool { return s.connecting() == 0 })
}

// A key others can read is refused, on whichever platform the check can
// be made on.
func TestTheModeCheckRefusesAnOpenKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_key")
	if _, err := HostKey(path); err != nil {
		t.Fatalf("make: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Forced on, so the check is pinned here as well as on the systems
	// where a file mode says something by itself.
	was := modesMeanSomething
	modesMeanSomething = true
	t.Cleanup(func() { modesMeanSomething = was })

	_, err := HostKey(path)

	if err == nil {
		t.Fatal("a key others can read was used anyway")
	}
	if !strings.Contains(err.Error(), "readable by others") {
		t.Errorf("it said %v", err)
	}
}

// turnedAwaySoFar is how many callers have been refused, for a test
// that has to wait for them to land: a refusal is reported from the
// goroutine that was serving the caller, after the caller has gone.
func (s *Server) turnedAwaySoFar() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnedAway
}

// A client that has not kept up is sent the latest snapshot, not every
// one it missed.
//
// What it wants is a picture of the window now. A queue of pictures it
// is already too late for would put it further behind with every one.
func TestAClientBehindIsSentTheLatestSnapshot(t *testing.T) {
	w := newWatcher(nil)

	w.put([]byte("first\n"))
	w.put([]byte("second\n"))

	if got := string(w.take()); got != "second\n" {
		t.Errorf("it kept %q", got)
	}
	if got := w.take(); got != nil {
		t.Errorf("there was another waiting: %q", got)
	}

	// And one that has gone is not queued for.
	w.stop()
	w.put([]byte("third\n"))
	if got := w.take(); got != nil {
		t.Errorf("it queued %q for a client that has gone", got)
	}
}
