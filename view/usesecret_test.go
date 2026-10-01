package view

import (
	"testing"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
)

// Use Secret asks for locked secrets to open, and lists them once they
// are, the ones for the pane's machine first; typing finds, Down moves,
// Enter types the one picked.
func TestUseSecretPicksAndTypes(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(900, 600))
	st := app.State{
		Panes: []app.Pane{{ID: "p1", Title: "web", Machine: "s1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Saved:    []remote.Host{{ID: "s1", Name: "web", Address: "web.example", User: "deploy"}},
		Machines: []machines.Info{{ID: "s1", Name: "web"}},
		Secrets:  app.Secrets{Exists: true},
	}
	publish(st)
	frames(2)
	drain()
	win.run("secrets.use", lastUI)
	if in := nextIntent(t); in != (app.UnlockSecrets{}) {
		t.Fatalf("locked, Use Secret sent %#v", in)
	}
	st.Secrets = app.Secrets{Exists: true, Open: true, Items: []app.SecretItem{
		{ID: "a", Name: "aws", Kind: "password"},
		{ID: "b", Name: "bank", Kind: "password"},
		{ID: "w", Name: "deploy", Kind: "password", Logins: []string{"deploy@web.example"}},
	}}
	publish(st)
	frames(10)
	if win.dialog == nil {
		t.Fatal("opened, the secrets' list did not come")
	}
	body, ok := win.dialog.Body.(*pickerBody)
	if !ok {
		t.Fatalf("the dialog shows %T", win.dialog.Body)
	}
	if k, _ := body.table.Cursor(); k != "w" {
		t.Fatalf("the first picked is %q, want the web server's", k)
	}
	lastWindow.Input(gi.TextInput{Text: "a"})
	frames(2)
	if keys := *body.keys; len(keys) != 2 || keys[0] != "a" {
		t.Fatalf("finding \"a\" lists %v", keys)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown})
	frames(1)
	if k, _ := body.table.Cursor(); k != "b" {
		t.Fatalf("Down picked %q", k)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	frames(1)
	if in := nextIntentPast(t); in != (app.TypeSecret{ID: "b"}) {
		t.Fatalf("Enter sent %#v", in)
	}
}
