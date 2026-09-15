package remote

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// With no keys anywhere, the reason says which: an agent that answered
// and failed is a different thing to go and fix from no agent at all.
func TestNoKeysSaysWhyThereAreNone(t *testing.T) {
	_, _, err := keysFor(t.Context(), "", NewRing(), nil,
		func() ([]ssh.Signer, io.Closer, error) {
			return nil, nil, errors.New("the agent said no")
		}, nil)

	if err == nil {
		t.Fatal("it found keys where there are none")
	}
	if !strings.Contains(err.Error(), "the agent said no") {
		t.Errorf("it said %v, without saying why there were none", err)
	}
}

// Taking over a window leaves an agent alone once it is known not to
// answer, the same as connecting to a machine does.
//
// It has its own way of reading the agent, and it went on paying the
// wait every time while every other connection had stopped.
func TestTakingOverAWindowLeavesASilentAgentAlone(t *testing.T) {
	ring := NewRing()
	ring.AgentGaveUp(errors.New("it had 10s to say what keys it holds and did not"))

	asked := false
	_, _, err := keysFor(t.Context(), "", ring, nil,
		func() ([]ssh.Signer, io.Closer, error) {
			asked = true
			return nil, nil, nil
		}, nil)
	if asked {
		t.Fatal("it asked an agent that is known not to answer")
	}
	if err == nil || !strings.Contains(err.Error(), "did not") {
		t.Fatalf("keysFor = %v, want it to say why there are no keys", err)
	}
}

// An agent that is not running is not held against it there either.
func TestTakingOverAWindowDoesNotHoldAMissingAgentAgainstIt(t *testing.T) {
	ring := NewRing()
	_, _, err := keysFor(t.Context(), "", ring, nil,
		func() ([]ssh.Signer, io.Closer, error) {
			return nil, nil, errors.New("remote: no SSH agent: open the pipe: not found")
		}, nil)
	if err == nil {
		t.Fatal("it found keys where there are none")
	}
	if why := ring.AgentTrouble(); why != nil {
		t.Fatalf("it will not ask the agent again because %v", why)
	}
}

// homeWith puts a private key in the usual place and points the home
// directory at it, so a test sees what a user's ~/.ssh looks like.
func homeWith(t *testing.T, name, from string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make .ssh: %v", err)
	}
	if from != "" {
		b, err := os.ReadFile(from)
		if err != nil {
			t.Fatalf("read the key: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatalf("write the key: %v", err)
		}
	}
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		return
	}
	t.Setenv("HOME", home)
}

// Taking over a window offers the key files in the usual places.
//
// It offered only what was already unlocked and what the agent held.
// A user with one key in ~/.ssh and an agent that says nothing was told
// there were no keys to offer, when the key that works was sitting
// there.
func TestTakingOverAWindowOffersTheUsualKeys(t *testing.T) {
	homeWith(t, "id_ed25519", sshtest.WriteKey(t))

	keys, closer, err := keysFor(t.Context(), "", NewRing(), nil,
		func() ([]ssh.Signer, io.Closer, error) {
			return nil, nil, errors.New("remote: no SSH agent here")
		}, nil)
	if closer != nil {
		_ = closer.Close()
	}
	if err != nil {
		t.Fatalf("keysFor: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("it found %d keys, want the one in ~/.ssh", len(keys))
	}
}

// A key in the usual place that needs a passphrase is asked about, once
// there is nothing else left to offer.
func TestTakingOverAWindowAsksForALockedUsualKey(t *testing.T) {
	const passphrase = "open sesame"
	homeWith(t, "id_ed25519", sshtest.WriteEncryptedKey(t, passphrase))

	asked := 0
	ask := &countingAsk{pass: passphrase, asked: &asked}
	keys, closer, err := keysFor(t.Context(), "", NewRing(), ask,
		func() ([]ssh.Signer, io.Closer, error) {
			return nil, nil, errors.New("remote: no SSH agent here")
		}, nil)
	if closer != nil {
		_ = closer.Close()
	}
	if err != nil {
		t.Fatalf("keysFor: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("it found %d keys, want the one in ~/.ssh", len(keys))
	}
	if asked != 1 {
		t.Fatalf("it asked for a passphrase %d times, want once", asked)
	}
}

// With nothing anywhere, it says where it looked.
func TestTakingOverAWindowWithNoKeysSaysWhereToPutOne(t *testing.T) {
	homeWith(t, "", "")

	_, _, err := keysFor(t.Context(), "", NewRing(), nil,
		func() ([]ssh.Signer, io.Closer, error) { return nil, nil, nil }, nil)
	if err == nil {
		t.Fatal("it found keys where there are none")
	}
	for _, want := range []string{"name a key file", "~/.ssh", "SSH agent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not mention %q: %v", want, err)
		}
	}
}

// countingAsk answers a passphrase and counts how often it was asked.
type countingAsk struct {
	Ask
	pass  string
	asked *int
}

func (c *countingAsk) Passphrase(context.Context, string) (string, error) {
	*c.asked++
	return c.pass, nil
}

// Finding a key says what it looked at, so a window that could not be
// reached says why rather than only that it could not.
func TestFindingAKeySaysWhatItLookedAt(t *testing.T) {
	homeWith(t, "id_ed25519", sshtest.WriteKey(t))
	ring := NewRing()
	ring.AgentGaveUp(errors.New("it had 5s to say what keys it holds and did not"))

	var said []string
	_, closer, err := keysFor(t.Context(), "", ring, nil,
		func() ([]ssh.Signer, io.Closer, error) { return nil, nil, nil },
		func(what string) { said = append(said, what) })
	if closer != nil {
		_ = closer.Close()
	}
	if err != nil {
		t.Fatalf("keysFor: %v", err)
	}

	account := strings.Join(said, "\n")
	for _, want := range []string{
		"leaving the SSH agent alone",
		"Forget unlocked keys",
		"1 private keys in the usual places need no passphrase",
	} {
		if !strings.Contains(account, want) {
			t.Errorf("the account does not say %q:\n%s", want, account)
		}
	}
}
