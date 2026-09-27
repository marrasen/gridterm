package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/secrets"
)

func TestANewKeyIsWrittenAndSaysHowToInstallIt(t *testing.T) {
	a, _ := secretsApp(t)
	at := filepath.Join(t.TempDir(), "made_ed25519")
	a.handle(MakeKey{Path: at, Comment: "me@here", Passphrase: "typed one"})
	if _, err := os.Stat(at + ".pub"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(at)
	if _, err := ssh.ParseRawPrivateKeyWithPassphrase(raw, []byte("typed one")); err != nil {
		t.Fatalf("the key does not open with its passphrase: %v", err)
	}
	waitFor(t, a, "the steps", func() bool { return len(a.st.Asks) == 1 })
	if a.st.Asks[0].Yes != "Copy Public Key" {
		t.Fatalf("the steps offer %q", a.st.Asks[0].Yes)
	}
}

func TestANewKeysPassphraseCanBeKeptInTheSecrets(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	at := filepath.Join(t.TempDir(), "kept_ed25519")
	a.handle(MakeKey{Path: at, Generate: true})
	var pass string
	waitFor(t, a, "the key", func() bool {
		if _, err := os.Stat(at); err != nil {
			return false
		}
		p, err := a.secrets.PassphraseFor(at)
		pass = p
		return err == nil
	})
	raw, _ := os.ReadFile(at)
	if _, err := ssh.ParseRawPrivateKeyWithPassphrase(raw, []byte(pass)); err != nil {
		t.Fatalf("the key does not open with the kept passphrase: %v", err)
	}
	items, _ := a.secrets.Items()
	if len(items) != 1 || items[0].Kind != secrets.Passphrase || items[0].File != at {
		t.Fatalf("the secrets hold %+v", items)
	}
}

func TestTheSecretsSayWhenATerminalIsWaitingForOne(t *testing.T) {
	a, code := agentApp(t)
	c, sh := dial(t, a, code)
	pane := a.st.Panes[0].ID
	a.lastTerminal = pane
	go func() { _, _ = c.Secret(sh.Panes[0].ID, "the password", 5*time.Second) }()
	waitFor(t, a, "the ask", func() bool { return a.terminal(pane).AskedForASecret() })
	if got := a.waitingForSecret(); got != a.titleOf(pane) {
		t.Fatalf("waiting, the secrets name %q", got)
	}
	if got := secretsHeading(Secrets{Waiting: "Terminal 1"}); got != "Secrets — Terminal 1 is waiting for one" {
		t.Fatalf("the heading reads %q", got)
	}
}
