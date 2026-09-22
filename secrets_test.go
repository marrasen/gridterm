package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// anEd25519KeyFile writes a private key with no passphrase and returns
// where it is, so a test opens a vault without a dialog in the way.
func anEd25519KeyFile(t *testing.T, path string) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "for the test")
	if err != nil {
		t.Fatalf("marshal the key: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write the key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("make the public half: %v", err)
	}
	if err := os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(sshPub), 0o600); err != nil {
		t.Fatalf("write the public half: %v", err)
	}
	return path
}

// aWindowWithSecrets is a window whose vault is in a directory the test
// owns, and a key with no passphrase that opens it.
func aWindowWithSecrets(t *testing.T) (*testApp, string) {
	t.Helper()
	// A home of its own before anything else: vaultKeys offers the
	// gridterm key beside the user's own, and a test that read the real
	// one would pass or fail on whether whoever ran it happens to have
	// made it.
	withHome(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withScreen(t, a)
	a.secretsAt = filepath.Join(t.TempDir(), secrets.Name)
	// No agent, unless the test says otherwise. Reaching for the real
	// socket would answer out of whatever keys the person running the
	// tests happens to have loaded.
	a.agentHolds = func(string) (bool, error) {
		return false, errors.New("no SSH agent in a test")
	}
	return a, anEd25519KeyFile(t, filepath.Join(t.TempDir(), "id_ed25519_test"))
}

// startTheVault makes the vault and waits for it, the way the button
// does.
func startTheVault(t *testing.T, a *testApp, keyFile string) *secrets.Vault {
	t.Helper()
	var made *secrets.Vault
	var failed error
	done := false
	a.makeVault(keyFile, func(v *secrets.Vault, err error) {
		made, failed, done = v, err, true
	})
	waitFor(t, a, "the vault to be made", func() bool { return done })
	if failed != nil {
		t.Fatalf("make the vault: %v", failed)
	}
	return made
}

// noticeUp is the message of the dialog on screen.
func noticeUp(t *testing.T, a *testApp) string {
	t.Helper()
	n, up := a.root.Modal().(*ui.Notice)
	if !up {
		t.Fatalf("there is no notice up, but %T", a.root.Modal())
	}
	return n.Message()
}

// noticeTitleUp is the heading of the notice on top. The title is the
// message under WORDING.md, so most of what these tests check is there
// rather than in the body.
func noticeTitleUp(t *testing.T, a *testApp) string {
	t.Helper()
	n, up := a.root.Modal().(*ui.Notice)
	if !up {
		t.Fatalf("there is no notice up, but %T", a.root.Modal())
	}
	return n.Title
}

// A secret goes on the clipboard, and what the window says about it
// names the item and not the secret.
func TestASecretGoesOnTheClipboard(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	it, err := v.Put(secrets.Item{Name: "margit", User: "marcus"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := a.copySecret(v, it); err != nil {
		t.Fatalf("copy: %v", err)
	}

	// The clipboard is written on a goroutine of its own, so this waits
	// for it the way every other test that reads the clipboard does.
	waitFor(t, a, "the secret to reach the clipboard", func() bool {
		return a.copiedText() == "hunter2"
	})

	// A line along the bottom rather than a dialog: it worked, and the
	// only thing to read is which secret and for how long.
	said := a.saying()
	if !strings.Contains(said, "margit") {
		t.Errorf("the line %q does not name the item", said)
	}
	if strings.Contains(said, "hunter2") {
		t.Error("the line shows the secret")
	}
}

// Typing one into a pane never puts it on the clipboard.
func TestASecretTypedIntoAPaneSkipsTheClipboard(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := a.typeSecret(v, it); err != nil {
		t.Fatalf("type: %v", err)
	}
	if got := a.copiedText(); got != "" {
		t.Errorf("typing it put %q on the clipboard", got)
	}
}

// A secret from the vault answers what an agent asked for.
//
// The agent is told a line was answered and never sees the line. The
// return goes with it, because an ask wants a whole answer.
func TestASecretFromTheVaultAnswersAnAgentsAsk(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	pane := onlyPaneOn(t, a)

	answered, stop, err := pane.WaitForSecret("-- gridterm: an agent wants something --")
	if err != nil {
		t.Fatalf("wait for a secret: %v", err)
	}
	defer stop()
	if !pane.AskedForASecret() {
		t.Fatal("the pane is not waiting for one")
	}

	if err := a.typeSecret(v, it); err != nil {
		t.Fatalf("type: %v", err)
	}

	select {
	case ok := <-answered:
		if !ok {
			t.Error("the ask ended without an answer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the ask was never answered, so the return did not go with the secret")
	}
	if got := a.copiedText(); got != "" {
		t.Errorf("answering the ask put %q on the clipboard", got)
	}
}

// A secret typed at an ordinary prompt carries no return.
//
// Nothing is waiting for a whole line there, and a password sent with a
// return cannot be taken back.
func TestASecretAtAnOrdinaryPromptCarriesNoReturn(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	pane := onlyPaneOn(t, a)
	if pane.AskedForASecret() {
		t.Fatal("the pane is waiting for a secret and should not be")
	}
	was := a.shells[0].sentText()

	if err := a.typeSecret(v, it); err != nil {
		t.Fatalf("type: %v", err)
	}
	waitFor(t, a, "the secret to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), "hunter2")
	})

	sent := strings.TrimPrefix(a.shells[0].sentText(), was)
	if strings.ContainsAny(sent, "\r\n") {
		t.Errorf("a return went with the secret at an ordinary prompt: %q", sent)
	}
}

// Locking the keys locks the secrets with them.
//
// The vault is opened by one of those keys, so a locked window still
// holding the secrets open would be locked in name only.
func TestLockingTheKeysLocksTheSecrets(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if _, err := v.Put(secrets.Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if v.Locked() {
		t.Fatal("the vault is locked straight after being made")
	}

	if err := a.lockKeys(); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if !v.Locked() {
		t.Error("the keys locked and the secrets stayed open")
	}
	if _, err := v.Items(); err == nil {
		t.Error("a locked vault still lists what is in it")
	}
}

// A window holding the key already opens the vault without asking,
// which is the case once it has reached a server.
func TestAKeyAlreadyUnlockedOpensTheVault(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	v.Lock()

	if !a.openWithKeysInHand(v) {
		t.Fatal("the vault did not open on a key the window already had")
	}
	if v.Locked() {
		t.Error("it says it opened and it is still locked")
	}
}

// Only an ed25519 key is offered to lock a vault, because only its
// signature is the same every time.
func TestOnlyAnEd25519KeyIsOfferedToLockAVault(t *testing.T) {
	a, _ := aWindowWithSecrets(t)
	dir := t.TempDir()
	good := anEd25519KeyFile(t, filepath.Join(dir, "good"))

	// A real RSA key, because the public half is what the filter reads
	// and a made-up one would not parse.
	bad := filepath.Join(dir, "bad")
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("make an rsa key: %v", err)
	}
	if err := os.WriteFile(bad, []byte("the private half is never read here"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	rsaPub, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatalf("make the public half: %v", err)
	}
	if err := os.WriteFile(bad+".pub", ssh.MarshalAuthorizedKey(rsaPub), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	set, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	a.keyFiles = newKeyIndex()
	a.keyFiles.remember(set)
	for _, path := range []string{good, bad} {
		if err := a.keyFiles.keep(path); err != nil {
			t.Fatalf("keep %s: %v", path, err)
		}
	}

	got := a.vaultKeys()
	if slices.Contains(got, bad) {
		t.Errorf("an rsa key is offered to lock a vault: %v", got)
	}
	if !slices.Contains(got, good) {
		t.Errorf("the ed25519 key is not offered: %v", got)
	}
}

// A second key is added and opens the vault on its own, which is what
// a second machine needs.
func TestASecondKeyIsAddedAndOpensTheVault(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if _, err := v.Put(secrets.Item{Name: "margit"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	second := anEd25519KeyFile(t, filepath.Join(t.TempDir(), "id_ed25519_other"))

	a.addVaultKeyOn(v, second)
	waitFor(t, a, "the key to be added", func() bool { return len(v.Keys()) == 2 })

	// The vault on disk now opens on the second key alone.
	shut, err := secrets.Open(a.secretsAt)
	if err != nil {
		t.Fatalf("open the file again: %v", err)
	}
	signer := signerFromFile(t, second)
	if err := shut.Unlock([]ssh.Signer{signer}); err != nil {
		t.Fatalf("the second key did not open it: %v", err)
	}
	items, err := shut.Items()
	if err != nil || len(items) != 1 {
		t.Fatalf("it opened onto %d items (%v)", len(items), err)
	}
}

// A key that already opens the vault is not offered again.
func TestAKeyThatAlreadyOpensItIsNotOfferedAgain(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	if have := alreadyOpens(v); !have[keyFile] {
		t.Fatalf("the vault does not say %s opens it: %v", keyFile, have)
	}
	// Nothing else to offer, so the window says why rather than putting
	// up an empty list.
	if err := a.chooseAKeyToAdd(v); err != nil {
		t.Fatalf("choose: %v", err)
	}
	if said := noticeUp(t, a); !strings.Contains(said, "already open") {
		t.Errorf("it said %q", said)
	}
}

// signerFromFile reads a key file a test wrote.
func signerFromFile(t *testing.T, path string) ssh.Signer {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		t.Fatalf("parse the key: %v", err)
	}
	return signer
}

// The form for changing one starts on what is there, and with the
// secret field empty: a password is not put on screen to be edited.
func TestTheChangeFormStartsOnWhatIsThere(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit", User: "marcus"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := a.askToChange(v, it); err != nil {
		t.Fatalf("ask: %v", err)
	}
	f, ok := a.root.Modal().(*ui.Form)
	if !ok {
		t.Fatalf("it showed %T", a.root.Modal())
	}
	if got := f.Field("Name").Text(); got != "margit" {
		t.Errorf("the name field holds %q", got)
	}
	if got := f.Field("For").Text(); got != "marcus" {
		t.Errorf("the for field holds %q", got)
	}
	if got := f.Field("New secret").Text(); got != "" {
		t.Errorf("the secret is in the form to be edited: %q", got)
	}
}

// Changing the name leaves the secret where it is.
func TestChangingTheNameKeepsTheSecret(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit", User: "marcus"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := a.askToChange(v, it); err != nil {
		t.Fatalf("ask: %v", err)
	}
	f := a.root.Modal().(*ui.Form)
	f.Field("Name").SetText("margit.skalarit.net")
	pressButton(t, a, f, btnSave)

	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 || items[0].Name != "margit.skalarit.net" {
		t.Fatalf("the vault holds %v", items)
	}
	if items[0].ID != it.ID {
		t.Error("changing the name gave it a new id")
	}
	got, err := v.Secret(it.ID)
	if err != nil || got != "hunter2" {
		t.Errorf("the secret came back %q (%v), want it untouched", got, err)
	}
}

// Filling the secret field puts a new one in.
func TestFillingTheSecretFieldChangesIt(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := a.askToChange(v, it); err != nil {
		t.Fatalf("ask: %v", err)
	}
	f := a.root.Modal().(*ui.Form)
	f.Field("New secret").SetText("hunter3")
	pressButton(t, a, f, btnSave)

	got, err := v.Secret(it.ID)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if got != "hunter3" {
		t.Errorf("the secret came back %q, want the new one", got)
	}
}

// Escape closes the form that takes a secret.
//
// It showed with no way to close it but the Cancel button, because the
// form was pushed without being told what takes it away.
func TestEscapeClosesTheAddForm(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	if err := a.askForSecret(v, secrets.Password); err != nil {
		t.Fatalf("ask: %v", err)
	}
	if _, up := a.root.Modal().(*ui.Form); !up {
		t.Fatalf("the form did not open, %T is up", a.root.Modal())
	}

	sendKey(t, a, press(input.KeyEscape, 0))
	a.pump.run()

	if a.root.Modal() != nil {
		t.Errorf("Escape left %T up", a.root.Modal())
	}
}

// Escape closes the form that changes one as well.
func TestEscapeClosesTheChangeForm(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := a.askToChange(v, it); err != nil {
		t.Fatalf("ask: %v", err)
	}
	sendKey(t, a, press(input.KeyEscape, 0))
	a.pump.run()

	if a.root.Modal() != nil {
		t.Errorf("Escape left %T up", a.root.Modal())
	}
}

// The Show button turns the stars off and back on, so what was typed
// can be checked before it is kept.
func TestShowTurnsTheStarsOff(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	if err := a.askForSecret(v, secrets.Password); err != nil {
		t.Fatalf("ask: %v", err)
	}
	f := a.root.Modal().(*ui.Form)
	value := f.Field("Secret")
	if value.Mask == 0 {
		t.Fatal("a password field starts unmasked")
	}

	pressButton(t, a, f, "Show")
	if value.Mask != 0 {
		t.Error("Show left the stars on")
	}
	if a.root.Modal() != ui.Widget(f) {
		t.Fatal("Show closed the form")
	}
	// And the button now offers to put them back.
	pressButton(t, a, f, "Hide")
	if value.Mask == 0 {
		t.Error("Hide left the secret on screen")
	}
}

// A note has nothing to show: it is not starred to begin with.
func TestANoteHasNoShowButton(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	if err := a.askForSecret(v, secrets.Note); err != nil {
		t.Fatalf("ask: %v", err)
	}
	f := a.root.Modal().(*ui.Form)
	for _, b := range f.Buttons() {
		if b.Title == "Show" || b.Title == "Hide" {
			t.Errorf("a note's form offers %q", b.Title)
		}
	}
}

// Both forms say what happens to what is typed into them.
func TestTheFormsSayItIsSealed(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	for _, kind := range []secrets.Kind{secrets.Password, secrets.Note} {
		if err := a.askForSecret(v, kind); err != nil {
			t.Fatalf("ask for %s: %v", kind, err)
		}
		f := a.root.Modal().(*ui.Form)
		said := strings.Join(f.Lines, " ")
		if !strings.Contains(strings.ToLower(said), "sealed") ||
			!strings.Contains(strings.ToLower(said), "key") {
			t.Errorf("the %s form says %q, which does not say it is sealed and keyed", kind, said)
		}
		sendKey(t, a, press(input.KeyEscape, 0))
		a.pump.run()
	}
}

// Showing one puts it on screen as it was written.
func TestShowingASecretPutsItOnScreen(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "codes", Kind: secrets.Note}, "1234 5678")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := a.showSecret(v, it); err != nil {
		t.Fatalf("show: %v", err)
	}
	if said := noticeUp(t, a); !strings.Contains(said, "1234 5678") {
		t.Errorf("the dialog says %q, and not the secret", said)
	}
	// Nothing went near the clipboard: showing is not copying.
	if got := a.copiedText(); got != "" {
		t.Errorf("showing it copied %q", got)
	}
}

// A key is taken away and stops opening the vault.
func TestAKeyTakenAwayStopsOpeningTheVault(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	second := anEd25519KeyFile(t, filepath.Join(t.TempDir(), "id_ed25519_other"))
	a.addVaultKeyOn(v, second)
	waitFor(t, a, "the key to be added", func() bool { return len(v.Keys()) == 2 })

	if err := v.RemoveKey(secrets.Fingerprint(signerFromFile(t, second).PublicKey())); err != nil {
		t.Fatalf("remove: %v", err)
	}

	shut, err := secrets.Open(a.secretsAt)
	if err != nil {
		t.Fatalf("open the file again: %v", err)
	}
	if err := shut.Unlock([]ssh.Signer{signerFromFile(t, second)}); !errors.Is(err, secrets.ErrWrongKey) {
		t.Errorf("the key that was taken away gave %v, want ErrWrongKey", err)
	}
	if err := shut.Unlock([]ssh.Signer{signerFromFile(t, keyFile)}); err != nil {
		t.Errorf("the key that was kept no longer opens it: %v", err)
	}
}

// The only key cannot be taken away, and the window says why rather
// than putting up a list of one.
func TestTheOnlyKeyCannotBeTakenAway(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	if err := a.chooseAKeyToRemove(v); err != nil {
		t.Fatalf("choose: %v", err)
	}
	if said := noticeTitleUp(t, a); !strings.Contains(said, "Only one key") {
		t.Errorf("it said %q", said)
	}
	if len(v.Keys()) != 1 {
		t.Error("the key went anyway")
	}
}

// Taking away the last key on this machine is said plainly, because it
// shuts the vault here.
func TestTakingTheLastKeyHereAwayIsSaidPlainly(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	// A second key the vault knows about but that is not on this
	// machine, which is what a retired laptop looks like.
	gone := anEd25519KeyFile(t, filepath.Join(t.TempDir(), "id_ed25519_gone"))
	a.addVaultKeyOn(v, gone)
	waitFor(t, a, "the key to be added", func() bool { return len(v.Keys()) == 2 })
	if err := os.Remove(gone); err != nil {
		t.Fatalf("take the other key off this machine: %v", err)
	}

	here, away := slotFor(t, v, keyFile), slotFor(t, v, gone)
	if !lastKeyHere(v, here) {
		t.Error("the key on this machine is not seen as the last one here")
	}
	if !strings.Contains(whatRemovingCosts(v, here), "only key here") {
		t.Errorf("it does not say what removing it costs: %q", whatRemovingCosts(v, here))
	}
	// And taking away the one that is not here costs nothing to say.
	if strings.Contains(whatRemovingCosts(v, away), "only key here") {
		t.Errorf("it warns about a key that is not on this machine: %q", whatRemovingCosts(v, away))
	}
	if !strings.Contains(keyRowNote(here), "on this machine") {
		t.Errorf("the row does not say the key is here: %q", keyRowNote(here))
	}
	if strings.Contains(keyRowNote(away), "on this machine") {
		t.Errorf("the row says a missing key is here: %q", keyRowNote(away))
	}
}

// slotFor is the vault's slot for a key file.
func slotFor(t *testing.T, v *secrets.Vault, keyFile string) secrets.KeySlot {
	t.Helper()
	for _, s := range v.Keys() {
		if s.KeyFile == keyFile {
			return s
		}
	}
	t.Fatalf("the vault has no slot for %s", keyFile)
	return secrets.KeySlot{}
}

// Asking for the secrets with a key to lock them with offers to create
// them, on that key.
//
// The key has to be one the window knows about. Without that this took
// the branch for a machine with no key at all, and passed on a phrase
// the two messages happened to share.
func TestNoSecretsYetOffersToCreateThem(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	set, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	a.keyFiles = newKeyIndex()
	a.keyFiles.remember(set)
	if err := a.keyFiles.keep(keyFile); err != nil {
		t.Fatalf("keep the key: %v", err)
	}

	if err := a.openSecrets(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := noticeTitleUp(t, a); got != "No secrets yet" {
		t.Errorf("it is titled %q", got)
	}
	if said := noticeUp(t, a); !strings.Contains(said, keyFile) {
		t.Errorf("it said %q, want it to name the key that would open them", said)
	}
	n := a.root.Modal().(*ui.Notice)
	if n.Action.Title != btnCreate {
		t.Errorf("the button says %q, want it to offer to create them", n.Action.Title)
	}
}

// And with no key at all it says so, rather than offering to create
// secrets nothing could open.
func TestNoKeyToLockTheSecretsWithSaysSo(t *testing.T) {
	a, _ := aWindowWithSecrets(t)
	if err := a.openSecrets(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := noticeTitleUp(t, a); got != "No key to lock the secrets with" {
		t.Errorf("it is titled %q", got)
	}
	if said := noticeUp(t, a); !strings.Contains(said, makeKeyTitle) {
		t.Errorf("it said %q, want it to name the command that writes one", said)
	}
}

// A secret goes off the clipboard when the window closes, rather than
// waiting for a timer that will never fire.
//
// Taking it off after half a minute is done by a timer that posts work
// to the queue the drawing goroutine drains, and by the goroutine that
// owns the clipboard. Neither outlives the window. Closing gridterm
// inside that half minute left the password on the clipboard for good,
// which is the one thing the timer is there to prevent.
func TestClosingTheWindowTakesASecretOffTheClipboard(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := a.copySecret(v, it); err != nil {
		t.Fatalf("copy: %v", err)
	}
	waitFor(t, a, "the secret to reach the clipboard", func() bool {
		return a.copiedText() == "hunter2"
	})

	// The window closing, the way it really closes: the frame that sees
	// quit set is the last chance anything has to run.
	a.quit.Store(true)
	if _, err := a.Update(), error(nil); err != nil {
		t.Fatalf("update: %v", err)
	}

	if got := a.copiedText(); got != "" {
		t.Errorf("the clipboard still holds %q after the window closed", got)
	}
}

// And what the user copied since is theirs, on the way out as much as
// on the timer.
func TestClosingTheWindowLeavesWhatWasCopiedSince(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := a.copySecret(v, it); err != nil {
		t.Fatalf("copy: %v", err)
	}
	waitFor(t, a, "the secret to reach the clipboard", func() bool {
		return a.copiedText() == "hunter2"
	})

	// Something of their own, copied after the secret.
	a.clip.set("a line they copied themselves")
	waitFor(t, a, "their own copy to land", func() bool {
		return a.copiedText() == "a line they copied themselves"
	})

	a.quit.Store(true)
	if _, err := a.Update(), error(nil); err != nil {
		t.Fatalf("update: %v", err)
	}

	if got := a.copiedText(); got != "a line they copied themselves" {
		t.Errorf("the clipboard holds %q, and what they copied was theirs", got)
	}
}

// Adding a secret suggests the machine the user is looking at, because
// that is what a password being typed now is nearly always for.
func TestAddingASecretSuggestsTheMachineInFront(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	withPanel(t, a)
	startTheVault(t, a, keyFile)

	name, path := aReadableFile(t, "one")
	if err := a.openReader(vfs.NewLocal(), "kettle", path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	a.focus(onlyReader(t, a))

	if err := a.addSecret(); err != nil {
		t.Fatalf("add a secret: %v", err)
	}
	f := awaitModal(t, a, "the add dialog", byTitle[*ui.Form](addSecretTitle))
	if got := f.Field(fldFor).Text(); got != "kettle" {
		t.Errorf("the %s field holds %q, want the machine in front", fldFor, got)
	}
	// And it is a suggestion: the machines the window knows of are on
	// the list, so another one is a keystroke away.
	if !slices.Contains(f.Field(fldFor).Options, "kettle") {
		t.Errorf("the %s field offers %v, want the machines", fldFor, f.Field(fldFor).Options)
	}
}

// With nothing but a local pane in front there is no machine to suggest,
// and the field is left empty rather than filled in with this one.
func TestAddingASecretOnTheLocalMachineSuggestsNothing(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	startTheVault(t, a, keyFile)

	if err := a.addSecret(); err != nil {
		t.Fatalf("add a secret: %v", err)
	}
	f := awaitModal(t, a, "the add dialog", byTitle[*ui.Form](addSecretTitle))
	if got := f.Field(fldFor).Text(); got != "" {
		t.Errorf("the %s field holds %q, want nothing", fldFor, got)
	}
}

// Generate fills the secret in, leaves the dialog up so it can be
// looked at, and Save keeps what it made.
func TestGenerateFillsTheSecretAndLeavesTheDialogUp(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	if err := a.addSecret(); err != nil {
		t.Fatalf("add a secret: %v", err)
	}
	f := awaitModal(t, a, "the add dialog", byTitle[*ui.Form](addSecretTitle))
	typeIntoField(t, a, f, fldName, "margit")
	pressButton(t, a, f, btnGenerate)

	made := f.Field(fldSecret).Text()
	if len(made) != secrets.PasswordLength {
		t.Fatalf("it generated %d characters, want %d", len(made), secrets.PasswordLength)
	}
	if a.root.Modal() != ui.Widget(f) {
		t.Fatalf("the dialog went away; the modal on top is %T", a.root.Modal())
	}
	// Still masked. Reading it back is what Show is for, and pressing
	// Generate is not asking to have a password put on screen.
	if f.Field(fldSecret).Mask == 0 {
		t.Error("Generate took the stars off the field")
	}

	pressButton(t, a, f, btnSave)
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("%d items in the vault, want the one that was saved", len(items))
	}
	got, err := v.Secret(items[0].ID)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if got != made {
		t.Errorf("the vault holds %q, want the generated %q", got, made)
	}
}

// A note has nothing to generate, so the button is not there.
func TestANoteHasNoGenerateButton(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	startTheVault(t, a, keyFile)

	if err := a.addNote(); err != nil {
		t.Fatalf("add a note: %v", err)
	}
	f := awaitModal(t, a, "the add dialog", byTitle[*ui.Form](addNoteTitle))
	for _, b := range f.Buttons() {
		if b.Title == btnGenerate {
			t.Fatalf("the %s dialog offers %s", addNoteTitle, btnGenerate)
		}
	}
}

// Changing a secret generates a new one too, which is how a password is
// rolled over on a machine.
func TestChangingASecretGeneratesANewOne(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)

	it, err := v.Put(secrets.Item{Name: "margit", User: "kettle"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := a.askToChange(v, it); err != nil {
		t.Fatalf("change it: %v", err)
	}
	f := awaitModal(t, a, "the change dialog",
		byTitlePrefix[*ui.Form](changeSecretTitle))
	pressButton(t, a, f, btnGenerate)
	made := f.Field(fldNewSecret).Text()
	pressButton(t, a, f, btnSave)

	got, err := v.Secret(it.ID)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if got != made || got == "hunter2" {
		t.Errorf("the vault holds %q, want the generated %q", got, made)
	}
	// The machine it belongs to is untouched by a new password.
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if items[0].User != "kettle" {
		t.Errorf("it is now for %q, want the machine it was saved for", items[0].User)
	}
}

// Taking a secret back off the clipboard never puts a dialog up.
//
// It went through the paste path, which reports a clipboard holding a
// picture: copy a secret, copy a screenshot, and half a minute later a
// "Could not paste" dialog appeared that nobody asked for. The same on
// the way out, over a window that is closing.
func TestClearingTheClipboardNeverOpensADialog(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := a.copySecret(v, it); err != nil {
		t.Fatalf("copy: %v", err)
	}

	// A picture has been copied since, which is what the paste path
	// says something about.
	a.hasClipText = func() bool { return false }
	a.readClipImage = func() (image.Image, bool, error) {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), true, nil
	}

	if a.stillOnTheClipboard("hunter2") {
		t.Error("a clipboard holding a picture was taken for the secret")
	}
	a.takeAnySecretOffTheClipboard()
	if up := a.root.Modal(); up != nil {
		t.Errorf("a %T dialog went up on the way out", up)
	}
}

// A clipboard that will not be read is left alone, and says nothing.
func TestAClipboardThatWillNotBeReadIsLeftAlone(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	startTheVault(t, a, keyFile)
	a.secretCopied = "hunter2"
	a.hasClipText = func() bool { return true }
	a.readClip = func() (string, error) { return "", errors.New("the clipboard said no") }

	a.takeAnySecretOffTheClipboard()
	if up := a.root.Modal(); up != nil {
		t.Errorf("a %T dialog went up for a clipboard that would not be read", up)
	}
}

// Copying the same secret again gives it a fresh half minute, rather
// than the first copy's timer cutting the second one short.
func TestCopyingASecretAgainGivesItAFreshHalfMinute(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	it, err := v.Put(secrets.Item{Name: "margit"}, "hunter2")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	for range 2 {
		if err := a.copySecret(v, it); err != nil {
			t.Fatalf("copy: %v", err)
		}
	}
	waitFor(t, a, "the secret to reach the clipboard", func() bool {
		return a.copiedText() == "hunter2"
	})

	// The first copy's timer goes off. Its half minute is up, but the
	// second copy's is not, so the clipboard is not its to clear.
	a.forgetClipboardCopy("hunter2", 1)
	if got := a.copiedText(); got != "hunter2" {
		t.Errorf("the clipboard holds %q, want the secret the second copy put there", got)
	}
	if a.secretCopied != "hunter2" {
		t.Errorf("the window has forgotten it copied %q", "hunter2")
	}

	// And the second copy's timer does clear it.
	a.forgetClipboardCopy("hunter2", 2)
	waitFor(t, a, "the clipboard to be cleared", func() bool {
		return a.copiedText() == ""
	})
	if a.secretCopied != "" {
		t.Errorf("the window still thinks %q is on the clipboard", a.secretCopied)
	}
}
