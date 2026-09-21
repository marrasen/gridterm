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

// keyFileForVault is the key file to unlock to open this vault.
//
// The vault remembers where each of its keys was, so the window asks
// for the passphrase of the right one rather than asking which key the
// user meant.
func keyFileForVault(v *secrets.Vault) (string, error) {
	for _, s := range v.Keys() {
		if s.KeyFile != "" {
			return s.KeyFile, nil
		}
	}
	return "", errors.New(
		"the vault does not say which key file opens it, so the key has to be unlocked by opening a server first")
}

// unlockVault unlocks the vault, asking for a passphrase if it has to.
//
// It blocks on a dialog, so it runs on a goroutine of its own and never
// on the one that draws. then is posted back to the drawing goroutine
// with whatever happened.
func (a *app) unlockVault(v *secrets.Vault, then func(error)) {
	keyFile, err := keyFileForVault(v)
	if err != nil {
		then(err)
		return
	}
	a.closes.inBackground(func() error {
		ctx := context.Background()
		signer, err := a.keys.Unlock(ctx, keyFile, &askUser{app: a})
		a.pump.post(func() {
			if err != nil {
				then(err)
				return
			}
			then(v.Unlock([]ssh.Signer{signer}))
		})
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
