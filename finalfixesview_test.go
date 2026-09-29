package main

import (
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"
)

// A Go To in a file pane made again, as moved to another window, is
// numbered past the ones the program has answered.
func TestGoToIsNumberedPastTheOnesAnswered(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Panes: []Pane{{ID: "p1", Title: "one", Kind: kindFiles}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/", Seq: 1, WentTo: 3}}})
	win.browsers["p1"].askGoToWith("/x", "", lastUI)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	for {
		if in, ok := nextIntent(t).(GoTo); ok {
			if in.Ask <= 3 {
				t.Fatalf("Go To is numbered %d, past none of the 3 answered", in.Ask)
			}
			return
		}
	}
}
