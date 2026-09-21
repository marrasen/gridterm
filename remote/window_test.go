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
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// takeOver builds the ladder taking over a window climbs, through the
// Reach that reach itself uses, so a test looks at the real thing
// without a window to reach.
func takeOver(t *testing.T, ring *Ring, ask Ask,
	from agentSource, say func(string)) (*auth, error) {

	t.Helper()
	return Reach{Ring: ring, Ask: ask, Agent: from, Saying: say}.ladder(t.Context())
}

// takeOverWith is takeOver for a window reached with a named key file.
func takeOverWith(t *testing.T, keyFile string, ring *Ring, ask Ask,
	from agentSource, say func(string)) (*auth, error) {

	t.Helper()
	r := Reach{KeyFile: keyFile, Ring: ring, Ask: ask, Agent: from, Saying: say}
	return r.ladder(t.Context())
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
func agentMissing() (io.Closer, agent.Agent, error) {
	return nil, nil, errors.New("remote: no SSH agent here")
}

// listingAgent is an agent that only lists keys. The embedded interface
// is nil, so anything else panics, and nothing here asks for anything
// else.
type listingAgent struct {
	agent.Agent
	signers func() ([]ssh.Signer, error)
}

func (l listingAgent) Signers() ([]ssh.Signer, error) { return l.signers() }

// agentHolding stands in for an agent that is running and holds keys.
// It counts the times it was asked to list them.
func agentHolding(listed *int, keys ...ssh.Signer) agentSource {
	return func() (io.Closer, agent.Agent, error) {
		return closerFunc(func() error { return nil }),
			listingAgent{signers: func() ([]ssh.Signer, error) {
				*listed++
				return keys, nil
			}}, nil
	}
}

// With no keys anywhere, the reason says which: an agent that answered
// and failed is a different thing to go and fix from no agent at all.
func TestNoKeysSaysWhyThereAreNone(t *testing.T) {
	homeWith(t, "", "")

	ring := NewRing()
	ring.AgentGaveUp(errors.New("the agent said no"))

	a, err := takeOver(t, ring, nil, nil, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if len(a.ladder) != 0 {
		t.Fatalf("it found %v to offer where there is nothing", rungs(a))
	}
	why := noKeysToOffer(a.agentTrouble())
	if !strings.Contains(why.Error(), "the agent said no") {
		t.Errorf("it said %v, without saying why there were none", why)
	}
}

// An agent that is not running is not named in the reason, because a
// user sent to add a key to one would be sent after a fault that is not
// there.
func TestNoKeysDoesNotBlameAnAgentThatIsNotRunning(t *testing.T) {
	homeWith(t, "", "")

	a, err := takeOver(t, NewRing(), nil, agentMissing, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	why := noKeysToOffer(a.agentTrouble()).Error()
	if strings.Contains(why, "could not be read") {
		t.Errorf("it blames an agent that is not running: %s", why)
	}
	for _, want := range []string{"name a key file", "~/.ssh", "SSH agent"} {
		if !strings.Contains(why, want) {
			t.Errorf("the failure does not mention %q: %v", want, why)
		}
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
	a, err := takeOver(t, ring, nil, func() (io.Closer, agent.Agent, error) {
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
	why := noKeysToOffer(a.agentTrouble())
	if !strings.Contains(why.Error(), "did not") {
		t.Fatalf("it said %v, want it to say why there are no keys", why)
	}
}

// An agent that is not running is not held against it there either.
func TestTakingOverAWindowDoesNotHoldAMissingAgentAgainstIt(t *testing.T) {
	homeWith(t, "", "")
	ring := NewRing()

	a, err := takeOver(t, ring, nil, func() (io.Closer, agent.Agent, error) {
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
	why := noKeysToOffer(a.agentTrouble())
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

func (c *countingAsk) Passphrase(context.Context, LockedKey) (string, error) {
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
	a, err := takeOver(t, ring, nil, nil, func(what string) { said = append(said, what) })
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	account := strings.Join(said, "\n")
	for _, want := range []string{
		"leaving the SSH agent alone",
		"Forget unlocked keys",
		"private keys with no passphrase: 1",
		"private keys that need one: 0",
	} {
		if !strings.Contains(account, want) {
			t.Errorf("the account does not say %q:\n%s", want, account)
		}
	}
}

// A key file named for a take-over is the only key offered.
//
// Naming one is the user saying which key this address may see. The
// ladder offered the ring and the agent alongside it, showing every key
// the window holds to a machine the user had only just been told about.
func TestANamedKeyFileIsTheOnlyKeyOffered(t *testing.T) {
	named := sshtest.WriteKey(t)
	// A key in ~/.ssh and a key already unlocked, neither of which was
	// named. Both would be offered if the ladder took no notice.
	homeWith(t, "id_ed25519", sshtest.WriteKey(t))
	ring := NewRing()
	if _, err := ring.Unlock(t.Context(), sshtest.WriteKey(t), nil); err != nil {
		t.Fatalf("unlock a key into the ring: %v", err)
	}

	listed := 0
	a, err := takeOverWith(t, named, ring, nil, agentHolding(&listed, unlockedKey(t)), nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if a.agent != nil {
		t.Error("it opened the SSH agent for a take-over with a key file named")
	}
	want := []string{sshtest.Fingerprint(t, named)}
	if got := climbed(t, a); !slices.Equal(got, want) {
		t.Fatalf("it offered %v, want only the key file that was named", got)
	}
	if listed != 0 {
		t.Errorf("it asked the SSH agent what it holds %d times, want not at all", listed)
	}
}

// A key file named that needs a passphrase is still the only key
// offered, and it is asked for once.
func TestANamedLockedKeyFileIsTheOnlyKeyOffered(t *testing.T) {
	const passphrase = "open sesame"
	named := sshtest.WriteEncryptedKey(t, passphrase)
	homeWith(t, "id_ed25519", sshtest.WriteKey(t))

	asked := 0
	ask := &countingAsk{pass: passphrase, asked: &asked}
	a, err := takeOverWith(t, named, NewRing(), ask, agentMissing, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	last := rungs(a)
	if len(last) != 1 || !strings.HasPrefix(last[0], "the private key ") {
		t.Fatalf("it offers %v, want only the locked key that was named", last)
	}
	if asked != 0 {
		t.Fatalf("it asked for a passphrase %d times before the key's turn came", asked)
	}
	want := []string{sshtest.Fingerprint(t, named, passphrase)}
	if got := climbed(t, a); !slices.Equal(got, want) {
		t.Fatalf("it offered %v, want only the key file that was named", got)
	}
	if asked != 1 {
		t.Fatalf("it asked for a passphrase %d times, want once", asked)
	}
}

// The SSH agent is asked what it holds when its rung's turn comes, not
// while the ladder is being built.
//
// The rung is what bounds the wait and what tells the ring an agent did
// not answer. An agent read before any of that is one nothing is
// watching.
func TestTheAgentIsAskedWhenItsTurnComes(t *testing.T) {
	homeWith(t, "", "")
	path := sshtest.WriteKey(t)
	signer, _, _, err := readIdentity(path)
	if err != nil {
		t.Fatalf("read a key: %v", err)
	}

	listed := 0
	a, err := takeOver(t, NewRing(), nil, agentHolding(&listed, signer), nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if want := []string{"the keys the SSH agent holds"}; !slices.Equal(rungs(a), want) {
		t.Fatalf("it offers %v, want the agent", rungs(a))
	}
	if listed != 0 {
		t.Fatalf("the agent was asked what it holds %d times while the ladder was built", listed)
	}
	if got := climbed(t, a); !slices.Equal(got, []string{sshtest.Fingerprint(t, path)}) {
		t.Fatalf("it offered %v, want the key the agent holds", got)
	}
	if listed != 1 {
		t.Fatalf("the agent was asked what it holds %d times, want once", listed)
	}
}

// An agent that will not say what it holds is let go of and remembered,
// for a take-over as much as for a machine.
func TestTakingOverLetsGoOfAnAgentThatWillNotList(t *testing.T) {
	homeWith(t, "", "")
	was := agentListing
	agentListing = 50 * time.Millisecond
	defer func() { agentListing = was }()

	ring := NewRing()
	// A listing that comes back only when the socket is closed, which is
	// what a real one does.
	closed := make(chan struct{})
	a, err := takeOver(t, ring, nil, func() (io.Closer, agent.Agent, error) {
		return closerFunc(func() error { close(closed); return nil }),
			listingAgent{signers: func() ([]ssh.Signer, error) {
				<-closed
				return nil, errors.New("the socket went")
			}}, nil
	}, nil)
	if err != nil {
		t.Fatalf("build the ladder: %v", err)
	}
	defer a.close()

	if got := climbed(t, a); len(got) != 0 {
		t.Fatalf("an agent that said nothing offered %v", got)
	}
	if why := ring.AgentTrouble(); why == nil {
		t.Fatal("the agent will be waited for again, so every take-over pays the wait")
	}
}

// climbed offers a ladder to a test SSH server that refuses every key,
// so the ladder is climbed to the end, and returns what it was shown.
func climbed(t *testing.T, a *auth) []string {
	t.Helper()
	s := sshtest.New(t)
	// A fingerprint no key has, so nothing is accepted.
	s.Accept("SHA256:nothing")
	_, err := dial(t.Context(), overTCP, s.Addr(), "tester", a.next,
		ssh.FixedHostKey(s.HostKey()), nil, nil)
	if err == nil {
		t.Fatal("the server accepted a key it refuses")
	}
	return s.Offered()
}

// unlockedKey is one private key, read the way the ladder reads one.
func unlockedKey(t *testing.T) ssh.Signer {
	t.Helper()
	signer, _, _, err := readIdentity(sshtest.WriteKey(t))
	if err != nil {
		t.Fatalf("read a key: %v", err)
	}
	return signer
}
