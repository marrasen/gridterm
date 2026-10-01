package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/secrets"
	"github.com/marrasen/kakel/settings"
)

// run runs f on a goroutine, as the dial does, and waits for what it
// gives while the program's goroutine runs.
func run(t *testing.T, a *app, f func() string) string {
	t.Helper()
	got := make(chan string, 1)
	go func() { got <- f() }()
	return await(t, a, got)
}

// await waits for what got gives while the program's goroutine runs.
func await(t *testing.T, a *app, got chan string) string {
	t.Helper()
	var s string
	waitFor(t, a, "the answer", func() bool {
		select {
		case s = <-got:
			return true
		default:
			return false
		}
	})
	return s
}

// A password the secrets keep for a login signs in without asking.
func TestASavedPasswordSignsInWithoutAsking(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	if _, err := a.secrets.Put(secrets.Item{Name: "web", Logins: []string{"me@web.example"}}, "hunter2"); err != nil {
		t.Fatal(err)
	}
	q := newAsker(a, "web").keeping(&signIns{})
	pass := run(t, a, func() string { p, _ := q.Password(t.Context(), "me", "web.example"); return p })
	if pass != "hunter2" || len(a.st.Asks) != 0 {
		t.Fatalf("signed in with %q, asking %+v", pass, a.st.Asks)
	}
	// Refused, it asks, saying so.
	go func() { _, _ = q.Password(context.Background(), "me", "web.example") }()
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	if got := a.st.Asks[0]; got.Text != "Invalid password." || len(got.Saved) != 1 || got.Also == "" {
		t.Fatalf("asked again %+v", got)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
}

// A password typed with its box ticked is kept once the connection
// goes through, for that login.
func TestATypedPasswordIsKeptForItsLogin(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	kept := &signIns{}
	q := newAsker(a, "srv").keeping(kept)
	got := make(chan string, 1)
	go func() { p, _ := q.Password(t.Context(), "me", "srv.example"); got <- p }()
	answer(t, a, "Sign in to me@srv.example", true, "s3cret", "yes")
	if p := await(t, a, got); p != "s3cret" {
		t.Fatalf("signed in with %q", p)
	}
	a.keepSignIns(kept)
	if pass, err := a.secrets.PasswordFor("me@srv.example"); err != nil || pass != "s3cret" {
		t.Fatalf("kept %q, %v", pass, err)
	}
}

// A saved secret picked unlocks a key, and from then on unlocks it
// without asking.
func TestAPickedSecretUnlocksAKeyFromThenOn(t *testing.T) {
	a, keyFile := secretsApp(t)
	startVault(t, a)
	if _, err := a.secrets.Put(secrets.Item{Name: "my usual"}, "opensesame"); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(filepath.Dir(keyFile), "locked_ed25519")
	kept := &signIns{}
	q := newAsker(a, "srv").keeping(kept)
	got := make(chan string, 1)
	go func() { p, _ := q.Passphrase(t.Context(), remote.LockedKey{Path: locked}); got <- p }()
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	ask := a.st.Asks[0]
	i := slices.Index(ask.Saved, "my usual")
	if i < 0 {
		t.Fatalf("the question offers %v", ask.Saved)
	}
	a.handle(AskAnswered{ID: ask.ID, Yes: true, Answers: []string{"", "", itoa(i)}})
	if p := await(t, a, got); p != "opensesame" {
		t.Fatalf("unlocked with %q", p)
	}
	a.keepSignIns(kept)
	if pass, err := a.secrets.PassphraseFor(locked); err != nil || pass != "opensesame" {
		t.Fatalf("the key's secret is %q, %v", pass, err)
	}
	// The next time, nothing is asked.
	pass := run(t, a, func() string {
		p, _ := newAsker(a, "srv").keeping(&signIns{}).Passphrase(t.Context(), remote.LockedKey{Path: locked})
		return p
	})
	if pass != "opensesame" || len(a.st.Asks) != 0 {
		t.Fatalf("the next time it gave %q, asking %+v", pass, a.st.Asks)
	}
}

// A password typed for a login a shared secret answers for leaves the
// shared one as it is: this login gets a secret of its own.
func TestATypedPasswordLeavesASharedSecretAlone(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	shared, err := a.secrets.Put(secrets.Item{Name: "work", Logins: []string{"me@a.example", "me@b.example"}}, "old")
	if err != nil {
		t.Fatal(err)
	}
	a.keepSignIns(&signIns{list: []signIn{{login: "me@b.example", user: "me", value: "new"}}})
	if got, _ := a.secrets.Secret(shared.ID); got != "old" {
		t.Fatalf("the shared secret became %q", got)
	}
	if got, _ := a.secrets.PasswordFor("me@a.example"); got != "old" {
		t.Fatalf("the other login signs in with %q", got)
	}
	if got, _ := a.secrets.PasswordFor("me@b.example"); got != "new" {
		t.Fatalf("this login signs in with %q", got)
	}
}

// A secret picked that already unlocks another key is refused before
// anything changes.
func TestAPickedSecretForAnotherKeyChangesNothing(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	mine, err := a.secrets.Put(secrets.Item{Name: "mine", Kind: secrets.Passphrase, File: "/k/one"}, "a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.secrets.Put(secrets.Item{Name: "other", File: "/k/two"}, "b")
	if err != nil {
		t.Fatal(err)
	}
	_ = mine
	if _, err := keepSignIn(a.secrets, signIn{file: "/k/one", picked: other.ID}); err == nil {
		t.Fatal("a secret for another key was linked")
	}
	if got, _ := a.secrets.PassphraseFor("/k/one"); got != "a" {
		t.Fatalf("the key lost its secret: %q", got)
	}
}

// A key whose passphrase the locked secrets hold asks for the secrets to
// be unlocked, and is unlocked from them, rather than asking for the
// key's own passphrase.
func TestLockedSecretsHoldingAKeysPassphraseAreAskedToOpen(t *testing.T) {
	a, keyFile := secretsApp(t)
	a.settings = mustSettings(t)
	startVault(t, a)
	locked := filepath.Join(filepath.Dir(keyFile), "id_rsa")
	if _, err := a.secrets.Put(secrets.Item{Name: "rsa", Kind: secrets.Passphrase, File: locked}, "s3cret"); err != nil {
		t.Fatal(err)
	}
	if err := a.secrets.AddPassphrase("correct horse"); err != nil {
		t.Fatal(err)
	}
	a.showVault()
	if hints, known := a.settings.SecretHints(); !known || !slices.Contains(hints, hintOf("key", locked)) {
		t.Fatalf("the hints are %v, known %v", hints, known)
	}
	a.handle(LockSecrets{})
	a.ring.Lock()
	if err := os.Rename(keyFile, keyFile+".gone"); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	go func() {
		p, _ := newAsker(a, "srv").keeping(&signIns{}).Passphrase(t.Context(), remote.LockedKey{Path: locked})
		got <- p
	}()
	answer(t, a, "Unlock Secrets", true, "correct horse")
	if p := await(t, a, got); p != "s3cret" {
		t.Fatalf("the key was unlocked with %q", p)
	}
	// A key the secrets hold nothing for asks for its passphrase.
	a.handle(LockSecrets{})
	other := filepath.Join(filepath.Dir(keyFile), "id_other")
	go func() {
		p, _ := newAsker(a, "srv").keeping(&signIns{}).Passphrase(t.Context(), remote.LockedKey{Path: other})
		got <- p
	}()
	answer(t, a, "Unlock your key", true, "typed", "")
	if p := await(t, a, got); p != "typed" {
		t.Fatalf("the other key was unlocked with %q", p)
	}
}

// mustSettings is settings of a test's own.
func mustSettings(t *testing.T) *settings.Settings {
	t.Helper()
	s, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
