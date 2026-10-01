package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/secrets"
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
