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
	answer(t, a, "Unlock your secrets", true, "correct horse")
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
	answer(t, a, "Unlock SSH key", true, "typed", "")
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

// A key is named by its file, its folder said only where it is not
// ~/.ssh.
func TestAKeyIsNamedByItsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got := keyFolder(filepath.Join(home, ".ssh", "id_rsa")); got != "" {
		t.Fatalf("a key in ~/.ssh says %q", got)
	}
	if got, want := keyFolder(filepath.Join(home, "keys", "work", "id_rsa")), filepath.Join("~", "keys", "work"); got != want {
		t.Fatalf("a key elsewhere says %q, want %q", got, want)
	}
}

// A key's note says the comment it was made with, from its public half.
func TestAKeysNoteSaysItsComment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	key := filepath.Join(home, ".ssh", "id_rsa")
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key+".pub", []byte("ssh-rsa AAAAB3Nza marcus at laptop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := keyNote(key); got != "marcus at laptop" {
		t.Fatalf("the note says %q", got)
	}
	elsewhere := filepath.Join(home, "keys", "id_rsa")
	if got := keyNote(elsewhere); got != filepath.Join("~", "keys") {
		t.Fatalf("a key elsewhere with no public half says %q", got)
	}
}

// Locking the secrets forgets the key that opens them, so nothing opens
// them again without asking: not a look for a saved password, not the
// next use.
func TestLockedSecretsStayLocked(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	if a.vaultInHand() == nil {
		t.Fatal("the open secrets are not in hand")
	}
	a.handle(LockSecrets{})
	if a.vaultInHand() != nil || !a.secrets.Locked() {
		t.Fatal("locked, the secrets opened again without asking")
	}
}

// A private key already there goes on the saved keys' list; a file
// that isn't one is refused.
func TestAnExistingKeyIsAddedToSavedKeys(t *testing.T) {
	a, keyFile := secretsApp(t)
	a.settings = mustSettings(t)
	a.handle(AddSavedKey{Path: keyFile})
	if !slices.Contains(a.st.KeyFiles, keyFile) {
		t.Fatalf("the saved keys are %v", a.st.KeyFiles)
	}
	if !slices.Contains(KeysHere(), keyFile) {
		t.Fatalf("the keys here are %v", KeysHere())
	}
	not := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(not, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.handle(AddSavedKey{Path: not})
	if slices.Contains(a.st.KeyFiles, not) {
		t.Fatal("a file that is no key was saved")
	}
	if n := a.st.Notices[len(a.st.Notices)-1]; n.Kind != NoticeFailed {
		t.Fatalf("refusing it said %+v", n)
	}
}

// A server that asks for its password through keyboard-interactive is
// answered as the plain password question is: from the secrets, by the
// login named without port 22, and typed, with the offer to keep it.
func TestAPasswordAskedInteractivelyIsALoginsPassword(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	if _, err := a.secrets.Put(secrets.Item{Name: "web", Logins: []string{"me@web.example"}}, "hunter2"); err != nil {
		t.Fatal(err)
	}
	q := newAsker(a, "web").keeping(&signIns{})
	rq := remote.Question{User: "me", Host: "web.example:22", Prompts: []string{"Password: "}, Echo: []bool{false}}
	pass := run(t, a, func() string {
		ans, _ := q.Question(t.Context(), rq)
		if len(ans) != 1 {
			return ""
		}
		return ans[0]
	})
	if pass != "hunter2" || len(a.st.Asks) != 0 {
		t.Fatalf("answered %q, asking %+v", pass, a.st.Asks)
	}

	// Typed for a login with none kept, it is kept once in.
	kept := &signIns{}
	q = newAsker(a, "srv").keeping(kept)
	got := make(chan string, 1)
	go func() {
		ans, _ := q.Question(t.Context(), remote.Question{User: "me", Host: "srv.example:22", Prompts: []string{"Password:"}, Echo: []bool{false}})
		if len(ans) == 1 {
			got <- ans[0]
		}
	}()
	answer(t, a, "Sign in to me@srv.example", true, "s3cret", "yes")
	if p := await(t, a, got); p != "s3cret" {
		t.Fatalf("answered %q", p)
	}
	a.keepSignIns(kept)
	if pass, err := a.secrets.PasswordFor("me@srv.example"); err != nil || pass != "s3cret" {
		t.Fatalf("kept %q, %v", pass, err)
	}
}

// Only a question for a password alone is one: not a code, not two
// answers, not one shown as it is typed.
func TestWhatIsAPasswordQuestion(t *testing.T) {
	for _, c := range []struct {
		rq   remote.Question
		want bool
	}{
		{remote.Question{Prompts: []string{"Password: "}, Echo: []bool{false}}, true},
		{remote.Question{Prompts: []string{"(me@host) Password:"}, Echo: []bool{false}}, true},
		{remote.Question{Prompts: []string{"Verification code: "}, Echo: []bool{false}}, false},
		{remote.Question{Prompts: []string{"One-time password: "}, Echo: []bool{false}}, false},
		{remote.Question{Prompts: []string{"New password: "}, Echo: []bool{false}}, false},
		{remote.Question{Prompts: []string{"Password: "}, Echo: []bool{true}}, false},
		{remote.Question{Prompts: []string{"Password: ", "Code: "}, Echo: []bool{false, false}}, false},
	} {
		if got := passwordQuestion(c.rq); got != c.want {
			t.Errorf("%q is a password question: %v", c.rq.Prompts, got)
		}
	}
	if got := loginHost("web.example:22"); got != "web.example" {
		t.Errorf("web.example:22 is %q", got)
	}
	if got := loginHost("web.example:2222"); got != "web.example:2222" {
		t.Errorf("web.example:2222 is %q", got)
	}
	if got := loginHost("[::1]:22"); got != "::1" {
		t.Errorf("[::1]:22 is %q", got)
	}
}
