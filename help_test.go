package main

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// helpChord is the chord a drawn line shows against a name, and the
// column it starts in, or -1 when there is no such line.
//
// Read off the screen rather than out of the message: the padding is
// what lines the chords up, and only the drawn rows show whether it
// survived.
func helpChord(drawn []string, what string) (chord string, col int) {
	for _, line := range drawn {
		// Inside the dialog's rule and its padding.
		inside := strings.Trim(line, " │")
		if !strings.HasPrefix(inside, what) {
			continue
		}
		rest := strings.TrimSpace(inside[len(what):])
		if rest == "" || strings.Contains(rest, " ") {
			continue
		}
		return rest, strings.LastIndex(line, rest)
	}
	return "", -1
}

// drawnLines is what a dialog puts on the screen, one string per row of
// the window, with the trailing blanks cut.
func drawnLines(a *testApp, w ui.Widget) []string {
	cols, rows := a.lastSize[0], a.lastSize[1]
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	w.Layout(ui.Size{Cols: cols, Rows: rows})
	w.Draw(g.View())

	out := make([]string, 0, rows)
	for y := range rows {
		var b strings.Builder
		for x := range cols {
			c := g.At(x, y)
			if c.Width == 0 {
				continue
			}
			if c.Rune == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(c.Rune)
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

// aWindowWithMenus is a window with the bar, the panel and the dialogs,
// which is what the help is read from.
func aWindowWithMenus(t *testing.T) *testApp {
	t.Helper()
	// Tall, so the whole list is drawn: the dialog scrolls what does not
	// fit, and a test that reads the screen can only read what is on it.
	a := newTestApp(t, 100, 90)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	return a
}

// helpChordKey is the chord that opens the key list.
var helpChordKey = ui.Chord{Key: input.KeyH, Mods: input.ModCtrl | input.ModShift}

// The help chord lists every key and what it does, in columns.
func TestTheHelpChordListsTheKeys(t *testing.T) {
	a := aWindowWithMenus(t)

	sendKey(t, a, press(helpChordKey.Key, helpChordKey.Mods))

	n := awaitModal(t, a, "the key list", byTitle[*ui.Notice](helpTitle))
	drawn := drawnLines(a, n)

	copyChord, copyAt := helpChord(drawn, "Copy")
	if copyChord != "ctrl+shift+C" {
		t.Errorf("Copy is listed against %q, want the chord it is bound to", copyChord)
	}
	// The file browser's bar has keys of its own, and nothing else
	// listed them. Spelled the way the bar spells them.
	if got, _ := helpChord(drawn, "Go to"); got != "^G" {
		t.Errorf("the browser's Go to is listed against %q", got)
	}
	// The columns reach the screen: a dialog that re-wrapped the list at
	// spaces would leave the chords wherever the words ended.
	pasteChord, pasteAt := helpChord(drawn, "Paste")
	if pasteChord == "" {
		t.Fatalf("Paste is not listed: %v", drawn)
	}
	if copyAt != pasteAt {
		t.Errorf("Copy's chord starts at column %d and Paste's at %d, so nothing lines up",
			copyAt, pasteAt)
	}
}

// F1 is left to whatever is running in the shell.
func TestF1IsNotTakenByTheWindow(t *testing.T) {
	a := aWindowWithMenus(t)
	if id, ok := a.root.Accelerators.Lookup(ui.Chord{Key: input.KeyF1}); ok {
		t.Errorf("F1 runs %q, and belongs to the program in the pane", id)
	}
}

// The Help menu opens the same list.
func TestTheHelpMenuListsTheKeys(t *testing.T) {
	a := aWindowWithMenus(t)

	m := openBarMenu(t, a, helpMenu)
	chooseMenuItem(t, m, helpCommand)

	n := awaitModal(t, a, "the key list", byTitle[*ui.Notice](helpTitle))
	if !strings.Contains(n.Message(), "New pane") {
		t.Errorf("the list leaves out a command the menus offer: %q", n.Message())
	}
}

// The list keeps to the keys: one line per saved machine and one per
// installed font would bury them.
func TestTheKeyListLeavesOutTheGeneratedCommands(t *testing.T) {
	a := aWindowWithMenus(t)
	saveHostNamed(t, a, "margit", "margit.example")
	a.refreshServers()
	deliverFonts(a, fakeFamilies("Consolas"))

	got := a.helpText()
	for _, not := range []string{"Connect to margit", "Terminal on margit", "Consolas"} {
		if strings.Contains(got, not) {
			t.Errorf("the list holds %q, which a menu generates: %q", not, got)
		}
	}
	// And it still holds the fixed lines of the menus that generate them.
	if !strings.Contains(got, "Add a server") {
		t.Errorf("the Servers menu's own lines went with them: %q", got)
	}
}

// Help stays last on the bar, however many menus the window adds while
// it runs.
func TestHelpStaysLastOnTheBar(t *testing.T) {
	a := aWindowWithMenus(t)
	saveHostNamed(t, a, "margit", "margit.example")
	a.refreshServers()
	deliverFonts(a, fakeFamilies("Consolas"))

	titles := barTitles(a)
	for _, want := range []string{serversMenu, "Font"} {
		if !hasTitle(titles, want) {
			t.Fatalf("the bar has no %q menu: %v", want, titles)
		}
	}
	if last := titles[len(titles)-1]; last != helpMenu {
		t.Errorf("the bar ends on %q: %v", last, titles)
	}
}

// A menu added in front of an open one takes that one down.
//
// An open menu holds the index of the title it hangs under, so one left
// open would be hanging under the new menu's name. The other half of the
// rule -- a menu whose index did not move stays open -- is
// TestFontScanLeavesAMenuThatDidNotMoveOpen.
func TestAddingAMenuClosesOneWhoseTitleItMoved(t *testing.T) {
	a := aWindowWithMenus(t)
	// Help is last, so anything added goes in front of it.
	if !a.bar.Open(menuTitled(t, a.bar, helpMenu)) {
		t.Fatal("the Help menu would not open")
	}

	deliverFonts(a, fakeFamilies("Consolas"))

	if a.bar.OpenIndex() != -1 {
		t.Errorf("open = %d, want the menu that moved taken down", a.bar.OpenIndex())
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

	sendKey(t, a, press(helpChordKey.Key, helpChordKey.Mods))

	n := awaitModal(t, a, "the key list", byTitle[*ui.Notice](helpTitle))
	if got, _ := helpChord(drawnLines(a, n), "Copy"); got != moved.String() {
		t.Errorf("Copy is listed against %q, want %q", got, moved)
	}
}

// barTitles is what the menu bar is offering, in order.
func barTitles(a *testApp) []string {
	out := make([]string, 0, len(a.bar.Menus))
	for _, def := range a.bar.Menus {
		out = append(out, def.Title)
	}
	return out
}

func hasTitle(titles []string, want string) bool {
	return slices.Contains(titles, want)
}

// The key list holds the file viewer's keys, including the ones that
// pick text out. Its own bar cannot say them: it has room for what a
// file of lines offers, and the copy only appears once there is
// something to copy.
func TestTheKeyListHoldsTheFileViewersKeys(t *testing.T) {
	a := aWindowWithMenus(t)

	got := a.helpText()

	for _, want := range []string{"Follow", "Hex", "Pick text out", "Pick out the whole file",
		"Copy what is picked out", "Drop what is picked out"} {
		if !strings.Contains(got, want) {
			t.Errorf("the list leaves out %q: %q", want, got)
		}
	}
}
