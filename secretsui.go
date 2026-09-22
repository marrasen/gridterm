package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// secretsTitle is what the vault calls itself, and the rest are what its
// commands are called.
//
// Constants because the dialogs quote them: a notice telling the user to
// take "Add Secret" has to name the line they will actually find, and a
// copy of that name in prose drifts the first time the command is
// reworded. See WORDING.md.
const (
	secretsTitle          = "Secrets"
	showSecretsTitle      = "Show Secrets"
	addSecretTitle        = "Add Secret"
	addNoteTitle          = "Add Note"
	changeSecretTitle     = "Change Secret"
	addSecretsKeyTitle    = "Add Secrets Key"
	removeSecretsKeyTitle = "Remove Secrets Key"
	removeSecretTitle     = "Remove Secret"
)

// clipboardHolds is how long a secret stays on the clipboard before the
// window takes it back off.
//
// Long enough to paste it somewhere, short enough that it is not still
// there at the end of the afternoon. Only the secret is taken back: a
// clipboard somebody has used since is left alone.
const clipboardHolds = 30 * time.Second

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
		n := a.newNotice("No key to lock the secrets with",
			`An ed25519 key is needed. Take "`+makeKeyTitle+`" to create one.`)
		n.SetNoCopy()
		a.presentNotice(n)
		return nil
	}
	if len(keys) == 1 {
		n := a.newNotice("No secrets yet", keys[0]+" will open them.")
		n.Action = ui.NoticeAction{Title: btnCreate, Do: func() { a.startVaultOn(keys[0]) }}
		n.FocusOK()
		a.presentNotice(n)
		return nil
	}
	var hide func()
	c := ui.NewChooser("Choose a key", func() {
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
		return errors.New("Window too small")
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
			a.reportError("Could not create the secrets", err)
		default:
			a.say("Secrets created — " + keyFile + " opens them")
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
		n := a.newNotice("No secrets yet", `Take "`+addSecretTitle+`" to add one.`)
		n.SetNoCopy()
		a.presentNotice(n)
		return nil
	}
	// The title says when picking one answers what the pane is waiting
	// for, because then the row does something more than copy a
	// password: it sends the line the program is sitting on.
	title := secretsTitle
	if pane := a.focusedTerminal(); pane != nil && pane.AskedForASecret() {
		title = secretsTitle + " — the pane is waiting for one"
	}
	var hide func()
	c := ui.NewChooser(title, func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	// A filter row and a bar of buttons: up and down pick the secret,
	// left and right pick what to do with it, and Enter does it.
	c.Filter = true
	c.Actions = []ui.ChooserAction{
		{Label: "Type", Do: a.onPicked(v, items, a.typeSecret)},
		{Label: "Copy", Do: a.onPicked(v, items, a.copySecret)},
		{Label: "Show", Do: a.onPicked(v, items, a.showSecret)},
		{Label: "Cancel", Do: func(int) error { return nil }},
	}
	for _, it := range items {
		c.Add(it.Name, secretNote(it), func() error { return a.copySecret(v, it) })
	}
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("Window too small")
	}
	a.markDirty()
	return nil
}

// onPicked turns a thing done to one secret into a button's Do, which
// is handed the line the bar was on.
func (a *app) onPicked(v *secrets.Vault, items []secrets.Item,
	do func(*secrets.Vault, secrets.Item) error) func(int) error {

	return func(i int) error {
		if i < 0 || i >= len(items) {
			return nil
		}
		return do(v, items[i])
	}
}

// showSecret puts one on screen, for reading off rather than pasting.
//
// Behind a dialog the user asked for, because the one thing this vault
// is for is not showing them by accident. It is theirs to ask for and
// theirs to dismiss.
func (a *app) showSecret(v *secrets.Vault, it secrets.Item) error {
	value, err := v.Secret(it.ID)
	if err != nil {
		return err
	}
	if value == "" {
		n := a.newNotice(it.Name, "Nothing is saved under this name.")
		n.SetNoCopy()
		a.presentNotice(n)
		return nil
	}
	n := a.newNotice(it.Name, value)
	// Kept as it was written: a recovery code in columns is read wrong
	// if the words are rewrapped to fit.
	n.Preformatted = true
	a.presentNotice(n)
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
	a.say(fmt.Sprintf("%s copied — the clipboard clears in %d seconds",
		it.Name, int(clipboardHolds.Seconds())))
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
	a.secretCopied = value
	time.AfterFunc(clipboardHolds, func() {
		// Through the pump, because reading the clipboard and taking a
		// secret off it belong to the goroutine that draws.
		a.pump.post(func() {
			if a.secretCopied == value {
				a.secretCopied = ""
			}
			if a.pasteText() != value {
				// They have copied something since. It is theirs.
				return
			}
			a.clip.clear()
		})
	})
}

