package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// secretsTitle is what the vault calls itself, and the rest are what its
// commands are called.
//
// Constants because the dialogs quote them: a notice telling the user to
// choose "Add Secret" has to name the line they will actually find, and a
// copy of that name in prose drifts the first time the command is
// reworded. See WORDING.md.
const (
	secretsTitle          = "Secrets"
	showSecretsTitle      = "Show Secrets"
	manageSecretsTitle    = "Manage Secrets"
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
			`An ed25519 key is needed. Choose "`+makeKeyTitle+`" to create one.`)
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
//
// The same question as adding one, because this key matters more than
// any added later: it is the only one the vault will have until another
// is.
func (a *app) startVaultOn(keyFile string) {
	a.askBeforeTrusting(nil, keyFile, dlgSecretsOn+keyFile+"?", btnCreate,
		func() { a.makeVaultOn(keyFile) })
}

// makeVaultOn writes the vault, once the key has been asked about.
func (a *app) makeVaultOn(keyFile string) {
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
		a.noSecretsYet()
		return nil
	}
	// The title says when picking one answers what the pane is waiting
	// for, because then the row does something more than copy a
	// password: it sends the line the program is sitting on.
	title := showSecretsTitle
	if pane := a.focusedTerminal(); pane != nil && pane.AskedForASecret() {
		title = showSecretsTitle + " — the pane is waiting for one"
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
		{Label: btnType, Do: a.onPicked(v, items, a.typeSecret)},
		{Label: btnCopy, Do: a.onPicked(v, items, a.copySecret)},
		{Label: btnShow, Do: a.onPicked(v, items, a.showSecret)},
		{Label: btnCancel, Do: func(int) error { return nil }},
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

// noSecretsYet says the vault is empty and where to start, which is one
// answer to three commands: showing, changing and removing all meet it.
func (a *app) noSecretsYet() {
	n := a.newNotice("No secrets yet", `Choose "`+addSecretTitle+`" to add one.`)
	n.SetNoCopy()
	a.presentNotice(n)
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
	// The key a passphrase opens, which is the one thing that says
	// which of two keys with the same file name this one is for.
	if it.File != "" {
		parts = append(parts, it.File)
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

// stillOnTheClipboard reports whether the clipboard still holds a
// secret this window put there.
//
// Not through pasteText, which is the paste path: that one puts a
// dialog up when the clipboard holds a picture or will not be read, and
// both of the callers here run on their own -- a timer half a minute
// later, and the window closing. Neither is a paste, so neither has
// anything to tell the user, and a "Could not paste" dialog nobody
// asked for is the last thing a window on its way out should draw.
//
// Anything that is not the secret answers false, and the clipboard is
// left as it is. That covers the picture, the read that failed and the
// clipboard somebody has used since, which all want the same thing.
func (a *app) stillOnTheClipboard(value string) bool {
	if value == "" || !a.clipboardText() {
		return false
	}
	read := a.readClip
	if read == nil {
		read = readClipboardText
	}
	got, err := read()
	return err == nil && got == value
}

// forgetClipboardLater takes a secret back off the clipboard once its
// time is up, and leaves alone a clipboard that holds something else by
// then.
func (a *app) forgetClipboardLater(value string) {
	a.secretCopied = value
	a.secretCopies++
	// Which copy this timer is for. A later one starts a timer of its
	// own and owns the clipboard from then on.
	mine := a.secretCopies
	time.AfterFunc(clipboardHolds, func() {
		// Through the pump, because reading the clipboard and taking a
		// secret off it belong to the goroutine that draws.
		a.pump.post(func() { a.forgetClipboardCopy(value, mine) })
	})
}

// forgetClipboardCopy is what one copy's timer does when it goes off.
//
// Its own method so a test can ring the timer rather than wait half a
// minute for it.
func (a *app) forgetClipboardCopy(value string, copied int) {
	if a.secretCopies != copied {
		// Something has been copied since, and that copy's own timer
		// has its own half minute to run.
		return
	}
	a.secretCopied = ""
	if !a.stillOnTheClipboard(value) {
		// They have copied something since. It is theirs.
		return
	}
	a.clip.clear()
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
	if !a.stillOnTheClipboard(value) {
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
	title, label, mask := addSecretTitle, fldSecret, '*'
	if kind == secrets.Note {
		title, label, mask = addNoteTitle, fldNote, rune(0)
	}
	f := a.newForm(title)
	f.Lines = []string{onlyYourKey}
	name := f.AddField(fldName, a.newField("", 0))
	user := f.AddField(fldFor, a.newField("Optional", 0))
	user.Hint = "Who or what the secret is for"
	a.offerServers(user)
	// The machine in front of the user, which is the one a password
	// typed now is nearly always for. It is a suggestion in a field
	// they can clear, not a decision.
	user.SetText(a.currentHost())
	value := f.AddField(label, a.newField("", mask))
	a.addShowBox(f, value, mask)
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
	a.addGenerateButton(f, value, kind)
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

// onlyYourKey is the line at the top of a form that takes a secret.
//
// Somebody typing a password into a window is owed the one thing the
// title cannot say: who can read it back. That is nobody else, and
// saying so is what this line is for.
//
// It says "the secrets" because that is what every title, command and
// line along the bottom calls them. The word vault belongs to the
// package and to the file on disk, and a second name for one thing is
// a second thing to learn.
const onlyYourKey = "Only your key opens the secrets."

// offerServers puts the machines the window knows of on a For field, so
// the one a secret belongs to is a keystroke away rather than typed out.
//
// An empty one is on the end of the list already, which is how a secret
// that belongs to no machine is cycled back to.
func (a *app) offerServers(user *ui.Field) {
	var hosts []string
	for _, host := range a.everyHost() {
		if host != conns.Local {
			hosts = append(hosts, host)
		}
	}
	user.Options = hosts
}

// addGenerateButton puts a button on a form that fills the field with a
// password nobody has to think of.
//
// Only for a password. A note is a recovery code or a licence that came
// from somewhere else, and there is nothing to generate.
//
// What it writes stays masked. Reading it back is what the Show button
// beside this one is for, so the two are one job each.
func (a *app) addGenerateButton(f *ui.Form, value *ui.Field, kind secrets.Kind) {
	if kind == secrets.Note {
		return
	}
	f.AddButton(ui.Button{Title: btnGenerate, Keep: true, Do: func() error {
		made, err := secrets.NewPassword(secrets.PasswordLength)
		if err != nil {
			return err
		}
		value.SetText(made)
		a.markDirty()
		return nil
	}})
}

// addShowBox puts a tick on a form that turns the stars off, for
// checking what was typed before saving it.
//
// A tick rather than the button this was, which said Show and then
// renamed itself to Hide. Rule 9: a button that renames itself is a bug
// wearing an explanation, and the explanation was a comment saying it
// had to say what the next press did rather than what the last one did.
// A tick says which way it is without being read twice, and it sits
// beside the field it is about rather than down among the verbs.
//
// Only on a field that is masked to begin with: a note is shown as it
// is typed and has nothing to turn off.
func (a *app) addShowBox(f *ui.Form, value *ui.Field, mask rune) {
	if mask == 0 {
		return
	}
	show := f.AddTick(fldShowSecret, false)
	show.OnChange = func(string) {
		if show.On() {
			value.Mask = 0
		} else {
			value.Mask = mask
		}
		a.markDirty()
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
	// Titled for the command, because this list and the one under
	// Remove Secrets Key were both "Choose a key" and the user had only
	// their memory to say which they were in.
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
	c := ui.NewChooser(addSecretsKeyTitle, func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	for _, keyFile := range spare {
		c.Add(keyFile, spareKeyNote(v, keyFile), func() error {
			a.confirmAddKey(v, keyFile)
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

// spareKeyNote marks a key whose passphrase is in this vault, and says
// nothing about any other.
//
// A second key is added so that losing the first does not lose the
// vault. A key whose passphrase is only in here cannot do that job: with
// the first key gone, opening the vault needs this key, unlocking this
// key needs the passphrase, and the passphrase is inside the vault.
func spareKeyNote(v *secrets.Vault, keyFile string) string {
	if _, err := v.PassphraseFor(keyFile); err != nil {
		return ""
	}
	return "passphrase in the secrets"
}

// The consequences worth saying before a key is trusted with the
// secrets. One sentence each, saying what it costs and not how any of
// it works: see rule 10 and rule 4 in WORDING.md.
//
// Constants because the tests and the dialog sheet quote them, and a
// wording that lived only where it is used would drift.
const (
	agentCanOpenThem = "A server you forward the agent to can open" +
		" any copy of the secrets it has."
	anotherKeyIsNeeded = "Its passphrase is in the secrets, so another" +
		" key is still needed to open them."
)

// warningsAboutKey is what is worth saying before this key is given a
// slot, worst first, and nothing for a key with nothing against it.
//
// v is the vault the key would open, and nil while there is none yet:
// the passphrase warning cannot apply to a vault that does not exist.
func (a *app) warningsAboutKey(v *secrets.Vault, keyFile string) []string {
	var out []string
	if a.agentHoldsKey(keyFile) {
		out = append(out, agentCanOpenThem)
	}
	if v != nil {
		if _, err := v.PassphraseFor(keyFile); err == nil {
			out = append(out, anotherKeyIsNeeded)
		}
	}
	return out
}

// agentHoldsKey reports whether the running SSH agent holds this key.
//
// By the public half beside it, so nothing has to be unlocked to ask.
// A key with no public half, or an agent that will not answer, is a
// question that cannot be put: false, and no warning, because a warning
// this window cannot stand behind is worse than none.
func (a *app) agentHoldsKey(keyFile string) bool {
	if a.keys.AgentTrouble() != nil {
		// The window has given up on the agent once already. Asking
		// again would buy the same silence, and this runs on the
		// goroutine that draws.
		return false
	}
	pub, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		return false
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(pub)
	if err != nil {
		return false
	}
	ask := a.agentHolds
	if ask == nil {
		ask = remote.AgentHolds
	}
	held, err := ask(secrets.Fingerprint(key))
	return err == nil && held
}

// askBeforeTrusting puts the warnings about a key in front of the user
// and does the thing when they say to go on. With nothing to say it
// goes ahead without a dialog.
//
// Said rather than refused, in both cases. A key in the agent is only a
// way in for somebody who also has the file, and a passphrase kept in
// the vault can be copied out and held elsewhere. Which of those is
// worth it is the user's to weigh, and the dialog gives them what to
// weigh it with.
func (a *app) askBeforeTrusting(v *secrets.Vault, keyFile, title, accept string, then func()) {
	warn := a.warningsAboutKey(v, keyFile)
	if len(warn) == 0 {
		then()
		return
	}
	// The title names the action and the key, the way every other
	// question about one does, so the body is the consequences and
	// nothing else. Two of them are two sentences, one each.
	var lines []string
	for i, says := range warn {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, wrapLines(says, errorLineWidth)...)
	}
	f := a.newConfirm(title, lines)
	f.AddButton(ui.Button{Title: accept, Do: func() error {
		// Not from here: this dialog closes as soon as this returns,
		// and closing one takes anything stacked on top of it.
		a.pump.post(then)
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	// Opens on the button that changes nothing, the way every question
	// about exposing something does.
	f.FocusButton(1)
	a.showForm(f, nil)
}

// confirmAddKey asks about a key before giving it a slot of its own.
func (a *app) confirmAddKey(v *secrets.Vault, keyFile string) {
	a.askBeforeTrusting(v, keyFile, dlgAddKey+keyFile+"?", btnAdd,
		func() { a.addVaultKeyOn(v, keyFile) })
}

// whyNoKeyToAdd says why there is nothing to offer, which is a
// different thing depending on how many already open it.
func whyNoKeyToAdd(opening int) string {
	if opening == 0 {
		return `No other ed25519 key is on this machine. Choose "` + makeKeyTitle +
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
		a.noSecretsYet()
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
	label, mask := fldNewSecret, '*'
	if it.Kind == secrets.Note {
		label, mask = fldNewNote, rune(0)
	}
	f := a.newForm(changeSecretTitle + " — " + it.Name)
	name := f.AddField(fldName, a.newField(it.Name, 0))
	name.SetText(it.Name)
	user := f.AddField(fldFor, a.newField("Optional", 0))
	user.Hint = "Who or what the secret is for"
	a.offerServers(user)
	user.SetText(it.User)
	value := f.AddField(label, a.newField("Optional", 0))
	value.Mask = mask
	value.Hint = "Leave empty to keep the saved one"
	a.addShowBox(f, value, mask)

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
	a.addGenerateButton(f, value, it.Kind)
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
	if a.onlyOneKeyOpensThem(v) {
		return nil
	}
	var hide func()
	c := ui.NewChooser(removeSecretsKeyTitle, func() {
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

// onlyOneKeyOpensThem says so when there is one key and nothing to
// remove, and reports whether that was the case.
//
// Shared by the chooser and the pane, which both have to stop before
// taking the last one: removing it would leave nothing that opens the
// vault, and nothing here could put it back.
func (a *app) onlyOneKeyOpensThem(v *secrets.Vault) bool {
	if len(v.Keys()) >= 2 {
		return false
	}
	n := a.newNotice("Only one key opens the secrets",
		`Choose "`+addSecretsKeyTitle+`" to add another first.`)
	n.SetNoCopy()
	a.presentNotice(n)
	return true
}

// keyRowName is what a key's row says it is: where it was, or its
// fingerprint when the vault was never told where.
func keyRowName(s secrets.KeySlot) string {
	if s.ByPassphrase() {
		// Not the name in the slot, which is drawn at random and says
		// nothing: what this slot is, is a passphrase.
		return passphraseRowName
	}
	if s.KeyFile != "" {
		return s.KeyFile
	}
	return s.Fingerprint
}

// passphraseRowName is what a passphrase slot is called in a list.
const passphraseRowName = "Passphrase"

// keyRowNote is the right-hand side of a key's row: whether the key is
// on this machine, and its fingerprint when the row does not already
// say it.
func keyRowNote(s secrets.KeySlot) string {
	if s.ByPassphrase() {
		return passphraseRowNote
	}
	var parts []string
	if onThisMachine(s) {
		parts = append(parts, "on this machine")
	}
	if s.KeyFile != "" {
		parts = append(parts, s.Fingerprint)
	}
	return strings.Join(parts, " · ")
}

// passphraseRowNote says what a passphrase slot is for, which is the
// one thing about it worth a line: it is the way in when the keys are
// not here.
const passphraseRowNote = "a way in without a key"

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
	// A question rather than a notice with an action on it, so the way
	// out says Cancel. On a notice it said OK, which reads as agreeing
	// to the removal rather than declining it, and carried a Copy
	// button over a body with nothing in it to copy.
	f := a.newConfirm(dlgRemove+keyRowName(s)+"?",
		wrapLines(whatRemovingCosts(v, s), errorLineWidth))
	f.AddButton(ui.Button{Title: btnRemove, Do: func() error {
		if err := v.RemoveKey(s.Fingerprint); err != nil {
			return err
		}
		a.say(fmt.Sprintf("%s removed — %d keys still open the secrets",
			keyRowName(s), len(v.Keys())))
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	// Opens on the button that changes nothing: a key that is gone
	// cannot be put back without the key itself.
	f.FocusButton(1)
	a.showForm(f, nil)
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
	if v.TakesAPassphrase() && !s.ByPassphrase() {
		return "The passphrase still opens them here."
	}
	return "Opening them here again needs a key from another machine."
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

// confirmRemoveSecrets asks before taking secrets away, and takes them
// when the user says so. then runs when any of them went.
//
// A question, because a secret that is gone is gone: the vault holds
// the only copy of what is in it, and nothing here can put one back.
// Rule 10 on both counts.
func (a *app) confirmRemoveSecrets(v *secrets.Vault, going []secrets.Item, then func()) {
	if len(going) == 0 {
		return
	}
	f := a.newConfirm(removingTitle(going), wrapLines(cannotBePutBack, errorLineWidth))
	f.AddButton(ui.Button{Title: btnRemove, Do: func() error {
		var failed error
		gone := 0
		for _, it := range going {
			switch err := v.Remove(it.ID); {
			case errors.Is(err, secrets.ErrNoSuchItem):
				// Taken away from somewhere else while this was being
				// asked. What the user wanted has happened.
				gone++
			case err != nil:
				// Every failure, not the first: removing four can fail
				// four ways and three would go unsaid.
				failed = errors.Join(failed, err)
			default:
				gone++
			}
		}
		if gone > 0 && then != nil {
			then()
		}
		if failed != nil {
			return failed
		}
		a.say(wentAway(going))
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	// Opens on the button that changes nothing.
	f.FocusButton(1)
	a.showForm(f, nil)
}

// removingTitle names what is about to go: the one, or how many.
func removingTitle(going []secrets.Item) string {
	if len(going) == 1 {
		return dlgRemove + going[0].Name + "?"
	}
	return dlgRemove + strconv.Itoa(len(going)) + " secrets?"
}

// wentAway says on the bottom row what has gone.
func wentAway(going []secrets.Item) string {
	if len(going) == 1 {
		return going[0].Name + " removed"
	}
	return strconv.Itoa(len(going)) + " secrets removed"
}

// cannotBePutBack is why removing one is worth a question.
const cannotBePutBack = "The vault holds the only copy of what is in it."

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
		a.noSecretsYet()
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
