package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// helpChordFor is the chord the help list shows against a line, or ""
// when there is no such line.
//
// By whole words, so "Copy" does not match the browser's own copy key
// on the line below it.
func helpChordFor(message, what string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, what) {
			continue
		}
		rest := strings.TrimSpace(line[len(what):])
		if rest == "" || strings.Contains(rest, " ") {
			continue
		}
		return rest
	}
	return ""
}

// aWindowWithMenus is a window with the bar, the panel and the dialogs,
// which is what the help is read from.
func aWindowWithMenus(t *testing.T) *testApp {
	t.Helper()
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	return a
}

// F1 lists every key and what it does.
func TestF1ListsTheKeys(t *testing.T) {
	a := aWindowWithMenus(t)

	if _, err := a.root.HandleKey(press(input.KeyF1, 0)); err != nil {
		t.Fatalf("F1: %v", err)
	}

	n := awaitModal(t, a, "the key list", byTitle[*ui.Notice](helpTitle))
	if got := helpChordFor(n.Message(), "Copy"); got != "ctrl+shift+C" {
		t.Errorf("Copy is listed against %q, want the chord it is bound to", got)
	}
	// The file browser's bar has keys of its own, and nothing else
	// listed them.
	if got := helpChordFor(n.Message(), "Go to"); got != "ctrl+G" {
		t.Errorf("the browser's Go to is listed against %q", got)
	}
}

// The Help menu opens the same list.
func TestTheHelpMenuListsTheKeys(t *testing.T) {
	a := aWindowWithMenus(t)

	m := openBarMenu(t, a, "Help")
	chooseMenuItem(t, m, helpCommand)

	n := awaitModal(t, a, "the key list", byTitle[*ui.Notice](helpTitle))
	if !strings.Contains(n.Message(), "New tab") {
		t.Errorf("the list leaves out a command the menus offer: %q", n.Message())
	}
}

// Help stays last on the bar, however many menus the window adds while
// it runs.
func TestHelpStaysLastOnTheBar(t *testing.T) {
	a := aWindowWithMenus(t)
	deliverFonts(a, fakeFamilies("Consolas"))

	titles := make([]string, 0, len(a.bar.Menus))
	for _, def := range a.bar.Menus {
		titles = append(titles, def.Title)
	}
	if last := titles[len(titles)-1]; last != helpMenu {
		t.Errorf("the bar ends on %q: %v", last, titles)
	}
}

// The list is read from the keymap, so a chord that moves moves in the
// list too.
func TestTheKeyListFollowsTheKeymap(t *testing.T) {
	a := aWindowWithMenus(t)
	moved := ui.Chord{Key: input.KeyY, Mods: input.ModCtrl | input.ModShift}
	for _, b := range a.root.Accelerators.Bindings() {
		if b.ID == copyCommand {
			a.root.Accelerators.Unbind(b.Chord)
		}
	}
	if err := a.root.Accelerators.Bind(moved, copyCommand); err != nil {
		t.Fatalf("rebind copy: %v", err)
	}

	if _, err := a.root.HandleKey(press(input.KeyF1, 0)); err != nil {
		t.Fatalf("F1: %v", err)
	}

	n := awaitModal(t, a, "the key list", byTitle[*ui.Notice](helpTitle))
	if got := helpChordFor(n.Message(), "Copy"); got != moved.String() {
		t.Errorf("Copy is listed against %q, want %q", got, moved)
	}
}
