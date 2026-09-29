package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	gi "github.com/marrasen/gunim/input"
)

// Split Right splits at once, and the new half is a chooser: a new
// terminal from its buttons, or a pane already open moved in from its
// pictures, and Escape gives the half back.
func TestSplitPutsAChooserInTheNewHalf(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "left", Kind: app.KindFiles}, {ID: "p2", Title: "right", Kind: app.KindFiles}},
		Stage: &app.Box{Pane: "p1"}, Focus: "p1", Browsers: map[string]app.Browser{"p1": {Path: "/"}, "p2": {Path: "/"}}}
	publish(st)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("pane.splitRight", lastUI)
	if in, ok := nextIntent(t).(app.ChooseSplit); !ok || in.Vertical {
		t.Fatalf("Split Right sent %#v", in)
	}
	// The program splits, with the chooser beside the pane.
	st.Panes = append(st.Panes, app.Pane{ID: "c1", Title: "Split", Kind: app.KindChooser, SplitFrom: "p1"})
	st.Stage = &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "c1"}, Share: 0.5}
	st.Focus = "c1"
	publish(st)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	c := win.choosers["c1"]
	if c == nil || len(c.buttons) == 0 || c.buttons[0].Label != "Terminal" {
		t.Fatalf("the chooser offers %+v", c)
	}
	if len(c.thumbs) != 1 || c.thumbs[0].id != "p2" {
		t.Fatalf("the chooser offers the panes %+v, want only p2", c.thumbs)
	}
	press := func(k gi.Key) {
		lastWindow.Input(gi.KeyPress{Key: k, Time: time.Now()})
		lastWindow.Frame(time.Second / 60)
	}
	// The keyboard starts on "+ Terminal".
	if lastUI.Focused() != c.buttons[0] {
		t.Fatalf("the keyboard is on %T", lastUI.Focused())
	}
	press(gi.KeyEnter)
	if in, ok := nextIntent(t).(app.SplitPane); !ok || in != (app.SplitPane{Instead: "c1"}) {
		t.Fatalf("+ Terminal sent %#v", in)
	}
	// Along to the picture of p2, and Enter moves it in.
	lastUI.Focus(c.thumbs[0])
	press(gi.KeyEnter)
	if in, ok := nextIntent(t).(app.MovePane); !ok || in != (app.MovePane{Pane: "p2", Instead: "c1"}) {
		t.Fatalf("picking p2 sent %#v", in)
	}
	press(gi.KeyEscape)
	if in, ok := nextIntent(t).(app.ClosePane); !ok || in.Pane != "c1" {
		t.Fatalf("Escape sent %#v", in)
	}
}
