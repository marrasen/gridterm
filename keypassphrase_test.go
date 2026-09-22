package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// aKeyWindowWithSecrets is a window that can both write a key file and
// keep its passphrase: the settings the key list lives in, and a vault
// a key with no passphrase opens.
func aKeyWindowWithSecrets(t *testing.T) (*testApp, *secrets.Vault) {
	t.Helper()
	a, keyFile := aWindowWithSecrets(t)
	if _, err := withSettings(t, a, filepath.Join(t.TempDir(), "settings.json")); err != nil {
		t.Fatalf("settings: %v", err)
	}
	return a, startTheVault(t, a, keyFile)
}

// The dialog offers to generate the passphrase, and ticking it turns
// off the two fields that would otherwise ask for one.
func TestTheGenerateBoxTurnsOffThePassphraseFields(t *testing.T) {
	a, _ := aKeyWindowWithSecrets(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")

	f := makeKeyDialog(t, a, at)
	typeIntoField(t, a, f, fldPassphrase, "typed by hand")
	tickBox(t, a, f, fldGeneratePass)

	for _, label := range []string{fldPassphrase, fldConfirmPass} {
		if !f.Field(label).Disabled {
			t.Errorf("the %q field still takes typing", label)
		}
		// Cleared as well as disabled: a passphrase left on screen
		// under a field that no longer applies is one the user
		// believes they set.
		if got := f.Field(label).Text(); got != "" {
			t.Errorf("the %q field still holds %q", label, got)
		}
	}
}

// Without a vault there is nowhere to put a generated passphrase, so
// the dialog does not offer one.
func TestWithoutAVaultTheKeyDialogOffersNoGeneratedPassphrase(t *testing.T) {
	withHome(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.secretsAt = filepath.Join(t.TempDir(), secrets.Name)
	if _, err := withSettings(t, a, filepath.Join(t.TempDir(), "settings.json")); err != nil {
		t.Fatalf("settings: %v", err)
	}

	f := makeKeyDialog(t, a, filepath.Join(t.TempDir(), "id_ed25519"))
	if f.Field(fldGeneratePass) != nil {
		t.Errorf("the dialog offers %q with no vault to keep one in", fldGeneratePass)
	}
}

// A key made with the box ticked is locked with a passphrase that is in
// the vault, opens the key, and was never put on screen.
func TestAGeneratedKeyPassphraseGoesIntoTheVault(t *testing.T) {
	a, v := aKeyWindowWithSecrets(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")

	f := makeKeyDialog(t, a, at)
	tickBox(t, a, f, fldGeneratePass)
	pressButton(t, a, f, btnCreate)

	n := awaitModal(t, a, "what to do with the key", byTitle[*ui.Notice](dlgKeyCreated))

	pass, err := v.PassphraseFor(at)
	if err != nil {
		t.Fatalf("the vault holds no passphrase for %s: %v", at, err)
	}
	if len(pass) != secrets.PassphraseLength {
		t.Errorf("the passphrase is %d characters, want %d", len(pass), secrets.PassphraseLength)
	}
	raw, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if _, err := ssh.ParsePrivateKeyWithPassphrase(raw, []byte(pass)); err != nil {
		t.Fatalf("the saved passphrase does not open the key: %v", err)
	}
	// It is not on the dialog, and nothing else is going to show it.
	if strings.Contains(n.Message(), pass) {
		t.Error("the dialog shows the passphrase")
	}
	if !strings.Contains(n.Message(), "The passphrase is saved in the secrets.") {
		t.Errorf("the dialog does not say where the passphrase went:\n%s", n.Message())
	}
}

// And the key is still there for the ordinary path, with nothing saved
// and nothing said about the secrets.
func TestAKeyMadeWithoutTheBoxSavesNothing(t *testing.T) {
	a, v := aKeyWindowWithSecrets(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")

	f := makeKeyDialog(t, a, at)
	typeIntoField(t, a, f, fldPassphrase, "one I chose")
	typeIntoField(t, a, f, fldConfirmPass, "one I chose")
	pressButton(t, a, f, btnCreate)

	n := awaitModal(t, a, "what to do with the key", byTitle[*ui.Notice](dlgKeyCreated))
	if _, err := v.PassphraseFor(at); !errors.Is(err, secrets.ErrNoSuchItem) {
		t.Errorf("the vault holds a passphrase for %s: %v", at, err)
	}
	if strings.Contains(n.Message(), "saved in the secrets") {
		t.Errorf("the dialog says the passphrase was saved:\n%s", n.Message())
	}
}

// The passphrase in the vault unlocks the key with no dialog at all.
func TestASavedPassphraseUnlocksTheKeyWithoutAsking(t *testing.T) {
	a, v := aKeyWindowWithSecrets(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")

	f := makeKeyDialog(t, a, at)
	tickBox(t, a, f, fldGeneratePass)
	pressButton(t, a, f, btnCreate)
	awaitModal(t, a, "what to do with the key", byTitle[*ui.Notice](dlgKeyCreated))
	sendKey(t, a, press(input.KeyEscape, 0))

	pass, err := v.PassphraseFor(at)
	if err != nil {
		t.Fatalf("PassphraseFor: %v", err)
	}

	got := make(chan string, 1)
	go func() {
		reply, err := (&askUser{app: a.app}).Passphrase(a.ctx, remote.LockedKey{Path: at})
		if err != nil {
			t.Errorf("Passphrase: %v", err)
		}
		got <- reply
	}()

	waitFor(t, a, "the saved passphrase to come back", func() bool { return len(got) > 0 })
	if answer := <-got; answer != pass {
		t.Errorf("it answered %q, want the saved passphrase", answer)
	}
	// Nothing was put on screen to get it.
	if up := a.root.Modal(); up != nil {
		t.Errorf("a %T dialog went up to read a saved passphrase", up)
	}
}

// A saved passphrase the key has since refused is not offered twice:
// the second time round the user is asked.
func TestARefusedSavedPassphraseFallsBackToTheDialog(t *testing.T) {
	a, v := aKeyWindowWithSecrets(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")
	if _, err := v.Put(secrets.Item{
		Name: "id_ed25519", Kind: secrets.Passphrase, File: at,
	}, "the wrong one"); err != nil {
		t.Fatalf("put: %v", err)
	}

	got := make(chan string, 1)
	go func() {
		reply, err := (&askUser{app: a.app}).Passphrase(a.ctx,
			remote.LockedKey{Path: at, Wrong: 1})
		if err != nil {
			t.Errorf("Passphrase: %v", err)
		}
		got <- reply
	}()

	answer(t, a, btnUnlock, fldPassphrase, "the right one")
	if typed := <-got; typed != "the right one" {
		t.Errorf("it answered %q, want what the user typed", typed)
	}
}

// A key the vault knows nothing about is asked about, the way it always
// was.
func TestAKeyWithNoSavedPassphraseIsStillAskedAbout(t *testing.T) {
	a, _ := aKeyWindowWithSecrets(t)

	got := make(chan string, 1)
	go func() {
		reply, err := (&askUser{app: a.app}).Passphrase(a.ctx,
			remote.LockedKey{Path: "/home/marcus/.ssh/id_ed25519"})
		if err != nil {
			t.Errorf("Passphrase: %v", err)
		}
		got <- reply
	}()

	answer(t, a, btnUnlock, fldPassphrase, "let me in")
	if typed := <-got; typed != "let me in" {
		t.Errorf("it answered %q, want what the user typed", typed)
	}
}

// A key whose passphrase is in the vault says so on the row that offers
// it as a second key: it cannot open the vault the passphrase is in.
func TestAKeyWhosePassphraseIsInTheVaultIsMarked(t *testing.T) {
	_, v := aKeyWindowWithSecrets(t)
	if _, err := v.Put(secrets.Item{
		Name: "id_ed25519", Kind: secrets.Passphrase, File: "/home/marcus/.ssh/id_ed25519",
	}, "generated"); err != nil {
		t.Fatalf("put: %v", err)
	}

	if got := spareKeyNote(v, "/home/marcus/.ssh/id_ed25519"); got == "" {
		t.Error("a key whose passphrase is in the vault is offered with no note")
	}
	if got := spareKeyNote(v, "/home/marcus/.ssh/id_other"); got != "" {
		t.Errorf("a key the vault knows nothing about is marked %q", got)
	}
}

// Adding a key whose passphrase is in the vault asks first, and says
// what the user would have to do to use it as a spare.
func TestAddingAKeyWhosePassphraseIsInTheVaultAsksFirst(t *testing.T) {
	a, v := aKeyWindowWithSecrets(t)
	keyFile := filepath.Join(t.TempDir(), "id_ed25519_spare")
	if _, err := v.Put(secrets.Item{
		Name: "id_ed25519_spare", Kind: secrets.Passphrase, File: keyFile,
	}, "generated"); err != nil {
		t.Fatalf("put: %v", err)
	}

	a.confirmAddKey(v, keyFile)
	f := awaitModal(t, a, "the question about the key",
		byTitle[*ui.Form](passphraseInTheSecrets))

	// Asked, not refused: there is a button that goes ahead.
	titles := []string{}
	for _, b := range f.Buttons() {
		titles = append(titles, b.Title)
	}
	if !slices.Contains(titles, btnAdd) {
		t.Errorf("the question offers %v, want a way to go ahead", titles)
	}
	if !slices.Contains(titles, btnCancel) {
		t.Errorf("the question offers %v, want a way out", titles)
	}
	// And it says what to do about it.
	said := strings.Join(f.Lines, " ")
	if !strings.Contains(said, keyFile) {
		t.Errorf("the question says %q, want the key it is about", said)
	}
	if !strings.Contains(said, "Copy the passphrase") {
		t.Errorf("the question says %q, want what to do about it", said)
	}
}

// A key the vault holds no passphrase for is added without a question.
func TestAddingAnOrdinaryKeyAsksNothing(t *testing.T) {
	a, v := aKeyWindowWithSecrets(t)
	spare := anEd25519KeyFile(t, filepath.Join(t.TempDir(), "id_ed25519_spare"))

	a.confirmAddKey(v, spare)
	waitFor(t, a, "the key to open the secrets", func() bool { return len(v.Keys()) == 2 })
	if up := a.root.Modal(); up != nil {
		t.Errorf("a %T dialog went up for an ordinary key", up)
	}
}

// On a second machine the vault is unlocked with the key that is here,
// not with the path the machine it was made on used.
//
// The slots are in the order they were added, so the first names a key
// file on the other machine. Asking for its passphrase failed on
// opening the file, which shut this machine out of a vault its own key
// opens -- the one thing a second slot exists for.
func TestTheVaultIsUnlockedWithTheKeyThatIsHere(t *testing.T) {
	a, v := aKeyWindowWithSecrets(t)
	here := anEd25519KeyFile(t, filepath.Join(t.TempDir(), "id_ed25519_here"))
	signer, err := ssh.ParsePrivateKey(readFileOrDie(t, here))
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if err := v.AddKey(signer, here); err != nil {
		t.Fatalf("add the key: %v", err)
	}
	// The key the vault was made on is not on this machine any more.
	away := v.Keys()[0].KeyFile
	if err := os.Remove(away); err != nil {
		t.Fatalf("take the first key away: %v", err)
	}

	if got, err := keyFileForVault(v); err != nil || got != here {
		t.Fatalf("keyFileForVault = %q, %v; want the key on this machine %q", got, err, here)
	}

	// And the whole path works: locked, it opens with no dialog because
	// the key here has no passphrase.
	v.Lock()
	a.keys.Lock()
	done := make(chan error, 1)
	a.unlockVault(v, func(err error) { done <- err })
	waitFor(t, a, "the vault to open on the key that is here", func() bool { return len(done) > 0 })
	if err := <-done; err != nil {
		t.Fatalf("unlock the vault: %v", err)
	}
	if v.Locked() {
		t.Error("the vault is still locked")
	}
}

// readFileOrDie reads a file a test has just written.
func readFileOrDie(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}
