package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
)

// secretsPath is where the vault is kept: beside everything else the
// window remembers, so a copy of gridterm carrying its own files
// carries its secrets too.
func (a *app) secretsPath() (string, error) {
	if a.secretsAt != "" {
		return a.secretsAt, nil
	}
	dir, err := conf.Dir()
	if err != nil {
		return "", fmt.Errorf("find somewhere for the secrets: %w", err)
	}
	return filepath.Join(dir, secrets.Name), nil
}

// vault is the window's secrets, read off disk the first time anything
// asks for them.
//
// Reading it does not open it: what comes back is locked until a key
// that fits is offered.
func (a *app) vault() (*secrets.Vault, error) {
	if a.secrets != nil {
		return a.secrets, nil
	}
	path, err := a.secretsPath()
	if err != nil {
		return nil, err
	}
	v, err := secrets.Open(path)
	if err != nil {
		return nil, err
	}
	a.secrets = v
	return v, nil
}

// haveSecrets reports whether there is a vault to keep something in.
//
// Whether it is open does not matter: a dialog that offers to save
// something opens the vault when the user presses the button, the way
// every secrets command does. What it cannot do is offer to save into
// a vault that does not exist yet.
func (a *app) haveSecrets() bool {
	v, err := a.vault()
	return err == nil && v.Exists()
}

// openWithKeysInHand unlocks the vault with the keys the window has
// already unlocked, and says whether that was enough.
//
// No dialog and no disk: a window that has reached a server has the key
// in hand, so the vault opens for nothing.
func (a *app) openWithKeysInHand(v *secrets.Vault) bool {
	if !v.Locked() {
		return true
	}
	return v.Unlock(a.keys.Signers()) == nil
}

// savedPassphrase is the passphrase the vault holds for a key file, and
// false when it holds none.
//
// Called from a goroutine that is connecting, so the lookup itself is
// posted to the goroutine that draws: the vault is one of that
// goroutine's, and it is the one that reads the file the first time
// anything asks.
func (a *app) savedPassphrase(ctx context.Context, keyFile string) (string, bool) {
	answer := make(chan string, 1)
	a.pump.post(func() { answer <- a.passphraseInHand(keyFile) })
	select {
	case pass := <-answer:
		return pass, pass != ""
	case <-ctx.Done():
		return "", false
	}
}

// passphraseInHand is the same lookup on the drawing goroutine.
//
// Only out of a vault a key already unlocked opens. Asking for a
// passphrase to read a passphrase would be a dialog to spare a dialog,
// and the key being unlocked may be the vault's own: that one has to be
// typed, or nothing here would ever open.
//
// Empty for every reason there is -- no vault, a locked one, no
// passphrase saved for this key. None of them is something to report:
// what happens next either way is the dialog that asks.
func (a *app) passphraseInHand(keyFile string) string {
	v, err := a.vault()
	if err != nil || !v.Exists() {
		return ""
	}
	if !a.openWithKeysInHand(v) {
		return ""
	}
	pass, err := v.PassphraseFor(keyFile)
	if err != nil {
		return ""
	}
	return pass
}

// keyFileForVault is the key file to unlock to open this vault.
//
// The vault remembers where each of its keys was, so the window asks
// for the passphrase of the right one rather than asking which key the
// user meant.
//
// A key that is on this machine first, and only then one that is not.
// The slots are in the order they were added, and the first was added
// wherever the vault was made: on the second machine that is a path
// belonging to the first, and asking for its passphrase failed on
// opening the file. More than one slot is exactly the case that is for,
// so taking the first was wrong in the one place it mattered.
func keyFileForVault(v *secrets.Vault) (string, error) {
	first := ""
	for _, s := range v.Keys() {
		if s.KeyFile == "" {
			continue
		}
		if onThisMachine(s) {
			return s.KeyFile, nil
		}
		if first == "" {
			first = s.KeyFile
		}
	}
	if first != "" {
		// None of them is here. The path is still worth trying: it is
		// what the vault knows, and the error from reading it names the
		// file the user has to bring over.
		return first, nil
	}
	return "", errors.New(
		"the secrets do not say which key file opens them, so the key has to be unlocked by opening a server first")
}

