package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	gi "github.com/marrasen/gunim/input"
)

func TestAMenuLineSaysItsFullTitleAtTheBottom(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{})
	frame := func() { lastWindow.Frame(time.Second / 60) }
	win.bar.Open(0, lastUI)
	frame()
	if win.status.hinted != "" {
		t.Fatalf("with nothing highlighted, the hint is %q", win.status.hinted)
	}
	// Right goes into File, at its first line, and Down to its second, Pane, under the caption Close.
	for _, k := range []gi.Key{gi.KeyRight, gi.KeyDown} {
		lastWindow.Input(gi.KeyPress{Key: k})
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
