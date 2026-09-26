package main

import (
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"

	"github.com/marrasen/gridterm/vt"
)

func TestASharedPaneGlows(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p1", openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p1").t.Close() })
	marks := marksOf(vt.DefaultPalette())
	st := State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1", Marks: marks}
	ring := func() bool {
		for _, op := range lastWindow.Offscreen().Ops() {
			if r, ok := op.(*paint.RRectOp); ok && r.Stroke.Width == markWidth && r.Stroke.Color.R == marks.Agent.R && r.Stroke.Color.G == marks.Agent.G {
				return true
			}
		}
		return false
	}
	publish(st)
	if ring() {
		t.Fatal("a pane shared with nobody has a ring")
	}
	st.Share = Share{Panes: []SharedPane{{Pane: "p1"}}}
	publish(st)
	if !ring() {
		t.Fatal("a pane shared with an agent has no ring")
	}
	if !win.glowing {
		t.Fatal("the ring glows without frames to glow in")
	}
	st.Share = Share{}
	publish(st)
	for range 5 {
		lastWindow.Frame(time.Second / 10)
	}
	if ring() || win.glowing {
		t.Fatal("unshared, the pane still glows")
	}
}

// A screen somebody watching has sized bigger than the pane is drawn
// whole, shrunk to fit and centred, and a click lands on the cell drawn
// under it.
func TestAHeldScreenBiggerThanThePaneIsDrawnToFit(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p1", openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p1").t.Close() })
	st := State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1"}
	publish(st)
	tm := win.terms["p1"]
	sh.get("p1").t.Hold(200, 60)
	tm.sync()
	publish(st)
	if tm.scale >= 1 || tm.scale <= 0 {
		t.Fatalf("a 200 by 60 screen in a %v pane is drawn at scale %v", lastWindow.Offscreen().Size(), tm.scale)
	}
	if cols, rows := tm.cells.GridSize(); cols != 200 || rows != 60 {
		t.Fatalf("the cells are %d by %d, want the whole screen", cols, rows)
	}
	box, _ := lastUI.Bounds(tm)
	cell := tm.cells.CellSize()
	// The last cell of the screen, drawn shrunk, is where a click on it
	// lands.
	at := geom.Pt(tm.offset.X+(199.5*cell.W)*tm.scale, tm.offset.Y+(59.5*cell.H)*tm.scale)
	if got := tm.cellAt(at); got.X != 199 || got.Y != 59 {
		t.Fatalf("a click on the last cell, at %v in %v, lands on %v", at, box, got)
	}
	sh.get("p1").t.Release()
	tm.sync()
	publish(st)
	if tm.scale != 1 {
		t.Fatalf("let go, the screen is still drawn at %v", tm.scale)
	}
}
