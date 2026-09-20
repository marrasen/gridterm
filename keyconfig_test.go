package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/ui"
)

// withShortcutFile points the window's shortcuts at a directory of the
// test's own and writes a file into it.
func withShortcutFile(t *testing.T, a *testApp, body string) string {
	t.Helper()
	dir := t.TempDir()
	a.keysDir = dir
	at := keys.Path(dir)
	if body != "" {
		if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
			t.Fatalf("write the shortcuts file: %v", err)
		}
	}
	return at
}

// The keymap gridterm comes with is built fresh each time it is asked
// for. A reload lays the file on top of it, so two of them must not be
// the same map.
func TestTheDefaultShortcutsAreBuiltFresh(t *testing.T) {
	one, two := defaultShortcuts(), defaultShortcuts()

	if one == two {
		t.Fatal("the same keymap came back twice, so a reload would change the original")
	}
	one.Unbind(ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift})

	if _, still := two.Lookup(ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}); !still {
		t.Error("unbinding one keymap changed the other")
	}
}

// Rereading the file picks up a chord added since the window opened.
func TestRereadingTheShortcutsPicksUpAChange(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// The reread says so in a notice, and a notice needs somewhere to
	// be drawn.
	withDialogs(t, a)
	a.commands()
	moved := ui.Chord{Key: input.KeyF7}
	if _, bound := a.root.Accelerators.Lookup(moved); bound {
		t.Fatal("F7 is already bound, so this proves nothing")
	}
	withShortcutFile(t, a, `{"version":1,"keys":{"F7":"palette.open"}}`)

	if err := a.reloadShortcuts(); err != nil {
		t.Fatalf("reread: %v", err)
	}

	if got, bound := a.root.Accelerators.Lookup(moved); !bound || got != "palette.open" {
		t.Errorf("F7 runs %q (bound %v), want the palette", got, bound)
	}
}

// Taking a line out of the file and rereading gives the built-in chord
// back. Laying the file on top of the keymap it already changed would
// not, which is why a reload starts from the defaults.
func TestRereadingGivesABuiltInChordBack(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// The reread says so in a notice, and a notice needs somewhere to
	// be drawn.
	withDialogs(t, a)
	a.commands()
	newPane := ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}
	if _, bound := a.root.Accelerators.Lookup(newPane); !bound {
		t.Fatal("ctrl+shift+T is not bound to start with")
	}
	at := withShortcutFile(t, a, `{"version":1,"keys":{"ctrl+shift+T":"nothing"}}`)
	if err := a.reloadShortcuts(); err != nil {
		t.Fatalf("the first reread: %v", err)
	}
	if _, bound := a.root.Accelerators.Lookup(newPane); bound {
		t.Fatal("the file did not take the shortcut away")
	}

	// The user changes their mind and deletes the line.
	if err := os.WriteFile(at, []byte(`{"version":1,"keys":{}}`), 0o600); err != nil {
		t.Fatalf("rewrite the file: %v", err)
	}
	if err := a.reloadShortcuts(); err != nil {
		t.Fatalf("the second reread: %v", err)
	}

	if _, bound := a.root.Accelerators.Lookup(newPane); !bound {
		t.Error("rereading did not give the built-in shortcut back")
	}
}

// A reread of a file that cannot be used leaves the window on the keys
// it had. A reload that went wrong should take nothing away.
func TestARereadThatFailsChangesNothing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// The reread says so in a notice, and a notice needs somewhere to
	// be drawn.
	withDialogs(t, a)
	a.commands()
	at := withShortcutFile(t, a, `{"version":1,"keys":{"F7":"palette.open"}}`)
	if err := a.reloadShortcuts(); err != nil {
		t.Fatalf("the first reread: %v", err)
	}

	if err := os.WriteFile(at, []byte(`{"version":1,"keys":{"F8":"no.such.command"}}`), 0o600); err != nil {
		t.Fatalf("rewrite the file: %v", err)
	}
	err := a.reloadShortcuts()

	if err == nil {
		t.Fatal("a file naming a command that is not there was accepted")
	}
	if got, bound := a.root.Accelerators.Lookup(ui.Chord{Key: input.KeyF7}); !bound || got != "palette.open" {
		t.Errorf("F7 runs %q (bound %v), want the reread to have changed nothing", got, bound)
	}
}

