package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
)

const testPassphrase = "let me in"

// keyConfig authenticates with a key file and nothing else, so a test
// can watch exactly what the key path does.
func keyConfig(t *testing.T, s *sshtest.Server, keys ...string) Config {
	t.Helper()
	cfg := testConfig(t, s)
	cfg.NoIdentities = false
	cfg.Identities = keys
	return cfg
}

func TestRingUnlocksAKeyOnce(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &testAsk{passphrase: testPassphrase}
	ring := NewRing()

	first, err := ring.Unlock(t.Context(), path, ask)
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	second, err := ring.Unlock(t.Context(), path, ask)
	if err != nil {
		t.Fatalf("second Unlock: %v", err)
	}
	if first != second {
		t.Error("the second Unlock built a different signer")
	}
	if keys, _, _ := ask.asked(); len(keys) != 1 {
		t.Fatalf("the passphrase was asked for %d times, want once", len(keys))
	}
}

func TestRingUnlocksAKeyWithNoPassphraseWithoutAsking(t *testing.T) {
	path := sshtest.WriteKey(t)
	ask := newTestAsk()
	ring := NewRing()

	if _, err := ring.Unlock(t.Context(), path, ask); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if keys, _, _ := ask.asked(); len(keys) != 0 {
		t.Fatalf("a key with no passphrase was still asked about: %v", keys)
	}
}

// A wrong passphrase has to say so. Silently skipping the key leaves the
// user watching the connection fail for no stated reason.
func TestRingWrongPassphraseSaysSo(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &testAsk{passphrase: "not it"}

	_, err := NewRing().Unlock(t.Context(), path, ask)
	if err == nil {
		t.Fatal("Unlock accepted the wrong passphrase")
	}
	if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("error = %v, want it to name the passphrase", err)
	}
}

// Cancelling the dialog stops the connection rather than moving on to
// the next thing to try.
func TestRingCancelledPromptIsReported(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	want := errors.New("the user closed the dialog")
	ask := &testAsk{passphraseErr: want}

	_, err := NewRing().Unlock(t.Context(), path, ask)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want the one the dialog gave", err)
	}
}

func TestRingWithNobodyToAskRefusesAnEncryptedKey(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	_, err := NewRing().Unlock(t.Context(), path, nil)
	if err == nil {
		t.Fatal("Unlock opened an encrypted key with nothing to ask for the passphrase")
	}
}

func TestRingForgetAndLock(t *testing.T) {
	first := sshtest.WriteEncryptedKey(t, testPassphrase)
	second := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &testAsk{passphrase: testPassphrase}
	ring := NewRing()

	if _, err := ring.Unlock(t.Context(), first, ask); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if _, err := ring.Unlock(t.Context(), second, ask); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if n := len(ring.Signers()); n != 2 {
		t.Fatalf("the ring holds %d keys, want 2", n)
	}

	ring.Forget(first)
	if ring.Has(first) {
		t.Error("a forgotten key is still in the ring")
	}
	if !ring.Has(second) {
		t.Error("Forget dropped the wrong key")
	}

	ring.Lock()
	if n := len(ring.Signers()); n != 0 {
		t.Fatalf("the ring holds %d keys after Lock, want none", n)
	}
	// Locking means the next connection asks again.
	if _, err := ring.Unlock(t.Context(), second, ask); err != nil {
		t.Fatalf("Unlock after Lock: %v", err)
	}
	if keys, _, _ := ask.asked(); len(keys) != 3 {
		t.Fatalf("the passphrase was asked for %d times, want 3", len(keys))
	}
}

// The nil ring is the "keep nothing" setting, and every method has to
// cope with it: a caller that wants no ring should not have to guard
// every call.
func TestRingNilIsUsable(t *testing.T) {
	var r *Ring
	if r.Has("x") || len(r.Signers()) != 0 || len(r.Paths()) != 0 {
		t.Fatal("a nil ring claimed to hold something")
	}
	r.Forget("x")
	r.Lock()

	path := sshtest.WriteKey(t)
	if _, err := r.Unlock(context.Background(), path, nil); err != nil {
		t.Fatalf("Unlock on a nil ring: %v", err)
	}
}

// The point of the ring: one passphrase, then every connection after it
// goes through without asking.
func TestConnectAsksForAPassphraseOnceAcrossConnections(t *testing.T) {
	s := sshtest.New(t)
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &testAsk{passphrase: testPassphrase, password: sshtest.Password}

	cfg := keyConfig(t, s, path)
	cfg.Ask = ask
	cfg.Ring = NewRing()

	for i := 0; i < 3; i++ {
		c, err := Connect(t.Context(), cfg)
		if err != nil {
			t.Fatalf("connection %d: %v", i+1, err)
		}
		_ = c.Close()
	}
	keys, _, _ := ask.asked()
	if len(keys) != 1 {
		t.Fatalf("the passphrase was asked for %d times over three connections, want once", len(keys))
	}
	if keys[0] != path {
		t.Errorf("asked about %q, want %q", keys[0], path)
	}
}

