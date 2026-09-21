package remote

import (
	"context"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// typingAsk answers with the next passphrase it was given, and records
// what it was asked each time.
//
// It runs out rather than repeating the last one: a test that expects
// three tries must fail when it gets four, not loop.
type typingAsk struct {
	Ask
	mu   sync.Mutex
	say  []string
	got  []LockedKey
	over int // how many times it was asked after the answers ran out
}

func (a *typingAsk) Passphrase(_ context.Context, key LockedKey) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.got = append(a.got, key)
	if len(a.got) > len(a.say) {
		a.over++
		return "", errors.New("it asked more times than the test had answers")
	}
	return a.say[len(a.got)-1], nil
}

// asked returns what the dialog was shown, in order.
func (a *typingAsk) asked() []LockedKey {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]LockedKey(nil), a.got...)
}

// A passphrase that does not open the key is asked for again.
//
// A wrong one is a typo far more often than it is a key the user cannot
// open. Asking once meant the connection went on without the key, and
// without a word about it anywhere.
func TestAWrongPassphraseIsAskedForAgain(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &typingAsk{say: []string{"not it", "still not it", testPassphrase}}

	if _, err := NewRing().Unlock(t.Context(), path, ask); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got := ask.asked(); len(got) != 3 {
		t.Fatalf("it asked %d times, want 3", len(got))
	}
}

// Each asking says which try it is, so the dialog can say the answer
// before it was wrong and how many tries are left.
func TestAskingAgainSaysHowTheLastTryWent(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &typingAsk{say: []string{"not it", "still not it", testPassphrase}}

	if _, err := NewRing().Unlock(t.Context(), path, ask); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	want := []LockedKey{
		{Path: path, Wrong: 0, Left: 2},
		{Path: path, Wrong: 1, Left: 1},
		{Path: path, Wrong: 2, Left: 0},
	}
	got := ask.asked()
	if len(got) != len(want) {
		t.Fatalf("it asked %d times, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("try %d was asked as %+v, want %+v", i+1, got[i], want[i])
		}
	}
}

// The tries do run out, and what comes back says the passphrase was
// wrong rather than anything about the key file itself.
func TestPassphraseTriesRunOut(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ask := &typingAsk{say: []string{"one", "two", "three"}}

	_, err := NewRing().Unlock(t.Context(), path, ask)
	if !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("error = %v, want it to be %v", err, ErrWrongPassphrase)
	}
	if got := ask.asked(); len(got) != PassphraseTries {
		t.Fatalf("it asked %d times, want %d", len(got), PassphraseTries)
	}
	if ask.over != 0 {
		t.Errorf("it asked %d times after the tries ran out", ask.over)
	}
}

// The wrong passphrase is not kept: the key is asked about again the
// next time something wants it, rather than the ring remembering that it
// failed.
func TestAKeyThatWasNotUnlockedIsAskedAboutAgain(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	ring := NewRing()

	wrong := &typingAsk{say: []string{"one", "two", "three"}}
	if _, err := ring.Unlock(t.Context(), path, wrong); err == nil {
		t.Fatal("Unlock accepted a wrong passphrase")
	}
	if ring.Has(path) {
		t.Fatal("the ring kept a key that was never unlocked")
	}

	right := &typingAsk{say: []string{testPassphrase}}
	if _, err := ring.Unlock(t.Context(), path, right); err != nil {
		t.Fatalf("Unlock with the right passphrase: %v", err)
	}
	if got := right.asked(); len(got) != 1 || got[0].Wrong != 0 {
		t.Fatalf("the second unlock asked %+v, want one asking as the first try", got)
	}
}

// Saying no to the dialog is an answer. Asking twice more would be
// asking about a decision the user has just made.
func TestADismissedPassphraseDialogIsNotAskedAgain(t *testing.T) {
	path := sshtest.WriteEncryptedKey(t, testPassphrase)
	want := errors.New("the user closed the dialog")
	ask := &testAsk{passphraseErr: want}

	_, err := NewRing().Unlock(t.Context(), path, ask)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want the one the dialog gave", err)
	}
	if keys, _, _ := ask.asked(); len(keys) != 1 {
		t.Fatalf("it asked %d times after the dialog was dismissed, want once", len(keys))
	}
}

