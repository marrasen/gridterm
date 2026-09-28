package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/remote"
)

// Split Right offers a saved server not connected to, which connects
// first, and a command beside the pane.
func TestSplitOffersServersNotConnectedAndACommand(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Panes: []Pane{{ID: "p1", Title: "one", Kind: kindFiles}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/"}}, Saved: []remote.Host{{Name: "far", Address: "far.example"}}})
	win.run("pane.splitRight", lastUI)
	titles := []string{}
	for _, it := range win.splitter.Items {
		titles = append(titles, it.Title)
	}
	for _, want := range []string{"Terminal on far, connecting first", "Run a Command…"} {
		if !slices.Contains(titles, want) {
			t.Fatalf("Split Right offers %q, and not %q", titles, want)
		}
	}
}

// A folder Go To could not go to asks again, with what was typed and
// why.
func TestGoToAsksAgainWhenTheFolderCannotBeRead(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Panes: []Pane{{ID: "p1", Title: "one", Kind: kindFiles}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/", Seq: 1}}}
	publish(st)
	b := win.browsers["p1"]
	b.askGoToWith("/nowhere", "", lastUI)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	var went GoTo
	for went.Path == "" {
		if in, ok := nextIntent(t).(GoTo); ok {
			went = in
		}
	}
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	st.Browsers = map[string]Browser{"p1": {Path: "/", Seq: 1, Err: "no such folder", WentTo: went.Ask, GoToErr: "no such folder"}}
	publish(st)
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	if b.goTo == nil || b.goTo.Text() != "/nowhere" || win.dialog == nil {
		t.Fatal("Go To did not ask again with what was typed")
	}
}

// Serving opens the dialog saying so, which keeps up with who connects
// and closes once serving stops.
func TestTheServingDialogKeepsUp(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Serving: Serving{Allowed: []string{"laptop"}}}
	publish(st)
	// Serve pressed, as a user does.
	win.servingDialog(st.Serving, lastUI)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	st.Serving = Serving{On: true, Addr: "0.0.0.0:7777", Fingerprint: "SHA256:x", Allowed: []string{"laptop"}, Tries: 1}
	publish(st)
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.served == nil {
		t.Fatal("serving began, and the dialog saying so did not open")
	}
	st.Serving.Clients = []ServedClient{{Name: "laptop", From: "10.0.0.2"}}
	publish(st)
	if got := win.served.who.Text; !strings.Contains(got, "laptop") {
		t.Fatalf("a window connected, and the dialog says %q", got)
	}
	st.Serving = Serving{Allowed: []string{"laptop"}, Tries: 1}
	publish(st)
	if win.served != nil {
		t.Fatal("serving stopped, and the dialog stays")
	}
}

// Serve that did not start opens nothing, then or later.
func TestAServeThatFailedOpensNothingLater(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Serving: Serving{Allowed: []string{"laptop"}}}
	publish(st)
	win.servingDialog(st.Serving, lastUI)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	st.Serving.Tries = 1
	publish(st)
	// Served later, from another window.
	st.Serving = Serving{On: true, Addr: "0.0.0.0:7777", Allowed: []string{"laptop"}, Tries: 2}
	publish(st)
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.served != nil {
		t.Fatal("a Serve that failed opened the dialog when serving started later")
	}
}
