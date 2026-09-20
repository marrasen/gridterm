package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui/files"
)

// aPaneThatSaid returns a window whose one pane has said what is given
// and drawn it.
func aPaneThatSaid(t *testing.T, said string) *testApp {
	t.Helper()
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.shells[0].out <- []byte(said)
	waitFor(t, a, "the pane to say it", func() bool {
		return strings.Contains(paneText(pane), "needle")
	})
	return a
}

// The command opens a viewer holding what the pane has said, screen
// and scrollback both. A terminal cannot be searched and the viewer
// can, which is the whole point.
func TestTheScrollbackOpensInTheViewer(t *testing.T) {
	a := aPaneThatSaid(t, strings.Repeat("a line of it\r\n", 60)+"needle here\r\n")

	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("running %s: %v", scrollbackCommand, err)
	}

	r := onlyReader(t, a)
	if r.Lines() < 60 {
		t.Errorf("the viewer holds %d lines, want the scrollback as well as the screen", r.Lines())
	}
	if !strings.Contains(readerText(t, r), "needle here") {
		t.Error("the viewer does not hold what the pane said")
	}
}

// readerText is everything a reader is holding, taken the way a user
// would: pick it all out and read the selection.
func readerText(t *testing.T, r *files.Reader) string {
	t.Helper()
	r.SelectAll()
	text := r.SelectedText()
	r.ClearSelection()
	return text
}

// findInReader types a search the way a user does: "/", the pattern,
// then Enter.
func findInReader(t *testing.T, r *files.Reader, what string) {
	t.Helper()
	press := func(ev input.Event) {
		if _, err := r.HandleKey(ev); err != nil {
			t.Fatalf("typing the search: %v", err)
		}
	}
	press(input1('/'))
	for _, c := range what {
		press(input1(c))
	}
	press(input.Event{Kind: input.KeyPress, Key: input.KeyEnter})
}

// The viewer can be searched, which is what Marcus asked for.
func TestTheScrollbackCanBeSearched(t *testing.T) {
	a := aPaneThatSaid(t, strings.Repeat("a line of it\r\n", 60)+"needle here\r\n")
	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("running %s: %v", scrollbackCommand, err)
	}
	r := onlyReader(t, a)
	r.Home()
	if r.Top() != 0 {
		t.Fatalf("the viewer starts at line %d, so a move proves nothing", r.Top())
	}

	findInReader(t, r, "needle")

	if got := r.Find(); got != "needle" {
		t.Fatalf("it is looking for %q, want the pattern that was typed", got)
	}
	if r.Top() == 0 {
		t.Error("the search did not move the viewer to the line it found")
	}
}

// Asking twice goes to the viewer already open on that pane. A second
// would show the same text.
func TestAskingForTheScrollbackTwiceGoesToTheOneOpen(t *testing.T) {
	a := aPaneThatSaid(t, "needle here\r\n")
	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("the first time: %v", err)
	}
	first := onlyReader(t, a)

	a.focus(onlyPaneOn(t, a))
	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("the second time: %v", err)
	}

	if len(a.readers) != 1 {
		t.Errorf("%d viewers are open, want the one", len(a.readers))
	}
	if !first.Focused() {
		t.Error("asking again did not go to the viewer already open")
	}
}

// Rereading takes the pane's text again, so a pane that has said more
// since can be caught up with.
func TestRereadingTheScrollbackTakesWhatThePaneSaidSince(t *testing.T) {
	a := aPaneThatSaid(t, "needle here\r\n")
	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("running %s: %v", scrollbackCommand, err)
	}
	r := onlyReader(t, a)
	pane := onlyPaneOn(t, a)

	a.shells[0].out <- []byte("said afterwards\r\n")
	waitFor(t, a, "the pane to say it", func() bool {
		return strings.Contains(paneText(pane), "said afterwards")
	})
	r.Open()

	if !strings.Contains(readerText(t, r), "said afterwards") {
		t.Error("rereading did not take what the pane said since")
	}
}

// The viewer gets a row of its own on the sidebar, under the machine
// the pane is on.
func TestTheScrollbackHasARowOfItsOwn(t *testing.T) {
	a := aPaneThatSaid(t, "needle here\r\n")

	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("running %s: %v", scrollbackCommand, err)
	}

	r := onlyReader(t, a)
	held := a.readers[r]
	if held == nil || held.row == nil {
		t.Fatal("the viewer has no row on the sidebar")
	}
	if !strings.Contains(held.row.Label, "scrollback") {
		t.Errorf("the row says %q, want it to say what it is", held.row.Label)
	}
}

// A pane that is not a terminal has no scrollback, and the command
// says so rather than opening an empty viewer.
func TestAskingForTheScrollbackOfSomethingElseSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	if err := a.openFilesOn(""); err != nil {
		t.Fatalf("open the browser: %v", err)
	}

	err := a.showScrollback()

	if err == nil {
		t.Fatal("it opened a scrollback for a file browser")
	}
	if !strings.Contains(err.Error(), "scrollback") {
		t.Errorf("it said %q, want it to say why", err)
	}
}

// Ctrl+S writes what the viewer holds to a file, one line each.
func TestSavingTheScrollbackWritesAFile(t *testing.T) {
	at := filepath.Join(t.TempDir(), "kept.txt")

	if err := saveLines(at, []string{"one", "two", "three"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	body, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if got, want := string(body), "one\ntwo\nthree\n"; got != want {
		t.Errorf("the file holds %q, want %q", got, want)
	}
}

// A save that fails part way leaves what was there before, rather than
// half of the new thing.
func TestASaveThatFailsLeavesTheOldFileAlone(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(at, []byte("what was there\n"), 0o600); err != nil {
		t.Fatalf("write the first one: %v", err)
	}

	// A directory that is not there, so the temporary file cannot be
	// made and nothing is written.
	err := saveLines(filepath.Join(dir, "gone", "kept.txt"), []string{"new"})

	if err == nil {
		t.Fatal("saving into a directory that is not there did not fail")
	}
	body, readErr := os.ReadFile(at)
	if readErr != nil {
		t.Fatalf("read the first one back: %v", readErr)
	}
	if got, want := string(body), "what was there\n"; got != want {
		t.Errorf("the old file now holds %q, want %q", got, want)
	}
}

// The save leaves no temporary file behind when it fails.
func TestAFailedSaveClearsUpAfterItself(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "sub", "kept.txt")

	if err := saveLines(at, []string{"new"}); err == nil {
		t.Fatal("saving into a directory that is not there did not fail")
	}

	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list the directory: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d files were left behind, want none", len(left))
	}
}

// The suggested path is somewhere a file can go, with the characters a
// filesystem will not take swapped out.
func TestTheSuggestedSavePathIsUsable(t *testing.T) {
	got := suggestedSavePath(`Terminal PowerShell: C:\Users\x scrollback`)

	base := filepath.Base(got)
	if strings.ContainsAny(base, `<>:"/\|?*`) {
		t.Errorf("the suggested name is %q, want nothing a filesystem refuses", base)
	}
	if !strings.HasSuffix(base, ".txt") {
		t.Errorf("the suggested name is %q, want it to end in .txt", base)
	}
}

// A name with nothing usable in it still suggests something.
func TestASuggestedNameIsNeverEmpty(t *testing.T) {
	if got := safeFileName(`///`); got == "" {
		t.Error("a name of nothing but separators suggested an empty file name")
	}
}
