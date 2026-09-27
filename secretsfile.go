package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/gridterm/secrets"
)

// Taking the secrets to another manager and bringing them in from one,
// as CSV files, the way gridterm does.

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
		f, err := os.OpenFile(at, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s is already there; the export writes a new file only", at)
		}
		if err != nil {
			return err
		}
		if err := secrets.WriteCSV(f, out); err != nil {
			return errors.Join(err, f.Close(), os.Remove(at))
		}
		if err := f.Sync(); err != nil {
			return errors.Join(err, f.Close(), os.Remove(at))
		}
		if err := f.Close(); err != nil {
			return errors.Join(err, os.Remove(at))
		}
		a.notify("Secrets exported", fmt.Sprintf("%s in plain text, to %s. Import it as Chrome or Other CSV, then remove the file.", count(len(out), "secret"), at), "")
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
		said := count(added, "secret") + " read in"
		if skipped > 0 {
			said += fmt.Sprintf(", %d left as they were", skipped)
		}
		a.notify("Secrets imported", said+". Every secret in "+at+" is in plain text, so remove it.", "")
		return nil
	})
}
