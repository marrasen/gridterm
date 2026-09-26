package main

import (
	"strconv"
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/gridterm/vt"
)

func TestCtrlTabWalksThePanesByLastUse(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	var panes []Pane
	for _, id := range []string{"p1", "p2", "p3"} {
		sh.set(id, openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.get(id).t.Close() })
		panes = append(panes, Pane{ID: id, Title: "Terminal " + id})
	}
	// Used in the order p1, p2, p3.
	for _, id := range []string{"p1", "p2", "p3"} {
		publish(State{Panes: panes, Stage: &Box{Pane: id}, Focus: id})
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	step := func() string {
		t.Helper()
		lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Mods: gi.ModControl})
		lastWindow.Frame(time.Second / 60)
		for {
			if f, ok := nextIntent(t).(FocusPane); ok {
				return f.Pane
			}
		}
	}
	if got := step(); got != "p2" {
		t.Fatalf("the first Ctrl+Tab went to %s, want the pane used before, p2", got)
	}
	if win.walkList == nil {
		t.Fatal("the walk shows no list")
	}
	if got := step(); got != "p1" {
		t.Fatalf("the second went to %s, want p1", got)
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl})
	lastWindow.Frame(time.Second / 60)
	if win.walk != nil || win.walkList != nil {
		t.Fatal("letting go of Ctrl left the walk going")
	}
	if win.recent[0] != "p1" {
		t.Fatalf("the walk ended on p1, and the most recent is %s", win.recent[0])
	}
	// From the palette, with no Ctrl to let go of, it takes one step.
	win.run("pane.next", lastUI)
	for {
		if f, ok := nextIntent(t).(FocusPane); ok {
			if f.Pane != "p3" {
				t.Fatalf("from the palette it went to %s, want p3", f.Pane)
			}
			break
		}
	}
	if win.walk != nil {
		t.Fatal("run from the palette, the walk goes on")
	}
}

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
