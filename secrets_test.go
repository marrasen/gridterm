package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
