package view

import (
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// Split Right splits at once, and the new half is a chooser: a new
// terminal from its buttons, or a pane already open moved in from its
// images, and Escape gives the half back.
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
	// Along to the image of p2, and Enter moves it in.
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

// A chooser on stage follows the panes: a pane opened while it shows
// arrives as an image, one retitled is renamed, one closed leaves.
func TestAChooserFollowsThePanes(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "left", Kind: app.KindFiles}, {ID: "c1", Title: "Split", Kind: app.KindChooser, SplitFrom: "p1"}},
		Stage: &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "c1"}, Share: 0.5}, Focus: "c1",
		Browsers: map[string]app.Browser{"p1": {Path: "/"}}}
	publish(st)
	frames := func() {
		for range 5 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	frames()
	c := win.choosers["c1"]
	if len(c.thumbs) != 0 {
		t.Fatalf("with no other pane, the chooser offers %d", len(c.thumbs))
	}
	st.Panes = append(st.Panes, app.Pane{ID: "p2", Title: "two", Kind: app.KindFiles}, app.Pane{ID: "p3", Title: "three", Kind: app.KindFiles})
	st.Browsers = map[string]app.Browser{"p1": {Path: "/"}, "p2": {Path: "/"}, "p3": {Path: "/"}}
	publish(st)
	frames()
	if len(c.thumbs) != 2 {
		t.Fatalf("with two more panes, the chooser offers %d", len(c.thumbs))
	}
	st.Panes[2].Title = "renamed"
	st.Panes = st.Panes[:3]
	publish(st)
	frames()
	if len(c.thumbs) != 1 || c.thumbs[0].title != "renamed" {
		t.Fatalf("with p3 closed and p2 renamed, the chooser offers %+v", c.thumbs)
	}
	if lastUI.Presence(c.thumbs[0]) == gunim.Exiting {
		t.Fatal("the image offered is not in the tree")
	}
}

// The chooser is one stop for Tab, which rings all of it; its arrows
// walk what it offers, which lights as it is reached, with no ring.
func TestAChoosersArrowsLightWhatTheyReach(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "left", Kind: app.KindFiles}, {ID: "p2", Title: "two", Kind: app.KindFiles},
		{ID: "c1", Title: "Split", Kind: app.KindChooser, SplitFrom: "p1"}},
		Stage: &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "c1"}, Share: 0.5}, Focus: "c1",
		Browsers: map[string]app.Browser{"p1": {Path: "/"}, "p2": {Path: "/"}}})
	run := func() {
		for range 20 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	run()
	c := win.choosers["c1"]
	pic := c.thumbs[0]
	lastUI.Focus(c.buttons[len(c.buttons)-1])
	run()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight, Time: time.Now()})
	run()
	if lastUI.Focused() != pic || pic.walked.Value() < 0.9 || c.ring.Value() > 0.01 {
		t.Fatalf("Right put the keyboard on %T, lit %v, the chooser's ring at %v", lastUI.Focused(), pic.walked.Value(), c.ring.Value())
	}
	// Tab out, which shows the rings, and back in by gunim's Tab order
	// (the file pane beside takes Tab itself, to go to the next pane):
	// the chooser rings, and the image it left from is lit again.
	lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Time: time.Now()})
	run()
	run()
	if lastUI.Focused() == pic || pic.walked.Value() > 0.01 {
		t.Fatalf("Tab left the keyboard on %T, lit %v", lastUI.Focused(), pic.walked.Value())
	}
	lastUI.FocusNext(false)
	run()
	if lastUI.Focused() != pic || c.ring.Value() < 0.9 || pic.walked.Value() < 0.9 {
		t.Fatalf("back in: on %T, the chooser's ring at %v, the image lit %v", lastUI.Focused(), c.ring.Value(), pic.walked.Value())
	}
}

// As a chooser opens with the keyboard on "+ Terminal", that button is
// lit, so what Enter presses shows before any arrow key is pressed.
func TestAChooserShowsWhereTheKeyboardIsAsItOpens(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "left", Kind: app.KindFiles}, {ID: "c1", Title: "Split", Kind: app.KindChooser, SplitFrom: "p1"}},
		Stage: &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "c1"}, Share: 0.5}, Focus: "c1",
		Browsers: map[string]app.Browser{"p1": {Path: "/"}}})
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	first := win.choosers["c1"].buttons[0]
	if lastUI.Focused() != first {
		t.Fatalf("the keyboard is on %T", lastUI.Focused())
	}
	// Lit: its fill is the one under the pointer, with nothing over it.
	var p paint.Painter
	hover := widget.ButtonHover.Get(lastUI.Theme())
	first.Paint(&p, gunim.Frame{Scale: 1, Theme: lastUI.Theme()}, geom.Sz(120, 36), gunim.Children{})
	for _, op := range p.Ops() {
		if r, ok := op.(*paint.RRectOp); ok && r.Fill.Solid == hover {
			return
		}
	}
	t.Fatal("the button with the keyboard is not lit")
}
