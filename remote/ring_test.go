package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
