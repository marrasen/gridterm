package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
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
	// The command opens the viewer with the prompt already up, so "/"
	// is only pressed when something else opened it.
	if _, _, asking := r.Asking(); !asking {
		press(input1('/'))
	}
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
	// Named for the pane it came from, which scrollbackName("") is not.
	if got, want := held.row.Label, scrollbackName(a.paneName(onlyPaneOn(t, a))); got != want {
		t.Errorf("the row says %q, want %q", got, want)
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

	// The rename is what fails: the target is a directory, which is
	// reached only after the temporary file has been written. A test
	// that failed at CreateTemp would never touch the path this is
	// about.
	onADir := filepath.Join(dir, "a-directory")
	if err := os.Mkdir(onADir, 0o700); err != nil {
		t.Fatalf("make the directory: %v", err)
	}
	err := saveLines(onADir, []string{"new"})

	if err == nil {
		t.Fatal("saving onto a directory did not fail")
	}
	body, readErr := os.ReadFile(at)
	if readErr != nil {
		t.Fatalf("read the first one back: %v", readErr)
	}
	if got, want := string(body), "what was there\n"; got != want {
		t.Errorf("the old file now holds %q, want %q", got, want)
	}
}

// A save that fails after writing leaves no temporary file behind.
//
// The failure is the rename, which happens after the temporary file
// exists: a test that failed earlier would prove nothing about
// clearing up.
func TestAFailedSaveClearsUpAfterItself(t *testing.T) {
	dir := t.TempDir()
	onADir := filepath.Join(dir, "a-directory")
	if err := os.Mkdir(onADir, 0o700); err != nil {
		t.Fatalf("make the directory: %v", err)
	}

	if err := saveLines(onADir, []string{"new"}); err == nil {
		t.Fatal("saving onto a directory did not fail")
	}

	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list the directory: %v", err)
	}
	// The directory it tried to write onto, and nothing else.
	if len(left) != 1 || left[0].Name() != "a-directory" {
		var names []string
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Errorf("the directory holds %v, want the temporary file cleared up", names)
	}
}

// Saving onto a file that is already there is refused rather than
// replacing it. The question comes up filled in, so Enter is the easy
// keystroke and a file the user meant to keep has no way back.
func TestSavingOntoAFileThatExistsIsRefused(t *testing.T) {
	at := filepath.Join(t.TempDir(), "kept.txt")
	if err := os.WriteFile(at, []byte("what was there\n"), 0o600); err != nil {
		t.Fatalf("write the first one: %v", err)
	}

	err := saveLines(at, []string{"new"})

	if err == nil {
		t.Fatal("it wrote over a file that was already there")
	}
	if !strings.Contains(err.Error(), "already there") {
		t.Errorf("it said %q, want it to say the file is already there", err)
	}
	body, readErr := os.ReadFile(at)
	if readErr != nil {
		t.Fatalf("read it back: %v", readErr)
	}
	if got, want := string(body), "what was there\n"; got != want {
		t.Errorf("the file now holds %q, want %q", got, want)
	}
}

// With nowhere to suggest, nothing is suggested. A bare file name
// would land in whatever directory gridterm was started in, and the
// message afterwards would name a place the user cannot find.
func TestWithNoHomeNothingIsSuggested(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if got := suggestedSavePath("a pane"); got != "" && !filepath.IsAbs(got) {
		t.Errorf("it suggested %q, which is not a place the user can find", got)
	}
}

// The wiring: a scrollback viewer really is given somewhere to save to
// and something to save with. Without this the widget tests pass
// against a stub while Ctrl+S in the window says there is nothing to
// save.
func TestAScrollbackViewerIsWiredForSaving(t *testing.T) {
	a := aPaneThatSaid(t, "needle here\r\n")
	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("running %s: %v", scrollbackCommand, err)
	}

	r := onlyReader(t, a)

	if r.OnSave == nil {
		t.Error("the viewer has nothing to save with, so Ctrl+S says there is nothing to save")
	}
	if r.SaveAs == "" {
		t.Error("the viewer suggests nowhere to save to")
	}
	if !strings.Contains(r.SaveAs, "scrollback") {
		t.Errorf("it suggests %q, want a name taken from the pane", r.SaveAs)
	}
}

