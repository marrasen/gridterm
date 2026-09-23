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

// exportSecretsTitle names the command and the dialog that writes every
// secret to a file.
const exportSecretsTitle = "Export Secrets"

// exportPerm is the mode the file is written with. Only the user, the
// way the vault itself is written.
const exportPerm = 0o600

// exportSecrets asks where to write them, and says what that costs.
//
// This is the way out. A password manager nobody can leave is one
// nobody should adopt, so whoever keeps passwords here can take them
// to another manager whenever they like.
//
// It is plaintext, because that is what every other manager reads, and
// that is the whole of what makes it worth a question: this is the one
// action in the window that takes every secret out of the thing built
// to hold them.
func (a *app) exportSecrets() error {
	return a.withOpenSecrets(couldNotExport, func(v *secrets.Vault) error {
		f := a.newForm(exportSecretsTitle)
		where := f.AddField(fldFile, a.newField("Where to write them", 0))
		where.Hint = "Comma-separated values, which other managers read"
		// Nothing filled in. What keeps this from happening by accident
		// is that there is no path until one is typed, which is a
		// better guard than where the focus starts: a form whose first
		// job is to be typed into opens in its field, or the first
		// thing typed goes to a button and nowhere.
		a.completePath(where, vfs.NewLocal())

		f.AddButton(ui.Button{Title: btnExport, Do: func() error {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			if strings.TrimSpace(where.Text()) == "" {
				return errors.New("Enter a path")
			}
			at, err := fromHome(where.Text())
			if err != nil {
				return err
			}
			// Not from here: this dialog closes as soon as this
			// returns, and closing one takes anything stacked on top.
			a.pump.post(func() { a.confirmExport(v, at) })
			return nil
		}})
		f.AddButton(ui.Button{Title: btnCancel})
		a.showForm(f, nil)
		return nil
	})
}

// confirmExport asks before every secret goes into a file, naming the
// file it would go into.
//
// The shape a tunnel uses: a form to fill in, then a question about
// what filling it in would do. The question names its target the way
// every other one in the window does, and opens on the way out.
func (a *app) confirmExport(v *secrets.Vault, at string) {
	f := a.newConfirm(dlgExportTo+at+"?", wrapLines(anyoneWhoCanReadIt, errorLineWidth))
	f.AddButton(ui.Button{Title: btnExport, Do: func() error {
		written, err := writeSecretsTo(at, v)
		if err != nil {
			return err
		}
		a.pump.post(func() { a.sayWhereTheyWent(at, written) })
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	// Opens on the button that changes nothing, the way every question
	// about exposing something does.
	f.FocusButton(1)
	a.showForm(f, nil)
}

// couldNotExport heads whatever went wrong on the way to the file.
const couldNotExport = "Could not export the secrets"

// anyoneWhoCanReadIt is what the export costs, in one sentence.
const anyoneWhoCanReadIt = "Anyone who can read the file can read them all."

// writeSecretsTo writes every secret to a file that is not there, and
// answers how many went into it.
//
// Created with O_EXCL, which refuses a file that already exists and
// makes the new one in one step. Both matter: a path typed over
// something else would take it away, and a file made first and locked
// down afterwards is readable by everyone in between.
func writeSecretsTo(at string, v *secrets.Vault) (int, error) {
	out, err := v.Everything()
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(at, os.O_CREATE|os.O_EXCL|os.O_WRONLY, exportPerm)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return 0, fmt.Errorf("%s is already there, and this does not write over a file", at)
		}
		return 0, fmt.Errorf("write %s: %w", at, err)
	}
	if err := secrets.WriteCSV(f, out); err != nil {
		// Taken away again: half an export is a file full of some of
		// somebody's passwords and no way to tell which are missing.
		return 0, errors.Join(err, f.Close(), os.Remove(at))
	}
	// Flushed before it is called written, or a machine that stopped
	// here would leave a file the user has been told holds everything.
	if err := f.Sync(); err != nil {
		return 0, errors.Join(fmt.Errorf("write %s: %w", at, err), f.Close(), os.Remove(at))
	}
	if err := f.Close(); err != nil {
		return 0, errors.Join(fmt.Errorf("write %s: %w", at, err), os.Remove(at))
	}
	return len(out), nil
}

// sayWhereTheyWent says what was written, how to read it in, and to
// take it away afterwards.
func (a *app) sayWhereTheyWent(at string, written int) {
	n := a.newNotice(dlgSecretsWritten, whatWasWritten(at, written))
	// A path and a line to follow, which the dialog would otherwise
	// re-wrap at the spaces.
	n.Preformatted = true
	n.FocusOK()
	a.presentNotice(n)
}

// whatWasWritten is the body of that notice.
//
// Which importer to pick, because that is not guessable: there is no
// standard CSV, and the managers that read this one read it as a
// browser's. And to take the file away, because it is the one place
// every secret sits in the clear.
func whatWasWritten(at string, written int) string {
	return at + "\n\n" +
		howMany(written, "secret") + ", in plain text.\n\n" +
		`Import it as "Chrome" or "Other CSV".` + "\n" +
		"Remove the file once it has been imported."
}