// takeAnySecretOffTheClipboard is the same thing on the way out, done
// where the window is closing rather than half a minute later.
//
// The timer that would have done it posts work to a queue that stops
// being drained, and the goroutine that writes the clipboard stops with
// the window, so both have to be gone around. Whatever the user copied
// since is left alone, the way the timer leaves it.
func (a *app) takeAnySecretOffTheClipboard() {
	value := a.secretCopied
	a.secretCopied = ""
	if value == "" || a.pasteText() != value {
		return
	}
	if err := a.clip.clearNow(); err != nil {
		a.logError(fmt.Errorf("taking a secret off the clipboard on the way out: %w", err))
	}
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
	return a.withOpenSecrets("Could not save the secret", func(v *secrets.Vault) error {
		return a.askForSecret(v, kind)
	})
}

// askForSecret is the form a new one is typed into.
func (a *app) askForSecret(v *secrets.Vault, kind secrets.Kind) error {
	title, label, mask := addSecretTitle, "Secret", '*'
	if kind == secrets.Note {
		title, label, mask = addNoteTitle, "Note", rune(0)
	}
	f := a.newForm(title)
	f.Lines = []string{keptSealed}
	name := f.AddField(fldName, a.newField("", 0))
	user := f.AddField(fldFor, a.newField("Optional", 0))
	user.Hint = "Who or what the secret is for"
	value := f.AddField(label, a.newField("", mask))
	// Said once the form has gone, so the message is not pushed over a
	// dialog that is about to be taken away underneath it.
	kept := ""
	f.AddButton(ui.Button{Title: btnSave, Do: func() error {
		it := secrets.Item{Name: name.Text(), User: user.Text(), Kind: kind}
		if _, err := v.Put(it, value.Text()); err != nil {
			return err
		}
		kept = it.Name
		return nil
	}})
	a.addShowButton(f, value, mask)
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, func() {
		if kept != "" {
			a.say(kept + " saved")
		}
	})
	if a.root.Modal() != ui.Widget(f) {
		return errors.New("Window too small")
	}
	a.markDirty()
	return nil
}

// keptSealed is the line at the top of a form that takes a secret.
//
// Somebody typing a password into a window is owed a word about where
// it goes, and a note is as worth sealing as a password. It says the
// two things that matter: it is sealed, and one key opens it.
const keptSealed = "Sealed in the vault. Only your key opens it."

// showTitle and hideTitle are what the button that turns the stars off
// says, and what finds it again to rename.
const (
	showTitle = "Show"
	hideTitle = "Hide"
)

// addShowButton puts a button on a form that turns the stars off, for
// checking what was typed before keeping it.
//
// Only on a field that is masked to begin with: a note is shown as it
// is typed and has nothing to turn off.
func (a *app) addShowButton(f *ui.Form, value *ui.Field, mask rune) {
	if mask == 0 {
		return
	}
	f.AddButton(ui.Button{Title: showTitle, Keep: true, Do: func() error {
		if value.Mask == 0 {
			value.Mask = mask
		} else {
			value.Mask = 0
		}
		retitleShow(f, value.Mask != 0)
		a.markDirty()
		return nil
	}})
}

// retitleShow renames the button in place, so it says what the next
// press does rather than what the last one did.
func retitleShow(f *ui.Form, masked bool) {
	title := hideTitle
	if masked {
		title = showTitle
	}
	buttons := slices.Clone(f.Buttons())
	for i, b := range buttons {
		if b.Title == showTitle || b.Title == hideTitle {
			buttons[i].Title = title
			// The same number of buttons, so the focus stays on this one.
			f.SetButtons(buttons)
			return
		}
	}
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
		n := a.newNotice("No key to add", whyNoKeyToAdd(len(have)))
		n.SetNoCopy()
		a.presentNotice(n)
		return nil
	}
	var hide func()
	c := ui.NewChooser("Choose a key", func() {
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
		return errors.New("Window too small")
	}
	a.markDirty()
	return nil
}

// whyNoKeyToAdd says why there is nothing to offer, which is a
// different thing depending on how many already open it.
func whyNoKeyToAdd(opening int) string {
	if opening == 0 {
		return `No other ed25519 key is on this machine. Take "` + makeKeyTitle +
			`" to create one.`
	}
	return "Every ed25519 key this window knows of already opens the secrets." +
		" A key from another machine has to be on this one first."
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
		a.say(fmt.Sprintf("%s opens the secrets — %d keys do now",
			keyFile, len(v.Keys())))
	})
}

// changeSecret puts a better name on one, or a new value in it.
func (a *app) changeSecret() error {
	return a.withOpenSecrets("Could not change the secret", a.chooseToChange)
}

