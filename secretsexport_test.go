package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// aVaultWithOneOfEach is a window whose vault holds a password, a note
// and a key's passphrase.
func aVaultWithOneOfEach(t *testing.T) (*testApp, *secrets.Vault) {
	t.Helper()
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	for _, put := range []struct {
		it    secrets.Item
		value string
	}{
		{secrets.Item{Name: "margit", User: "kettle"}, "hunter2"},
		{secrets.Item{Name: "licence", Kind: secrets.Note}, "ABCD-1234"},
		{secrets.Item{Name: "id_ed25519", Kind: secrets.Passphrase, File: "/keys/id"}, "a long one"},
	} {
		if _, err := v.Put(put.it, put.value); err != nil {
			t.Fatalf("put %s: %v", put.it.Name, err)
		}
	}
	return a, v
}

// exported reads a written file back as rows keyed by the header.
func exported(t *testing.T, at string) []map[string]string {
	t.Helper()
	raw, err := os.Open(at)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	defer func() { _ = raw.Close() }()
	rows, err := csv.NewReader(raw).ReadAll()
	if err != nil {
		t.Fatalf("parse it: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("the file has %d lines, want a header and the secrets", len(rows))
	}
	out := make([]map[string]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		one := map[string]string{}
		for i, head := range rows[0] {
			if i < len(row) {
				one[head] = row[i]
			}
		}
		out = append(out, one)
	}
	return out
}

// Everything goes out, with each value in the column that says what it
// is: a note is notes, a password and a passphrase are password.
func TestExportWritesEverySecret(t *testing.T) {
	_, v := aVaultWithOneOfEach(t)
	at := filepath.Join(t.TempDir(), "out.csv")

	written, err := writeSecretsTo(at, v)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if written != 3 {
		t.Fatalf("it wrote %d secrets, want three", written)
	}

	rows := exported(t, at)
	by := map[string]map[string]string{}
	for _, row := range rows {
		by[row["name"]] = row
	}
	if got := by["margit"]; got["password"] != "hunter2" || got["username"] != "kettle" {
		t.Errorf("the password came out as %v", got)
	}
	if got := by["licence"]; got["notes"] != "ABCD-1234" || got["password"] != "" {
		t.Errorf("the note came out as %v, want its text in the notes column", got)
	}
	if got := by["id_ed25519"]; got["password"] != "a long one" || got["file"] != "/keys/id" {
		t.Errorf("the passphrase came out as %v, want the key it opens", got)
	}
}

// The header is the shape a browser writes, so the managers that read a
// Chrome export read this.
func TestExportUsesTheBrowserShape(t *testing.T) {
	_, v := aVaultWithOneOfEach(t)
	at := filepath.Join(t.TempDir(), "out.csv")
	if _, err := writeSecretsTo(at, v); err != nil {
		t.Fatalf("export: %v", err)
	}

	raw, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	head := strings.SplitN(string(raw), "\n", 2)[0]
	for _, want := range []string{"url", "username", "password"} {
		if !strings.Contains(head, want) {
			t.Errorf("the header is %q, and Chrome insists on %q", head, want)
		}
	}
	// And this window's own on the end, so it reads its own export back.
	for _, want := range []string{"kind", "file"} {
		if !strings.Contains(head, want) {
			t.Errorf("the header is %q without %q", head, want)
		}
	}
}

// The file is readable by its owner alone.
func TestTheExportIsReadableByItsOwnerAlone(t *testing.T) {
	_, v := aVaultWithOneOfEach(t)
	at := filepath.Join(t.TempDir(), "out.csv")
	if _, err := writeSecretsTo(at, v); err != nil {
		t.Fatalf("export: %v", err)
	}

	info, err := os.Stat(at)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("the file is mode %o, want nobody but the owner", mode)
	}
}

