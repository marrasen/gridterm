package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// aChromeExport writes the file a browser gives you.
func aChromeExport(t *testing.T, dir string) string {
	t.Helper()
	at := filepath.Join(dir, "passwords.csv")
	body := "name,url,username,password\n" +
		"GitHub,https://github.com,marcus,hunter2\n" +
		"Mail,https://mail,marcus,letmein\n"
	if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	return at
}

// A file another manager wrote comes in.
func TestImportReadsAFileIn(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	at := aChromeExport(t, t.TempDir())

	added, skipped, err := readSecretsFrom(at, v, secrets.KeepBoth)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if added != 2 || skipped != 0 {
		t.Fatalf("%d added and %d skipped, want both in", added, skipped)
	}
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("the vault holds %d, want two", len(items))
	}
}

// The form opens in its field and offers the three answers, with the
// one that loses nothing first.
func TestTheImportFormOffersWhatToDoAboutDuplicates(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	startTheVault(t, a, keyFile)

	if err := a.importSecrets(); err != nil {
		t.Fatalf("import: %v", err)
	}
	f := awaitModal(t, a, "the import form", byTitle[*ui.Form](importSecretsTitle))

	same := f.Field(fldDuplicates)
	if same == nil {
		t.Fatal("the form does not ask what to do about a duplicate")
	}
	if got := same.Text(); got != keepBothTitle {
		t.Errorf("it starts on %q, want the answer that loses nothing", got)
	}
	for _, want := range []string{keepBothTitle, skipTitle, replaceTitle} {
		if !slices.Contains(dropLabels(same), want) {
			t.Errorf("it offers %v, want %q among them", dropLabels(same), want)
		}
	}
	if at, isButton := f.Focused(); isButton || at != 0 {
		t.Errorf("it opens on %d (button %v), want the first field", at, isButton)
	}
}

// An empty path is refused by name rather than turned into the home
// directory.
func TestTheImportRefusesAnEmptyPath(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	startTheVault(t, a, keyFile)
	if err := a.importSecrets(); err != nil {
		t.Fatalf("import: %v", err)
	}
	f := awaitModal(t, a, "the import form", byTitle[*ui.Form](importSecretsTitle))

	pressButton(t, a, f, btnImport)
	if a.root.Modal() != ui.Widget(f) {
		t.Fatalf("the form went away; the modal on top is %T", a.root.Modal())
	}
	if got := f.Error(); got == nil || !strings.Contains(got.Error(), "path") {
		t.Errorf("it says %v, want that a path is needed", got)
	}
}

// The notice afterwards says how many came in and that the file is
// still where it was, in the clear.
func TestTheImportSaysTheFileIsStillThere(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	startTheVault(t, a, keyFile)
	at := aChromeExport(t, t.TempDir())

	a.sayWhatCameIn(at, 2, 1)
	n := awaitModal(t, a, "what came in", byTitle[*ui.Notice](dlgSecretsRead))
	said := n.Message()
	for _, want := range []string{at, "2 secrets read in", "1 was already here", "Remove the file"} {
		if !strings.Contains(said, want) {
			t.Errorf("it says %q without %q", said, want)
		}
	}
}

// A file with nothing worth reading is said to be one, rather than
// counted as nothing imported.
func TestImportingAFileWithNoSecretsSaysSo(t *testing.T) {
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	at := filepath.Join(t.TempDir(), "bookmarks.csv")
	if err := os.WriteFile(at, []byte("name,url\nGitHub,https://github.com\n"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	if _, _, err := readSecretsFrom(at, v, secrets.KeepBoth); err == nil {
		t.Fatal("a file of bookmarks was read as a file of secrets")
	}
}
