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

// openDialog runs the pump until a dialog is on the stack and returns it.
func openDialog(t *testing.T, a *testApp) *ui.Form {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		if f, ok := a.root.Modal().(*ui.Form); ok {
			return f
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no dialog opened")
	return nil
}

// answer waits for a dialog, types into its fields and presses a button
// by name.
func answer(t *testing.T, a *testApp, button string, values ...string) {
	t.Helper()
	f := openDialog(t, a)
	for i, v := range values {
		if i > 0 {
			a.root.HandleKey(press(input.KeyTab, 0))
		}
		for _, r := range v {
			a.root.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
		}
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
			a.root.HandleKey(press(input.KeyEnter, 0))
			a.pump.run()
			return
		}
		a.root.HandleKey(press(input.KeyTab, 0))
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

	answer(t, a, "Unlock", "let me in")
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

	go ask.Passphrase(a.ctx, "/home/marcus/.ssh/id_ed25519")
	f := openDialog(t, a)
	for _, r := range "hunter2" {
		a.root.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}

	fld := f.Fields()[0]
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

	openDialog(t, a)
	a.root.HandleKey(press(input.KeyEscape, 0))
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

	openDialog(t, a)
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
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		if a.root.Modal() == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the dialog was left open after the connection was cancelled")
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

	f := openDialog(t, a)
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

	f := openDialog(t, a)
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

	f := openDialog(t, a)
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
	if f.Fields()[0].Mask == 0 {
		t.Error("an answer the server did not mark as echoed was not masked")
	}
	if f.Fields()[1].Mask != 0 {
		t.Error("an answer the server marked as echoed was masked")
	}
	answer(t, a, "Answer", "123456", "marcus")

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

	f := openDialog(t, a)
	const secret = "hunter2"
	for _, r := range secret {
		a.root.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}

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
