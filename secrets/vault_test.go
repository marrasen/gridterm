package secrets

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// aKey is an ed25519 signer, which is what opens a vault.
func aKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("make a signer: %v", err)
	}
	return s
}

// aVault is a new vault in a directory the test owns, already open.
func aVault(t *testing.T) (*Vault, ssh.Signer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), Name)
	key := aKey(t)
	v, err := Create(path, key, "the test's key")
	if err != nil {
		t.Fatalf("create the vault: %v", err)
	}
	return v, key, path
}

// What goes in comes back out, after the vault has been closed and
// opened again with the same key.
func TestWhatGoesInComesBackOut(t *testing.T) {
	v, key, path := aVault(t)

	saved, err := v.Put(Item{Name: "margit", Kind: Password, User: "marcus"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if saved.ID == "" {
		t.Error("the item was saved without an id")
	}
	if _, err := v.Put(Item{Name: "recovery codes", Kind: Note}, "1234 5678"); err != nil {
		t.Fatalf("put a note: %v", err)
	}

	// A second window opening the same file.
	again, err := Open(path)
	if err != nil {
		t.Fatalf("open again: %v", err)
	}
	if !again.Locked() {
		t.Fatal("a vault just read off disk is not locked")
	}
	if err := again.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	items, err := again.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("the vault holds %d items, want 2", len(items))
	}
	// Sorted by name, so the note comes first.
	if items[0].Name != "margit" && items[1].Name != "margit" {
		t.Errorf("the items are %v, want margit among them", items)
	}
	got, err := again.Secret(saved.ID)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("the password came back as %q", got)
	}
}

// The list says what is in the vault and none of the secrets, so a
// window can draw it without holding any.
func TestTheListCarriesNoSecrets(t *testing.T) {
	v, _, path := aVault(t)
	if _, err := v.Put(Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	for _, it := range items {
		// Item has no value field at all; this catches one being added
		// later without thinking about it.
		if strings.Contains(strings.ToLower(it.Name+it.User+string(it.Kind)), "hunter2") {
			t.Error("a secret is in the list")
		}
	}
	// And the file on disk holds it nowhere in the clear.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file: %v", err)
	}
	for _, clear := range []string{"hunter2", "margit"} {
		if strings.Contains(string(raw), clear) {
			t.Errorf("%q is in the vault file in the clear", clear)
		}
	}
}

// Another key does not open the vault.
func TestAnotherKeyDoesNotOpenIt(t *testing.T) {
	_, _, path := aVault(t)

	v, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := v.Unlock([]ssh.Signer{aKey(t)}); !errors.Is(err, ErrWrongKey) {
		t.Errorf("a stranger's key gave %v, want ErrWrongKey", err)
	}
	if !v.Locked() {
		t.Error("the vault opened to the wrong key")
	}
}

// A key whose signature is different every time is refused, because a
// vault sealed with one would never open again.
func TestAKeyThatSignsDifferentlyEveryTimeIsRefused(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("make a signer: %v", err)
	}
	path := filepath.Join(t.TempDir(), Name)
	if _, err := Create(path, signer, ""); !errors.Is(err, ErrNotDeterministic) {
		t.Errorf("creating with an ecdsa key gave %v, want ErrNotDeterministic", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("a vault was written for a key that cannot open it")
	}
}

