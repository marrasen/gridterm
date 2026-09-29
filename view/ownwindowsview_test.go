package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
)

// A pane dragged over the window from another lights it, and a drop
// moves the pane in.
func TestAPaneDroppedFromAnotherWindowMovesIn(t *testing.T) {
	_, _, publish := windowStage(t)
	publish(app.State{Window: 2, Panes: []app.Pane{{ID: "p1", Title: "Jobs", Kind: app.KindJobs}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	at := geom.Pt(450, 300)
	lastWindow.Input(gi.Drop{Pos: at, Data: app.PaneDrag{Pane: "p9", Window: 1}})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.PaneToWindow); !ok || in.Pane != "p9" {
		t.Fatalf("the drop sent %#v", in)
	}
	// Its own pane, dropped back on it, is no move.
	lastWindow.Input(gi.Drop{Pos: at, Data: app.PaneDrag{Pane: "p1", Window: 2}})
	lastWindow.Frame(time.Second / 60)
	select {
	case env := <-lastWindow.Client().Intents():
		if _, ok := env.Intent.(app.PaneToWindow); ok {
			t.Fatalf("its own pane dropped back sent %#v", env.Intent)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

// A window with another pane dragged over it lights up.
func TestAWindowLightsUnderAPaneFromAnother(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Window: 2, Panes: []app.Pane{{ID: "p1", Title: "Jobs", Kind: app.KindJobs}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	if !win.paneDrop(gi.DragOver{Data: app.PaneDrag{Pane: "p9", Window: 1}}, lastUI) {
		t.Fatal("the window did not take a pane from another")
	}
	if win.paneDrop(gi.DragOver{Data: app.PaneDrag{Pane: "p1", Window: 2}}, lastUI) {
		t.Fatal("the window took its own pane")
	}
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.dropLit.Value() < 0.9 {
		t.Fatalf("the window is lit %.2f", win.dropLit.Value())
	}
}

// A click on a tile picks its pane.
func TestAClickOnATilePicksIt(t *testing.T) {
	win := switcherStage(t)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	at := win.sw.tiles[1].box.Value().Center()
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.FocusPane); !ok || in.Pane != "p2" {
		t.Fatalf("the click sent %#v", in)
	}
}

// A tile dragged away lifts off, and let go outside every window asks
// for a window of its own, where the picture was let go.
func TestATileLetGoOutsideAsksForAWindow(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	r := sw.tiles[1].box.Value()
	at := r.Min.Add(geom.Pt(10, 10))
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerMove{Pos: at.Add(geom.Pt(20, 0))})
	lastWindow.Frame(time.Second / 60)
	if sw.carried == nil || sw.carried.id != "p2" {
		t.Fatal("the tile did not lift off")
	}
	sw.Handle(gi.DragEnd{Out: true, At: geom.Pt(1200, 300)}, lastUI)
	in, ok := nextIntent(t).(app.PaneToNewWindow)
	if !ok || in.Pane != "p2" || in.At != geom.Pt(1200, 300).Sub(sw.grab) || in.Size != sw.size {
		t.Fatalf("let go outside, the switcher sent %#v", in)
	}
	if len(sw.tiles) != 2 {
		t.Fatalf("%d tiles are left, want 2", len(sw.tiles))
	}
}

// A tile let go over its own window goes back to its place.
func TestATileLetGoOverItsWindowGoesBack(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	r := sw.tiles[0].box.Value()
	at := r.Min.Add(geom.Pt(10, 10))
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerMove{Pos: at.Add(geom.Pt(20, 20))})
	lastWindow.Frame(time.Second / 60)
	sw.Handle(gi.DragEnd{}, lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	if len(sw.tiles) != 3 || sw.tiles[0].fade.Value() < 0.95 || sw.carried != nil {
		t.Fatalf("%d tiles, the first at %.2f", len(sw.tiles), sw.tiles[0].fade.Value())
	}
}

// A tile another window took leaves the switcher.
func TestATileTakenByAnotherWindowLeaves(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	at := sw.tiles[2].box.Value().Center()
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerMove{Pos: at.Add(geom.Pt(30, 0))})
	lastWindow.Frame(time.Second / 60)
	sw.Handle(gi.DragEnd{Taken: true}, lastUI)
	if len(sw.tiles) != 2 || sw.tiles[0].id != "p1" || sw.tiles[1].id != "p2" {
		t.Fatalf("the tiles left are %d", len(sw.tiles))
	}
}
