package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	keepBoth = "Keep Both"
	skipThem = "Skip"
	replace  = "Replace"
)

// expandHome reads a path the way a shell does, with ~ for home.
func expandHome(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}

// exportSecrets writes the secrets to a new file, readable by its
// owner alone, and never over one already there.
func (a *app) exportSecrets(in ExportSecrets) {
	a.withSecrets("Couldn't export the secrets", func(v *secrets.Vault) error {
		at, err := expandHome(in.Path)
		if err != nil {
			return err
		}
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
		at, err := expandHome(in.Path)
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
		case skipThem:
			dup = secrets.SkipThem
		case replace:
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
