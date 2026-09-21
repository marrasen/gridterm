package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// secretsTitle is what the list of secrets calls itself.
const secretsTitle = "Secrets"

// clipboardHolds is how long a secret stays on the clipboard before the
// window takes it back off.
//
// Long enough to paste it somewhere, short enough that it is not still
// there at the end of the afternoon. Only the secret is taken back: a
// clipboard somebody has used since is left alone.
const clipboardHolds = 30 * time.Second

// typeButton is the mark at the end of each row, which types the secret
// into the pane instead of putting it on the clipboard.
const typeButton = '↵'

// openSecrets shows what is in the vault.
//
// The vault opens on a key the window has already unlocked, and asks
// for a passphrase only when it has none that fits.
func (a *app) openSecrets() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	if !v.Exists() {
		return a.offerAVault()
	}
	if a.openWithKeysInHand(v) {
		return a.showSecrets(v)
	}
	a.unlockVault(v, func(err error) {
		switch {
		case errors.Is(err, errDismissed):
			// The user closed the passphrase dialog. They know.
		case err != nil:
			a.reportError("Could not open the secrets", err)
		default:
			if err := a.showSecrets(v); err != nil {
				a.reportError("Could not open the secrets", err)
			}
		}
	})
	return nil
}

// offerAVault asks whether to start one, since there is none.
//
// On the key that opens it, which is one of the keys already used to
// reach a server. With more than one to choose from the choice is the
// user's: it is the only key the vault will have until another is
// added, and a vault whose key is gone is a vault nobody opens.
func (a *app) offerAVault() error {
	keys := a.vaultKeys()
	if len(keys) == 0 {
		a.showNotice(secretsTitle, "There are no secrets yet, and no ed25519 key to lock them "+
			`with. "Make an SSH key" writes one, and that key then opens both a server and these.`,
			false)
		return nil
	}
	if len(keys) == 1 {
		n := a.newNotice(secretsTitle, "There are no secrets yet. "+keys[0]+
			" would open them, and it is a key you already unlock to reach a server.")
		n.Action = ui.NoticeAction{Title: "Start one", Do: func() { a.startVaultOn(keys[0]) }}
		a.presentNotice(n)
		return nil
	}
	var hide func()
	c := ui.NewChooser("Which key opens the secrets?", func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	for _, keyFile := range keys {
		c.Add(keyFile, "", func() error {
			a.startVaultOn(keyFile)
			return nil
		})
	}
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("there is no room to show them")
	}
	a.markDirty()
	return nil
}

// startVaultOn makes the vault on a key and shows what is in it, which
// is nothing yet.
func (a *app) startVaultOn(keyFile string) {
	a.makeVault(keyFile, func(v *secrets.Vault, err error) {
		switch {
		case errors.Is(err, errDismissed):
			// The user closed the passphrase dialog. They know.
		case err != nil:
			a.reportError("Could not start the secrets", err)
		default:
			a.showNotice(secretsTitle, `The secrets are started, and `+keyFile+
				` opens them. "Add a secret" puts the first one in.`, false)
		}
	})
}

// showSecrets puts the list up: the name of each, and none of them.
func (a *app) showSecrets(v *secrets.Vault) error {
	items, err := v.Items()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		a.showNotice(secretsTitle,
			`Nothing is kept yet. "Add a secret" puts the first one in.`, false)
		return nil
	}
	var hide func()
	c := ui.NewChooser(secretsTitle, func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	c.Button = typeButton
	c.OnPress = func(i int) error {
		if i < 0 || i >= len(items) {
			return nil
		}
		return a.typeSecret(v, items[i])
	}
	for _, it := range items {
		c.Add(it.Name, secretNote(it), func() error { return a.copySecret(v, it) })
	}
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("there is no room to show them")
	}
	a.markDirty()
	return nil
}

// secretNote is the right-hand side of a row: who it is for, and what
// kind of thing it is when that is not a password.
func secretNote(it secrets.Item) string {
	var parts []string
	if it.User != "" {
		parts = append(parts, it.User)
	}
	if it.Kind != "" && it.Kind != secrets.Password {
		parts = append(parts, string(it.Kind))
	}
	return strings.Join(parts, " · ")
}

