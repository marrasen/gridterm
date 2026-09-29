package app

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/marrasen/kakel/conf"
	"github.com/marrasen/kakel/secrets"
	"github.com/marrasen/kakel/words"
)

// Taking the secrets to another manager and bringing them in from one,
// as CSV files.

// Intents for secrets files.
type (
	// ExportSecrets writes every secret, in plain text, to a new CSV
	// file at Path.
	ExportSecrets struct{ Path string }
	// ImportSecrets reads the secrets in a CSV file another manager
	// wrote. Duplicates says what to do with one already here: keep
	// both, skip it, or replace it.
	ImportSecrets struct{ Path, Duplicates string }
)

// The answers to what to do with a secret already here.
const (
	KeepBoth = "Keep Both"
	SkipThem = "Skip"
	Replace  = "Replace"
)

// exportSecrets writes the secrets to a new file, readable by its
// owner alone, and never over one already there.
func (a *app) exportSecrets(in ExportSecrets) {
	at, err := conf.ExpandHome(in.Path)
	if err != nil {
		a.failed("Couldn't export the secrets", err.Error())
		return
	}
	// The secrets open first, and a file there already is refused,
	// before anything is asked: a question answered for nothing is worse
	// than none.
	a.withSecrets("Couldn't export the secrets", func(*secrets.Vault) error {
		if _, err := os.Lstat(at); err == nil {
			return fmt.Errorf("%s is already there; the export writes a new file only", at)
		}
		// Asked, naming the file, and opening on Cancel: this is the
		// one thing that takes every secret out of what keeps them.
		go func() {
			ans, err := a.ask(a.ctx, Ask{Title: "Export every secret to " + at + "?", Text: "Anyone who can read the file can read them all.", Yes: "Export", Careful: true})
			if err != nil || !ans.Yes {
				return
			}
			a.events <- func() { a.exportSecretsTo(at) }
		}()
		return nil
	})
}

// exportSecretsTo writes every secret to at, a file that is not there.
func (a *app) exportSecretsTo(at string) {
	a.withSecrets("Couldn't export the secrets", func(v *secrets.Vault) error {
		out, err := v.Everything()
		if err != nil {
			return err
		}
		err = createNew(at, func(w io.Writer) error { return secrets.WriteCSV(w, out) })
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s is already there; the export writes a new file only", at)
		}
		if err != nil {
			return err
		}
		a.worked("Secrets exported", fmt.Sprintf("%s in plain text, to %s. Import it as Chrome or Other CSV, then remove the file.", words.Count(len(out), "secret"), at), "")
		return nil
	})
}

// importSecrets reads the secrets in a file another manager wrote.
func (a *app) importSecrets(in ImportSecrets) {
	a.withSecrets("Couldn't import the secrets", func(v *secrets.Vault) error {
		at, err := conf.ExpandHome(in.Path)
		if err != nil {
			return err
		}
		f, err := os.Open(at)
		if err != nil {
			return err
		}
		read, err := secrets.ReadCSV(f)
		if err := errors.Join(err, f.Close()); err != nil {
			return err
		}
		if len(read) == 0 {
			return fmt.Errorf("%s has no secrets this can read", at)
		}
		dup := secrets.KeepBoth
		switch in.Duplicates {
		case SkipThem:
			dup = secrets.SkipThem
		case Replace:
			dup = secrets.ReplaceThem
		}
		added, skipped, err := v.Import(read, dup)
		if err != nil {
			return err
		}
		said := words.Count(added, "secret") + " read in"
		if skipped > 0 {
			said += fmt.Sprintf(", %d left as they were", skipped)
		}
		a.worked("Secrets imported", said+". Every secret in "+at+" is in plain text, so remove it.", "")
		return nil
	})
}
