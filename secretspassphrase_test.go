package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// Nothing adds a passphrase. It is the weaker door and it stays shut
// until somebody opens it on purpose.
func TestAVaultTakesNoPassphraseUnlessAsked(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if v.TakesAPassphrase() {
		t.Error("a new vault takes a passphrase nobody asked for")
	}
}

// The form says what it costs, asks twice, and opens on the way out.
func TestTheSecretsPassphraseFormSaysWhatItCosts(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	startTheVault(t, a, keyFile)

	if err := a.addSecretsPassphrase(); err != nil {
		t.Fatalf("add: %v", err)
	}
	f := awaitModal(t, a, "the passphrase form",
		byTitle[*ui.Form](addSecretsPassphraseTitle))

	if said := strings.Join(f.Lines, " "); !strings.Contains(said, anyoneCanTryAtIt) {
		t.Errorf("the form says %q without what it costs", said)
	}
	if f.Field(fldPassphrase) == nil || f.Field(fldConfirmPass) == nil {
		t.Error("it does not ask twice for something that cannot be got back")
	}
	if at, isButton := f.Focused(); !isButton || f.Buttons()[at].Title != btnCancel {
		t.Errorf("it opens on button %d (%v), want %s", at, isButton, btnCancel)
	}
}

// Two that do not match are refused, with what was typed still there.
func TestTwoDifferentSecretsPassphrasesAreRefused(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if err := a.addSecretsPassphrase(); err != nil {
		t.Fatalf("add: %v", err)
	}
	f := awaitModal(t, a, "the passphrase form",
		byTitle[*ui.Form](addSecretsPassphraseTitle))

	typeIntoField(t, a, f, fldPassphrase, "one thing")
	typeIntoField(t, a, f, fldConfirmPass, "another")
	pressButton(t, a, f, btnAdd)

	if a.root.Modal() != ui.Widget(f) {
		t.Fatalf("the form went away; the modal on top is %T", a.root.Modal())
	}
	if got := f.Error(); got == nil || !strings.Contains(got.Error(), "match") {
		t.Errorf("it says %v", got)
	}
	if v.TakesAPassphrase() {
		t.Error("a passphrase was added anyway")
	}
}

// The whole way round: add one, lose every key, and get back in.
func TestAPassphraseIsTheWayBackInWhenTheKeyIsGone(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if _, err := v.Put(secrets.Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := v.AddPassphrase("a long one nobody guesses"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// The key this vault was made on is gone, and so is the one in the
	// ring: this is the machine after the old one died.
	if err := os.Remove(keyFile); err != nil {
		t.Fatalf("take the key away: %v", err)
	}
	v.Lock()
	a.keys.Lock()

	done := make(chan error, 1)
	a.unlockVault(v, func(err error) { done <- err })
	answer(t, a, btnUnlock, fldPassphrase, "a long one nobody guesses")
	waitFor(t, a, "the vault to open on the passphrase", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("unlock: %v", err)
	}
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 || items[0].Name != "margit" {
		t.Errorf("it holds %v, want what was put in", items)
	}
}

// The wrong one is asked about again rather than giving up, the way a
// key's passphrase is.
func TestAWrongSecretsPassphraseIsAskedAboutAgain(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if err := v.AddPassphrase("the right one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	v.Lock()

	done := make(chan error, 1)
	a.askForTheSecretsPassphrase(v, func(err error) { done <- err })
	answer(t, a, btnUnlock, fldPassphrase, "the wrong one")
	answer(t, a, btnUnlock, fldPassphrase, "the right one")
	waitFor(t, a, "the vault to open", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if v.Locked() {
		t.Error("it is still locked")
	}
}

// A dismissed box is not asked about again: the user said no.
func TestDismissingTheSecretsPassphraseGivesUp(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	v.Lock()

	done := make(chan error, 1)
	a.askForTheSecretsPassphrase(v, func(err error) { done <- err })
	awaitModal(t, a, "the passphrase box", byTitle[*ui.Form](dlgUnlockSecrets))
	sendKey(t, a, press(input.KeyEscape, 0))
	waitFor(t, a, "it to give up", func() bool { return len(done) > 0 })
	if err := <-done; !errors.Is(err, errDismissed) {
		t.Errorf("it answered %v, want the user's no", err)
	}
}

// A passphrase slot is listed as what it is, not as the random name
// the file gives it.
func TestAPassphraseSlotIsListedAsAPassphrase(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}

	var found bool
	for _, s := range v.Keys() {
		if !s.ByPassphrase() {
			continue
		}
		found = true
		if got := keyRowName(s); got != passphraseRowName {
			t.Errorf("the row says %q, want %q", got, passphraseRowName)
		}
		if got := keyRowNote(s); got == "" {
			t.Error("the row says nothing about what it is for")
		}
	}
	if !found {
		t.Fatal("the passphrase slot is not in the list")
	}
	_ = a
}

// And taking the last key away says the passphrase still opens them,
// rather than that a key from another machine is needed.
func TestTheLastKeyGoingSaysThePassphraseRemains(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	here := slotFor(t, v, keyFile)
	if got := whatRemovingCosts(v, here); !strings.Contains(got, "another machine") {
		t.Fatalf("without a passphrase it says %q", got)
	}

	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := whatRemovingCosts(v, here); !strings.Contains(got, "passphrase still opens") {
		t.Errorf("with a passphrase it says %q", got)
	}
	_ = a
}

// A key replaced at the same path is the commonest way to be locked
// out, and the passphrase has to answer it.
//
// Losing a key and making another where it was is what ssh-keygen does
// and what New SSH Key does. The file opens perfectly and is simply not
// the key the vault remembers, so the fallback has to be on the answer
// Unlock gives and not only on the file failing to be read.
func TestAKeyReplacedAtTheSamePathFallsBackToThePassphrase(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if _, err := v.Put(secrets.Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := v.AddPassphrase("the way back in"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Another key, written where the old one was.
	if err := os.Remove(keyFile); err != nil {
		t.Fatalf("take the old key away: %v", err)
	}
	if err := os.Remove(keyFile + ".pub"); err != nil {
		t.Fatalf("take its public half away: %v", err)
	}
	anEd25519KeyFile(t, keyFile)
	v.Lock()
	a.keys.Lock()

	done := make(chan error, 1)
	a.unlockVault(v, func(err error) { done <- err })
	answer(t, a, btnUnlock, fldPassphrase, "the way back in")
	waitFor(t, a, "the vault to open on the passphrase", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("unlock: %v", err)
	}
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 || items[0].Name != "margit" {
		t.Errorf("it holds %v, want what was put in", items)
	}
}

// Asking for a second is answered before the typing, not after.
func TestASecondPassphraseIsRefusedBeforeTheAsking(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if err := v.AddPassphrase("one"); err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := a.addSecretsPassphrase(); err != nil {
		t.Fatalf("ask again: %v", err)
	}
	if said := noticeTitleUp(t, a); !strings.Contains(said, "already opens") {
		t.Errorf("it said %q", said)
	}
	// And no form went up to type into.
	if _, isForm := a.root.Modal().(*ui.Form); isForm {
		t.Error("it asked for a passphrase it was going to refuse")
	}
}
