package main

import (
	"slices"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/widget"
)

// The palette finds commands by the words people use for them, and has
// the ones the menus alone had.
func TestThePaletteFindsCommandsByOtherWords(t *testing.T) {
	win, _, _ := windowStage(t)
	find := func(title string) widget.PaletteItem {
		t.Helper()
		i := slices.IndexFunc(win.palette.Items, func(it widget.PaletteItem) bool { return it.Title == title })
		if i < 0 {
			t.Fatalf("the palette has no %q", title)
		}
		return win.palette.Items[i]
	}
	if it := find("Exit"); !slices.Contains(it.Also, "quit") {
		t.Fatalf("Exit is found by %v", it.Also)
	}
	if it := find("Larger Font"); !slices.Contains(it.Also, "zoom in") {
		t.Fatalf("Larger Font is found by %v", it.Also)
	}
	find("Scroll Page Up")
	find("Open the Menus")
}

// A failure is headed with what was being done.
func TestAFailureSaysWhatWasBeingDone(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.handle(PickTheme{Name: "No Such Theme"})
	if n := a.st.Notices; len(n) != 1 || n[0].Title != "Couldn't change the theme" {
		t.Fatalf("it said %+v", n)
	}
}

// Next Pane goes in the sidebar's order, the machine's panes together,
// not in the order the panes were opened.
func TestNextPaneGoesInTheSidebarsOrder(t *testing.T) {
	win, _, publish := windowStage(t)
	panes := []Pane{{ID: "p1", Title: "one", Kind: kindFiles}, {ID: "p2", Title: "two", Kind: kindFiles, Machine: "srv"}, {ID: "p3", Title: "three", Kind: kindFiles}}
	publish(State{Panes: panes, Stage: &Box{Pane: "p1"}, Focus: "p1", Browsers: map[string]Browser{"p1": {Path: "/"}, "p2": {Path: "/"}, "p3": {Path: "/"}}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("pane.nextInSidebar", lastUI)
	var got FocusPane
	for got.Pane == "" {
		if in, ok := nextIntent(t).(FocusPane); ok {
			got = in
		}
	}
	if got.Pane != "p3" {
		t.Fatalf("Next Pane from one went to %s, want three, the next on this computer", got.Pane)
	}
}