// The suggested path is somewhere a file can go, with the characters a
// filesystem will not take swapped out.
func TestTheSuggestedSavePathIsUsable(t *testing.T) {
	// Asked of the naming itself. Going through suggestedSavePath and
	// taking filepath.Base would cut an unmapped name at the backslash
	// it should never have kept, and pass against the very bug this is
	// here for.
	name := safeFileName(`Terminal PowerShell: C:\Users\x scrollback`)

	if strings.ContainsAny(name, `<>:"/\|?*`) {
		t.Errorf("the suggested name is %q, want nothing a filesystem refuses", name)
	}
	at := suggestedSavePath(`Terminal PowerShell: C:\Users\x scrollback`)
	if !strings.HasSuffix(at, ".txt") {
		t.Errorf("the suggested path is %q, want it to end in .txt", at)
	}
	if got := filepath.Base(at); got != name+".txt" {
		t.Errorf("the path ends in %q, want the mapped name %q", got, name+".txt")
	}
}

// A name with nothing usable in it still suggests something.
func TestASuggestedNameIsNeverEmpty(t *testing.T) {
	if got := safeFileName(`///`); got == "" {
		t.Error("a name of nothing but separators suggested an empty file name")
	}
}

// Closing the pane lets its viewer go of it: the text stays, because
// the user opened it to read, but the viewer stops following a
// terminal that is not there and stops holding it alive.
func TestClosingThePaneLetsTheViewerGoOfIt(t *testing.T) {
	a := aPaneThatSaid(t, "needle here\r\n")
	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("running %s: %v", scrollbackCommand, err)
	}
	r := onlyReader(t, a)
	r.Follow(true)
	pane := onlyPaneOn(t, a)
	was := readerText(t, r)

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}

	held := a.readers[r]
	if held == nil {
		t.Fatal("closing the pane took the viewer with it")
	}
	if held.pane != nil {
		t.Error("the viewer is still holding the terminal that closed")
	}
	if r.Following() {
		t.Error("the viewer is still following a pane that has gone")
	}
	if got := readerText(t, r); got != was {
		t.Error("closing the pane took away the text the viewer was showing")
	}
	if !strings.Contains(r.Name(), "closed") {
		t.Errorf("the viewer is called %q, want it to say the pane has gone", r.Name())
	}
}

// And a reread of a viewer whose pane has gone finds nothing to ask,
// rather than asking a terminal that is not there.
func TestRereadingAViewerWhosePaneWentDoesNothing(t *testing.T) {
	a := aPaneThatSaid(t, "needle here\r\n")
	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("running %s: %v", scrollbackCommand, err)
	}
	r := onlyReader(t, a)
	if err := a.closePane(onlyPaneOn(t, a)); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	was := readerText(t, r)

	if r.Open() {
		t.Error("a reread went out for a pane that has closed")
	}

	if got := readerText(t, r); got != was {
		t.Error("the reread took away what the viewer was showing")
	}
}

// Closing the scrollback gives the keys back to the pane it is of, not
// to whichever pane the window opened last.
func TestClosingTheScrollbackGoesBackToItsPane(t *testing.T) {
	a := aPaneThatSaid(t, "needle here\r\n")
	first := onlyPaneOn(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("open another pane: %v", err)
	}
	a.focus(first)
	if err := a.root.Commands.Run(scrollbackCommand); err != nil {
		t.Fatalf("running %s: %v", scrollbackCommand, err)
	}
	r := onlyReader(t, a)
	if _, err := r.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape}); err != nil {
		t.Fatalf("Escape: %v", err)
	}

	if _, err := r.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyD, Mods: input.ModCtrl}); err != nil {
		t.Fatalf("Ctrl+D: %v", err)
	}
	a.pump.run()

	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(first) {
		t.Errorf("the keys went to %v, want the pane the scrollback is of", got)
	}
}