// unlockVault unlocks the vault, asking for a passphrase if it has to.
//
// It blocks on a dialog, so it runs on a goroutine of its own and never
// on the one that draws. then is posted back to the drawing goroutine
// with whatever happened.
func (a *app) unlockVault(v *secrets.Vault, then func(error)) {
	keyFile, err := keyFileForVault(v)
	if err != nil {
		// No key of this vault's is on this machine. A passphrase is
		// the way back in when that happens, and asking for it is
		// better than telling the user to go and find a key.
		if v.TakesAPassphrase() {
			a.askForTheSecretsPassphrase(v, then)
			return
		}
		then(err)
		return
	}
	a.unlockKeyFile(keyFile, func(signer ssh.Signer, err error) {
		if err == nil {
			err = v.Unlock([]ssh.Signer{signer})
		}
		// Anything but the user shutting the box is a reason to fall
		// back, and there are two of them: the key file would not be
		// read, or it was read and is not the key the vault remembers.
		//
		// The second is the one that matters and the one this used to
		// miss. Losing a key and making another at the same path is
		// what ssh-keygen does and what this window's own New SSH Key
		// does, so the commonest way to be locked out arrives here
		// with the file opening perfectly and Unlock saying no. That
		// is exactly what a passphrase is for.
		if err != nil && !errors.Is(err, errDismissed) && v.TakesAPassphrase() {
			a.askForTheSecretsPassphrase(v, then)
			return
		}
		then(err)
	})
}

// unlockKeyFile reads a key and keeps it in the ring, asking for its
// passphrase if it has one.
//
// It blocks on a dialog, so it runs on a goroutine of its own. then is
// posted back to the drawing goroutine.
func (a *app) unlockKeyFile(keyFile string, then func(ssh.Signer, error)) {
	a.closes.inBackground(func() error {
		signer, err := a.keys.Unlock(context.Background(), keyFile, &askUser{app: a})
		a.pump.post(func() { then(signer, err) })
		return nil
	})
}

// vaultKeys are the key files a vault could be started on: the ones the
// window knows about that are on disk, with anything the public half
// says is not ed25519 left out.
//
// Only an ed25519 key signs the same way every time, and only a key
// that does can hold a vault open. Leaving the rest out here is better
// than asking for a passphrase and refusing the key afterwards.
func (a *app) vaultKeys() []string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		if pub, err := os.ReadFile(path + ".pub"); err == nil {
			key, _, _, _, err := ssh.ParseAuthorizedKey(pub)
			if err == nil && key.Type() != ssh.KeyAlgoED25519 {
				return
			}
		}
		seen[path] = true
		out = append(out, path)
	}
	if mine, err := remote.DefaultKeyPath(); err == nil {
		add(mine)
	}
	if a.keyFiles != nil {
		for _, path := range a.keyFiles.all() {
			add(path)
		}
	}
	return out
}

// makeVault creates the vault on a key, unlocking that key first.
//
// The key is one the user already unlocks to reach a server, so the
// vault opens with everything else rather than asking for anything of
// its own.
func (a *app) makeVault(keyFile string, then func(*secrets.Vault, error)) {
	path, err := a.secretsPath()
	if err != nil {
		then(nil, err)
		return
	}
	a.closes.inBackground(func() error {
		ctx := context.Background()
		signer, err := a.keys.Unlock(ctx, keyFile, &askUser{app: a})
		var v *secrets.Vault
		if err == nil {
			v, err = secrets.Create(path, signer, keyFile)
		}
		a.pump.post(func() {
			if err == nil {
				a.secrets = v
			}
			then(v, err)
		})
		return nil
	})
}

// lockSecrets shuts the vault. What is on disk stays.
func (a *app) lockSecrets() {
	if a.secrets != nil {
		a.secrets.Lock()
	}
}
