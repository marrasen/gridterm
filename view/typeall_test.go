package view

import (
	"strings"
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
)

// Type in All Panes sends what is typed in one terminal of the split on
// stage to every terminal in it, each showing its cursor, and the
// chip's × turns it off again. A terminal off stage is not typed in.
func TestTypingGoesToEveryPaneInTheSplit(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sessions := map[string]*sessiontest.Typed{}
	var panes []app.Pane
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		s := sessiontest.New()
		sessions[id] = s
		sh.Set(id, screen.Open(s, vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
		panes = append(panes, app.Pane{ID: id, Title: "Terminal " + id})
	}
	// p1, p2 and p3 in a split on stage; p4 elsewhere.
	split := &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{ID: "s2", A: &app.Box{Pane: "p2"}, B: &app.Box{Pane: "p3"}, Share: 0.5}, Share: 0.33}
	publish(app.State{Panes: panes, Stage: split, Focus: "p1"})
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	win.run("pane.typeAll", lastUI)
	lastWindow.Frame(time.Second / 60)
	for _, id := range []string{"p1", "p2", "p3"} {
		if !win.terms[id].cursorShown {
			t.Fatalf("typing in all panes, %s shows no cursor", id)
		}
	}
	if len(win.chips.chips) == 0 || win.chips.chips[0].text != "Typing in All Panes" || win.chips.chips[0].off == nil {
		t.Fatalf("the chips are %+v", win.chips.chips)
	}
	lastWindow.Input(gi.TextInput{Text: "ls"})
	lastWindow.Frame(time.Second / 60)
	for _, id := range []string{"p1", "p2", "p3"} {
		if got := sessions[id].Sent(); !strings.Contains(got, "ls") {
			t.Fatalf("%s was sent %q", id, got)
		}
	}
	if got := sessions["p4"].Sent(); got != "" {
		t.Fatalf("p4, off stage, was sent %q", got)
	}
	// The × on the chip turns it off.
	win.chips.chips[0].off(lastUI)
	lastWindow.Frame(time.Second / 60)
	if win.typeAll || win.terms["p2"].cursorShown || len(win.chips.chips) != 0 {
		t.Fatalf("turned off, typing in all is %v, p2's cursor %v, chips %+v", win.typeAll, win.terms["p2"].cursorShown, win.chips.chips)
	}
	lastWindow.Input(gi.TextInput{Text: "pwd"})
	lastWindow.Frame(time.Second / 60)
	if strings.Contains(sessions["p2"].Sent(), "pwd") {
		t.Fatal("turned off, p2 was still typed in")
	}
	// On again, and a click on the chip's × turns it off.
	win.run("pane.typeAll", lastUI)
	for range 3 {
		lastWindow.Frame(time.Second / 60)
	}
	// Handed to the chip bar in its own space: the test's window is too
	// narrow to hold the whole title bar.
	at := win.chips.crossOf(0).Center()
	win.chips.Handle(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()}, lastUI)
	lastWindow.Frame(time.Second / 60)
	if win.typeAll {
		t.Fatal("a click on the chip's × left typing in all panes on")
	}
}
