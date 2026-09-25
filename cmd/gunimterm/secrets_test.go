package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
)

// secretsApp is the program side with a home of its own, holding an
// ed25519 key at ~/.ssh/id_ed25519 and no vault yet.
func secretsApp(t *testing.T) (a *app, keyFile string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	keyFile = filepath.Join(home, ".ssh", "id_ed25519")
	writeKey(t, keyFile)
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a = newApp(w.Client(), &shells{m: map[string]*shell{}})
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
	waitFor(t, a, "the secrets pane", func() bool { return a.kindOfPane(a.st.Focus) == kindSecrets })
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

// typed records what a pane's shell is sent.
type typed struct {
	mu   sync.Mutex
	got  []byte
	done chan struct{}
}

func (s *typed) Read([]byte) (int, error) { <-s.done; return 0, io.EOF }
func (s *typed) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, p...)
	return len(p), nil
}
func (s *typed) Close() error          { close(s.done); return nil }
func (s *typed) Resize(int, int) error { return nil }
func (s *typed) Wait() error           { return nil }
func (s *typed) sent() string          { s.mu.Lock(); defer s.mu.Unlock(); return string(s.got) }

func TestASecretIsTypedIntoTheTerminalUsedLast(t *testing.T) {
	a, _ := secretsApp(t)
	sess := &typed{done: make(chan struct{})}
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	a.addPane(Pane{ID: "p1", Title: "Terminal 1"}, openShell(sess, a.palette, quiet), placement{})
	t.Cleanup(func() { a.remove("p1") })
	a.lastTerminal = "p1"
	startVault(t, a)
	a.handle(PutSecret{Name: "db", Kind: secrets.Password, Value: "hunter2"})
	a.handle(TypeSecret{ID: a.st.Secrets.Items[0].ID})
	waitFor(t, a, "the secret typed", func() bool { return sess.sent() == "hunter2" })
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
	if first.Removing != "Another key on this machine still opens the secrets." {
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
		pass, _ := asker{a}.Passphrase(t.Context(), remote.LockedKey{Path: locked})
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
	info, err := os.Stat(at)
	if err != nil {
		t.Fatalf("exported, the file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the file can be read by others: %v", info.Mode())
	}
	notices := len(a.st.Notices)
	a.handle(ExportSecrets{Path: at})
	if last := a.st.Notices[len(a.st.Notices)-1]; len(a.st.Notices) != notices+1 || !strings.Contains(last.Body, "already there") {
		t.Fatalf("exporting over the file said %+v", last)
	}

	// Into secrets of their own, on another machine.
	b, _ := secretsApp(t)
	startVault(t, b)
	b.handle(ImportSecrets{Path: at, Duplicates: keepBoth})
	names := map[string]bool{}
	for _, it := range b.st.Secrets.Items {
		names[it.Name] = true
	}
	if len(b.st.Secrets.Items) != 2 || !names["db"] || !names["codes"] {
		t.Fatalf("imported, the items are %+v", b.st.Secrets.Items)
	}
	b.handle(ImportSecrets{Path: at, Duplicates: skipThem})
	if n := len(b.st.Secrets.Items); n != 2 {
		t.Fatalf("imported again, skipping, there are %d", n)
	}
	if last := b.st.Notices[len(b.st.Notices)-1]; !strings.Contains(last.Body, "0 secrets read in, 2 left as they were") {
		t.Fatalf("the second import said %q", last.Body)
	}
}