// Without a ring the key still works; it is just asked about every time.
func TestConnectWithoutARingAsksEveryTime(t *testing.T) {
	s := sshtest.New(t)
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &testAsk{passphrase: testPassphrase, password: sshtest.Password}

	cfg := keyConfig(t, s, path)
	cfg.Ask = ask

	for i := 0; i < 2; i++ {
		c, err := Connect(t.Context(), cfg)
		if err != nil {
			t.Fatalf("connection %d: %v", i+1, err)
		}
		_ = c.Close()
	}
	if keys, _, _ := ask.asked(); len(keys) != 2 {
		t.Fatalf("the passphrase was asked for %d times, want 2", len(keys))
	}
}

// A key that needs no passphrase must not produce a dialog at all.
func TestConnectWithAPlainKeyAsksNothing(t *testing.T) {
	s := sshtest.New(t)
	path := sshtest.WriteKey(t)
	ask := newTestAsk()

	cfg := keyConfig(t, s, path)
	cfg.Ask = ask

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = c.Close()

	keys, passwords, _ := ask.asked()
	if len(keys) != 0 || passwords != 0 {
		t.Fatalf("a plain key produced %d passphrase prompts and %d password prompts",
			len(keys), passwords)
	}
	if got := s.Offered(); len(got) == 0 || got[0] != sshtest.Fingerprint(t, path) {
		t.Fatalf("the server was offered %v, want the key file", got)
	}
}

// Cancelling the passphrase dialog stops the connection. Falling through
// to the password would ask a second question for a decision the user
// has already made.
func TestConnectStopsWhenThePassphraseDialogIsCancelled(t *testing.T) {
	s := sshtest.New(t)
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &testAsk{passphraseErr: errors.New("cancelled"), password: sshtest.Password}

	cfg := keyConfig(t, s, path)
	cfg.Ask = ask

	if _, err := Connect(t.Context(), cfg); err == nil {
		t.Fatal("Connect carried on after the passphrase dialog was cancelled")
	}
	if _, passwords, _ := ask.asked(); passwords != 0 {
		t.Fatalf("the password was asked for %d times after the user cancelled", passwords)
	}
}

// Cancelling the window while a dialog is open has to let the connecting
// goroutine go, or closing gridterm waits for an answer nobody will give.
func TestConnectCancelsWhileADialogIsOpen(t *testing.T) {
	s := sshtest.New(t)
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &testAsk{passphrase: testPassphrase, held: make(chan struct{})}

	cfg := keyConfig(t, s, path)
	cfg.Ask = ask

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		c, err := Connect(ctx, cfg)
		if c != nil {
			_ = c.Close()
		}
		done <- err
	}()

	// Wait until the dialog is open, then close the window under it.
	waitFor(t, func() bool { keys, _, _ := ask.asked(); return len(keys) > 0 })
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Connect succeeded after it was cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect never returned after its context was cancelled")
	}
}

// waitFor blocks until cond is true, or fails the test.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the condition never became true")
}

// A plain key that the server refuses must not stop the encrypted key
// behind it from being tried.
//
// x/crypto picks the next authentication method by name and skips any it
// has already tried, so several public-key methods collapse into one and
// every key after the first is unreachable. That is the shape this test
// pins: with an agent, a ring entry or any unencrypted key present, a
// passphrase-protected key used to be skipped in silence.
func TestConnectTriesAnEncryptedKeyAfterAPlainOneIsRefused(t *testing.T) {
	s := sshtest.New(t)
	plain := sshtest.WriteKey(t)
	locked := sshtest.WriteEncryptedKey(t, testPassphrase)
	s.Accept(sshtest.Fingerprint(t, locked, testPassphrase))

	ask := &testAsk{passphrase: testPassphrase, password: sshtest.Password}
	cfg := keyConfig(t, s, plain, locked)
	cfg.Ask = ask
	cfg.Ring = NewRing()

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = c.Close()

	keys, passwords, _ := ask.asked()
	if len(keys) != 1 || keys[0] != locked {
		t.Fatalf("the passphrase was asked for %q, want %q once", keys, locked)
	}
	if passwords != 0 {
		t.Errorf("the password was asked for %d times; the key should have worked", passwords)
	}
}

// With two encrypted keys, the second is only asked about once the first
// has been refused. Asking for both up front would mean two dialogs for
// a connection one key would have made.
func TestConnectAsksForEachEncryptedKeyInTurn(t *testing.T) {
	s := sshtest.New(t)
	first := sshtest.WriteEncryptedKey(t, testPassphrase)
	second := sshtest.WriteEncryptedKey(t, testPassphrase)
	s.Accept(sshtest.Fingerprint(t, second, testPassphrase))

	ask := &testAsk{passphrase: testPassphrase, password: sshtest.Password}
	cfg := keyConfig(t, s, first, second)
	cfg.Ask = ask
	cfg.Ring = NewRing()

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = c.Close()

	keys, _, _ := ask.asked()
	want := []string{first, second}
	if len(keys) != 2 || keys[0] != want[0] || keys[1] != want[1] {
		t.Fatalf("the passphrase was asked for %q, want %q in that order", keys, want)
	}
}

