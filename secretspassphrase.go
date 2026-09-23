package main

import (
	"context"
	"errors"

	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// addSecretsPassphraseTitle names the command that lets a passphrase
// open the secrets.
const addSecretsPassphraseTitle = "Add Secrets Passphrase"

// addSecretsPassphrase asks for a passphrase and lets it open the
// vault, as a way back in when every key is gone.
//
// Nothing adds one. Every slot is an SSH key until the user asks for
// this, which is the point: a passphrase is the weaker door and it
// stays shut unless somebody opens it on purpose.
func (a *app) addSecretsPassphrase() error {
	return a.withOpenSecrets(couldNotAddThePassphrase, func(v *secrets.Vault) error {
		f := a.newForm(addSecretsPassphraseTitle)
		f.Lines = wrapLines(asStrongAsThis, errorLineWidth)
		pass := a.newField("", '*')
		f.AddField(fldPassphrase, pass)
		// Twice, because it is masked and it is the thing that gets the
		// user back in years from now: a typo makes a way in nobody can
		// find.
		again := a.newField("", '*')
		f.AddField(fldConfirmPass, again)

		f.AddButton(ui.Button{Title: btnAdd, Do: func() error {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			if pass.Text() != again.Text() {
				return errors.New("Passphrases do not match")
			}
			if pass.Text() == "" {
				return errors.New("Enter a passphrase")
			}
			// Deriving takes a tenth of a second and this is the
			// goroutine that draws, so it goes off this one.
			a.addPassphraseInBackground(v, pass.Text())
			return nil
		}})
		f.AddButton(ui.Button{Title: btnCancel})
		// Opens on the button that changes nothing, the way every
		// question about a second way in does.
		f.FocusButton(1)
		a.showForm(f, nil)
		return nil
	})
}

// couldNotAddThePassphrase heads whatever went wrong.
const couldNotAddThePassphrase = "Could not add the passphrase"

// asStrongAsThis is what a passphrase slot costs, in one sentence.
//
// Rule 10. Every other way into this vault is a key in a file; this one
// is what somebody types, and anybody holding the file can guess at it
// for as long as they like.
const asStrongAsThis = "Anyone with the file can guess at it." +
	" The secrets are then only as strong as what you type."

// addPassphraseInBackground derives the slot key off the drawing
// goroutine and says how it went.
func (a *app) addPassphraseInBackground(v *secrets.Vault, pass string) {
	a.closes.inBackground(func() error {
		err := v.AddPassphrase(pass)
		a.pump.post(func() {
			if err != nil {
				a.reportError(couldNotAddThePassphrase, err)
				return
			}
			a.say(passphraseOpensThem)
		})
		return nil
	})
}

// passphraseOpensThem is said on the bottom row once one does.
const passphraseOpensThem = "A passphrase opens the secrets now"

// askForTheSecretsPassphrase asks for the passphrase and opens the
// vault with it.
//
// Only where one has been added. It blocks on a dialog, so it runs on a
// goroutine of its own, and so does the deriving after it.
func (a *app) askForTheSecretsPassphrase(v *secrets.Vault, then func(error)) {
	a.closes.inBackground(func() error {
		ask := &askUser{app: a}
		for wrong := 0; ; wrong++ {
			said, err := ask.secretsPassphrase(context.Background(), wrong)
			if err != nil {
				a.pump.post(func() { then(err) })
				return nil
			}
			switch err := v.UnlockWith(said); {
			case errors.Is(err, secrets.ErrWrongPassphrase):
				// Asked again, the way a key's passphrase is. The file
				// is on this machine and this user can already read it,
				// so a limit guards nothing and only strands whoever
				// mistyped a long one.
				continue
			default:
				a.pump.post(func() { then(err) })
				return nil
			}
		}
	})
}