// A path with something at it is refused rather than written over.
func TestTheExportWillNotWriteOverAFile(t *testing.T) {
	_, v := aVaultWithOneOfEach(t)
	at := filepath.Join(t.TempDir(), "out.csv")
	if err := os.WriteFile(at, []byte("something else"), 0o600); err != nil {
		t.Fatalf("put a file there: %v", err)
	}

	if _, err := writeSecretsTo(at, v); err == nil {
		t.Fatal("it wrote over a file that was already there")
	}
	raw, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if string(raw) != "something else" {
		t.Errorf("the file now holds %q", raw)
	}
}

// The form offers no path and opens in its field: what keeps this from
// happening by accident is that there is nothing to export to until a
// path is typed.
func TestTheExportFormOpensInItsField(t *testing.T) {
	a, _ := aVaultWithOneOfEach(t)

	if err := a.exportSecrets(); err != nil {
		t.Fatalf("export: %v", err)
	}
	f := awaitModal(t, a, "the export form", byTitle[*ui.Form](exportSecretsTitle))

	if got := f.Field(fldFile).Text(); got != "" {
		t.Errorf("the path is filled in with %q", got)
	}
	// In the field, not on a button. A form whose first job is to be
	// typed into and opens on a button swallows everything typed.
	if at, isButton := f.Focused(); isButton || at != 0 {
		t.Errorf("it opens on %d (button %v), want the first field", at, isButton)
	}
}

// An empty path is refused by name, not turned into the home directory
// and then refused for being a directory.
func TestTheExportRefusesAnEmptyPath(t *testing.T) {
	a, _ := aVaultWithOneOfEach(t)
	if err := a.exportSecrets(); err != nil {
		t.Fatalf("export: %v", err)
	}
	f := awaitModal(t, a, "the export form", byTitle[*ui.Form](exportSecretsTitle))

	pressButton(t, a, f, btnExport)
	if a.root.Modal() != ui.Widget(f) {
		t.Fatalf("the form went away; the modal on top is %T", a.root.Modal())
	}
	if got := f.Error(); got == nil || !strings.Contains(got.Error(), "path") {
		t.Errorf("it says %v, want that a path is needed", got)
	}
}

// The question names the file and says what it costs, and opens on the
// way out.
func TestTheExportQuestionNamesTheFile(t *testing.T) {
	a, v := aVaultWithOneOfEach(t)
	at := filepath.Join(t.TempDir(), "out.csv")

	a.confirmExport(v, at)
	f := awaitModal(t, a, "the question about exporting", byTitle[*ui.Form](dlgExportTo+at+"?"))

	if said := strings.Join(f.Lines, " "); !strings.Contains(said, anyoneWhoCanReadIt) {
		t.Errorf("the question says %q without what it costs", said)
	}
	var titles []string
	for _, b := range f.Buttons() {
		titles = append(titles, b.Title)
	}
	if !slices.Contains(titles, btnExport) || !slices.Contains(titles, btnCancel) {
		t.Errorf("the question offers %v", titles)
	}
	if on, isButton := f.Focused(); !isButton || f.Buttons()[on].Title != btnCancel {
		t.Errorf("it opens on button %d (%v), want %s", on, isButton, btnCancel)
	}

	// And going on writes the file.
	pressButton(t, a, f, btnExport)
	if _, err := os.Stat(at); err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
}

// And the notice afterwards says which importer to pick and to take
// the file away.
func TestTheExportSaysWhatToDoWithTheFile(t *testing.T) {
	a, _ := aVaultWithOneOfEach(t)
	at := filepath.Join(t.TempDir(), "out.csv")

	a.sayWhereTheyWent(at, 3)
	n := awaitModal(t, a, "what was written", byTitle[*ui.Notice](dlgSecretsWritten))
	said := n.Message()
	for _, want := range []string{at, "Chrome", "Remove the file", "plain text"} {
		if !strings.Contains(said, want) {
			t.Errorf("it says %q without %q", said, want)
		}
	}
}
