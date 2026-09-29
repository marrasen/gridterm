package main

import (
	"strconv"
	"testing"
)

func TestNewTerminalsAreNumberedInTurn(t *testing.T) {
	a, _ := agentApp(t)
	for range 2 {
		if err := a.open("", placement{}); err != nil {
			t.Fatal(err)
		}
	}
	for i, p := range a.st.Panes {
		if want := "Terminal " + strconv.Itoa(i+1); p.Title != want {
			t.Fatalf("pane %d is called %q, want %q", i, p.Title, want)
		}
	}
}