// A second machine's key is added without re-encrypting anything, and
// opens the vault on its own.
func TestASecondKeyOpensIt(t *testing.T) {
	v, first, path := aVault(t)
	if _, err := v.Put(Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	second := aKey(t)
	if err := v.AddKey(second, "the linux machine"); err != nil {
		t.Fatalf("add a key: %v", err)
	}

	for _, tc := range []struct {
		what string
		key  ssh.Signer
	}{{"the first key", first}, {"the second key", second}} {
		open, err := Open(path)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if err := open.Unlock([]ssh.Signer{tc.key}); err != nil {
			t.Fatalf("%s did not open it: %v", tc.what, err)
		}
		items, err := open.Items()
		if err != nil || len(items) != 1 {
			t.Fatalf("%s opened it onto %d items (%v)", tc.what, len(items), err)
		}
	}
	if got := len(v.Keys()); got != 2 {
		t.Errorf("the vault lists %d keys, want 2", got)
	}
}

// The last key cannot be taken away, or nothing would open the vault.
func TestTheLastKeyCannotGo(t *testing.T) {
	v, key, _ := aVault(t)
	err := v.RemoveKey(Fingerprint(key.PublicKey()))
	if err == nil {
		t.Fatal("the only key was removed, so the vault is now shut for good")
	}
	if !strings.Contains(err.Error(), "only key") {
		t.Errorf("it said %q, which does not explain why", err)
	}
}

// A locked vault hands nothing over.
func TestALockedVaultHandsNothingOver(t *testing.T) {
	v, _, _ := aVault(t)
	saved, err := v.Put(Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	v.Lock()

	if !v.Locked() {
		t.Fatal("it did not lock")
	}
	if _, err := v.Items(); !errors.Is(err, ErrLocked) {
		t.Errorf("items gave %v, want ErrLocked", err)
	}
	if _, err := v.Secret(saved.ID); !errors.Is(err, ErrLocked) {
		t.Errorf("secret gave %v, want ErrLocked", err)
	}
	if _, err := v.Put(Item{Name: "another"}, "x"); !errors.Is(err, ErrLocked) {
		t.Errorf("put gave %v, want ErrLocked", err)
	}
	if err := v.Remove(saved.ID); !errors.Is(err, ErrLocked) {
		t.Errorf("remove gave %v, want ErrLocked", err)
	}
}

// A save that fails takes the change back.
//
// The file is what the vault is. An item that never reached disk must
// not sit in the window looking saved.
func TestAFailedSaveTakesTheChangeBack(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{Name: "first"}, "one"); err != nil {
		t.Fatalf("put: %v", err)
	}

	was := rename
	rename = func(from, to string) error { return errors.New("the disk said no") }
	t.Cleanup(func() { rename = was })

	if _, err := v.Put(Item{Name: "second"}, "two"); err == nil {
		t.Fatal("a save that failed was reported as having worked")
	}
	rename = was

	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 || items[0].Name != "first" {
		t.Errorf("the vault holds %v, want only the item that saved", items)
	}
}

// Taking an item out takes it out of the file too.
func TestRemovingAnItemRemovesIt(t *testing.T) {
	v, key, path := aVault(t)
	saved, err := v.Put(Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := v.Remove(saved.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := v.Remove(saved.ID); !errors.Is(err, ErrNoSuchItem) {
		t.Errorf("removing it twice gave %v, want ErrNoSuchItem", err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := again.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	items, err := again.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("the file still holds %v", items)
	}
}

// Changing an item keeps its id and the day it was made.
func TestChangingAnItemKeepsItsId(t *testing.T) {
	v, _, _ := aVault(t)
	first, err := v.Put(Item{Name: "margit", Kind: Password}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	first.Name = "margit.skalarit.net"
	second, err := v.Put(first, "hunter3")
	if err != nil {
		t.Fatalf("put again: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("the id changed from %s to %s", first.ID, second.ID)
	}
	if !second.Made.Equal(first.Made) {
		t.Errorf("the day it was made changed from %v to %v", first.Made, second.Made)
	}
	got, err := v.Secret(first.ID)
	if err != nil || got != "hunter3" {
		t.Errorf("the value came back %q (%v), want hunter3", got, err)
	}
}

// Putting a better name on something leaves the secret where it is.
func TestPutDetailsLeavesTheSecretAlone(t *testing.T) {
	v, key, path := aVault(t)
	first, err := v.Put(Item{Name: "margit", Kind: Password, User: "marcus"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	first.Name, first.User = "margit.skalarit.net", "marcusj"
	changed, err := v.PutDetails(first)
	if err != nil {
		t.Fatalf("put the details: %v", err)
	}
	if changed.Kind != Password {
		t.Errorf("the kind became %q", changed.Kind)
	}
	if !changed.Made.Equal(first.Made) {
		t.Error("the day it was made changed")
	}

	// Off disk, so this is the file and not just what is in memory.
	again, err := Open(path)
	if err != nil {
		t.Fatalf("open again: %v", err)
	}
	if err := again.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	got, err := again.Secret(first.ID)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("the secret came back %q, want it untouched", got)
	}
	items, err := again.Items()
	if err != nil || len(items) != 1 {
		t.Fatalf("the vault holds %d items (%v)", len(items), err)
	}
	if items[0].Name != "margit.skalarit.net" || items[0].User != "marcusj" {
		t.Errorf("the details did not change: %+v", items[0])
	}
}

// Details of something that is not there, and of a locked vault, are
// refused.
func TestPutDetailsRefusesWhatItCannotDo(t *testing.T) {
	v, _, _ := aVault(t)
	saved, err := v.Put(Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := v.PutDetails(Item{ID: "nothing", Name: "x"}); !errors.Is(err, ErrNoSuchItem) {
		t.Errorf("an unknown id gave %v, want ErrNoSuchItem", err)
	}
	if _, err := v.PutDetails(Item{ID: saved.ID, Name: "  "}); err == nil {
		t.Error("an item was left with no name")
	}
	v.Lock()
	if _, err := v.PutDetails(Item{ID: saved.ID, Name: "x"}); !errors.Is(err, ErrLocked) {
		t.Errorf("a locked vault gave %v, want ErrLocked", err)
	}
}

// An item needs a name, or the list has a blank row nobody can pick.
func TestAnItemNeedsAName(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{Name: "   "}, "x"); err == nil {
		t.Error("an item with no name was saved")
	}
}

// Opening where there is no vault is not a failure: it says so, and
// the window offers to make one.
func TestNoVaultYetIsNotAFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	v, err := Open(path)
	if err != nil {
		t.Fatalf("open nothing: %v", err)
	}
	if v.Exists() {
		t.Error("it says there is a vault where there is no file")
	}
	if err := v.Unlock([]ssh.Signer{aKey(t)}); err == nil {
		t.Error("a vault that does not exist unlocked")
	}
}

// A vault is not written over, because the file holds the only copy of
// what is in it.
func TestCreateWillNotWriteOverAVault(t *testing.T) {
	_, _, path := aVault(t)
	if _, err := Create(path, aKey(t), ""); err == nil {
		t.Fatal("Create wrote over a vault that was already there")
	}
}

// The vault says which keys open it, so the window can offer one it has
// already unlocked instead of asking for a passphrase.
func TestTheVaultSaysWhichKeysOpenIt(t *testing.T) {
	v, key, _ := aVault(t)
	want := Fingerprint(key.PublicKey())
	got := v.Wants()
	if len(got) != 1 || got[0] != want {
		t.Errorf("it wants %v, want just %s", got, want)
	}
}
