package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

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

// withOpenSecrets hands the vault to then, opening it first.
//
// It opens on a key the window has already unlocked, and asks for a
// passphrase only when it has none that fits. Asking means a dialog,
// so then may run on this frame or several frames later, and whatever
// it reports is shown rather than returned: by then there is nobody
// left to return it to.
func (a *app) withOpenSecrets(what string, then func(*secrets.Vault) error) error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	if !v.Exists() {
		return a.offerAVault()
	}
	if a.openWithKeysInHand(v) {
		return then(v)
	}
	a.unlockVault(v, func(err error) {
		switch {
		case errors.Is(err, errDismissed):
			// The user closed the passphrase dialog. They know.
		case err != nil:
			a.reportError(what, err)
		default:
			if err := then(v); err != nil {
				a.reportError(what, err)
			}
		}
	})
	return nil
}

// openSecrets shows what is in the vault.
func (a *app) openSecrets() error {
	return a.withOpenSecrets("Could not open the secrets", a.showSecrets)
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
	// The title says when picking one answers what the pane is waiting
	// for, because then the row does something more than copy a
	// password: it sends the line the program is sitting on.
	title := secretsTitle
	if pane := a.focusedTerminal(); pane != nil && pane.AskedForASecret() {
		title = "Secrets — the pane is waiting for one"
	}
	var hide func()
	c := ui.NewChooser(title, func() {
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
// The agent that asked never sees it either: what is typed goes to the
// program in the pane, and gridterm tells the agent only that a line
// was answered.
//
// A return goes with it when something is waiting for one, and not
// otherwise: an ask wants a whole answer, and a password sent into an
// ordinary prompt with a return cannot be taken back.
func (a *app) typeSecret(v *secrets.Vault, it secrets.Item) error {
	pane := a.focusedTerminal()
	if pane == nil {
		return errors.New("there is no pane to type it into")
	}
	value, err := v.Secret(it.ID)
	if err != nil {
		return err
	}
	if pane.AskedForASecret() {
		value += "\r"
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
	return a.withOpenSecrets("Could not keep it", func(v *secrets.Vault) error {
		return a.askForSecret(v, kind)
	})
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

// addVaultKey lets another key open the secrets, for a second machine.
//
// The key has to be on this machine: a slot is made by having the key
// sign, so the private half has to be here. Adding the key of a machine
// you are not sitting at means bringing that key here first.
func (a *app) addVaultKey() error {
	return a.withOpenSecrets("Could not add the key", a.chooseAKeyToAdd)
}

// alreadyOpens reports which key files already open the vault.
func alreadyOpens(v *secrets.Vault) map[string]bool {
	have := map[string]bool{}
	for _, s := range v.Keys() {
		if s.KeyFile != "" {
			have[s.KeyFile] = true
		}
	}
	return have
}

// chooseAKeyToAdd lists the keys that could be added and adds the one
// picked.
func (a *app) chooseAKeyToAdd(v *secrets.Vault) error {
	have := alreadyOpens(v)
	var spare []string
	for _, keyFile := range a.vaultKeys() {
		if !have[keyFile] {
			spare = append(spare, keyFile)
		}
	}
	if len(spare) == 0 {
		a.showNotice(secretsTitle, whyNoKeyToAdd(len(have)), false)
		return nil
	}
	var hide func()
	c := ui.NewChooser("Which key should also open the secrets?", func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	for _, keyFile := range spare {
		c.Add(keyFile, "", func() error {
			a.addVaultKeyOn(v, keyFile)
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

// whyNoKeyToAdd says why there is nothing to offer, which is a
// different thing depending on how many already open it.
func whyNoKeyToAdd(opening int) string {
	if opening == 0 {
		return "There is no other ed25519 key on this machine to add. " +
			`"Make an SSH key" writes one.`
	}
	return "Every ed25519 key this window knows about already opens the secrets. " +
		"A key from another machine has to be on this one before it can be added."
}

// addVaultKeyOn unlocks a key and gives it a slot of its own.
func (a *app) addVaultKeyOn(v *secrets.Vault, keyFile string) {
	a.unlockKeyFile(keyFile, func(signer ssh.Signer, err error) {
		if errors.Is(err, errDismissed) {
			return
		}
		if err == nil {
			err = v.AddKey(signer, keyFile)
		}
		if err != nil {
			a.reportError("Could not add the key", err)
			return
		}
		a.showNotice(secretsTitle, fmt.Sprintf(
			"%s opens the secrets as well now, and %d keys open them in all.",
			keyFile, len(v.Keys())), false)
	})
}

// removeVaultKey stops a key opening the secrets, for a machine that
// is gone or a key being replaced.
func (a *app) removeVaultKey() error {
	return a.withOpenSecrets("Could not take the key away", a.chooseAKeyToRemove)
}

// chooseAKeyToRemove lists the keys that open the vault and takes the
// one picked away.
func (a *app) chooseAKeyToRemove(v *secrets.Vault) error {
	keys := v.Keys()
	if len(keys) < 2 {
		a.showNotice(secretsTitle, "Only one key opens the secrets, and it cannot go: "+
			`nothing would open them again. "Let another key open the secrets" adds one first.`,
			false)
		return nil
	}
	var hide func()
	c := ui.NewChooser("Which key should stop opening the secrets?", func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	for _, s := range keys {
		c.Add(keyRowName(s), keyRowNote(s), func() error {
			if hide != nil {
				hide()
			}
			a.confirmRemoveKey(v, s)
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

// keyRowName is what a key's row says it is: where it was, or its
// fingerprint when the vault was never told where.
func keyRowName(s secrets.KeySlot) string {
	if s.KeyFile != "" {
		return s.KeyFile
	}
	return s.Fingerprint
}

// keyRowNote is the right-hand side of a key's row: whether the key is
// on this machine, and its fingerprint when the row does not already
// say it.
func keyRowNote(s secrets.KeySlot) string {
	var parts []string
	if onThisMachine(s) {
		parts = append(parts, "on this machine")
	}
	if s.KeyFile != "" {
		parts = append(parts, s.Fingerprint)
	}
	return strings.Join(parts, " · ")
}

// onThisMachine reports whether a slot's key file is here.
func onThisMachine(s secrets.KeySlot) bool {
	if s.KeyFile == "" {
		return false
	}
	_, err := os.Stat(s.KeyFile)
	return err == nil
}

// confirmRemoveKey asks before taking a key away, because a key that is
// gone cannot be put back without the key itself.
func (a *app) confirmRemoveKey(v *secrets.Vault, s secrets.KeySlot) {
	n := a.newNotice(secretsTitle, whatRemovingCosts(v, s))
	n.Action = ui.NoticeAction{Title: "Take it away", Do: func() {
		if err := v.RemoveKey(s.Fingerprint); err != nil {
			a.reportError("Could not take the key away", err)
			return
		}
		a.showNotice(secretsTitle, fmt.Sprintf(
			"%s no longer opens the secrets, and %d keys still do.",
			keyRowName(s), len(v.Keys())), false)
	}}
	a.presentNotice(n)
}

// whatRemovingCosts says what taking this key away means, which is a
// different thing when it is the only one here.
//
// The secrets stay open until the window locks them, so somebody who
// has just shut themselves out has a moment to put the key back.
func whatRemovingCosts(v *secrets.Vault, s secrets.KeySlot) string {
	said := keyRowName(s) + " would stop opening the secrets."
	if !lastKeyHere(v, s) {
		return said + " Another key on this machine still opens them."
	}
	return said + " It is the only key here that opens them, so this machine" +
		" would not open them again until one of the others is on it." +
		" They stay open until the keys are locked."
}

// lastKeyHere reports whether this is the only key of the vault's that
// is on this machine.
func lastKeyHere(v *secrets.Vault, s secrets.KeySlot) bool {
	for _, other := range v.Keys() {
		if other.Fingerprint != s.Fingerprint && onThisMachine(other) {
			return false
		}
	}
	return true
}

// forgetSecret takes one out of the vault.
func (a *app) forgetSecret() error {
	return a.withOpenSecrets("Could not forget it", a.chooseToForget)
}

// chooseToForget is the list a secret is taken out from.
func (a *app) chooseToForget(v *secrets.Vault) error {
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
