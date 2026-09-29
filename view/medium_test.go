package view

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/remote"
)

// A split's chooser offers a saved server not connected to, which
// connects first, and a command in its place.
func TestAChooserOffersServersNotConnectedAndACommand(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "one", Kind: app.KindFiles}, {ID: "c1", Title: "Split", Kind: app.KindChooser, SplitFrom: "p1"}},
		Stage: &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "c1"}, Share: 0.5}, Focus: "c1",
		Browsers: map[string]app.Browser{"p1": {Path: "/"}}, Saved: []remote.Host{{ID: "far", Name: "far", Address: "far.example"}}})
	var labels []string
	for _, b := range win.choosers["c1"].buttons {
		labels = append(labels, b.Label)
	}
	for _, want := range []string{"Terminal", "Terminal on far", "Command…"} {
		if !slices.Contains(labels, want) {
			t.Fatalf("the chooser offers %q, and not %q", labels, want)
		}
	}
}

// A folder Go To could not go to asks again, with what was typed and
// why.
func TestGoToAsksAgainWhenTheFolderCannotBeRead(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "one", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}}
	publish(st)
	b := win.browsers["p1"]
	b.askGoToWith("/nowhere", "", lastUI)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	var went app.GoTo
	for went.Path == "" {
		if in, ok := nextIntent(t).(app.GoTo); ok {
			went = in
		}
	}
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	st.Browsers = map[string]app.Browser{"p1": {Path: "/", Seq: 1, Err: "no such folder", WentTo: went.Ask, GoToErr: "no such folder"}}
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
	st := app.State{Serving: app.Serving{Allowed: []string{"laptop"}}}
	publish(st)
	// Serve pressed, as a user does.
	win.servingDialog(st.Serving, lastUI)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	st.Serving = app.Serving{On: true, Addr: "0.0.0.0:7777", Fingerprint: "SHA256:x", Allowed: []string{"laptop"}, Tries: 1}
	publish(st)
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.served == nil {
		t.Fatal("serving began, and the dialog saying so did not open")
	}
	st.Serving.Clients = []app.ServedClient{{Name: "laptop", From: "10.0.0.2"}}
	publish(st)
	if got := win.served.who.Text; !strings.Contains(got, "laptop") {
		t.Fatalf("a window connected, and the dialog says %q", got)
	}
	st.Serving = app.Serving{Allowed: []string{"laptop"}, Tries: 1}
	publish(st)
	if win.served != nil {
		t.Fatal("serving stopped, and the dialog stays")
	}
}

// Serve that did not start opens nothing, then or later.
func TestAServeThatFailedOpensNothingLater(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Serving: app.Serving{Allowed: []string{"laptop"}}}
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
	st.Serving = app.Serving{On: true, Addr: "0.0.0.0:7777", Allowed: []string{"laptop"}, Tries: 2}
	publish(st)
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.served != nil {
		t.Fatal("a Serve that failed opened the dialog when serving started later")
	}
}
