package view

import (
	"strings"
	"testing"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"
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
	// It stays inside the dialog: no wider than the row of buttons.
	field, _ := lastUI.Bounds(body.find)
	list, _ := lastUI.Bounds(body.table)
	panel, _ := lastUI.Bounds(body)
	if field.Max.X > panel.Max.X+0.5 || list.Max.X > panel.Max.X+0.5 || field.Size().W > pickerWidth*1.02 { // the dialog may still be scaling in
		t.Fatalf("the field is at %v and the list at %v, in a body at %v", field, list, panel)
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

// Use Secret's buttons stand Cancel, Copy, Type. Left and Right choose
// among them, from the field too; Up and Down move along the list from a
// button as from the field; typing on a button goes on in the field.
func TestUseSecretsKeysWorkTheListAndTheButtons(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(900, 600))
	publish(app.State{Secrets: app.Secrets{Exists: true, Open: true, Items: []app.SecretItem{
		{ID: "a", Name: "aws"}, {ID: "b", Name: "bank"}, {ID: "c", Name: "cloud"},
	}}})
	frames(2)
	drain()
	win.run("secrets.use", lastUI)
	frames(10)
	body := win.dialog.Body.(*pickerBody)
	row := win.dialog.Buttons()
	labels := []string{}
	for _, n := range row {
		labels = append(labels, n.(*widget.Button).Label)
	}
	if strings.Join(labels, "|") != "Cancel|Copy|Type" {
		t.Fatalf("the buttons are %v", labels)
	}
	press := func(k gi.Key) {
		lastWindow.Input(gi.KeyPress{Key: k})
		frames(1)
	}
	press(gi.KeyRight)
	if lastUI.Focused() != row[2] {
		t.Fatalf("Right from the field went to %v", lastUI.Focused())
	}
	press(gi.KeyLeft)
	if lastUI.Focused() != row[1] {
		t.Fatalf("Left from Type went to %v", lastUI.Focused())
	}
	press(gi.KeyDown)
	if k, _ := body.table.Cursor(); k != "b" || lastUI.Focused() != row[1] {
		t.Fatalf("Down on Copy picked %q, the keyboard with %v", k, lastUI.Focused())
	}
	press(gi.KeyEnter)
	if in := nextIntentPast(t); in != (app.CopySecret{ID: "b"}) {
		t.Fatalf("Enter on Copy sent %#v", in)
	}
	// Opened again: typing while a button has the keyboard finds.
	win.run("secrets.use", lastUI)
	frames(10)
	press(gi.KeyRight)
	lastWindow.Input(gi.TextInput{Text: "cl"})
	frames(2)
	body = win.dialog.Body.(*pickerBody)
	if lastUI.Focused() != gunim.Node(body.find) || body.find.Text() != "cl" || len(*body.keys) != 1 {
		t.Fatalf("typing on a button left the field saying %q, %d listed", body.find.Text(), len(*body.keys))
	}
}
