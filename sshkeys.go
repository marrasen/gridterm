package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/serve"
)

// Making SSH keys, and locking them, as gridterm does. A new key is an
// ed25519 pair, with a passphrase typed or, when the secrets are there,
// one made up and kept in them, which unlocks it from then on without
// asking.

// Intents for SSH keys.
type (
	// MakeKey writes a new key pair at Path. Generate makes up its
	// passphrase and keeps it in the secrets; otherwise Passphrase is
	// used, and may be empty.
	MakeKey struct {
		Path, Comment, Passphrase string
		Generate                  bool
	}
	// LockKeys forgets every key unlocked, and locks the secrets.
	LockKeys struct{}
)

// mostKeptKeys is how many key files are remembered.
const mostKeptKeys = 20

// makeKey writes a new key pair.
func (a *app) makeKey(in MakeKey) error {
	at, err := expandHome(in.Path)
	if err != nil {
		return err
	}
	if !in.Generate {
		key, err := remote.MakeKey(at, strings.TrimSpace(in.Comment), in.Passphrase)
		if err != nil {
			return err
		}
		a.keyWritten(key, false)
		return nil
	}
	a.withSecrets("Couldn't create the key", func(v *secrets.Vault) error {
		switch _, err := os.Lstat(at); {
		case err == nil:
			return fmt.Errorf("there is already a key at %s", at)
		case !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("look at %s: %w", at, err)
		}
		pass, err := secrets.NewPassword(secrets.PassphraseLength)
		if err != nil {
			return err
		}
		// A passphrase kept for a key that was at this path before is
		// for a key that is gone; it stays, but no longer claims the file.
		items, _ := v.Items()
		for _, it := range items {
			if it.Kind == secrets.Passphrase && it.File == at {
				it.File = ""
				if _, err := v.PutDetails(it); err != nil {
					return err
				}
			}
		}
		it, err := v.Put(secrets.Item{Name: filepath.Base(at), Kind: secrets.Passphrase, File: at}, pass)
		if err != nil {
			return err
		}
		key, err := remote.MakeKey(at, strings.TrimSpace(in.Comment), pass)
		if err != nil {
			return errors.Join(err, v.Remove(it.ID))
		}
		a.keyWritten(key, true)
		return nil
	})
	return nil
}

// keyWritten keeps a new key's file in the list, and says how to
// install it.
func (a *app) keyWritten(key remote.NewKey, savedPassphrase bool) {
	if a.settings != nil {
		if err := a.settings.KeepKey(key.Path, mostKeptKeys); err != nil {
			a.notify("Key created, but not added to the list", err.Error(), "")
		}
	}
	var b strings.Builder
	b.WriteString("Private key: " + key.Path + "\nPublic key: " + key.Pub + "\n\n")
	if savedPassphrase {
		b.WriteString("The passphrase is kept in the secrets.\n\n")
	}
	b.WriteString("To install it on a server, run ssh-copy-id -i " + key.Pub + " user@host, or add its public key line to ~/.ssh/authorized_keys there.\n\n")
	b.WriteString(`For OpenSSH on Windows, add it to %USERPROFILE%\.ssh\authorized_keys, unless that account is an administrator, and then only to %ProgramData%\ssh\administrators_authorized_keys. Either file has to be readable by its owner alone, or sshd ignores it and says nothing about why.`)
	if at, err := serve.AuthorizedKeysPath(); err == nil {
		b.WriteString("\n\nFor a gridterm window serving from this machine, add it to " + at + ".")
	}
	line := key.Line
	go func() {
		ans, err := a.ask(a.ctx, Ask{Title: "SSH key created", Text: b.String(), Yes: "Copy Public Key", No: "Close"})
		if err == nil && ans.Yes {
			a.events <- func() { a.notify("Public key copied", key.Pub, line) }
		}
	}()
}

// lockKeys forgets every key unlocked, and locks the secrets.
func (a *app) lockKeys() {
	a.ring.Lock()
	a.lockSecrets()
	a.notify("SSH keys locked", "Each asks for its passphrase again when next used.", "")
}