// The password is a last resort, and only when the server will take one.
func TestConnectFallsBackToThePasswordWhenNoKeyWorks(t *testing.T) {
	s := sshtest.New(t)
	mine := sshtest.WriteKey(t)
	other := sshtest.WriteKey(t)
	s.Accept(sshtest.Fingerprint(t, other))

	ask := &testAsk{password: sshtest.Password}
	cfg := keyConfig(t, s, mine)
	cfg.Ask = ask

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = c.Close()

	if _, passwords, _ := ask.asked(); passwords != 1 {
		t.Fatalf("the password was asked for %d times, want once", passwords)
	}
}

// A question from a server has to carry which machine asked it, in
// fields the server cannot write.
//
// Everything else in that dialog is the server's own wording. A hostile
// one that sets the name to "Unlock a private key" and the instruction
// to a plausible key path would otherwise produce a dialog the user
// cannot tell from the local one, and be handed the passphrase to their
// private key.
func TestKeyboardInteractiveSaysWhichMachineAsked(t *testing.T) {
	ask := &testAsk{answers: []string{"123456"}}
	cfg := Config{Host: "margit.skalarit.net", Port: 22, User: "marcus", Ask: ask}

	challenge := keyboardInteractive(t.Context(), cfg)
	if _, err := challenge("Unlock a private key", "/home/marcus/.ssh/id_ed25519",
		[]string{"Passphrase"}, []bool{false}); err != nil {
		t.Fatalf("challenge: %v", err)
	}

	ask.mu.Lock()
	defer ask.mu.Unlock()
	if len(ask.questions) != 1 {
		t.Fatalf("the user was asked %d times, want once", len(ask.questions))
	}
	q := ask.questions[0]
	if q.User != "marcus" {
		t.Errorf("question came from user %q, want marcus", q.User)
	}
	if q.Host != "margit.skalarit.net:22" {
		t.Errorf("question came from host %q, want the address dialled", q.Host)
	}
	// The server's own wording is carried, not used as the dialog's own.
	if q.Name != "Unlock a private key" {
		t.Errorf("the server's wording was changed to %q", q.Name)
	}
}

// A server that says nothing to answer is told nothing.
func TestKeyboardInteractiveWithNoPromptsAsksNobody(t *testing.T) {
	ask := newTestAsk()
	cfg := Config{Host: "h", User: "u", Ask: ask}

	answers, err := keyboardInteractive(t.Context(), cfg)("", "a notice", nil, nil)
	if err != nil || answers != nil {
		t.Fatalf("= %q, %v, want nothing", answers, err)
	}
	ask.mu.Lock()
	defer ask.mu.Unlock()
	if len(ask.questions) != 0 {
		t.Fatalf("a notice with nothing to answer opened %d dialogs", len(ask.questions))
	}
}

// Cancelling has to reach a dial that is stuck in the handshake, with no
// dialog involved at all.
//
// ssh.Dial cannot be cancelled: its timeout covers reaching the host and
// nothing after it. Closing the connection under the handshake is what
// ends it, and this is the test that says so -- the dialog tests would
// pass on the fake noticing the context by itself.
func TestConnectCancelReachesAStuckHandshake(t *testing.T) {
	host, port := sshtest.Deaf(t)
	cfg := Config{
		Host: host, Port: port, User: "tester",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		NoAgent:         true,
		Identities:      []string{sshtest.WriteKey(t)},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		c, err := Connect(ctx, cfg)
		if c != nil {
			_ = c.Close()
		}
		done <- err
	}()

	// Long enough to be inside the handshake, far short of dialTimeout.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Connect succeeded against a server that never spoke")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not reach the handshake; the dial ran on")
	}
}

// Two connections wanting the same key produce one dialog, not two. The
// second waits for the first rather than asking again.
func TestRingAsksOnceWhenTwoConnectionsWantTheSameKey(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &testAsk{passphrase: testPassphrase, held: make(chan struct{})}
	ring := NewRing()

	type result struct {
		signer ssh.Signer
		err    error
	}
	got := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			s, err := ring.Unlock(context.Background(), path, ask)
			got <- result{s, err}
		}()
	}

	// Wait until one of them is at the dialog, then let it answer.
	waitFor(t, func() bool { keys, _, _ := ask.asked(); return len(keys) > 0 })
	close(ask.held)

	var signers []ssh.Signer
	for i := 0; i < 2; i++ {
		select {
		case r := <-got:
			if r.err != nil {
				t.Fatalf("Unlock: %v", r.err)
			}
			signers = append(signers, r.signer)
		case <-time.After(5 * time.Second):
			t.Fatal("an Unlock never returned; the second waited for ever")
		}
	}
	if signers[0] != signers[1] {
		t.Error("the two connections got different keys for the same file")
	}
	if keys, _, _ := ask.asked(); len(keys) != 1 {
		t.Fatalf("the passphrase was asked for %d times, want once", len(keys))
	}
}
