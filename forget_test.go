package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// A saved machine can be edited and forgotten from its own plus menu.
//
// It is where they belong: the row is the machine, so the plus on it is
// where everything about that machine is. There was no way to remove a
// server at all.
func TestThePlusOnASavedServerOffersToForgetIt(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	saveServer(t, a, "edge", "user@edge.example:22")
	a.refreshServers()

	menu := clickPlus(t, a, "edge")

	for _, want := range []string{"server.editThis", "server.forget"} {
		if !offers(menu, want) {
			t.Errorf("the menu offers %v, with no %s", menuCommands(menu), want)
		}
	}
}

// A machine that is not in the list has nothing to forget, and says so
// rather than doing nothing.
func TestForgettingAMachineThatIsNotSavedSaysSo(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	a.actOn, a.acting = "somewhere", true

	err := a.forgetThisServer()

	if err == nil || !strings.Contains(err.Error(), "not in the server list") {
		t.Fatalf("forgetting an unsaved machine returned %v", err)
	}
}

// Closing a connection that is still being made gives up on it.
//
// It did nothing at all: a machine only reaches the list of what is
// open once it is open, so closing one that had not arrived found
// nothing, reported success and left it hanging.
func TestClosingAConnectionStillOnItsWayGivesUp(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.holdNames(&dialling{cancel: cancel, names: []string{"edge"}})
	a.actOn, a.acting = "edge", true

	if err := a.disconnectHere(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if a.opening["edge"] != nil {
		t.Error("the window is still holding the name, so nothing can try again")
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the connection was not given up on")
	}
}

// And a machine that is neither open nor on its way says so, rather
// than reporting success and changing nothing.
func TestClosingNothingSaysSo(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	a.actOn, a.acting = "edge", true

	err := a.disconnectHere()

	if err == nil || !strings.Contains(err.Error(), "nothing is connected") {
		t.Fatalf("closing nothing returned %v", err)
	}
}

// A server that says something without asking is shown, and the window
// does not wait for an answer.
//
// It is how a server that signs people in through a browser sends the
// link. The message used to be dropped, so the connection sat there
// with nothing on screen and no way to know why.
func TestAServerMessageIsShownWithoutWaiting(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	ask := &askUser{app: a.app}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		ask.Notice(ctx, remote.Notice{
			User: "rdp", Host: "marras-skylake:22",
			Instruction: "To authenticate, visit: https://login.example/a/1234",
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("showing a message waited for an answer")
	}

	f := openDialog(t, a)
	text := strings.Join(f.Lines, "\n")
	for _, want := range []string{"rdp@marras-skylake:22", "https://login.example/a/1234"} {
		if !strings.Contains(text, want) {
			t.Errorf("the message does not say %q:\n%s", want, text)
		}
	}
}

// The message goes when the connection is settled, whichever way it
// went: nobody has to dismiss it.
func TestAServerMessageGoesWhenTheConnectionIsSettled(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	ask := &askUser{app: a.app}

	ctx, cancel := context.WithCancel(context.Background())
	ask.Notice(ctx, remote.Notice{User: "rdp", Host: "here", Text: "hello"})
	openDialog(t, a)

	cancel()

	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		if a.root.Modal() == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the message stayed up after the connection was settled")
}

// The "Give up" button on a server's message gives up.
//
// It used to look the machine up by what the server calls itself, which
// is an address, while the window holds the name the user gave it. The
// two never matched, so the one button in front of a user waiting on a
// browser sign-in did nothing at all.
func TestGivingUpOnAServerMessageGivesUp(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)

	gave := false
	ask := &askUser{app: a.app, stop: func() { gave = true }}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ask.Notice(ctx, remote.Notice{
		User: "rdp", Host: "10.0.0.5:22",
		Instruction: "To authenticate, visit: https://login.example/a/1234",
	})

	f := openDialog(t, a)
	pressButton(t, a, f, "Give up")
	a.pump.run()
	if !gave {
		t.Error("the button gave up on nothing")
	}
}

// A message that arrives for a connection already given up on is not
// shown.
//
// A handshake that was walked away from goes on running, and its dialog
// would otherwise take the screen minutes later for a connection nobody
// is waiting for.
func TestAServerMessageForAConnectionAlreadyGivenUpOnIsNotShown(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	ask := &askUser{app: a.app}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ask.Notice(ctx, remote.Notice{User: "rdp", Host: "here", Text: "hello"})
	if n := a.pump.pending(); n != 0 {
		t.Errorf("%d pieces of work were posted, want none: the dialog was built anyway", n)
	}

	a.pump.run()
	if f, ok := a.root.Modal().(*ui.Form); ok {
		t.Errorf("a dialog opened anyway: %v", f.Lines)
	}
}

// A question asked for a connection already given up on is not shown.
//
// An abandoned handshake goes on running and can reach the point where
// it wants a password. A dialog for it would take the screen, and the
// keys, for a connection nobody is waiting for.
func TestAQuestionForAConnectionAlreadyGivenUpOnIsNotAsked(t *testing.T) {
	a := newTestApp(t, 100, 30)
	withPanel(t, a)
	withDialogs(t, a)
	ask := &askUser{app: a.app}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ask.Password(ctx, "rdp", "marras-skylake"); err == nil {
		t.Fatal("a password was asked for a connection given up on")
	}
	if n := a.pump.pending(); n != 0 {
		t.Errorf("%d pieces of work were posted, want none: the dialog was built anyway", n)
	}

	a.pump.run()
	if f, ok := a.root.Modal().(*ui.Form); ok {
		t.Errorf("a dialog opened anyway: %v", f.Title)
	}
}