// A key file that no passphrase will open is asked about once.
//
// Asking again would be asking the user to fix something that is not
// theirs to fix: the file is encrypted with something this cannot read,
// and the passphrase is not what is wrong with it.
func TestAKeyThatIsBeyondAPassphraseIsAskedAboutOnce(t *testing.T) {
	path := unreadableEncryptedKey(t)
	ask := &typingAsk{say: []string{"one", "two", "three"}}

	_, err := NewRing().Unlock(t.Context(), path, ask)
	if err == nil {
		t.Fatal("a key that cannot be decrypted came back unlocked")
	}
	if errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("error = %v, want it not to blame the passphrase", err)
	}
	if got := ask.asked(); len(got) != 1 {
		t.Fatalf("it asked %d times, want once", len(got))
	}
}

// unreadableEncryptedKey writes a key file that says it is encrypted and
// that no passphrase will open: the cipher is one thing and the body is
// not a whole block of it.
func unreadableEncryptedKey(t *testing.T) string {
	t.Helper()
	at := filepath.Join(t.TempDir(), "id_rsa")
	b := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY",
		Headers: map[string]string{
			"Proc-Type": "4,ENCRYPTED",
			"DEK-Info":  "DES-EDE3-CBC,0123456789ABCDEF",
		},
		Bytes: []byte("not a whole block of it"),
	})
	if err := os.WriteFile(at, b, 0o600); err != nil {
		t.Fatalf("write the key: %v", err)
	}
	return at
}

// A connection whose key was never unlocked says so, in the account and
// in what it failed with.
//
// x/crypto counts a key that could not be offered as one more thing that
// did not work: it moves on, keeps only the last failure, and a user who
// mistyped a passphrase was left reading "no supported methods remain".
func TestAConnectionSaysAWrongPassphraseWentWrong(t *testing.T) {
	s := sshtest.New(t)
	path := sshtest.WriteEncryptedKey(t, testPassphrase)

	var said, wrong []string
	cfg := keyConfig(t, s, path)
	cfg.Ask = &typingAsk{Ask: newTestAsk(), say: []string{"one", "two", "three"}}
	cfg.KeysOnly = true
	cfg.Saying = func(what string) { said = append(said, what) }
	cfg.Wrong = func(what string) { wrong = append(wrong, what) }

	_, err := Connect(t.Context(), cfg)
	if err == nil {
		t.Fatal("it connected with a key it never unlocked")
	}
	if !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("it failed with %v, want the passphrase named", err)
	}
	if !saidSomethingWith(wrong, "passphrase") {
		t.Fatalf("what went wrong was %v, want the passphrase among it", wrong)
	}
	// And not among the steps that went well, which are drawn as though
	// nothing had happened.
	if saidSomethingWith(said, "passphrase did not unlock") {
		t.Errorf("the passphrase failure was said as a step that went well: %v", said)
	}
}

// A connection that signs in some other way afterwards still says the
// key was not unlocked.
//
// This is the case with nothing else to show for it: the connection is
// made, so no failure is ever reported, and a user watching the account
// would never learn that the key they typed a passphrase for was not the
// thing that let them in.
func TestAWrongPassphraseIsSaidEvenWhenTheConnectionIsMade(t *testing.T) {
	s := sshtest.New(t)
	path := sshtest.WriteEncryptedKey(t, testPassphrase)

	var wrong []string
	cfg := keyConfig(t, s, path)
	cfg.Ask = &typingAsk{Ask: newTestAsk(), say: []string{"one", "two", "three"}}
	cfg.Wrong = func(what string) { wrong = append(wrong, what) }

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	if !saidSomethingWith(wrong, "passphrase") {
		t.Fatalf("what went wrong was %v, want the passphrase among it", wrong)
	}
	if !saidSomethingWith(wrong, path) {
		t.Errorf("what went wrong was %v, want the key file named", wrong)
	}
}

// saidSomethingWith reports whether any line holds this.
func saidSomethingWith(lines []string, what string) bool {
	for _, line := range lines {
		if strings.Contains(line, what) {
			return true
		}
	}
	return false
}

// Giving up on the dialog is not a key that went wrong.
//
// Dismissing it stops the connection, and the account of that is "given
// up on". A red line about a key as well would be the window telling the
// user something went wrong with a decision they made on purpose.
func TestADismissedDialogIsNotSaidAsAKeyThatFailed(t *testing.T) {
	s := sshtest.New(t)
	path := sshtest.WriteEncryptedKey(t, testPassphrase)

	var wrong []string
	cfg := keyConfig(t, s, path)
	cfg.Ask = &testAsk{passphraseErr: errors.New("the user closed the dialog")}
	cfg.Wrong = func(what string) { wrong = append(wrong, what) }

	if _, err := Connect(t.Context(), cfg); err == nil {
		t.Fatal("it connected after the dialog was dismissed")
	}
	if saidSomethingWith(wrong, path) {
		t.Fatalf("what went wrong was %v, want nothing about the key", wrong)
	}
}
