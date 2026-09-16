package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"image/color"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
)

// waitBudget is how long a test waits for the drawing goroutine and a
// connecting one to meet. Generous on purpose: a passing test never
// waits this long, and a race build on a busy machine is slow enough to
// make a tight budget flaky rather than informative.
const waitBudget = 30 * time.Second

// withDialogs gives a test app what a dialog needs: a compositor for the
// layers, a context to cancel, and a key ring.
func withDialogs(t *testing.T, a *testApp) *askUser {
	t.Helper()
	a.comp = render.NewCompositor(nil)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.keys = remote.NewRing()
	a.ctx, a.stop = context.WithCancel(context.Background())
	t.Cleanup(a.stop)
	a.commands()
	return &askUser{app: a.app}
}

// answer waits for a dialog, fills in the fields it names and presses a
// button by name.
//
// Label and value pairs, not values in field order: adding a row to a
// dialog would otherwise swap the answers of every test after it.
func answer(t *testing.T, a *testApp, button string, fields ...string) {
	t.Helper()
	if len(fields)%2 != 0 {
		t.Fatalf("answer wants label and value pairs, got %d strings", len(fields))
	}
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	for i := 0; i < len(fields); i += 2 {
		typeIntoField(t, a, f, fields[i], fields[i+1])
	}
	pressButton(t, a, f, button)
}

// pressButton moves focus onto a named button and presses it.
func pressButton(t *testing.T, a *testApp, f *ui.Form, title string) {
	t.Helper()
	at := -1
	for i, b := range f.Buttons() {
		if b.Title == title {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the dialog has no %q button", title)
	}
	for i := 0; i < len(f.Fields())+len(f.Buttons())+1; i++ {
		if got, isButton := f.Focused(); isButton && got == at {
			sendKey(t, a, press(input.KeyEnter, 0))
			a.pump.run()
			return
		}
		sendKey(t, a, press(input.KeyTab, 0))
	}
	t.Fatalf("focus never reached the %q button", title)
}

func TestAskPassphraseReturnsWhatWasTyped(t *testing.T) {
	a := newTestApp(t, 80, 24)
	ask := withDialogs(t, a)

	got := make(chan string, 1)
	go func() {
		s, err := ask.Passphrase(a.ctx, "/home/marcus/.ssh/id_ed25519")
		if err != nil {
			t.Errorf("Passphrase: %v", err)
		}
		got <- s
	}()

	answer(t, a, "Unlock", "Passphrase", "let me in")
	select {
	case s := <-got:
		if s != "let me in" {
			t.Fatalf("passphrase = %q, want what was typed", s)
		}
	case <-time.After(waitBudget):
		t.Fatal("the dialog never answered")
	}
}

// A passphrase must not be readable over the user's shoulder.
func TestAskPassphraseIsMasked(t *testing.T) {
	a := newTestApp(t, 80, 24)
	ask := withDialogs(t, a)

	// Nobody answers this one: the test is about how the field draws, and
	// the dialog goes when the test's own context is cancelled.
	go func() { _, _ = ask.Passphrase(a.ctx, "/home/marcus/.ssh/id_ed25519") }()
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	typeIntoField(t, a, f, "Passphrase", "hunter2")

	fld := f.Field("Passphrase")
	if fld.Text() != "hunter2" {
		t.Fatalf("the field holds %q", fld.Text())
	}
	if fld.Mask == 0 {
		t.Fatal("the passphrase field draws what was typed")
	}
}

// Closing the dialog without answering is a refusal, not a goroutine
// waiting for ever.
func TestAskDismissedDialogIsARefusal(t *testing.T) {
	a := newTestApp(t, 80, 24)
	ask := withDialogs(t, a)

	got := make(chan error, 1)
	go func() {
		_, err := ask.Passphrase(a.ctx, "/key")
		got <- err
	}()

	awaitModal[*ui.Form](t, a, "a dialog", nil)
	sendKey(t, a, press(input.KeyEscape, 0))
	a.pump.run()

	select {
	case err := <-got:
		if !errors.Is(err, errDismissed) {
			t.Fatalf("error = %v, want it to say the dialog was dismissed", err)
		}
	case <-time.After(waitBudget):
		t.Fatal("dismissing the dialog left the connection waiting")
	}
}

// Closing the window has to let go of a connection sitting on a dialog,
// or quitting gridterm waits for an answer nobody will give.
func TestAskCancelledContextClosesTheDialog(t *testing.T) {
	a := newTestApp(t, 80, 24)
	ask := withDialogs(t, a)

	got := make(chan error, 1)
	go func() {
		_, err := ask.Passphrase(a.ctx, "/key")
		got <- err
	}()

	awaitModal[*ui.Form](t, a, "a dialog", nil)
	a.stop()

	select {
	case err := <-got:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want it to say the connection was cancelled", err)
		}
	case <-time.After(waitBudget):
		t.Fatal("cancelling left the connection waiting")
	}

	// And the dialog goes with it, rather than being left on screen with
	// nothing behind it.
	waitFor(t, a, "the dialog to go when the connection was cancelled", func() bool {
		return a.root.Modal() == nil
	})
}