// A file naming a command id that has since moved goes on working. A
// saved shortcut names an id, so a rename would otherwise break it.
func TestAFileNamingAMovedCommandStillWorks(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// The reread says so in a notice, and a notice needs somewhere to
	// be drawn.
	withDialogs(t, a)
	a.commands()
	keys.Renamed["old.name.for.the.palette"] = "palette.open"
	t.Cleanup(func() { delete(keys.Renamed, "old.name.for.the.palette") })
	withShortcutFile(t, a, `{"version":1,"keys":{"F7":"old.name.for.the.palette"}}`)

	if err := a.reloadShortcuts(); err != nil {
		t.Fatalf("reread: %v", err)
	}

	got, bound := a.root.Accelerators.Lookup(ui.Chord{Key: input.KeyF7})
	if !bound {
		t.Fatal("the old name was not bound at all")
	}
	if got != "palette.open" {
		t.Errorf("F7 runs %q, want it followed to the new name", got)
	}
}

// A name that has not moved and is not a command is still refused, so
// the rename table cannot hide a typo.
func TestAnUnknownCommandIsStillRefused(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// The reread says so in a notice, and a notice needs somewhere to
	// be drawn.
	withDialogs(t, a)
	a.commands()
	withShortcutFile(t, a, `{"version":1,"keys":{"F7":"palete.open"}}`)

	err := a.reloadShortcuts()

	if err == nil {
		t.Fatal("a misspelled command was accepted")
	}
	if !strings.Contains(err.Error(), "palete.open") {
		t.Errorf("it said %q, want it to name the line", err)
	}
}

// The walk holds whatever modifier its shortcut holds, read from the
// keymap rather than fixed at Ctrl. A user who moves the walk to
// another chord keeps the holding as well as the key.
func TestTheWalkHoldsWhateverItsShortcutHolds(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// The reread says so in a notice, and a notice needs somewhere to
	// be drawn.
	withDialogs(t, a)
	a.commands()

	if got, want := a.walkHeld(), input.ModCtrl; got != want {
		t.Errorf("it waits on %v, want the %v its shortcut holds", got, want)
	}

	// Moved to alt, the way the file can.
	for _, b := range a.root.Accelerators.Bindings() {
		if b.ID == "pane.next" || b.ID == "pane.previous" {
			a.root.Accelerators.Unbind(b.Chord)
		}
	}
	if err := a.root.Accelerators.Bind(
		ui.Chord{Key: input.KeyTab, Mods: input.ModAlt}, "pane.next"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	if got, want := a.walkHeld(), input.ModAlt; got != want {
		t.Errorf("after moving it to alt it waits on %v, want %v", got, want)
	}
}

// Shift is not what holds the walk open: it picks the direction, and
// the two chords differ by it.
func TestShiftIsNotWhatHoldsTheWalkOpen(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// The reread says so in a notice, and a notice needs somewhere to
	// be drawn.
	withDialogs(t, a)
	a.commands()

	if a.walkHeld()&input.ModShift != 0 {
		t.Error("the walk waits on shift, which is what picks the direction")
	}
}

// With the walk bound to nothing there is no modifier to wait on, so a
// walk started from the palette ends on the next frame rather than
// hanging about.
func TestAWalkWithNothingBoundWaitsOnNothing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// The reread says so in a notice, and a notice needs somewhere to
	// be drawn.
	withDialogs(t, a)
	a.commands()
	for _, b := range a.root.Accelerators.Bindings() {
		if b.ID == "pane.next" || b.ID == "pane.previous" {
			a.root.Accelerators.Unbind(b.Chord)
		}
	}

	if got := a.walkHeld(); got != 0 {
		t.Errorf("it waits on %v, want nothing", got)
	}
}

// The starting file the window writes can be read back, which is what
// makes it a starting point rather than an example.
func TestTheStartingFileCanBeReadBack(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// It says so in a notice when it worked, and a notice needs
	// somewhere to be drawn.
	withDialogs(t, a)
	a.commands()
	dir := t.TempDir()
	a.keysDir = dir

	if err := a.writeShortcutStart(); err != nil {
		t.Fatalf("write it: %v", err)
	}

	changes, err := keys.Load(filepath.Join(dir, keys.File))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("the starting file holds no shortcuts")
	}
	if err := a.applyShortcuts(changes); err != nil {
		t.Errorf("the file the window wrote is not one it will take: %v", err)
	}
}

// Ctrl+Shift+T opens another terminal like the one the user is in:
// the same shell, on the same machine. A window whose last pick was
// another shell would otherwise answer with that one.
func TestAnotherTerminalLikeThisOneUsesTheSameShell(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	first := onlyPaneOn(t, a)
	a.started[first].argv = []string{"cmd.exe", "/k", "echo the one I am in"}
	a.focus(first)

	// What the chord runs, not just that it runs something: the whole
	// change is which command it points at.
	got, on := a.root.Accelerators.Lookup(
		ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift})
	if !on || got != "conn.terminal" {
		t.Fatalf("ctrl+shift+T runs %q (bound %v), want conn.terminal", got, on)
	}
	if _, err := a.root.HandleKey(
		press(input.KeyT, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("pressing it: %v", err)
	}

	if got := a.shellCount(); got != 2 {
		t.Fatalf("%d shells were started, want a second", got)
	}
	a.shellsMu.Lock()
	started := strings.Join(a.argvs[len(a.argvs)-1], " ")
	a.shellsMu.Unlock()
	if want := "cmd.exe /k echo the one I am in"; started != want {
		t.Errorf("the second shell is %q, want the same as the first at %q", started, want)
	}
}

