package secrets

import (
	"errors"
	"testing"

	"golang.org/x/crypto/ssh"
)

// A passphrase opens a vault whose keys are all gone, which is the
// whole of what it is for.
func TestAPassphraseOpensAVaultWithNoKeyLeft(t *testing.T) {
	v, key, path := aVault(t)
	if _, err := v.Put(Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := v.AddPassphrase("a long one nobody guesses"); err != nil {
		t.Fatalf("add a passphrase: %v", err)
	}

	// The machine the key was on is gone, and this is the copy.
	back, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := back.Unlock([]ssh.Signer{aKey(t)}); err == nil {
		t.Fatal("another key opened it")
	}
	if err := back.UnlockWith("a long one nobody guesses"); err != nil {
		t.Fatalf("unlock with the passphrase: %v", err)
	}
	items, err := back.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 || items[0].Name != "margit" {
		t.Errorf("it holds %v, want what was put in", items)
	}
	// And the key still opens it too.
	back.Lock()
	if err := back.Unlock([]ssh.Signer{key}); err != nil {
		t.Errorf("the key no longer opens it: %v", err)
	}
}

// The wrong one is told apart from there being none.
func TestAWrongPassphraseIsToldApartFromNoneAtAll(t *testing.T) {
	v, key, _ := aVault(t)
	// Locked first: an open vault opens with anything, the way Unlock
	// answers a key it has already been opened by.
	v.Lock()
	if err := v.UnlockWith("anything"); !errors.Is(err, ErrNoPassphrase) {
		t.Errorf("a vault with no passphrase slot says %v", err)
	}
	if err := v.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if err := v.AddPassphrase("the right one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	v.Lock()
	if err := v.UnlockWith("the wrong one"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("the wrong passphrase says %v", err)
	}
	if err := v.UnlockWith("the right one"); err != nil {
		t.Errorf("the right one says %v", err)
	}
}

// A passphrase of nothing is refused. It would open the vault to
// anybody who pressed the button.
func TestAPassphraseOfNothingIsRefused(t *testing.T) {
	v, _, _ := aVault(t)
	for _, empty := range []string{"", "   "} {
		if err := v.AddPassphrase(empty); err == nil {
			t.Errorf("a passphrase of %q was taken", empty)
		}
	}
}

// Nothing has a passphrase unless one is asked for.
func TestAVaultTakesNoPassphraseUntilOneIsAdded(t *testing.T) {
	v, _, _ := aVault(t)
	if v.TakesAPassphrase() {
		t.Error("a new vault takes a passphrase nobody asked for")
	}
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !v.TakesAPassphrase() {
		t.Error("it does not know it takes one")
	}
}

// A passphrase slot is not offered as a key: there is nothing to hold
// and nothing to match it against.
func TestAPassphraseSlotIsNotOfferedAsAKey(t *testing.T) {
	v, key, _ := aVault(t)
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := len(v.Wants()); got != 1 {
		t.Errorf("it wants %d keys, want only the real one", got)
	}
	// But it is a slot, so the list of them shows it and says which.
	slots := v.Keys()
	if len(slots) != 2 {
		t.Fatalf("%d slots, want the key and the passphrase", len(slots))
	}
	byPass := 0
	for _, s := range slots {
		if s.ByPassphrase() {
			byPass++
			if s.Fingerprint == "" {
				t.Error("the passphrase slot has no name to remove it by")
			}
		}
	}
	if byPass != 1 {
		t.Errorf("%d slots say they take a passphrase, want one", byPass)
	}
	_ = key
}

// It can be taken away again, by the name the list gives it.
func TestAPassphraseSlotCanBeRemoved(t *testing.T) {
	v, _, _ := aVault(t)
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	var name string
	for _, s := range v.Keys() {
		if s.ByPassphrase() {
			name = s.Fingerprint
		}
	}
	if err := v.RemoveKey(name); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if v.TakesAPassphrase() {
		t.Error("it still takes a passphrase")
	}
	v.Lock()
	if err := v.UnlockWith("one"); !errors.Is(err, ErrNoPassphrase) {
		t.Errorf("the passphrase still says %v", err)
	}
}

// The cost of a guess is written into the slot, so raising it later
// does not shut anybody out of a vault sealed under the old cost.
func TestTheCostOfAGuessIsWrittenDown(t *testing.T) {
	v, _, _ := aVault(t)
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	var found bool
	for _, s := range v.file.Slots {
		if s.Kind != slotKindPassphrase {
			continue
		}
		found = true
		if s.Time == 0 || s.Memory == 0 || s.Threads == 0 {
			t.Errorf("the slot says time %d, memory %d, threads %d", s.Time, s.Memory, s.Threads)
		}
	}
	if !found {
		t.Fatal("no passphrase slot was written")
	}
}
