package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
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
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withScreen(t, a)
	a.secretsAt = filepath.Join(t.TempDir(), secrets.Name)
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

	if got := a.copiedText(); got != "hunter2" {
		t.Errorf("the clipboard got %q, want the password", got)
	}
	said := noticeUp(t, a)
	if !strings.Contains(said, "margit") {
		t.Errorf("the notice %q does not name the item", said)
	}
	if strings.Contains(said, "hunter2") {
		t.Error("the notice shows the secret")
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
	pressButton(t, a, f, "Keep")

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
	pressButton(t, a, f, "Keep")

	got, err := v.Secret(it.ID)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if got != "hunter3" {
		t.Errorf("the secret came back %q, want the new one", got)
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
	if said := noticeUp(t, a); !strings.Contains(said, "cannot go") {
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

// Asking for the secrets when there are none offers to start them.
func TestNoSecretsYetOffersToStartThem(t *testing.T) {
	a, _ := aWindowWithSecrets(t)
	if err := a.openSecrets(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if said := noticeUp(t, a); !strings.Contains(said, "no secrets yet") {
		t.Errorf("it said %q", said)
	}
}
