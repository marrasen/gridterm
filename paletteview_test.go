package main

import (
	"slices"
	"testing"

	"github.com/marrasen/kakel/app"

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

// Next Pane goes in the sidebar's order, the machine's panes together,
// not in the order the panes were opened.
func TestNextPaneGoesInTheSidebarsOrder(t *testing.T) {
	win, _, publish := windowStage(t)
	panes := []app.Pane{{ID: "p1", Title: "one", Kind: app.KindFiles}, {ID: "p2", Title: "two", Kind: app.KindFiles, Machine: "srv"}, {ID: "p3", Title: "three", Kind: app.KindFiles}}
	publish(app.State{Panes: panes, Stage: &app.Box{Pane: "p1"}, Focus: "p1", Browsers: map[string]app.Browser{"p1": {Path: "/"}, "p2": {Path: "/"}, "p3": {Path: "/"}}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("pane.nextInSidebar", lastUI)
	var got app.FocusPane
	for got.Pane == "" {
		if in, ok := nextIntent(t).(app.FocusPane); ok {
			got = in
		}
	}
	if got.Pane != "p3" {
		t.Fatalf("Next Pane from one went to %s, want three, the next on this computer", got.Pane)
	}
}