// copySecret puts one on the clipboard and says so without showing it.
func (a *app) copySecret(v *secrets.Vault, it secrets.Item) error {
	value, err := v.Secret(it.ID)
	if err != nil {
		return err
	}
	a.clip.set(value)
	a.forgetClipboardLater(value)
	a.showNotice(secretsTitle, fmt.Sprintf(
		"%s is on the clipboard, and comes off it in %d seconds.",
		it.Name, int(clipboardHolds.Seconds())), false)
	return nil
}

// typeSecret puts one into the pane in front, for a program sitting at
// a prompt waiting for it.
//
// Typed rather than copied, so it never reaches the clipboard at all.
// No newline: what the secret is for decides whether it is a whole
// answer, and a password sent with a return cannot be taken back.
func (a *app) typeSecret(v *secrets.Vault, it secrets.Item) error {
	pane := a.focusedTerminal()
	if pane == nil {
		return errors.New("there is no pane to type it into")
	}
	value, err := v.Secret(it.ID)
	if err != nil {
		return err
	}
	pane.Paste(value)
	return nil
}

// forgetClipboardLater takes a secret back off the clipboard once its
// time is up, and leaves alone a clipboard that holds something else by
// then.
func (a *app) forgetClipboardLater(value string) {
	time.AfterFunc(clipboardHolds, func() {
		// Through the pump, because reading the clipboard and taking a
		// secret off it belong to the goroutine that draws.
		a.pump.post(func() {
			if a.pasteText() != value {
				// They have copied something since. It is theirs.
				return
			}
			a.clip.clear()
		})
	})
}

// addSecret asks for a password and keeps it.
func (a *app) addSecret() error { return a.addOfKind(secrets.Password) }

// addNote asks for a note and keeps it.
//
// Shown as it is typed, because a note is a licence or a recovery code
// read off the screen as often as it is pasted, and a row of stars
// helps nobody check it.
func (a *app) addNote() error { return a.addOfKind(secrets.Note) }

// addOfKind opens the vault if it has to and then asks.
func (a *app) addOfKind(kind secrets.Kind) error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	if !v.Exists() {
		return a.offerAVault()
	}
	if !a.openWithKeysInHand(v) {
		a.unlockVault(v, func(err error) {
			switch {
			case errors.Is(err, errDismissed):
			case err != nil:
				a.reportError("Could not open the secrets", err)
			default:
				if err := a.askForSecret(v, kind); err != nil {
					a.reportError("Could not keep it", err)
				}
			}
		})
		return nil
	}
	return a.askForSecret(v, kind)
}

// askForSecret is the form a new one is typed into.
func (a *app) askForSecret(v *secrets.Vault, kind secrets.Kind) error {
	title, label, mask := "Add a secret", "Secret", '*'
	if kind == secrets.Note {
		title, label, mask = "Add a note", "Note", rune(0)
	}
	f := a.newForm(title)
	name := f.AddField("Name", a.newField("what to call it", 0))
	user := f.AddField("For", a.newField("optional", 0))
	value := f.AddField(label, a.newField("", mask))
	var hide func()
	f.AddButton(ui.Button{Title: "Keep", Do: func() error {
		it := secrets.Item{Name: name.Text(), User: user.Text(), Kind: kind}
		if _, err := v.Put(it, value.Text()); err != nil {
			return err
		}
		if hide != nil {
			hide()
		}
		a.showNotice(secretsTitle, name.Text()+" is kept.", false)
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel", Do: func() error {
		if hide != nil {
			hide()
		}
		return nil
	}})
	hide = a.showModal(f, nil)
	if a.root.Modal() != ui.Widget(f) {
		return errors.New("there is no room for the form")
	}
	a.markDirty()
	return nil
}

// forgetSecret takes one out of the vault.
func (a *app) forgetSecret() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	if !v.Exists() || !a.openWithKeysInHand(v) {
		return a.openSecrets()
	}
	items, err := v.Items()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		a.showNotice(secretsTitle, "There is nothing to forget.", false)
		return nil
	}
	var hide func()
	c := ui.NewChooser("Forget a secret", func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	for _, it := range items {
		c.Add(it.Name, secretNote(it), func() error {
			if err := v.Remove(it.ID); err != nil {
				return err
			}
			a.showNotice(secretsTitle, it.Name+" is forgotten.", false)
			return nil
		})
	}
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("there is no room to show them")
	}
	a.markDirty()
	return nil
}
