package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"golang.org/x/crypto/ssh"
)

// aKeyWindow is a window whose settings are in a file the test owns.
func aKeyWindow(t *testing.T) *testApp {
	t.Helper()
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	if _, err := withSettings(t, a, filepath.Join(t.TempDir(), "settings.json")); err != nil {
		t.Fatalf("settings: %v", err)
	}
	return a
}

// makeKeyDialog opens the dialog that writes a key and puts a path in
// it.
func makeKeyDialog(t *testing.T, a *testApp, at string) *ui.Form {
	t.Helper()
	if err := a.openMakeKey(); err != nil {
		t.Fatalf("open it: %v", err)
	}
	f := awaitModal(t, a, "the make a key dialog", byTitle[*ui.Form](makeKeyTitle))
	retypeField(t, a, f, fldFile, at)
	return f
}

// The dialog writes a key pair and keeps the key in the list.
func TestMakingAKeyWritesItAndKeepsIt(t *testing.T) {
	a := aKeyWindow(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")

	f := makeKeyDialog(t, a, at)
	typeIntoField(t, a, f, fldComment, "marcus@laptop")
	pressButton(t, a, f, btnCreate)

	if _, err := os.Stat(at); err != nil {
		t.Fatalf("the key is not there: %v", err)
	}
	if _, err := os.Stat(at + ".pub"); err != nil {
		t.Fatalf("the public half is not there: %v", err)
	}
	if got := a.keyFiles.all(); !slices.Equal(got, []string{at}) {
		t.Errorf("the window keeps %v, want the key it made", got)
	}
}

// And it says where the public half is and how to install it.
func TestMakingAKeySaysHowToInstallIt(t *testing.T) {
	a := aKeyWindow(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")

	f := makeKeyDialog(t, a, at)
	pressButton(t, a, f, btnCreate)

	n := awaitModal(t, a, "what to do with the key", func(got *ui.Notice) bool {
		return got.Title == "Key created"
	})
	body := n.Message()
	line, err := os.ReadFile(at + ".pub")
	if err != nil {
		t.Fatalf("read the public half: %v", err)
	}
	if !strings.Contains(body, strings.TrimSpace(string(line))) {
		t.Errorf("it says %q, want the line to paste", body)
	}
	for _, want := range []string{installKeyRow, "authorized_keys", "administrators_authorized_keys"} {
		if !strings.Contains(body, want) {
			t.Errorf("it says nothing about %q:\n%s", want, body)
		}
	}
}

// A key that could not be written keeps the dialog open with the reason,
// and nothing goes in the list.
func TestAKeyThatCannotBeWrittenKeepsTheDialogOpen(t *testing.T) {
	a := aKeyWindow(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(at, []byte("the key somebody is using"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	f := makeKeyDialog(t, a, at)
	pressButton(t, a, f, btnCreate)

	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a key it could not write")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why the key was not written")
	}
	if got := a.keyFiles.all(); len(got) != 0 {
		t.Errorf("the window keeps %v from a key it did not write", got)
	}
}

// The server dialog offers the keys the window keeps, so one is a key
// away rather than a path to remember.
func TestTheServerDialogOffersTheKeptKeys(t *testing.T) {
	a := aKeyWindow(t)
	if err := a.keyFiles.keep("/home/marcus/.ssh/id_work"); err != nil {
		t.Fatalf("keep: %v", err)
	}

	if err := a.openAddServer(); err != nil {
		t.Fatalf("add a server: %v", err)
	}
	f := awaitModal(t, a, "the add dialog", byTitle[*ui.Form]("Add Server"))

	got := f.Field(fldKeyFile).Options
	if !slices.Contains(got, "/home/marcus/.ssh/id_work") {
		t.Errorf("the field offers %v, want the key the window keeps", got)
	}
	if len(got) == 0 || got[0] != "" {
		t.Errorf("the field offers %v, want the empty answer first", got)
	}
}

// A key named on a server goes into the list, so the next server can be
// given it without typing the path again.
func TestAKeyNamedOnAServerIsKept(t *testing.T) {
	a := aKeyWindow(t)

	if err := a.openAddServer(); err != nil {
		t.Fatalf("add a server: %v", err)
	}
	f := awaitModal(t, a, "the add dialog", byTitle[*ui.Form]("Add Server"))
	typeIntoField(t, a, f, fldName, "margit")
	typeIntoField(t, a, f, fldServer, "margit.example")
	typeIntoField(t, a, f, fldKeyFile, "/home/marcus/.ssh/id_work")
	pressButton(t, a, f, btnSave)

	if got := a.keyFiles.all(); !slices.Contains(got, "/home/marcus/.ssh/id_work") {
		t.Errorf("the window keeps %v, want the key the server was given", got)
	}
}

// The list survives a restart: it is what the dialog offers on the next
// run too.
func TestTheKeptKeysSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
	}
	at := filepath.Join(t.TempDir(), "id_ed25519")
	if _, err := remote.MakeKey(at, "", ""); err != nil {
		t.Fatalf("make a key: %v", err)
	}
	if err := a.keyFiles.keep(at); err != nil {
		t.Fatalf("keep: %v", err)
	}

	again := newTestApp(t, 80, 24)
	withDialogs(t, again)
	if _, err := withSettings(t, again, path); err != nil {
		t.Fatalf("the second window's settings: %v", err)
	}

	if got := again.keyFiles.all(); !slices.Equal(got, []string{at}) {
		t.Errorf("the second window keeps %v", got)
	}
}

// Keeping one already kept moves it to the front rather than doubling
// it, so the list the dialog offers has no line twice.
func TestKeepingAKeyAgainMovesItToTheFront(t *testing.T) {
	a := aKeyWindow(t)
	for _, at := range []string{"/one", "/two", "/three"} {
		if err := a.keyFiles.keep(at); err != nil {
			t.Fatalf("keep %q: %v", at, err)
		}
	}

	if err := a.keyFiles.keep("/one"); err != nil {
		t.Fatalf("keep it again: %v", err)
	}

	if got, want := a.keyFiles.all(), []string{"/one", "/three", "/two"}; !slices.Equal(got, want) {
		t.Errorf("the window keeps %v, want %v", got, want)
	}
}

// The passphrase is not on screen as it is typed.
func TestThePassphraseIsMasked(t *testing.T) {
	a := aKeyWindow(t)

	f := makeKeyDialog(t, a, filepath.Join(t.TempDir(), "id_ed25519"))
	typeIntoField(t, a, f, fldPassphrase, "let me in")

	for _, label := range []string{"Passphrase", "Confirm passphrase"} {
		if f.Field(label).Mask == 0 {
			t.Errorf("the %q field shows what is typed into it", label)
		}
	}
}

// A passphrase typed twice differently is refused, because the key
// cannot be written again at the same path to correct it.
func TestTwoDifferentPassphrasesAreRefused(t *testing.T) {
	a := aKeyWindow(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")

	f := makeKeyDialog(t, a, at)
	typeIntoField(t, a, f, fldPassphrase, "let me in")
	typeIntoField(t, a, f, fldConfirmPass, "let me nn")
	pressButton(t, a, f, btnCreate)

	if a.root.Modal() != f {
		t.Fatal("the dialog closed on two passphrases that differ")
	}
	if f.Error() == nil || !strings.Contains(f.Error().Error(), "do not match") {
		t.Errorf("it said %v", f.Error())
	}
	if _, err := os.Stat(at); err == nil {
		t.Error("it wrote a key anyway")
	}
}

// The passphrase reaches the key, so one made with it cannot be opened
// without it.
func TestThePassphraseReachesTheKey(t *testing.T) {
	a := aKeyWindow(t)
	at := filepath.Join(t.TempDir(), "id_ed25519")

	f := makeKeyDialog(t, a, at)
	typeIntoField(t, a, f, fldPassphrase, "let me in")
	typeIntoField(t, a, f, fldConfirmPass, "let me in")
	pressButton(t, a, f, btnCreate)

	raw, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if _, err := ssh.ParsePrivateKey(raw); err == nil {
		t.Fatal("the key opened with no passphrase")
	}
	if _, err := ssh.ParsePrivateKeyWithPassphrase(raw, []byte("let me in")); err != nil {
		t.Errorf("the key would not open with what was typed: %v", err)
	}
}

// A key that was made is said so even when it could not be added to the
// list: the key is on disk and the line to paste is what the user came
// for.
func TestAKeyThatCannotBeKeptIsStillReported(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	at := filepath.Join(t.TempDir(), "id_ed25519")
	a.keyFiles.remember(settings.Unusable(errors.New("the settings file is unreadable")))

	f := makeKeyDialog(t, a, at)
	pressButton(t, a, f, btnCreate)

	if _, err := os.Stat(at); err != nil {
		t.Fatalf("the key is not there: %v", err)
	}
	// The reason it was not remembered is on top, and what to do with
	// the key is under it.
	awaitModal(t, a, "the reason it was not remembered",
		byTitle[*ui.Notice]("Key created, but not added to the list"))
	// Put away, rather than dismissNotice: what to do with the key is
	// under it and stays.
	if _, err := a.root.HandleKey(press(input.KeyEscape, 0)); err != nil {
		t.Fatalf("Escape: %v", err)
	}
	awaitModal(t, a, "what to do with the key", func(got *ui.Notice) bool {
		return got.Title == "Key created"
	})
}

// The instructions name the private half only as the label saying where
// it went.
func TestTheInstructionsNameThePublicHalfOnly(t *testing.T) {
	at := filepath.Join(t.TempDir(), "id_ed25519")
	key, err := remote.MakeKey(at, "", "")
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}

	got, err := installKeyLines(key, false)
	if err != nil {
		t.Fatalf("say how to install it: %v", err)
	}

	// The private half is named once, as a label saying where it went.
	// Every other line is an instruction to follow, and one naming the
	// private half would have the user paste the wrong file.
	for line := range strings.SplitSeq(got, "\n") {
		if strings.HasPrefix(line, "Private key:") {
			continue
		}
		if strings.Contains(line, key.Path) && !strings.Contains(line, key.Pub) {
			t.Errorf("an instruction names the private half: %q", line)
		}
	}
	if !strings.Contains(got, "readable by its owner alone") {
		t.Errorf("it says nothing about what sshd insists on:\n%s", got)
	}
}

// The list is capped, and it is the one kept longest ago that goes.
func TestTheKeptKeysAreCapped(t *testing.T) {
	a := aKeyWindow(t)
	for i := range mostKeptKeys + 3 {
		if err := a.keyFiles.keep("/key" + strconv.Itoa(i)); err != nil {
			t.Fatalf("keep %d: %v", i, err)
		}
	}

	got := a.keyFiles.all()
	if len(got) != mostKeptKeys {
		t.Fatalf("the window keeps %d keys, want %d", len(got), mostKeptKeys)
	}
	if slices.Contains(got, "/key0") {
		t.Errorf("the one kept longest ago is still there: %v", got)
	}
}

// A server saved without touching its key file does not put that key in
// the list: the user chose nothing.
func TestSavingAServerWithoutTouchingItsKeyKeepsNothing(t *testing.T) {
	a := aKeyWindow(t)
	if err := a.book.Put(remote.Host{
		Name: "margit", Address: "margit.example", Identities: []string{"/already/there"},
	}, ""); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := a.openEditServer("margit"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal(t, a, "the edit dialog", byTitle[*ui.Form]("Edit margit"))
	pressButton(t, a, f, btnSave)

	if got := a.keyFiles.all(); len(got) != 0 {
		t.Errorf("the window keeps %v, and the key field was not touched", got)
	}
}
