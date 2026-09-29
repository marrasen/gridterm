package main

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/vt"
)

func TestADropGoesToTheTerminalUnderItOrTheFocusedOne(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1"})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	drop := func(at geom.Point) DropFiles {
		t.Helper()
		lastWindow.Input(gi.Drop{Pos: at, Paths: []string{"/tmp/x.png"}})
		lastWindow.Frame(time.Second / 60)
		in, ok := nextIntent(t).(DropFiles)
		if !ok || len(in.Paths) != 1 {
			t.Fatalf("the drop sent %#v", in)
		}
		return in
	}
	box, _ := lastUI.Bounds(win.terms["p1"])
	if in := drop(box.Center()); in.Pane != "p1" {
		t.Fatalf("dropped on the terminal, it went to %q", in.Pane)
	}
	if in := drop(geom.Pt(20, 200)); in.Pane != "" {
		t.Fatalf("dropped on the sidebar, it went to %q, want the focused pane", in.Pane)
	}
}
