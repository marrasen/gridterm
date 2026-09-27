package main

import (
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/gridterm/vfs"
)

// A pane open on a stage of its own moves into a split beside another.
func TestAPaneMovesIntoASplit(t *testing.T) {
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.addPane(Pane{ID: "p1", Title: "one", Kind: kindFiles}, nil, placement{})
	a.addPane(Pane{ID: "p2", Title: "two", Kind: kindFiles}, nil, placement{})
	if a.groupOf["p1"] == a.groupOf["p2"] {
		t.Fatal("the panes began together")
	}
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	g := a.groupOf["p1"]
	if a.groupOf["p2"] != g {
		t.Fatalf("moved, the panes are in groups %d and %d", g, a.groupOf["p2"])
	}
	if b := a.groups[g]; b.A == nil || b.A.Pane != "p1" || b.B == nil || b.B.Pane != "p2" || b.Vertical {
		t.Fatalf("moved, the group is %+v", b)
	}
	if len(a.groups) != 1 || a.st.Focus != "p2" {
		t.Fatalf("moved, there are %d groups and the focus is on %s", len(a.groups), a.st.Focus)
	}
}

// Files opened from a file pane opens beside it, for the two side by
// side a copy goes between.
func TestFilesFromAFilePaneOpenBesideIt(t *testing.T) {
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	local := vfs.NewLocal()
	dir := t.TempDir()
	if err := a.openFilesOn("", local, dir); err != nil {
		t.Fatal(err)
	}
	first := a.st.Focus
	if err := a.openFilesOn("", local, dir); err != nil {
		t.Fatal(err)
	}
	if second := a.st.Focus; second == first || a.groupOf[second] != a.groupOf[first] {
		t.Fatalf("the second file pane, %s, is in group %d, and the first, %s, in %d", second, a.groupOf[second], first, a.groupOf[first])
	}
}

// Split Right asks what goes in the new half, and a pane already open
// can be moved in.
func TestSplitAsksWhatGoesBeside(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Panes: []Pane{{ID: "p1", Title: "left", Kind: kindFiles}, {ID: "p2", Title: "right", Kind: kindFiles}},
		Stage: &Box{Pane: "p1"}, Focus: "p1", Browsers: map[string]Browser{"p1": {Path: "/"}, "p2": {Path: "/"}}})
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
	if in, ok := nextIntent(t).(MovePane); !ok || in != (MovePane{Pane: "p2", Beside: "p1"}) {
		t.Fatalf("picking Move right sent %#v", in)
	}
}
