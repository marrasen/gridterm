package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// importSecretsTitle names the command and the dialog that reads a file
// of secrets in.
const importSecretsTitle = "Import Secrets"

// The three answers to a secret the file names that the vault already
// holds, as the field offers them.
//
// Keep both first, because it is the one that loses nothing: a file is
// not a reason for something somebody already has to disappear.
const (
	keepBothTitle = "Keep both"
	skipTitle     = "Skip"
	replaceTitle  = "Replace"
)

// importSecrets asks for a file and reads what is in it.
//
// The easy direction. The plaintext file is already on the user's disk
// -- it came out of whatever they are leaving -- and this moves it into
// something sealed. The one thing worth saying afterwards is that the
// file is still where it was.
func (a *app) importSecrets() error {
	return a.withOpenSecrets(couldNotImport, func(v *secrets.Vault) error {
		f := a.newForm(importSecretsTitle)
		where := f.AddField(fldFile, a.newField("CSV file path", 0))
		where.Hint = "Comma-separated values, as another manager writes them"
		a.completePath(where, vfs.NewLocal())
		same := f.AddField(fldDuplicates, a.newField("", 0))
		same.Options = []string{keepBothTitle, skipTitle, replaceTitle}
		same.SetText(keepBothTitle)
		same.Hint = "What to do with a secret that is already here"

		f.AddButton(ui.Button{Title: btnImport, Do: func() error {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			if strings.TrimSpace(where.Text()) == "" {
				return errors.New("Enter a path")
			}
			at, err := fromHome(where.Text())
			if err != nil {
				return err
			}
			added, skipped, err := readSecretsFrom(at, v, whichDuplicates(same.Text()))
			if err != nil {
				return err
			}
			// Not from here: this dialog closes as soon as this
			// returns, and closing one takes anything stacked on top.
			a.pump.post(func() { a.sayWhatCameIn(at, added, skipped) })
			return nil
		}})
		f.AddButton(ui.Button{Title: btnCancel})
		a.showForm(f, nil)
		return nil
	})
}

// couldNotImport heads whatever went wrong on the way in.
const couldNotImport = "Could not import the secrets"

// whichDuplicates turns what the field says into what the vault takes.
func whichDuplicates(said string) secrets.Duplicates {
	switch said {
	case skipTitle:
		return secrets.SkipThem
	case replaceTitle:
		return secrets.ReplaceThem
	default:
		return secrets.KeepBoth
	}
}

// readSecretsFrom reads a file into the vault.
func readSecretsFrom(at string, v *secrets.Vault, dup secrets.Duplicates) (added, skipped int, err error) {
	f, err := os.Open(at)
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", at, err)
	}
	in, err := secrets.ReadCSV(f)
	if err != nil {
		return 0, 0, errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", at, err)
	}
	if len(in) == 0 {
		return 0, 0, fmt.Errorf("%s has no secrets this can read", at)
	}
	return v.Import(in, dup)
}

// sayWhatCameIn says how many were read, how many were passed over, and
// that the file has not moved.
func (a *app) sayWhatCameIn(at string, added, skipped int) {
	n := a.newNotice(dlgSecretsRead, whatCameIn(at, added, skipped))
	n.Preformatted = true
	n.FocusOK()
	a.presentNotice(n)
}

// wereAlreadyHere counts what was passed over, in a sentence that
// reads for one as well as for many.
func wereAlreadyHere(n int) string {
	if n == 1 {
		return "1 was already here."
	}
	return howMany(n, "secret") + " were already here."
}

// whatCameIn is the body of that notice.
//
// The file is named because it is still there, in the clear, and
// whoever exported it from another manager to get here may not have
// thought about that since.
func whatCameIn(at string, added, skipped int) string {
	said := howMany(added, "secret") + " read in."
	if skipped > 0 {
		said += "\n" + wereAlreadyHere(skipped)
	}
	return said + "\n\n" + at + "\n" +
		"Remove the file: every secret in it is in plain text."
}
