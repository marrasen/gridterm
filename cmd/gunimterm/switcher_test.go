package main

import (
	"testing"
	"time"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"

	"github.com/marrasen/gridterm/vfs"
	"github.com/marrasen/gridterm/vt"
)

// switcherStage puts three terminal panes in a window, p1 on stage, and
// opens the switcher over them.
func switcherStage(t *testing.T) *window {
	t.Helper()
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	var panes []Pane
	for _, id := range []string{"p1", "p2", "p3"} {
		sh.set(id, openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.get(id).t.Close() })
		panes = append(panes, Pane{ID: id, Title: "Terminal " + id})
	}
	publish(State{Panes: panes, Stage: &Box{Pane: "p1"}, Focus: "p1"})
	win.run("view.switcher", lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.sw == nil {
		t.Fatal("the switcher did not open")
	}
	return win
}

// Picking a pane, the others fade as it grows, rather than standing in
// their places until the overview goes.
func TestThePanesNotPickedFadeAsThePickGrows(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	for i, tl := range sw.tiles {
		if i == sw.picked {
			continue
		}
		if a := tl.fade.Value(); a > 0.05 {
			t.Fatalf("a third of a second after the pick, %s is still at %.2f", tl.id, a)
		}
	}
}

// A pane that is no terminal shows in the switcher too, drawn small as
// it was last drawn.
func TestTheSwitcherShowsAFilePane(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p2", openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p2").t.Close() })
	publish(State{Panes: []Pane{{ID: "p1", Title: "srv", Kind: kindFiles}, {ID: "p2", Title: "Terminal 2"}},
		Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/srv", Entries: []vfs.Entry{{Name: "a.txt"}, {Name: "b.txt"}}, Seq: 1}}})
	for range 10 {
		lastWindow.Frame(time.Second / 60)
	}
	if d := win.drawings["p1"]; d == nil || d.Recording().Empty() {
		t.Fatal("the file pane's drawing was not kept")
	}
	win.run("view.switcher", lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	small := false
	for _, op := range lastWindow.Offscreen().Ops() {
		if tx, ok := op.(*paint.TextOp); ok && tx.Transform.A < 0.9 && tx.Transform.A > 0 {
			small = true
		}
	}
	if !small {
		t.Fatal("the switcher shows no text drawn small: the file pane's tile is empty")
	}
}

// The pane picked comes on stage once it has grown into place, and the
// switcher stays until it is there.
func TestThePanePickedComesOnStageOnceItHasGrown(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	if n := len(lastWindow.Client().Intents()); n != 0 {
		t.Fatalf("at the pick, %d intents went out; the pane should wait to grow", n)
	}
	var in FocusPane
	for range 120 {
		lastWindow.Frame(time.Second / 60)
		time.Sleep(time.Millisecond)
		if len(lastWindow.Client().Intents()) > 0 {
			in, _ = nextIntent(t).(FocusPane)
			break
		}
	}
	if in.Pane != sw.tiles[sw.picked].id || sw.tiles[sw.picked].box.Active() {
		t.Fatalf("grown, it asked for %#v, still moving %v", in, sw.tiles[sw.picked].box.Active())
	}
	if lastUI.Presence(sw) != gunim.Exiting {
		t.Fatal("the switcher left before the pane was on stage")
	}
}
