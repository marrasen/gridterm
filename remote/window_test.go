package remote

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// takeOver builds the ladder taking over a window climbs, the way reach
// does, so a test can look at it without a window to reach.
func takeOver(t *testing.T, ring *Ring, ask Ask,
	from func() ([]ssh.Signer, io.Closer, error), say func(string)) (*auth, error) {

	t.Helper()
	r := Reach{Ring: ring, Ask: ask, Agent: from, Saying: say}
	return authMethods(t.Context(), Config{
		Ring: ring, Ask: ask, Saying: say, KeysOnly: true, agent: r.agentSource(),
	})
}

// rungs names each thing the ladder will try, in order.
func rungs(a *auth) []string {
	var out []string
	for _, r := range a.ladder {
		out = append(out, r.what)
	}
	return out
}

// agentMissing stands in for an agent that is not running.
func agentMissing() ([]ssh.Signer, io.Closer, error) {
	return nil, nil, errors.New("remote: no SSH agent here")
}

// With no keys anywhere, the reason says which: an agent that answered
// and failed is a different thing to go and fix from no agent at all.
func TestNoKeysSaysWhyThereAreNone(t *testing.T) {
	homeWith(t, "", "")

	a, err := takeOver(t, NewRing(), nil, func() ([]ssh.Signer, io.Closer, error) {
		return nil, nil, errors.New("the agent said no")
	}, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if len(a.ladder) != 0 {
		t.Fatalf("it found %v to offer where there is nothing", rungs(a))
	}
	why := noKeysToOffer(a.noAgent)
	if !strings.Contains(why.Error(), "the agent said no") {
		t.Errorf("it said %v, without saying why there were none", why)
	}
}

// Taking over a window leaves an agent alone once it is known not to
// answer, the same as connecting to a machine does.
//
// It has its own way of reading the agent, and it went on paying the
// wait every time while every other connection had stopped.
func TestTakingOverAWindowLeavesASilentAgentAlone(t *testing.T) {
	homeWith(t, "", "")
	ring := NewRing()
	ring.AgentGaveUp(errors.New("it had 10s to say what keys it holds and did not"))

	asked := false
	a, err := takeOver(t, ring, nil, func() ([]ssh.Signer, io.Closer, error) {
		asked = true
		return nil, nil, nil
	}, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if asked {
		t.Fatal("it asked an agent that is known not to answer")
	}
	why := noKeysToOffer(a.noAgent)
	if !strings.Contains(why.Error(), "did not") {
		t.Fatalf("it said %v, want it to say why there are no keys", why)
	}
}

// An agent that is not running is not held against it there either.
func TestTakingOverAWindowDoesNotHoldAMissingAgentAgainstIt(t *testing.T) {
	homeWith(t, "", "")
	ring := NewRing()

	a, err := takeOver(t, ring, nil, func() ([]ssh.Signer, io.Closer, error) {
		return nil, nil, errors.New("remote: no SSH agent: open the pipe: not found")
	}, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if len(a.ladder) != 0 {
		t.Fatalf("it found %v to offer where there is nothing", rungs(a))
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

	a, err := takeOver(t, NewRing(), nil, agentMissing, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if want := []string{"the keys already to hand"}; !slices.Equal(rungs(a), want) {
		t.Fatalf("it offers %v, want the key in ~/.ssh as %v", rungs(a), want)
	}
}

// A key in the usual place that needs a passphrase is offered last, and
// asked about only when its turn comes.
func TestTakingOverAWindowAsksForALockedUsualKey(t *testing.T) {
	const passphrase = "open sesame"
	key := sshtest.WriteEncryptedKey(t, passphrase)
	homeWith(t, "id_ed25519", key)

	asked := 0
	ask := &countingAsk{pass: passphrase, asked: &asked}
	a, err := takeOver(t, NewRing(), ask, agentMissing, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	last := rungs(a)
	if len(last) != 1 || !strings.HasPrefix(last[0], "the private key ") {
		t.Fatalf("it offers %v, want the locked key in ~/.ssh", last)
	}
	if asked != 0 {
		t.Fatalf("it asked for a passphrase %d times before the key's turn came, want none",
			asked)
	}
}

// Taking over a window offers keys and nothing else.
//
// The other end accepts no password and no keyboard-interactive, so a
// ladder that offered either would put a dialog on screen for an answer
// that cannot be used.
func TestTakingOverAWindowOffersKeysOnly(t *testing.T) {
	homeWith(t, "id_ed25519", sshtest.WriteKey(t))

	a, err := takeOver(t, NewRing(), &countingAsk{pass: "x", asked: new(int)}, agentMissing, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	for _, r := range a.ladder {
		if r.method != methodPublicKey {
			t.Errorf("it would try %q, which is %s and not a key", r.what, r.method)
		}
	}
}

// With nothing anywhere, it says where it looked.
func TestTakingOverAWindowWithNoKeysSaysWhereToPutOne(t *testing.T) {
	homeWith(t, "", "")

	a, err := takeOver(t, NewRing(), nil, agentMissing, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if len(a.ladder) != 0 {
		t.Fatalf("it found %v to offer where there is nothing", rungs(a))
	}
	why := noKeysToOffer(a.noAgent)
	for _, want := range []string{"name a key file", "~/.ssh", "SSH agent"} {
		if !strings.Contains(why.Error(), want) {
			t.Errorf("the failure does not mention %q: %v", want, why)
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
	a, err := takeOver(t, ring, nil, func() ([]ssh.Signer, io.Closer, error) {
		return nil, nil, nil
	}, func(what string) { said = append(said, what) })
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	account := strings.Join(said, "\n")
	for _, want := range []string{
		"leaving the SSH agent alone",
		"Forget unlocked keys",
		"1 private keys need no passphrase",
	} {
		if !strings.Contains(account, want) {
			t.Errorf("the account does not say %q:\n%s", want, account)
		}
	}
}
