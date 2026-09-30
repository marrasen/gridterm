package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/screen"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/secrets"
)

// secretsApp is the program side with a home of its own, holding an
// ed25519 key at ~/.ssh/id_ed25519 and no vault yet.
func secretsApp(t *testing.T) (a *app, keyFile string) {
	t.Helper()
	home := testhome.New(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	keyFile = filepath.Join(home, ".ssh", "id_ed25519")
	writeKey(t, keyFile)
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a = newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	a.secretsAt = filepath.Join(home, "secrets.json")
	return a, keyFile
}

// answer answers the question waiting, once there is one.
func answer(t *testing.T, a *app, title string, yes bool, answers ...string) {
	t.Helper()
	waitFor(t, a, "the question "+title, func() bool { return len(a.st.Asks) > 0 })
	q := a.st.Asks[0]
	if q.Title != title {
		t.Fatalf("the question is %q, want %q", q.Title, title)
	}
	a.handle(AskAnswered{ID: q.ID, Yes: yes, Answers: answers})
}

// startVault starts the secrets on the key, as the user would.
func startVault(t *testing.T, a *app) {
	t.Helper()
	a.handle(ShowSecrets{})
	answer(t, a, "No secrets yet", true)
	waitFor(t, a, "the secrets pane", func() bool { return a.kindOfPane(a.st.Focus) == KindSecrets })
}

func TestTheSecretsStartOnTheUsualKey(t *testing.T) {
	a, keyFile := secretsApp(t)
	startVault(t, a)
	s := a.st.Secrets
	if !s.Exists || !s.Open || len(s.Keys) != 1 || s.Keys[0].Name != keyFile {
		t.Fatalf("started, the secrets read %+v", s)
	}
}

func TestASecretIsKeptAndOnlyItsNameIsShown(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	a.handle(PutSecret{Name: "db", User: "admin", Kind: secrets.Password, Value: "hunter2"})
	items := a.st.Secrets.Items
	if len(items) != 1 || items[0].Name != "db" || items[0].User != "admin" {
		t.Fatalf("kept, the items are %+v", items)
	}
	if strings.Contains(strings.Join([]string{items[0].ID, items[0].Name, items[0].User, items[0].File, string(items[0].Kind)}, " "), "hunter2") {
		t.Fatal("the window was sent the secret itself")
	}
	id := items[0].ID

	a.handle(CopySecret{ID: id})
	n := a.st.Notices[len(a.st.Notices)-1]
	if n.Clipboard != "hunter2" || !n.Forget || strings.Contains(n.Title+n.Body, "hunter2") {
		t.Fatalf("copying said %+v", n)
	}
	// And it reaches the window in front, which is what puts it on the
	// clipboard: a window shows only the notices meant for it.
	if got := a.stateFor(a.cur, a.st).Notices; len(got) == 0 || got[len(got)-1].Clipboard != "hunter2" {
		t.Fatalf("the window in front was shown %+v", got)
	}

	a.handle(PutSecret{ID: id, Name: "database", User: "admin", Kind: secrets.Password})
	if got := a.st.Secrets.Items; len(got) != 1 || got[0].Name != "database" || got[0].ID != id {
		t.Fatalf("renamed, the items are %+v", got)
	}
	a.handle(RevealSecret{ID: id})
	waitFor(t, a, "the secret shown", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "database" || q.Text != "hunter2" || !q.Plain {
		t.Fatalf("shown, the dialog is %+v; renaming should keep the value", q)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true})

	a.handle(RemoveSecret{ID: id})
	if got := a.st.Secrets.Items; len(got) != 0 {
		t.Fatalf("removed, the items are %+v", got)
	}
}

func TestASecretIsTypedIntoTheTerminalUsedLast(t *testing.T) {
	a, _ := secretsApp(t)
	sess := sessiontest.New()
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	a.next++ // the pane takes a number, as the program's own do
	a.addPane(Pane{ID: "p1", Title: "Terminal 1"}, screen.Open(sess, a.palette, quiet), Placement{})
	t.Cleanup(func() { a.remove("p1") })
	a.lastTerminal = "p1"
	startVault(t, a)
	a.handle(PutSecret{Name: "db", Kind: secrets.Password, Value: "hunter2"})
	a.handle(TypeSecret{ID: a.st.Secrets.Items[0].ID})
	waitFor(t, a, "the secret typed", func() bool { return sess.Sent() == "hunter2" })
	if a.st.Focus != "p1" {
		t.Fatalf("typed, the focus is on %q, want the terminal", a.st.Focus)
	}
}

func TestLockedSecretsShowNoNames(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	a.handle(PutSecret{Name: "db", Kind: secrets.Password, Value: "hunter2"})
	a.handle(LockSecrets{})
	if s := a.st.Secrets; s.Open || len(s.Items) != 0 || len(s.Keys) != 0 {
		t.Fatalf("locked, the secrets read %+v", s)
	}
	// The key is in the ring, so unlocking needs nothing asked.
	a.handle(UnlockSecrets{})
	if s := a.st.Secrets; !s.Open || len(s.Items) != 1 {
		t.Fatalf("unlocked, the secrets read %+v", s)
	}
}

func TestAPassphraseOpensTheSecretsWhenTheirKeyIsGone(t *testing.T) {
	a, keyFile := secretsApp(t)
	startVault(t, a)
	a.handle(PutSecret{Name: "db", Kind: secrets.Password, Value: "hunter2"})
	if err := a.secrets.AddPassphrase("correct horse"); err != nil {
		t.Fatal(err)
	}
	a.handle(LockSecrets{})
	a.ring.Lock()
	if err := os.Rename(keyFile, keyFile+".gone"); err != nil {
		t.Fatal(err)
	}
	a.handle(UnlockSecrets{})
	answer(t, a, "Unlock Secrets", true, "wrong horse")
	waitFor(t, a, "a second try", func() bool { return len(a.st.Asks) > 0 })
	if !strings.Contains(a.st.Asks[0].Text, "did not open") {
		t.Fatalf("after a wrong passphrase, the question says %q", a.st.Asks[0].Text)
	}
	answer(t, a, "Unlock Secrets", true, "correct horse")
	waitFor(t, a, "the secrets open", func() bool { return a.st.Secrets.Open })
	if len(a.st.Secrets.Items) != 1 {
		t.Fatalf("opened by passphrase, the items are %+v", a.st.Secrets.Items)
	}
}

// writeKey writes a new ed25519 key, with no passphrase, at path.
func writeKey(t *testing.T, path string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(sshPub), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestASecondKeyOpensTheSecretsAndTheLastStays(t *testing.T) {
	a, keyFile := secretsApp(t)
	startVault(t, a)
	work := filepath.Join(filepath.Dir(keyFile), "work_ed25519")
	writeKey(t, work)
	a.handle(AddSecretsKey{})
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	if q := a.st.Asks[0]; len(q.Choose) != 1 || q.Choose[0] != "work_ed25519" {
		t.Fatalf("adding a key offers %+v, want work_ed25519 alone", q.Choose)
	}
	answer(t, a, "Add Secrets Key", true, "work_ed25519")
	waitFor(t, a, "a second key", func() bool { return len(a.st.Secrets.Keys) == 2 })
	first := a.st.Secrets.Keys[0]
	if first.Removing != "Another key on this computer still unlocks the secrets." {
		t.Fatalf("with two keys here, removing one says %q", first.Removing)
	}
	a.handle(RemoveSecretsKey{Fingerprint: first.Fingerprint})
	if keys := a.st.Secrets.Keys; len(keys) != 1 || keys[0].Name != work {
		t.Fatalf("after removing the first, the keys are %+v", keys)
	}
	a.handle(RemoveSecretsKey{Fingerprint: a.st.Secrets.Keys[0].Fingerprint})
	if len(a.st.Secrets.Keys) != 1 {
		t.Fatal("the last key was removed")
	}
}

func TestAPassphraseIsAddedOnce(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	a.handle(AddSecretsPassphrase{Passphrase: "correct horse"})
	waitFor(t, a, "the passphrase", func() bool { return a.st.Secrets.Passphrase })
	if keys := a.st.Secrets.Keys; len(keys) != 2 || !keys[1].Passphrase {
		t.Fatalf("with a passphrase, the ways in are %+v", keys)
	}
	notices := len(a.st.Notices)
	a.handle(AddSecretsPassphrase{Passphrase: "another"})
	if len(a.st.Notices) != notices+1 || !strings.Contains(a.st.Notices[notices].Title, "already") {
		t.Fatalf("a second passphrase said %+v", a.st.Notices[notices:])
	}
}

func TestAKeysSavedPassphraseIsUsedWithoutAsking(t *testing.T) {
	a, keyFile := secretsApp(t)
	startVault(t, a)
	locked := filepath.Join(filepath.Dir(keyFile), "locked_ed25519")
	if _, err := a.secrets.Put(secrets.Item{Name: "locked key", Kind: secrets.Passphrase, File: locked}, "s3cret"); err != nil {
		t.Fatal(err)
	}
	a.handle(LockSecrets{})
	got := make(chan string, 1)
	go func() {
		pass, _ := newAsker(a, "").Passphrase(t.Context(), remote.LockedKey{Path: locked})
		got <- pass
	}()
	var pass string
	waitFor(t, a, "the saved passphrase", func() bool {
		select {
		case pass = <-got:
			return true
		default:
			return false
		}
	})
	if pass != "s3cret" || len(a.st.Asks) != 0 {
		t.Fatalf("the passphrase came back %q, with questions %+v", pass, a.st.Asks)
	}
}

func TestSecretsGoOutToACSVFileAndComeBackIn(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	a.handle(PutSecret{Name: "db", User: "admin", Kind: secrets.Password, Value: "hunter2"})
	a.handle(PutSecret{Name: "codes", Kind: secrets.Note, Value: "1234\n5678"})
	a.handle(ExportSecrets{Path: "~/out.csv"})
	at := filepath.Join(os.Getenv("HOME"), "out.csv")
	// Asked first, naming the file, and on Cancel until answered.
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "Export every secret to "+at+"?" || !q.Careful {
		t.Fatalf("the question is %+v", q)
	}
	answer(t, a, "Export every secret to "+at+"?", true)
	waitFor(t, a, "the file", func() bool { _, err := os.Stat(at); return err == nil })
	info, err := os.Stat(at)
	if err != nil {
		t.Fatalf("exported, the file: %v", err)
	}
	// On Windows the mode shows only whether the file is read-only.
	// Who may read it is the folder's access list, which in the user's
	// profile is the user's alone.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("the file can be read by others: %v", info.Mode())
	}
	// A file there already is refused before anything is asked.
	notices := len(a.st.Notices)
	a.handle(ExportSecrets{Path: at})
	if len(a.st.Asks) != 0 {
		t.Fatalf("with the file there, it asks %+v", a.st.Asks)
	}
	if last := a.st.Notices[len(a.st.Notices)-1]; len(a.st.Notices) != notices+1 || !strings.Contains(last.Body, "already there") {
		t.Fatalf("exporting over the file said %+v", last)
	}

	// Into secrets of their own, on another machine.
	b, _ := secretsApp(t)
	startVault(t, b)
	b.handle(ImportSecrets{Path: at, Duplicates: KeepBoth})
	names := map[string]bool{}
	for _, it := range b.st.Secrets.Items {
		names[it.Name] = true
	}
	if len(b.st.Secrets.Items) != 2 || !names["db"] || !names["codes"] {
		t.Fatalf("imported, the items are %+v", b.st.Secrets.Items)
	}
	b.handle(ImportSecrets{Path: at, Duplicates: SkipThem})
	if n := len(b.st.Secrets.Items); n != 2 {
		t.Fatalf("imported again, skipping, there are %d", n)
	}
	if last := b.st.Notices[len(b.st.Notices)-1]; !strings.Contains(last.Body, "0 secrets read in, 2 left as they were") {
		t.Fatalf("the second import said %q", last.Body)
	}
}

// A secret still on the clipboard as the window closes is taken off it;
// something copied since is left.
func TestASecretIsTakenOffTheClipboardAtExit(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	a.handle(PutSecret{Name: "db", Kind: secrets.Password, Value: "hunter2"})
	board := ""
	wasRead, wasWrite := readClipboard, writeClipboard
	readClipboard = func() (string, error) { return board, nil }
	writeClipboard = func(s string) error { board = s; return nil }
	t.Cleanup(func() { readClipboard, writeClipboard = wasRead, wasWrite })

	a.handle(CopySecret{ID: a.st.Secrets.Items[0].ID})
	board = "hunter2" // as the window put it there
	a.exitNow()
	if board != "" {
		t.Fatalf("closed, the clipboard holds %q", board)
	}
	a.copied, a.copiedAt, board = "hunter2", time.Now(), "something else"
	a.takeSecretBack()
	if board != "something else" {
		t.Fatalf("what was copied since became %q", board)
	}
}

// An open secrets pane shows what changed behind its back, such as a
// secret another window kept.
func TestTheSecretsPaneReadsTheVaultAgain(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	if _, err := a.secrets.Put(secrets.Item{Name: "from elsewhere", Kind: secrets.Password}, "x"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the secret to show", func() bool { return len(a.st.Secrets.Items) == 1 })
}

func TestSeveralSecretsAreRemovedAtOnce(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	for _, name := range []string{"one", "two", "three"} {
		a.handle(PutSecret{Name: name, Kind: secrets.Password, Value: "x"})
	}
	ids := []string{a.st.Secrets.Items[0].ID, a.st.Secrets.Items[1].ID}
	a.handle(RemoveSecrets{IDs: ids})
	if len(a.st.Secrets.Items) != 1 {
		t.Fatalf("two removed, %d are left", len(a.st.Secrets.Items))
	}
}

// Saving, changing and removing a secret each say so; one removed from
// somewhere else is said as that; removing several carries on past one
// already gone.
func TestWhatIsDoneToASecretIsSaid(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	said := func(title string) bool {
		return slices.ContainsFunc(a.st.Notices, func(n Notice) bool { return n.Title == title })
	}
	a.handle(PutSecret{Name: "db", Kind: secrets.Password, Value: "hunter2"})
	a.handle(PutSecret{Name: "web", Kind: secrets.Password, Value: "swordfish"})
	a.handle(PutSecret{Name: "mail", Kind: secrets.Password, Value: "letmein"})
	if !said("db saved") {
		t.Fatalf("saved, it said %+v", a.st.Notices)
	}
	items := a.st.Secrets.Items
	a.handle(PutSecret{ID: items[0].ID, Name: "database", User: "admin", Kind: secrets.Password})
	if !said("database changed") {
		t.Fatalf("changed, it said %+v", a.st.Notices)
	}
	notices := len(a.st.Notices)
	a.handle(PutSecret{ID: items[0].ID, Name: "database", User: "admin", Kind: secrets.Password})
	if len(a.st.Notices) != notices {
		t.Fatalf("changing nothing, it said %+v", a.st.Notices[notices:])
	}
	a.handle(RemoveSecret{ID: items[1].ID})
	if !said(items[1].Name + " removed") {
		t.Fatalf("removed, it said %+v", a.st.Notices)
	}
	a.handle(RemoveSecret{ID: items[1].ID})
	if !said("That secret was removed from somewhere else already") {
		t.Fatalf("removing one gone, it said %+v", a.st.Notices)
	}
	a.handle(CopySecret{ID: items[1].ID})
	if !slices.ContainsFunc(a.st.Notices, func(n Notice) bool { return n.Body == "That secret has been removed from somewhere else." }) {
		t.Fatalf("copying one gone, it said %+v", a.st.Notices)
	}
	a.handle(RemoveSecrets{IDs: []string{items[1].ID, items[0].ID, items[2].ID}})
	if len(a.st.Secrets.Items) != 0 || !said("3 secrets removed") {
		t.Fatalf("removing three, one gone already, leaves %+v and said %+v", a.st.Secrets.Items, a.st.Notices)
	}
}

// Keys to choose between are told apart where two share a file's name.
func TestKeysToChooseAreToldApart(t *testing.T) {
	got := keyChoices(nil, []string{"/home/me/.ssh/id_ed25519", "/home/me/work/id_ed25519", "/home/me/.ssh/deploy"})
	want := []string{"/home/me/.ssh/id_ed25519", "/home/me/work/id_ed25519", "deploy"}
	if !slices.Equal(got, want) {
		t.Fatalf("the choices are %q", got)
	}
}

// Open Secrets Window gives the secrets a window of their own; asked
// for again from another window, they stay there, and that window comes
// to the front.
func TestTheSecretsGetAWindowOfTheirOwn(t *testing.T) {
	a, _ := secretsApp(t)
	a.next = 100
	a.addPane(Pane{ID: "p1", Kind: KindFiles}, nil, Placement{})
	startVault(t, a)
	first := a.cur
	var opened *gunim.Window
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size) (gunim.Client, *gunim.Window, error) {
		opened = gunimtest.New(t, s, nil)
		return opened.Client(), nil, nil
	}
	a.handle(ToolWindow{Kind: KindSecrets, Size: geom.Sz(400, 300)})
	waitFor(t, a, "the window for the secrets", func() bool { return len(a.wins) == 2 })
	id := a.st.Focus
	own := a.ownerOf(id)
	if a.kindOfPane(id) != KindSecrets || own == first || len(a.panesIn(own)) != 1 {
		t.Fatalf("the secrets are %q in window %d with %d panes", id, own.id, len(a.panesIn(own)))
	}
	a.front(first)
	a.handle(ShowSecrets{})
	if a.ownerOf(id) != own || a.cur != own || opened.Offscreen().Raised() != 1 {
		t.Fatalf("asked for again, the secrets are in window %d, window %d in front, raised %d times", a.ownerOf(id).id, a.cur.id, opened.Offscreen().Raised())
	}
}
