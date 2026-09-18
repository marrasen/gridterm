package remote

import (
	"context"
	"sync"
	"testing"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// testAsk stands in for the window: it answers with whatever the test
// set, and records what it was asked.
type testAsk struct {
	mu sync.Mutex

	// What to answer with.
	passphrase string
	password   string
	answers    []string
	trust      bool

	// What to fail with instead of answering, which is what cancelling a
	// dialog looks like.
	passphraseErr error
	passwordErr   error
	trustErr      error

	// held blocks Passphrase until it is closed, so a test can cancel
	// while a dialog is open.
	held chan struct{}

	// What was asked, and what was said without asking.
	keyfiles  []string
	passwords int
	questions []Question
	hostKeys  []HostKey
	notices   []Notice
}

func newTestAsk() *testAsk { return &testAsk{password: sshtest.Password, trust: true} }

func (a *testAsk) Passphrase(ctx context.Context, keyfile string) (string, error) {
	a.mu.Lock()
	a.keyfiles = append(a.keyfiles, keyfile)
	held, err := a.held, a.passphraseErr
	pass := a.passphrase
	a.mu.Unlock()

	if held != nil {
		select {
		case <-held:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return pass, err
}

func (a *testAsk) Password(ctx context.Context, user, host string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.passwords++
	return a.password, a.passwordErr
}

func (a *testAsk) Question(ctx context.Context, q Question) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.questions = append(a.questions, q)
	return a.answers, nil
}

func (a *testAsk) TrustHostKey(ctx context.Context, key HostKey) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hostKeys = append(a.hostKeys, key)
	return a.trust, a.trustErr
}

// asked returns what the window was asked for, for a test that cares
// about how often rather than what.
// Notice records what a server said and returns at once, the way a real
// one must: nothing is waiting on it, and the handshake is.
func (a *testAsk) Notice(ctx context.Context, n Notice) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notices = append(a.notices, n)
}

func (a *testAsk) asked() (keyfiles []string, passwords int, hostKeys []HostKey) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.keyfiles...), a.passwords, append([]HostKey(nil), a.hostKeys...)
}

func TestHostKeyFingerprintMatchesSSH(t *testing.T) {
	s := sshtest.New(t)
	h := HostKey{Addr: s.Addr(), Key: s.HostKey()}

	// The same spelling ssh and ssh-keygen use, so a user can compare
	// what the dialog shows with what they were sent.
	if got := h.Fingerprint(); len(got) < 8 || got[:7] != "SHA256:" {
		t.Fatalf("fingerprint = %q, want it to start with SHA256:", got)
	}
	if got := h.Type(); got != "ssh-ed25519" {
		t.Fatalf("type = %q, want ssh-ed25519", got)
	}
}

func TestHostKeyZeroValueSaysNothing(t *testing.T) {
	var h HostKey
	if h.Fingerprint() != "" || h.Type() != "" {
		t.Fatalf("a host key with no key described itself as %q %q", h.Type(), h.Fingerprint())
	}
}
