package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	gi "github.com/marrasen/gunim/input"
)

// Split Right asks what goes in the new half, and a pane already open
// can be moved in.
func TestSplitAsksWhatGoesBeside(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "left", Kind: app.KindFiles}, {ID: "p2", Title: "right", Kind: app.KindFiles}},
		Stage: &app.Box{Pane: "p1"}, Focus: "p1", Browsers: map[string]app.Browser{"p1": {Path: "/"}, "p2": {Path: "/"}}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("pane.splitRight", lastUI)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.splitter == nil || !win.splitter.IsOpen() {
		t.Fatal("Split Right asked nothing")
	}
	lastWindow.Input(gi.TextInput{Text: "move right"})
	lastWindow.Frame(time.Second / 60)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.MovePane); !ok || in != (app.MovePane{Pane: "p2", Beside: "p1"}) {
		t.Fatalf("picking Move right sent %#v", in)
	}
}
