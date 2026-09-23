package secrets

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// What this window wrote comes back in as what went out.
func TestAnExportReadsBackIn(t *testing.T) {
	v, key, _ := aVault(t)
	for _, put := range []struct {
		it    Item
		value string
	}{
		{Item{Name: "margit", User: "marcus", URL: "https://margit"}, "hunter2"},
		{Item{Name: "licence", Kind: Note}, "ABCD,1234"},
		{Item{Name: "id", Kind: Passphrase, File: "/keys/id"}, "a long one"},
	} {
		if _, err := v.Put(put.it, put.value); err != nil {
			t.Fatalf("put %s: %v", put.it.Name, err)
		}
	}
	out, err := v.Everything()
	if err != nil {
		t.Fatalf("everything: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, out); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A vault of its own, the way another machine would read it.
	other, _, _ := aVault(t)
	in, err := ReadCSV(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	added, _, err := other.Import(in, KeepBoth)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if added != 3 {
		t.Fatalf("%d came in, want all three", added)
	}
	_ = key

	items, err := other.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	by := map[string]Item{}
	for _, it := range items {
		by[it.Name] = it
	}
	if got := by["margit"]; got.User != "marcus" || got.URL != "https://margit" {
		t.Errorf("the password came back as %+v, want its user and url", got)
	}
	if got := by["licence"]; got.Kind != Note {
		t.Errorf("the note came back as a %v", got.Kind)
	}
	if got := by["id"]; got.Kind != Passphrase || got.File != "/keys/id" {
		t.Errorf("the passphrase came back as %+v", got)
	}
	// And the values, including the one with a comma in it.
	for name, want := range map[string]string{
		"margit": "hunter2", "licence": "ABCD,1234", "id": "a long one",
	} {
		got, err := other.Secret(by[name].ID)
		if err != nil {
			t.Fatalf("secret %s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s came back as %q, want %q", name, got, want)
		}
	}
}

// The headers other managers write are the ones this reads.
func TestTheHeadersOtherManagersWriteAreRead(t *testing.T) {
	for what, file := range map[string]string{
		"chrome":    "name,url,username,password\nGitHub,https://github.com,marcus,hunter2\n",
		"bitwarden": "folder,favorite,type,name,notes,fields,login_uri,login_username,login_password\n,,login,GitHub,,,https://github.com,marcus,hunter2\n",
		"lastpass":  "url,username,password,totp,extra,name,grouping,fav\nhttps://github.com,marcus,hunter2,,,GitHub,,\n",
		"keepassxc": "Group,Title,Username,Password,URL,Notes\nRoot,GitHub,marcus,hunter2,https://github.com,\n",
		"1password": "Title,Url,Username,Password,Notes\nGitHub,https://github.com,marcus,hunter2,\n",
	} {
		in, err := ReadCSV(strings.NewReader(file))
		if err != nil {
			t.Errorf("%s: %v", what, err)
			continue
		}
		if len(in) != 1 {
			t.Errorf("%s: %d secrets, want the one", what, len(in))
			continue
		}
		got := in[0]
		if got.Name != "GitHub" || got.User != "marcus" || got.Value != "hunter2" {
			t.Errorf("%s came back as %+v", what, got)
		}
		if got.URL != "https://github.com" {
			t.Errorf("%s lost the url: %+v", what, got)
		}
	}
}

// A row with a note and no password is a note, which is what every
// manager's licences and recovery codes look like.
func TestARowWithOnlyANoteIsANote(t *testing.T) {
	in, err := ReadCSV(strings.NewReader(
		"name,username,password,notes\nlicence,,,ABCD-1234\n"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(in) != 1 {
		t.Fatalf("%d secrets, want the one", len(in))
	}
	if in[0].Kind != Note || in[0].Value != "ABCD-1234" {
		t.Errorf("it came back as %+v, want a note", in[0])
	}
}

// A row with neither is passed over: a bookmark is not a secret.
func TestARowWithNothingInItIsPassedOver(t *testing.T) {
	in, err := ReadCSV(strings.NewReader(
		"name,url,username,password,notes\nbookmark,https://x,,,\nreal,,,hunter2,\n"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(in) != 1 || in[0].Name != "real" {
		t.Errorf("it read %+v, want only the one with something in it", in)
	}
}

// A file with no column worth reading says so, rather than importing
// nothing and calling it done.
func TestAFileWithNothingToReadSaysSo(t *testing.T) {
	for _, file := range []string{
		"name,url\nGitHub,https://github.com\n",
		"",
		"name,password\n",
	} {
		if _, err := ReadCSV(strings.NewReader(file)); err == nil {
			t.Errorf("%q was read as a file of secrets", file)
		}
	}
}

// Keep both is what an import does unless it is told otherwise, so
// nothing somebody already has disappears because a file said so.
func TestKeepBothKeepsWhatIsAlreadyThere(t *testing.T) {
	v, key, _ := aVault(t)
	if _, err := v.Put(Item{Name: "margit", User: "marcus"}, "the one I have"); err != nil {
		t.Fatalf("put: %v", err)
	}
	in := []Export{{Item: Item{Name: "margit", User: "marcus"}, Value: "the one in the file"}}

	added, skipped, err := v.Import(in, KeepBoth)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if added != 1 || skipped != 0 {
		t.Fatalf("%d added and %d skipped, want both kept", added, skipped)
	}
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("%d secrets, want the old one and the new", len(items))
	}
	var values []string
	for _, it := range items {
		got, err := v.Secret(it.ID)
		if err != nil {
			t.Fatalf("secret: %v", err)
		}
		values = append(values, got)
	}
	for _, want := range []string{"the one I have", "the one in the file"} {
		if !contains(values, want) {
			t.Errorf("the vault holds %v, want %q among them", values, want)
		}
	}
	_ = key
}

// Skip leaves what is here, and Replace puts the file's value over it.
func TestSkipAndReplaceDoWhatTheySay(t *testing.T) {
	for _, on := range []struct {
		dup  Duplicates
		want string
	}{{SkipThem, "the one I have"}, {ReplaceThem, "the one in the file"}} {
		v, _, _ := aVault(t)
		if _, err := v.Put(Item{Name: "margit", User: "marcus"}, "the one I have"); err != nil {
			t.Fatalf("put: %v", err)
		}
		in := []Export{{Item: Item{Name: "margit", User: "marcus"}, Value: "the one in the file"}}
		if _, _, err := v.Import(in, on.dup); err != nil {
			t.Fatalf("import: %v", err)
		}
		items, err := v.Items()
		if err != nil {
			t.Fatalf("items: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("%v left %d secrets, want the one", on.dup, len(items))
		}
		got, err := v.Secret(items[0].ID)
		if err != nil {
			t.Fatalf("secret: %v", err)
		}
		if got != on.want {
			t.Errorf("%v left %q, want %q", on.dup, got, on.want)
		}
	}
}

// A locked vault takes nothing in.
func TestALockedVaultImportsNothing(t *testing.T) {
	v, _, _ := aVault(t)
	v.Lock()
	if _, _, err := v.Import([]Export{{Item: Item{Name: "one"}, Value: "a"}}, KeepBoth); err == nil {
		t.Fatal("a locked vault took an import")
	}
}

// Importing writes the file once, not once per secret.
func TestAnImportSavesOnce(t *testing.T) {
	v, key, path := aVault(t)
	was := v.Saves()
	in := []Export{}
	for _, name := range []string{"a", "b", "c", "d"} {
		in = append(in, Export{Item: Item{Name: name}, Value: name})
	}
	if _, _, err := v.Import(in, KeepBoth); err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := v.Saves(); got != was+1 {
		t.Errorf("the count went from %d to %d, want one write for the lot", was, got)
	}
	// And all four are on the disk.
	back, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := back.Unlock([]ssh.Signer{key}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	items, err := back.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 4 {
		t.Errorf("%d secrets reached the disk, want four", len(items))
	}
}

// contains is slices.Contains under a name the tests above read with.
func contains(in []string, want string) bool {
	for _, one := range in {
		if one == want {
			return true
		}
	}
	return false
}
