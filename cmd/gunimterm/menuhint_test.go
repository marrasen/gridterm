package main

import (
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"
)

func TestAMenuLineSaysItsFullTitleAtTheBottom(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{})
	frame := func() { lastWindow.Frame(time.Second / 60) }
	win.bar.Open(0, lastUI)
	frame()
	if win.status.hinted != "" {
		t.Fatalf("with nothing highlighted, the hint is %q", win.status.hinted)
	}
	// File's second line is Pane, under the caption Close.
	for range 2 {
		lastWindow.Input(gi.KeyPress{Key: gi.KeyDown})
		frame()
	}
	if win.status.hinted != "Close Pane" {
		t.Fatalf("on File's Pane, the hint is %q", win.status.hinted)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEscape})
	frame()
	if win.status.hinted != "" {
		t.Fatalf("closed, the hint is %q", win.status.hinted)
	}
}