// The fingerprint is the only thing a user can check, so it has to be on
// screen and spelled the way ssh spells it.
func TestAskHostKeyShowsTheFingerprint(t *testing.T) {
	a := newTestApp(t, 80, 24)
	ask := withDialogs(t, a)
	key := testHostKey(t)

	got := make(chan bool, 1)
	go func() {
		ok, err := ask.TrustHostKey(a.ctx, key)
		if err != nil {
			t.Errorf("TrustHostKey: %v", err)
		}
		got <- ok
	}()

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	joined := strings.Join(f.Lines, "\n")
	if !strings.Contains(joined, key.Fingerprint()) {
		t.Errorf("the dialog does not show the fingerprint: %q", joined)
	}
	if !strings.Contains(joined, key.Addr) {
		t.Errorf("the dialog does not say which host: %q", joined)
	}
	pressButton(t, a, f, "Connect")

	select {
	case ok := <-got:
		if !ok {
			t.Fatal("pressing Connect did not accept the key")
		}
	case <-time.After(waitBudget):
		t.Fatal("the dialog never answered")
	}
}

// Refusing is an answer rather than a failure: the connection stops and
// nothing tells the user they did something wrong.
func TestAskHostKeyCancelMeansNo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	ask := withDialogs(t, a)

	type result struct {
		ok  bool
		err error
	}
	got := make(chan result, 1)
	go func() {
		ok, err := ask.TrustHostKey(a.ctx, testHostKey(t))
		got <- result{ok, err}
	}()

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	pressButton(t, a, f, "Cancel")

	select {
	case r := <-got:
		if r.ok {
			t.Fatal("Cancel accepted the host key")
		}
		if r.err != nil {
			t.Fatalf("Cancel reported %v, want a plain no", r.err)
		}
	case <-time.After(waitBudget):
		t.Fatal("the dialog never answered")
	}
}

// Whatever the server asks, the answers come back in the order it asked.
func TestAskQuestionAnswersInOrder(t *testing.T) {
	a := newTestApp(t, 80, 24)
	ask := withDialogs(t, a)

	got := make(chan []string, 1)
	go func() {
		answers, err := ask.Question(a.ctx, remote.Question{
			User:        "marcus",
			Host:        "margit:22",
			Name:        "Two-factor",
			Instruction: "Check your phone",
			Prompts:     []string{"Code", "Account"},
			Echo:        []bool{false, true},
		})
		if err != nil {
			t.Errorf("Question: %v", err)
		}
		got <- answers
	}()

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	// The title is ours. A server that chose "Unlock a private key" and
	// a plausible key path would otherwise produce a dialog the user
	// cannot tell from the local one, and be handed the passphrase to
	// their private key.
	if f.Title == "Two-factor" {
		t.Error("the server chose the dialog's title")
	}
	joined := strings.Join(f.Lines, "\n")
	if !strings.Contains(joined, "marcus@margit:22") {
		t.Errorf("the dialog does not say which machine asked: %q", joined)
	}
	// The server's own wording is still shown, under ours.
	if !strings.Contains(joined, "Check your phone") {
		t.Errorf("the server's instruction was dropped: %q", joined)
	}
	// The code is a secret; the account name the server said may be shown.
	if f.Field("Code").Mask == 0 {
		t.Error("an answer the server did not mark as echoed was not masked")
	}
	if f.Field("Account").Mask != 0 {
		t.Error("an answer the server marked as echoed was masked")
	}
	answer(t, a, "Answer", "Code", "123456", "Account", "marcus")

	select {
	case answers := <-got:
		want := []string{"123456", "marcus"}
		if len(answers) != 2 || answers[0] != want[0] || answers[1] != want[1] {
			t.Fatalf("answers = %q, want %q", answers, want)
		}
	case <-time.After(waitBudget):
		t.Fatal("the dialog never answered")
	}
}

// testHostKey returns a host key to show in a dialog.
func testHostKey(t *testing.T) remote.HostKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return remote.HostKey{Addr: "margit.skalarit.net:22", Key: signer.PublicKey()}
}

// What a password dialog draws must be stars, not the password. The
// field test covers the widget; this covers the dialog the user sees.
func TestAskPasswordDrawsNothingReadable(t *testing.T) {
	a := newTestApp(t, 80, 24)
	ask := withDialogs(t, a)

	got := make(chan string, 1)
	go func() {
		s, err := ask.Password(a.ctx, "marcus", "margit.skalarit.net:22")
		if err != nil {
			t.Errorf("Password: %v", err)
		}
		got <- s
	}()

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	const secret = "hunter2"
	typeIntoField(t, a, f, "Password", secret)

	// Draw the dialog the way its own layer is drawn, and read it back.
	g := grid.New(80, 24, color.RGBA{}, color.RGBA{})
	f.Draw(g.View())
	var drawn strings.Builder
	_, rows := g.Size()
	for y := 0; y < rows; y++ {
		cols, _ := g.Size()
		for x := 0; x < cols; x++ {
			if c := g.At(x, y); c.Width != 0 {
				drawn.WriteRune(c.Rune)
			}
		}
	}
	if strings.Contains(drawn.String(), secret) {
		t.Fatal("the password was drawn on screen")
	}
	if !strings.Contains(drawn.String(), strings.Repeat("*", len(secret))) {
		t.Fatalf("the password was not drawn as stars:\n%s", drawn.String())
	}
	// And it still says whose password it wants.
	if joined := strings.Join(f.Lines, "\n"); !strings.Contains(joined, "marcus@") {
		t.Errorf("the dialog does not say whose password: %q", joined)
	}

	pressButton(t, a, f, "Sign in")
	select {
	case s := <-got:
		if s != secret {
			t.Fatalf("password = %q, want what was typed", s)
		}
	case <-time.After(waitBudget):
		t.Fatal("the dialog never answered")
	}
}
