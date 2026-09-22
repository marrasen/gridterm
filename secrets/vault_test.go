package secrets

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"slices"
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

// And it takes it back out of the sealed half as well, which is what a
// later unlock reads.
//
// Taking v.items back was not enough on its own: Lock drops them and
// Unlock builds them again out of the sealed blob, so a change that
// never reached the disk came back as though it had.
func TestAFailedSaveDoesNotSurviveALockAndUnlock(t *testing.T) {
	v, key, _ := aVault(t)
	if _, err := v.Put(Item{Name: "keep me"}, "one"); err != nil {
		t.Fatalf("put: %v", err)
	}
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}

	was := rename
	rename = func(from, to string) error { return errors.New("the disk said no") }
	t.Cleanup(func() { rename = was })
	if err := v.Remove(items[0].ID); err == nil {
		t.Fatal("a removal that failed was reported as having worked")
	}
	rename = was

	v.Lock()
	if err := v.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	got, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(got) != 1 || got[0].Name != "keep me" {
		t.Errorf("the vault holds %v, want the item the removal failed to take", got)
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

// A slot that will not open does not shut out a key whose own slot is
// untouched.
//
// More than one key is the whole point of the slots: a second machine
// gets its own, and losing one is not losing the vault. Giving up on the
// first slot that failed took that away, because the keys are offered in
// whatever order the window has them.
func TestADamagedSlotDoesNotShutOutAGoodKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	a, b := aKey(t), aKey(t)

	v, err := Create(path, a, "a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := v.Put(Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := v.AddKey(b, "b"); err != nil {
		t.Fatalf("add the second key: %v", err)
	}

	// A's slot no longer opens: the wrapped data key in it is damaged.
	f, err := readFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for i := range f.Slots {
		if f.Slots[i].Fingerprint == Fingerprint(a.PublicKey()) {
			f.Slots[i].Wrapped[0] ^= 0xff
		}
	}
	if err := writeFile(path, f); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A is offered first, so the damaged slot is met first.
	again, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := again.Unlock([]ssh.Signer{a, b}); err != nil {
		t.Fatalf("the good key did not open the vault: %v", err)
	}
	if got, err := again.Secret(itemNamed(t, again, "margit").ID); err != nil || got != "hunter2" {
		t.Fatalf("the secret came back as %q, %v", got, err)
	}
}

