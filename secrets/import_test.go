package secrets

import (
	"bytes"
	"os"
	"path/filepath"
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

// Reading this window's own export back in does not leave two
// passphrases filed against one key.
//
// Keep both is right for a password and impossible for a key's
// passphrase: the vault takes one per key file, and two would leave
// nobody able to say which locks the key. The guard was on the branch
// that adds a new secret and not on the one that keeps both, which is
// the branch the ordinary answer takes.
func TestReadingOurOwnExportBackKeepsOnePassphrasePerKey(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{Name: "id", Kind: Passphrase, File: "/keys/id"}, "the one"); err != nil {
		t.Fatalf("put: %v", err)
	}
	out, err := v.Everything()
	if err != nil {
		t.Fatalf("everything: %v", err)
	}

	if _, skipped, err := v.Import(out, KeepBoth); err != nil {
		t.Fatalf("import: %v", err)
	} else if skipped != 1 {
		t.Errorf("%d were passed over, want the passphrase", skipped)
	}

	filed := 0
	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	for _, it := range items {
		if it.Kind == Passphrase && it.File == "/keys/id" {
			filed++
		}
	}
	if filed != 1 {
		t.Errorf("%d passphrases are filed against the key, want one", filed)
	}
	// And the one that is there can still be renamed, which two would
	// have made impossible.
	for _, it := range items {
		if it.Kind != Passphrase {
			continue
		}
		it.Name = "a better name"
		if _, err := v.PutDetails(it); err != nil {
			t.Errorf("renaming it: %v", err)
		}
	}
}

