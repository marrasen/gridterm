package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/ui"
)

// aKeyedWindow is a window with every command registered and the
// shortcuts file in a directory the test owns.
func aKeyedWindow(t *testing.T) (*testApp, string) {
	t.Helper()
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.commands()
	a.keysDir = t.TempDir()
	return a, a.keysDir
}

// writeShortcuts puts a shortcuts file in the window's directory.
func writeShortcuts(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(keys.Path(dir), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// runsOn is the command a chord runs, and whether anything does.
func runsOn(a *testApp, c ui.Chord) (string, bool) { return a.root.Accelerators.Lookup(c) }

// A line in the file moves a shortcut onto another chord.
func TestAShortcutMovesToTheChordTheFileNames(t *testing.T) {
	a, dir := aKeyedWindow(t)
	writeShortcuts(t, dir, `{"version":1,"keys":{"ctrl+shift+O":"palette.open"}}`)

	loadShortcuts(t, a)

	want := ui.Chord{Key: input.KeyO, Mods: input.ModCtrl | input.ModShift}
	if got, on := runsOn(a, want); !on || got != "palette.open" {
		t.Errorf("%s runs %q (%v)", want, got, on)
	}
	// The one built in is left alone. Taking it away is a line of its
	// own, so a user who wants a second chord gets one.
	was := ui.Chord{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}
	if got, on := runsOn(a, was); !on || got != "palette.open" {
		t.Errorf("%s runs %q (%v), want the shortcut it always had", was, got, on)
	}
}

// A chord set to "nothing" stops running anything, so a shortcut can be
// given back to whatever is in the pane.
func TestAChordSetToNothingRunsNothing(t *testing.T) {
	a, dir := aKeyedWindow(t)
	gone := ui.Chord{Key: input.KeyF10}
	if _, on := runsOn(a, gone); !on {
		t.Fatal("F10 was not bound to begin with, so there is nothing to take away")
	}
	writeShortcuts(t, dir, `{"version":1,"keys":{"F10":"nothing"}}`)

	loadShortcuts(t, a)

	if got, on := runsOn(a, gone); on {
		t.Errorf("F10 still runs %q", got)
	}
}

// Shortcuts the file says nothing about keep working, so a user writes
// one line rather than the whole map.
func TestTheShortcutsTheFileLeavesOutStillWork(t *testing.T) {
	a, dir := aKeyedWindow(t)
	writeShortcuts(t, dir, `{"version":1,"keys":{"ctrl+shift+O":"palette.open"}}`)

	loadShortcuts(t, a)

	// The file was read, or the rest of this proves nothing: a window
	// that ignored it has every built-in shortcut too.
	read := ui.Chord{Key: input.KeyO, Mods: input.ModCtrl | input.ModShift}
	if got, on := runsOn(a, read); !on || got != "palette.open" {
		t.Fatalf("%s runs %q (%v), so the file was not read", read, got, on)
	}
	for _, c := range []struct {
		chord ui.Chord
		want  string
	}{
		{ui.Chord{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}, copyCommand},
		{ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}, "conn.terminal"},
		{ui.Chord{Key: input.KeyTab, Mods: input.ModCtrl}, "pane.next"},
	} {
		if got, on := runsOn(a, c.chord); !on || got != c.want {
			t.Errorf("%s runs %q (%v), want %q", c.chord, got, on, c.want)
		}
	}
}

// A line naming a command this gridterm does not have changes nothing at
// all, and says which line: a window with half the file applied is one
// the user cannot reason about.
func TestALineNamingNoCommandChangesNothing(t *testing.T) {
	a, dir := aKeyedWindow(t)
	writeShortcuts(t, dir, `{"version":1,"keys":{`+
		`"ctrl+shift+O":"palette.open","ctrl+shift+Y":"pane.teleport"}}`)

	loadShortcuts(t, a)

	free := ui.Chord{Key: input.KeyO, Mods: input.ModCtrl | input.ModShift}
	if got, on := runsOn(a, free); on {
		t.Errorf("%s runs %q, want the good line left unapplied too", free, got)
	}
	n := awaitModal(t, a, "the notice", byTitlePrefix[*ui.Notice]("The keyboard shortcuts"))
	if !strings.Contains(n.Message(), "pane.teleport") {
		t.Errorf("it says:\n%s", n.Message())
	}
	if !strings.Contains(n.Message(), "ctrl+shift+Y") {
		t.Errorf("it does not say which line is wrong:\n%s", n.Message())
	}
}

// A file that cannot be read leaves the shortcuts built in and says why.
func TestAFileThatCannotBeReadLeavesTheShortcutsAlone(t *testing.T) {
	a, dir := aKeyedWindow(t)
	writeShortcuts(t, dir, `{"version":1,"keys":{"hyper+Q":"pane.close"}}`)

	loadShortcuts(t, a)

	was := ui.Chord{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}
	if got, on := runsOn(a, was); !on || got != "palette.open" {
		t.Errorf("%s runs %q (%v), want the shortcut it always had", was, got, on)
	}
	n := awaitModal(t, a, "the notice", byTitlePrefix[*ui.Notice]("The keyboard shortcuts"))
	if !strings.Contains(n.Message(), "hyper") {
		t.Errorf("it says:\n%s", n.Message())
	}
}

// A starting file holds every shortcut the window has, and says where it
// went.
func TestWritingAStartingShortcutsFileSaysWhereItWent(t *testing.T) {
	a, dir := aKeyedWindow(t)
	was := len(a.root.Accelerators.Bindings())

	if err := a.writeShortcutStart(); err != nil {
		t.Fatalf("write it: %v", err)
	}

	got, err := keys.Load(keys.Path(dir))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if len(got) != was {
		t.Errorf("it holds %d shortcuts, want the %d the window has", len(got), was)
	}
	n := awaitModal(t, a, "the notice", byTitlePrefix[*ui.Notice]("Wrote "))
	if !strings.Contains(n.Title, keys.Path(dir)) {
		t.Errorf("it says %q, want the path it wrote", n.Title)
	}
}

// And a file that is already there is not written over.
func TestWritingAStartingShortcutsFileDoesNotWriteOverOne(t *testing.T) {
	a, dir := aKeyedWindow(t)
	writeShortcuts(t, dir, `{"version":1,"keys":{}}`)

	err := a.writeShortcutStart()

	if err == nil {
		t.Fatal("it wrote over the file")
	}
	if !strings.Contains(err.Error(), keys.Path(dir)) {
		t.Errorf("it says %q, want the path it would not write", err)
	}
}

// What the starting file holds reads back onto the same window
// unchanged, so a user who writes one and edits nothing keeps the keys
// they had.
func TestAStartingFileAppliedChangesNothing(t *testing.T) {
	a, dir := aKeyedWindow(t)
	was := a.root.Accelerators.Bindings()
	if err := a.writeShortcutStart(); err != nil {
		t.Fatalf("write it: %v", err)
	}
	// Read on its own first, so a file that could not be read is told
	// apart from one that changed nothing.
	changes, err := keys.Load(keys.Path(dir))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if len(changes) != len(was) {
		t.Fatalf("it holds %d shortcuts, want the %d the window has", len(changes), len(was))
	}

	loadShortcuts(t, a)

	now := a.root.Accelerators.Bindings()
	if len(now) != len(was) {
		t.Fatalf("the window has %d shortcuts, want the %d it had", len(now), len(was))
	}
	for i := range was {
		if now[i] != was[i] {
			t.Errorf("%s runs %q, want %q", was[i].Chord, now[i].ID, was[i].ID)
		}
	}
}

// A line naming a server, a font or a shell is taken as written. Those
// commands are registered after the file is read, so checking them here
// would throw the whole file away over a line that is right.
func TestALineNamingAServerIsTakenAsWritten(t *testing.T) {
	a, dir := aKeyedWindow(t)
	writeShortcuts(t, dir, `{"version":1,"keys":{"ctrl+shift+Y":"`+openPrefix+`prod"}}`)

	loadShortcuts(t, a)

	want := ui.Chord{Key: input.KeyY, Mods: input.ModCtrl | input.ModShift}
	if got, on := runsOn(a, want); !on || got != openPrefix+"prod" {
		t.Errorf("%s runs %q (%v)", want, got, on)
	}
	if a.root.Modal() != nil {
		t.Errorf("it complained about a line that is right: %v", a.root.Modal())
	}
}

// The notice about a bad line says the whole file was left unused, so a
// window with the shortcuts it came with is not a mystery.
func TestTheNoticeSaysTheWholeFileWasLeftUnused(t *testing.T) {
	a, dir := aKeyedWindow(t)
	writeShortcuts(t, dir, `{"version":1,"keys":{"ctrl+shift+Y":"pane.teleport"}}`)

	loadShortcuts(t, a)

	n := awaitModal(t, a, "the notice", byTitlePrefix[*ui.Notice]("The keyboard shortcuts"))
	if !strings.Contains(n.Message(), "None of the file was used") {
		t.Errorf("it says:\n%s", n.Message())
	}
	if !strings.Contains(n.Message(), helpTitle) {
		t.Errorf("it does not say where to find the names:\n%s", n.Message())
	}
}

// The help dialog says what the file calls each command, so the file can
// be written without reading the source.
func TestTheHelpDialogSaysWhatTheFileCallsEachCommand(t *testing.T) {
	a, _ := aKeyedWindow(t)
	withMenubar(t, a)

	got := a.helpText()

	for _, id := range []string{"palette.open", "view.theme", "agent.hand", keysCommand} {
		if !strings.Contains(got, id) {
			t.Errorf("it does not name %q:\n%s", id, got)
		}
	}
	// And a command with no chord at all is named, because the file is
	// how one gets a chord.
	if _, on := a.root.Accelerators.ChordFor("view.theme"); on {
		t.Fatal("view.theme has a chord, so it is no test of a command without one")
	}
}

// loadShortcuts reads the file, failing the test when the disk does.
func loadShortcuts(t *testing.T, a *testApp) {
	t.Helper()
	if err := a.loadShortcuts(); err != nil {
		t.Fatalf("read the shortcuts: %v", err)
	}
}

// A chord the file moves is what the menus and the help dialog show, so
// what the window says about itself follows the file.
func TestWhatTheWindowSaysFollowsTheFile(t *testing.T) {
	a, dir := aKeyedWindow(t)
	withMenubar(t, a)
	writeShortcuts(t, dir, `{"version":1,"keys":{"ctrl+shift+O":"palette.open",`+
		`"ctrl+shift+K":"nothing"}}`)

	loadShortcuts(t, a)

	got := a.helpText()
	if !strings.Contains(got, "ctrl+shift+O") {
		t.Errorf("the help dialog does not show the chord the file gave:\n%s", got)
	}
	if strings.Contains(got, "ctrl+shift+K") {
		t.Errorf("it still shows the chord the file took away:\n%s", got)
	}
}

// A file that could not be read off the disk stops the window rather
// than opening it with keys the user did not ask for.
func TestAFileThatTheDiskCannotReadStopsTheWindow(t *testing.T) {
	a, dir := aKeyedWindow(t)
	// A directory where the file goes: reading it fails for a reason
	// that is not "it is not there".
	if err := os.Mkdir(keys.Path(dir), 0o700); err != nil {
		t.Fatalf("make it: %v", err)
	}

	err := a.loadShortcuts()

	if err == nil {
		t.Fatal("a file the disk could not read was passed over")
	}
	if !errors.Is(err, keys.ErrDisk) {
		t.Errorf("it says %q, want a disk failure", err)
	}
}

// A chord bound to nothing at all is a mistake, not a way to take a
// shortcut away, so the window says which line.
func TestAChordBoundToNothingAtAllIsSaidSo(t *testing.T) {
	a, dir := aKeyedWindow(t)
	writeShortcuts(t, dir, `{"version":1,"keys":{"ctrl+shift+O":""}}`)

	loadShortcuts(t, a)

	n := awaitModal(t, a, "the notice", byTitlePrefix[*ui.Notice]("The keyboard shortcuts"))
	if !strings.Contains(n.Message(), "ctrl+shift+O") {
		t.Errorf("it does not say which line is wrong:\n%s", n.Message())
	}
	if !strings.Contains(n.Message(), keys.Nothing) {
		t.Errorf("it does not say how to take a shortcut away:\n%s", n.Message())
	}
}

// The help dialog leaves out the commands built from a scan. There is
// one per shell on the machine and one per font family, and their names
// change as the scan finds different things, so a file naming one would
// stop working without a word.
func TestTheHelpDialogLeavesOutTheCommandsAScanBuilds(t *testing.T) {
	a, _ := aKeyedWindow(t)
	withMenubar(t, a)
	onMachine(a, testShells())
	scanShells(t, a)

	got := a.helpText()

	shell := a.shellPick.lines(false)
	if len(shell) == 0 {
		t.Fatal("the scan registered no shell commands, so this tests nothing")
	}
	for _, line := range shell {
		if strings.Contains(got, line.Command) {
			t.Errorf("it names %q, which a later scan may not register:\n%s", line.Command, got)
		}
	}
}