// And with nothing that opens it, what comes back says which key was
// named by a slot it could not open, rather than only that no key fits.
func TestADamagedSlotIsStillReportedWhenNothingOpensIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	a := aKey(t)

	if _, err := Create(path, a, "a"); err != nil {
		t.Fatalf("create: %v", err)
	}
	f, err := readFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	f.Slots[0].Wrapped[0] ^= 0xff
	if err := writeFile(path, f); err != nil {
		t.Fatalf("write: %v", err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	err = again.Unlock([]ssh.Signer{a})
	if err == nil {
		t.Fatal("a damaged slot opened the vault")
	}
	if !strings.Contains(err.Error(), "named by a slot it does not open") {
		t.Errorf("it said %v, want it to name the slot that failed", err)
	}
}

// Every write says it is later than the one before it.
//
// Every copy of the file is validly sealed, so the crypto cannot tell
// yesterday's from today's. The count can: a file that opens with a
// lower one than a file opened before is an older copy of it.
func TestEveryWriteCountsUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	v, err := Create(path, aKey(t), "a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	was := v.Saves()
	if was == 0 {
		t.Fatal("a vault that has been written says it has been written no times")
	}
	if _, err := v.Put(Item{Name: "one"}, "a"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := v.Saves(); got <= was {
		t.Fatalf("the count went from %d to %d, and should have gone up", was, got)
	}
}

// An older copy of the file put back in place says so, by opening with
// a count below the one already seen.
func TestAnOlderFilePutBackSaysSo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	key := aKey(t)

	v, err := Create(path, key, "a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := v.AddKey(aKey(t), "b"); err != nil {
		t.Fatalf("add a key: %v", err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("keep a copy: %v", err)
	}
	seen := v.Saves()

	// The second key is revoked, which is a write and a higher count.
	if err := v.RemoveKey(v.Keys()[1].Fingerprint); err != nil {
		t.Fatalf("remove the key: %v", err)
	}
	if v.Saves() <= seen {
		t.Fatal("revoking a key did not count as a write")
	}
	seen = v.Saves()

	// And the copy from before it goes back, bringing the slot with it.
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatalf("put the old file back: %v", err)
	}
	back, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := back.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if len(back.Keys()) != 2 {
		t.Fatal("the old file did not bring the revoked key back, so this proves nothing")
	}
	if got := back.Saves(); got >= seen {
		t.Errorf("the old file says it was written %d times against %d already seen,"+
			" so putting it back cannot be told from a new write", got, seen)
	}
}

// Two windows creating a vault at once: one wins and the other is told,
// rather than one quietly writing over the other.
func TestCreatingAVaultTwiceRefusesTheSecond(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	if _, err := Create(path, aKey(t), "a"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := Create(path, aKey(t), "b"); err == nil {
		t.Fatal("the second create wrote over the first vault")
	}
}

// A create that fails leaves no file behind. An empty one would read as
// a vault that cannot be opened, which is worse than no vault.
func TestAFailedCreateLeavesNothingBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	was := rename
	rename = func(string, string) error { return errors.New("the disk went away") }
	t.Cleanup(func() { rename = was })

	if _, err := Create(path, aKey(t), "a"); err == nil {
		t.Fatal("a create whose write failed said it worked")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("something was left at %s: %v", path, err)
	}
}

// itemNamed is the one item with this name, for a test that put it
// there and wants its id back.
func itemNamed(t *testing.T, v *Vault, name string) Item {
	t.Helper()
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	for _, it := range items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("nothing in the vault is called %q", name)
	return Item{}
}

// A key's passphrase is found by the file it opens, whatever the item
// has since been renamed to.
func TestAPassphraseIsFoundByItsKeyFile(t *testing.T) {
	v, _, _ := aVault(t)
	keyFile := "/home/marcus/.ssh/id_ed25519_gridterm"

	saved, err := v.Put(Item{Name: "id_ed25519_gridterm", Kind: Passphrase, File: keyFile},
		"a long generated one")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := v.PassphraseFor(keyFile)
	if err != nil {
		t.Fatalf("PassphraseFor: %v", err)
	}
	if got != "a long generated one" {
		t.Errorf("PassphraseFor = %q, want what was saved", got)
	}

	saved.Name = "the laptop key"
	if _, err := v.PutDetails(saved); err != nil {
		t.Fatalf("rename it: %v", err)
	}
	if got, err := v.PassphraseFor(keyFile); err != nil || got != "a long generated one" {
		t.Errorf("PassphraseFor after a rename = %q, %v", got, err)
	}
}

// A key the vault holds nothing for says so, and so does a password
// that happens to be named after one.
func TestNoPassphraseForAKeyIsNotAFailure(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{Name: "/home/marcus/.ssh/id_ed25519"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}

	for _, keyFile := range []string{"/home/marcus/.ssh/id_ed25519", "", "/nothing/here"} {
		if _, err := v.PassphraseFor(keyFile); !errors.Is(err, ErrNoSuchItem) {
			t.Errorf("PassphraseFor(%q) = %v, want ErrNoSuchItem", keyFile, err)
		}
	}
}

// A second passphrase for the same key file is refused: the first is
// the one PassphraseFor would answer with, and the second would sit
// there unused with nothing saying which locks the key.
func TestOnlyOnePassphrasePerKeyFile(t *testing.T) {
	v, _, _ := aVault(t)
	keyFile := "/home/marcus/.ssh/id_ed25519_gridterm"
	first, err := v.Put(Item{Name: "the first", Kind: Passphrase, File: keyFile}, "one")
	if err != nil {
		t.Fatalf("put the first: %v", err)
	}

	if _, err := v.Put(Item{Name: "the second", Kind: Passphrase, File: keyFile}, "two"); err == nil {
		t.Fatal("a second passphrase for the same key file was taken")
	}

	// Changing the one that is there is not a clash with itself.
	if _, err := v.Put(first, "one again"); err != nil {
		t.Fatalf("change the first: %v", err)
	}
	if got, err := v.PassphraseFor(keyFile); err != nil || got != "one again" {
		t.Errorf("PassphraseFor = %q, %v, want the changed one", got, err)
	}
}

// A locked vault hands over no passphrase either.
func TestALockedVaultHandsOverNoPassphrase(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{Name: "key", Kind: Passphrase, File: "/key"}, "shh"); err != nil {
		t.Fatalf("put: %v", err)
	}
	v.Lock()
	if _, err := v.PassphraseFor("/key"); !errors.Is(err, ErrLocked) {
		t.Errorf("PassphraseFor on a locked vault = %v, want ErrLocked", err)
	}
}