// A file written by Excel starts with a byte order mark, which is not
// whitespace and hid the first column.
func TestAByteOrderMarkDoesNotHideTheFirstColumn(t *testing.T) {
	// Written as an escape rather than as the character: Go refuses a
	// source file with one at its head, and a literal one here reads
	// as nothing at all.
	in, err := ReadCSV(strings.NewReader(
		"\ufeffname,url,username,password\nGitHub,https://x,marcus,hunter2\n"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(in) != 1 {
		t.Fatalf("%d secrets, want the one", len(in))
	}
	if in[0].Name != "GitHub" {
		t.Errorf("it came back named %q, want the name column", in[0].Name)
	}
}

// A file says what a secret is, not what kind of thing this window
// has. A word it does not use is not stored as one.
func TestAKindThisWindowDoesNotUseIsNotKept(t *testing.T) {
	in, err := ReadCSV(strings.NewReader(
		"name,password,notes,kind\nodd,hunter2,,banana\n"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(in) != 1 {
		t.Fatalf("%d secrets, want the one", len(in))
	}
	if in[0].Kind != Password {
		t.Errorf("it came back a %q, want a password", in[0].Kind)
	}
}

// And a file cannot file itself against a key on this machine unless
// it says it is that key's passphrase.
func TestAFileCannotClaimAKeyItIsNotThePassphraseFor(t *testing.T) {
	in, err := ReadCSV(strings.NewReader(
		"name,password,kind,file\nplanted,hunter2,password,/home/u/.ssh/id_ed25519\n"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(in) != 1 {
		t.Fatalf("%d secrets, want the one", len(in))
	}
	if in[0].File != "" {
		t.Errorf("it claims %q, and it is not a passphrase", in[0].File)
	}
}

// A file a real KeePassXC wrote, read whole.
//
// testdata/keepassxc.csv came out of keepassxc-cli 2.7.6, not out of
// anybody's memory of what one looks like. It carries what a real
// export carries and a hand-written one does not: quoted fields,
// escaped quotes inside them, a note running over three physical lines
// inside one field, Japanese, and four columns this window has never
// heard of.
//
// It is the file that found the bug below. An entry with a password
// and three recovery codes in its notes came in as the password alone.
func TestAFileARealKeePassXCWrote(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "keepassxc.csv"))
	if err != nil {
		t.Fatalf("open it: %v", err)
	}
	defer func() { _ = f.Close() }()

	in, err := ReadCSV(f)
	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	by := map[string]Export{}
	for _, e := range in {
		by[e.Name] = e
	}
	if len(by) != 4 {
		t.Fatalf("read %d secrets, want the four in it", len(by))
	}

	// A comma and an escaped quote inside quoted fields.
	if got := by["Comma, in the name"]; got.Value != `pa,ss"word` || got.User != "user,name" {
		t.Errorf("the awkward one came back as %+v", got)
	}
	// Unicode, including a script that is not Latin at all.
	if got := by["Unicode ünïcödé"]; got.Value != "påsswörd-日本語" || got.User != "mårcus" {
		t.Errorf("the unicode one came back as %+v", got)
	}
	// A password and a note on one entry: both are kept. Most managers
	// let a login carry a note, and this used to throw it away.
	codes := by["Recovery codes"]
	if codes.Value != "x" {
		t.Errorf("its password came back as %q", codes.Value)
	}
	want := "line one\nline two, with a comma\nline \"three\" quoted"
	if codes.Notes != want {
		t.Errorf("its notes came back as %q, want %q", codes.Notes, want)
	}
}

// And what goes in comes back out, notes and all.
func TestARealExportSurvivesTheRoundTrip(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "keepassxc.csv"))
	if err != nil {
		t.Fatalf("open it: %v", err)
	}
	in, err := ReadCSV(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("read it: %v", err)
	}

	v, _, _ := aVault(t)
	if _, _, err := v.Import(in, KeepBoth); err != nil {
		t.Fatalf("import: %v", err)
	}
	out, err := v.Everything()
	if err != nil {
		t.Fatalf("everything: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, out); err != nil {
		t.Fatalf("write: %v", err)
	}
	back, err := ReadCSV(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}

	was := map[string]Export{}
	for _, e := range in {
		was[e.Name] = e
	}
	for _, e := range back {
		want, had := was[e.Name]
		if !had {
			t.Errorf("%q came out and never went in", e.Name)
			continue
		}
		if e.Value != want.Value || e.User != want.User ||
			e.URL != want.URL || e.Notes != want.Notes {
			t.Errorf("%q came back as %+v, want %+v", e.Name, e, want)
		}
		delete(was, e.Name)
	}
	for name := range was {
		t.Errorf("%q went in and did not come out", name)
	}
}

// Replace does not put one machine's key passphrase over another's.
//
// A passphrase is named after the key file and carries no user, so two
// machines that each made the key this window offers by default hold
// items alike in every other way. Replacing across them would lock one
// machine's key with a passphrase that exists nowhere: the vault is
// the only record and nothing is ever on screen.
func TestReplaceWillNotCrossOneKeyPassphraseWithAnother(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{
		Name: "id_ed25519_gridterm", Kind: Passphrase, File: "/home/b/.ssh/id_ed25519_gridterm",
	}, "this machine's"); err != nil {
		t.Fatalf("put: %v", err)
	}

	// The same name and kind, for the other machine's key.
	in := []Export{{Item: Item{
		Name: "id_ed25519_gridterm", Kind: Passphrase, File: "/home/a/.ssh/id_ed25519_gridterm",
	}, Value: "the other machine's"}}
	if _, _, err := v.Import(in, ReplaceThem); err != nil {
		t.Fatalf("import: %v", err)
	}

	got, err := v.PassphraseFor("/home/b/.ssh/id_ed25519_gridterm")
	if err != nil {
		t.Fatalf("PassphraseFor: %v", err)
	}
	if got != "this machine's" {
		t.Errorf("this machine's key is now locked with %q", got)
	}
}

// And Replace does not empty what the file says nothing about.
//
// A file with no notes column is not a file saying the notes are
// empty. Nothing on screen shows them, so losing them is a loss
// nobody would see.
func TestReplaceKeepsWhatTheFileDoesNotMention(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{
		Name: "GitHub", User: "marcus",
		URL: "https://github.com", Notes: "the recovery codes",
	}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}

	// A Chrome export: a name, a user and a password, and no more.
	in, err := ReadCSV(strings.NewReader(
		"name,url,username,password\nGitHub,,marcus,newpassword\n"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, _, err := v.Import(in, ReplaceThem); err != nil {
		t.Fatalf("import: %v", err)
	}

	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("%d secrets, want the one replaced", len(items))
	}
	if items[0].Notes != "the recovery codes" {
		t.Errorf("the notes are now %q", items[0].Notes)
	}
	if items[0].URL != "https://github.com" {
		t.Errorf("the url is now %q", items[0].URL)
	}
	// And the password did change, which is what Replace is for.
	got, err := v.Secret(items[0].ID)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if got != "newpassword" {
		t.Errorf("the password is %q, want the one from the file", got)
	}
}
