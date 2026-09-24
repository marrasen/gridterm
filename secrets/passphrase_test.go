package secrets

import (
	"errors"
	"os"
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

// A file that will not be read is refused, not opened out of memory.
//
// The passphrase is reached wherever a key fails, and one of those is
// the file being unreadable. Opening the copy read when the window
// started would hand over secrets out of a file nobody can say is
// still there, and the next save would put that copy over the disk.
func TestAPassphraseWillNotOpenAFileThatCannotBeRead(t *testing.T) {
	v, _, path := aVault(t)
	if _, err := v.Put(Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	v.Lock()

	// There and unreadable, which a restore with the wrong owner gives.
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skipf("cannot make it unreadable here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("this user reads it anyway, so this proves nothing")
	}

	if err := v.UnlockWith("one"); err == nil {
		t.Fatal("it opened a vault whose file will not be read")
	}
	if !v.Locked() {
		t.Error("it is open on a copy of a file nobody can read")
	}
}

// One passphrase, not two: two rows both saying "Passphrase" have
// nothing to tell them apart when one is being removed.
func TestASecondPassphraseIsRefused(t *testing.T) {
	v, _, _ := aVault(t)
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := v.AddPassphrase("another"); err == nil {
		t.Fatal("a second passphrase was added")
	}
	// And the first still opens it.
	v.Lock()
	if err := v.UnlockWith("one"); err != nil {
		t.Errorf("the first no longer opens it: %v", err)
	}
}