// Two windows on one vault do not write each other's secrets away.
//
// Each reads the file when it opens it and each save writes the whole
// of it back, so the one that saved second used to leave the other's
// item nowhere at all, with nothing reported to either of them.
func TestASecondWindowDoesNotWriteTheFirstsSecretsAway(t *testing.T) {
	first, key, path := aVault(t)
	second, err := Open(path)
	if err != nil {
		t.Fatalf("open it again: %v", err)
	}
	if err := second.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock the second: %v", err)
	}

	if _, err := first.Put(Item{Name: "from the first"}, "one"); err != nil {
		t.Fatalf("put in the first: %v", err)
	}
	if _, err := second.Put(Item{Name: "from the second"}, "two"); err != nil {
		t.Fatalf("put in the second: %v", err)
	}

	// Read back off the disk, which is the only copy either of them has.
	third, err := Open(path)
	if err != nil {
		t.Fatalf("open it a third time: %v", err)
	}
	if err := third.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock the third: %v", err)
	}
	items, err := third.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	if len(names) != 2 {
		t.Fatalf("the vault holds %v, want both windows' items", names)
	}
	for _, want := range []string{"from the first", "from the second"} {
		if !slices.Contains(names, want) {
			t.Errorf("the vault holds %v, want %q among them", names, want)
		}
	}
}

// And a key one window adds is one the other can be opened by, without
// the second window writing the slot away again.
func TestASecondWindowKeepsAKeyTheFirstAdded(t *testing.T) {
	first, key, path := aVault(t)
	second, err := Open(path)
	if err != nil {
		t.Fatalf("open it again: %v", err)
	}
	if err := second.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock the second: %v", err)
	}

	spare := aKey(t)
	if err := first.AddKey(spare, "the spare"); err != nil {
		t.Fatalf("add the key: %v", err)
	}
	if _, err := second.Put(Item{Name: "from the second"}, "two"); err != nil {
		t.Fatalf("put in the second: %v", err)
	}

	third, err := Open(path)
	if err != nil {
		t.Fatalf("open it a third time: %v", err)
	}
	if err := third.Unlock([]ssh.Signer{spare}); err != nil {
		t.Fatalf("the key the first window added no longer opens it: %v", err)
	}
}

// A vault that was locked reads the file again when it is unlocked, so
// what another window wrote meanwhile is what it shows.
func TestUnlockingReadsTheFileAgain(t *testing.T) {
	v, key, path := aVault(t)
	v.Lock()

	other, err := Open(path)
	if err != nil {
		t.Fatalf("open it again: %v", err)
	}
	if err := other.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock the other: %v", err)
	}
	if _, err := other.Put(Item{Name: "added while locked"}, "one"); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := v.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 || items[0].Name != "added while locked" {
		t.Errorf("the vault holds %v, want what the other window added", items)
	}
}