// With the sidebar focused the user named a machine rather than
// pointing at a pane, so the shell is that machine's business.
func TestNamingAMachineDoesNotCopyThePanesShell(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	first := onlyPaneOn(t, a)
	a.started[first].argv = []string{"cmd.exe", "/k", "echo the one I am in"}
	if err := a.focusPanel(); err != nil {
		t.Fatalf("focus the sidebar: %v", err)
	}

	if got := a.shellLikeThePaneHere(a.current()); got != nil {
		t.Errorf("it would copy %v, want the machine's own shell", got)
	}
}

// A command pane is not a terminal to make another of: another one of
// those is a rerun, which "Run it again" is for.
func TestACommandPaneIsNotCopied(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.panes[pane].Kind = conns.Command
	a.started[pane].argv = []string{"make", "deploy"}
	a.focus(pane)

	if got := a.shellLikeThePaneHere(a.current()); got != nil {
		t.Errorf("it would run %v again, want a terminal instead", got)
	}
}

// A pane that never said what it was started on is not copied either.
func TestAPaneWithNoShellRecordedIsNotCopied(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.started[pane].argv = nil
	a.focus(pane)

	if got := a.shellLikeThePaneHere(a.current()); got != nil {
		t.Errorf("it would copy %v from a pane that recorded nothing", got)
	}
}

// The shell copied is a copy: changing what comes back must not reach
// into what the first pane remembers it was started on.
func TestTheShellCopiedIsACopy(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.started[pane].argv = []string{"cmd.exe", "/k", "echo hello"}
	a.focus(pane)

	got := a.shellLikeThePaneHere(a.current())
	if len(got) == 0 {
		t.Fatal("nothing was copied")
	}
	got[0] = "OVERWRITTEN"

	if a.started[pane].argv[0] != "cmd.exe" {
		t.Error("changing the copy changed what the pane remembers")
	}
}

// A reread reaches the menu bar. The bar is built once and keeps the
// keymap it was given, so a reload that swapped it for a new one would
// leave every menu printing chords nothing runs.
func TestARereadReachesTheMenuBar(t *testing.T) {
	a := aWindowWithMenus(t)
	withShortcutFile(t, a, `{"version":1,"keys":{"ctrl+shift+H":"nothing","F7":"help.keys"}}`)

	if err := a.reloadShortcuts(); err != nil {
		t.Fatalf("reread: %v", err)
	}
	// It says so in a notice, which is over the bar until it is read.
	dismissNotice(t, a)

	m := openBarMenu(t, a, helpMenu)
	drawn := strings.Join(drawnLines(a, m), " | ")

	if strings.Contains(drawn, "ctrl+shift+H") {
		t.Errorf("the Help menu still shows ctrl+shift+H, which the reread took away: %s", drawn)
	}
	if !strings.Contains(drawn, "F7") {
		t.Errorf("the Help menu does not show the F7 the file moved it to: %s", drawn)
	}
}

// A walk started on a chord that holds alt stays open while alt is
// held. Without the walk remembering what to wait on, a window with
// the walk moved off ctrl would take one step and stop.
func TestAWalkStaysOpenWhileItsOwnModifierIsHeld(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	if err := a.openPane(); err != nil {
		t.Fatalf("open a second pane: %v", err)
	}
	for _, b := range a.root.Accelerators.Bindings() {
		if b.ID == "pane.next" || b.ID == "pane.previous" {
			a.root.Accelerators.Unbind(b.Chord)
		}
	}
	if err := a.root.Accelerators.Bind(
		ui.Chord{Key: input.KeyTab, Mods: input.ModAlt}, "pane.next"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	held := input.ModAlt
	a.modsNow = func() input.Mods { return held }

	if err := a.root.Commands.Run("pane.next"); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if a.walk == nil {
		t.Fatal("the walk did not start")
	}
	a.stepWalk()

	if a.walk == nil {
		t.Error("the walk ended while the modifier it was started on was still held")
	}

	// And it ends once that modifier goes.
	held = 0
	a.stepWalk()
	if a.walk != nil {
		t.Error("the walk stayed open after its modifier was let go")
	}
}