// chooseToChange is the list a secret is picked from to change.
func (a *app) chooseToChange(v *secrets.Vault) error {
	items, err := v.Items()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		a.showNotice(secretsTitle, "There is nothing to change yet.", false)
		return nil
	}
	var hide func()
	c := ui.NewChooser(changeSecretTitle, func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	for _, it := range items {
		c.Add(it.Name, secretNote(it), func() error {
			if hide != nil {
				hide()
			}
			return a.askToChange(v, it)
		})
	}
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("Window too small")
	}
	a.markDirty()
	return nil
}

// askToChange is the form an existing one is changed in.
//
// The value starts empty and empty means keep it. The secret is not
// put in the field to be edited: it would be a password sitting on
// screen behind a row of stars, and nothing here needs it. Leaving it
// empty means changing a name never reads the secret at all.
func (a *app) askToChange(v *secrets.Vault, it secrets.Item) error {
	label, mask := "New secret", '*'
	if it.Kind == secrets.Note {
		label, mask = "New note", rune(0)
	}
	f := a.newForm(changeSecretTitle + " — " + it.Name)
	name := f.AddField(fldName, a.newField(it.Name, 0))
	name.SetText(it.Name)
	user := f.AddField(fldFor, a.newField("Optional", 0))
	user.SetText(it.User)
	value := f.AddField(label, a.newField("Optional", 0))
	value.Mask = mask
	value.Hint = "Leave empty to keep the saved one"

	changedTo := ""
	f.AddButton(ui.Button{Title: btnSave, Do: func() error {
		changed := it
		changed.Name, changed.User = name.Text(), user.Text()
		var err error
		if value.Text() == "" {
			_, err = v.PutDetails(changed)
		} else {
			_, err = v.Put(changed, value.Text())
		}
		if err != nil {
			return err
		}
		changedTo = changed.Name
		return nil
	}})
	a.addShowButton(f, value, mask)
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, func() {
		if changedTo != "" {
			a.say(changedTo + " changed")
		}
	})
	if a.root.Modal() != ui.Widget(f) {
		return errors.New("Window too small")
	}
	a.markDirty()
	return nil
}

// removeVaultKey stops a key opening the secrets, for a machine that
// is gone or a key being replaced.
func (a *app) removeVaultKey() error {
	return a.withOpenSecrets("Could not remove the key", a.chooseAKeyToRemove)
}

// chooseAKeyToRemove lists the keys that open the vault and takes the
// one picked away.
func (a *app) chooseAKeyToRemove(v *secrets.Vault) error {
	keys := v.Keys()
	if len(keys) < 2 {
		n := a.newNotice("Only one key opens the secrets",
			`Removing it would leave nothing that can. Take "`+addSecretsKeyTitle+
				`" to add another first.`)
		n.SetNoCopy()
		a.presentNotice(n)
		return nil
	}
	var hide func()
	c := ui.NewChooser("Choose a key", func() {
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
		return errors.New("Window too small")
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
	n := a.newNotice("Remove "+keyRowName(s)+"?", whatRemovingCosts(v, s))
	n.Action = ui.NoticeAction{Title: btnRemove, Do: func() {
		if err := v.RemoveKey(s.Fingerprint); err != nil {
			a.reportError("Could not remove the key", err)
			return
		}
		a.say(fmt.Sprintf("%s removed — %d keys still open the secrets",
			keyRowName(s), len(v.Keys())))
	}}
	// Opens on OK, which changes nothing: a key that is gone cannot be
	// put back without the key itself.
	n.FocusOK()
	a.presentNotice(n)
}

// whatRemovingCosts says what taking this key away means, which is a
// different thing when it is the only one here.
//
// The secrets stay open until the window locks them, so somebody who
// has just shut themselves out has a moment to put the key back.
func whatRemovingCosts(v *secrets.Vault, s secrets.KeySlot) string {
	if !lastKeyHere(v, s) {
		return "Another key on this machine still opens the secrets."
	}
	return "This is the only key here that opens them. This machine cannot" +
		" open them again until one of the others is on it."
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
	return a.withOpenSecrets("Could not remove the secret", a.chooseToForget)
}

// chooseToForget is the list a secret is taken out from.
func (a *app) chooseToForget(v *secrets.Vault) error {
	items, err := v.Items()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		n := a.newNotice("No secrets yet", `Take "`+addSecretTitle+`" to add one.`)
		n.SetNoCopy()
		a.presentNotice(n)
		return nil
	}
	var hide func()
	c := ui.NewChooser(removeSecretTitle, func() {
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
			a.say(it.Name + " removed")
			return nil
		})
	}
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("Window too small")
	}
	a.markDirty()
	return nil
}
