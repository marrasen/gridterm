package main

import (
	"testing"
	"time"

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
