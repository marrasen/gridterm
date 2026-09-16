package main

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui"
)

// testShells is the machine most tests run on: two shells, so there is a
// choice to make.
func testShells() []shells.Shell {
	return []shells.Shell{
		{ID: "cmd", Title: "Command Prompt", Path: `C:\Windows\system32\cmd.exe`},
		{ID: "pwsh", Title: "PowerShell", Path: `C:\Program Files\PowerShell\7\pwsh.exe`},
	}
}

// argvOf is the argv a pane on one of those shells runs.
func argvOf(t *testing.T, list []shells.Shell, id string) []string {
	t.Helper()
	sh, ok := shells.Lookup(list, id)
	if !ok {
		t.Fatalf("no shell %q on this machine", id)
	}
	return sh.Command("")
}

// onMachine gives the window a machine with these shells on it.
func onMachine(a *testApp, list []shells.Shell) {
	a.shellPick.findShells = func() ([]shells.Shell, error) { return list, nil }
	a.shellPick.namedShell = func(id string) (shells.Shell, bool) {
		return shells.Lookup(list, id)
	}
}

// scanShells looks for the shells the way the window does, on a
// goroutine of its own, and lets the draw loop pick the result up.
func scanShells(t *testing.T, a *testApp) {
	t.Helper()
	a.startShellScan()
	waitFor(t, a, "the shell scan to land", func() bool {
		a.reapShellScan()
		return len(a.shellPick.found) > 0
	})
}

// lastArgv is the argv the window last started a shell on.
func (ta *testApp) lastArgv(t *testing.T) []string {
	t.Helper()
	ta.shellsMu.Lock()
	defer ta.shellsMu.Unlock()
	if len(ta.argvs) == 0 {
		t.Fatal("no shell has been started")
	}
	return ta.argvs[len(ta.argvs)-1]
}

// shellLines are the shell commands a menu is offering.
func shellLines(ids []string) []string {
	var out []string
	for _, id := range ids {
		if strings.HasPrefix(id, shellCommandPrefix) {
			out = append(out, id)
		}
	}
	return out
}

// fileMenuLines opens the File menu and returns the command ids on it.
func fileMenuLines(t *testing.T, a *testApp, bar *ui.Menubar) []string {
	t.Helper()
	if !bar.Open(menuTitled(t, bar, "File")) {
		t.Fatal("the File menu would not open")
	}
	menu, ok := a.root.Modal().(*ui.Menu)
	if !ok {
		t.Fatalf("top modal = %T, want a menu", a.root.Modal())
	}
	ids := menuCommands(menu)
	bar.Close()
	return ids
}

func TestShellCommandID(t *testing.T) {
	for id, want := range map[string]string{
		"cmd":                  shellCommandPrefix + "cmd",
		"powershell":           shellCommandPrefix + "powershell",
		"wsl:Ubuntu":           shellCommandPrefix + "wsl-ubuntu",
		"wsl:Ubuntu 22.04 LTS": shellCommandPrefix + "wsl-ubuntu-22-04-lts",
	} {
		if got := shellCommandID(id); got != want {
			t.Errorf("shellCommandID(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestChoosingAShellFromThePlusOpensAPaneOnIt drives the whole thing the
// way a user does: the plus on this machine's row, and the line for the
// shell they want.
func TestChoosingAShellFromThePlusOpensAPaneOnIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	menu := clickPlus(t, a, conns.Local)
	chooseMenuItem(t, menu, shellCommandID("pwsh"))

	want := argvOf(t, testShells(), "pwsh")
	if got := a.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("the pane started on %v, want %v", got, want)
	}
	if len(a.panes) != 2 {
		t.Errorf("%d panes, want the one the window opened with and the new one", len(a.panes))
	}
	checkTree(t, a)
}

// TestThePickedShellIsRememberedForTheNextTerminal checks the pick is
// written down and that "Terminal" opens on it afterwards, which is what
// keeps the common case one click.
func TestThePickedShellIsRememberedForTheNextTerminal(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("pwsh"))

	if id, saved := a.shellPick.remembered.Shell(); !saved || id != "pwsh" {
		t.Errorf("the settings remember %q (saved %v), want pwsh", id, saved)
	}
	chooseMenuItem(t, clickPlus(t, a, conns.Local), "conn.terminal")
	want := argvOf(t, testShells(), "pwsh")
	if got := a.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("Terminal opened on %v, want the remembered %v", got, want)
	}
}

// TestAShellThatIsNotInstalledIsNotOffered checks that the menu is built
// from what the looking found rather than from what Windows usually has.
func TestAShellThatIsNotInstalledIsNotOffered(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	without := []shells.Shell{
		{ID: "cmd", Title: "Command Prompt", Path: `C:\Windows\system32\cmd.exe`},
		{ID: "powershell", Title: "Windows PowerShell", Path: `C:\Windows\powershell.exe`},
	}
	onMachine(a, without)
	scanShells(t, a)

	ids := menuCommands(clickPlus(t, a, conns.Local))

	if !slices.Contains(ids, shellCommandID("cmd")) {
		t.Errorf("the menu does not offer the shells that are here: %v", ids)
	}
	if slices.Contains(ids, shellCommandID("pwsh")) {
		t.Errorf("the menu offers pwsh, which is not installed: %v", ids)
	}
	if _, ok := a.root.Commands.Lookup(shellCommandID("pwsh")); ok {
		t.Error("a command was registered for a shell that is not installed")
	}
}

// TestARememberedShellThatHasGoneFallsBackAndSaysSo checks the one case
// where the window opens something other than what was asked for. It is
// said once a run, not once a pane.
func TestARememberedShellThatHasGoneFallsBackAndSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	if err := a.shellPick.remembered.PutShell("wsl:Ubuntu"); err != nil {
		t.Fatalf("remember a shell: %v", err)
	}
	a.shellPick.namedShell = func(string) (shells.Shell, bool) { return shells.Shell{}, false }

	if err := a.openTabHere(); err != nil {
		t.Fatalf("open a tab: %v", err)
	}

	if got := a.lastArgv(t); len(got) != 0 {
		t.Errorf("the pane started on %v, want the default shell", got)
	}
	n := awaitModal[*ui.Notice](t, a, "a notice about the shell that has gone", nil)
	if !strings.Contains(n.Message(), "wsl:Ubuntu") {
		t.Errorf("the notice does not name the shell:\n%s", n.Message())
	}
	// Said once a run: another pane opens on the default in silence.
	modals := len(a.modals)
	if err := a.openTabHere(); err != nil {
		t.Fatalf("open another tab: %v", err)
	}
	a.pump.run()
	if len(a.modals) != modals {
		t.Errorf("%d dialogs after a second pane, want the %d already up", len(a.modals), modals)
	}
}

// TestTheFileMenuOffersTheSameShellsAsThePlus checks the two ways in are
// one list.
func TestTheFileMenuOffersTheSameShellsAsThePlus(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	bar := withMenubar(t, a)
	scanShells(t, a)

	onFile := shellLines(fileMenuLines(t, a, bar))
	onPlus := shellLines(menuCommands(clickPlus(t, a, conns.Local)))

	want := []string{shellCommandID("cmd"), shellCommandID("pwsh")}
	if !slices.Equal(onFile, want) {
		t.Errorf("the File menu offers %v, want %v", onFile, want)
	}
	if !slices.Equal(onPlus, want) {
		t.Errorf("the plus offers %v, want %v", onPlus, want)
	}
}

// TestOneShellAddsNoLines checks every machine that is not Windows:
// there is nothing to choose, so "Terminal" stays the only way in.
func TestOneShellAddsNoLines(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	bar := withMenubar(t, a)
	onMachine(a, []shells.Shell{{ID: "login", Title: "bash", Path: "/bin/bash"}})
	scanShells(t, a)

	if got := shellLines(fileMenuLines(t, a, bar)); len(got) != 0 {
		t.Errorf("the File menu offers %v on a machine with one shell", got)
	}
	ids := menuCommands(clickPlus(t, a, conns.Local))
	if got := shellLines(ids); len(got) != 0 {
		t.Errorf("the plus offers %v on a machine with one shell", got)
	}
	if !slices.Contains(ids, "conn.terminal") {
		t.Errorf("the plus no longer offers a terminal: %v", ids)
	}
}

// TestAShellThatWillNotStartIsNotRemembered checks the order: the pane
// opens first, and only a shell that started is worth writing down.
func TestAShellThatWillNotStartIsNotRemembered(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)
	a.newShell = func([]string, int, int) (session.Session, error) {
		return nil, errors.New("pwsh.exe is not where it was")
	}

	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("pwsh"))

	n := awaitModal[*ui.Notice](t, a, "a notice about the shell that would not start", nil)
	if !strings.Contains(n.Message(), "pwsh.exe is not where it was") {
		t.Errorf("the notice does not say why:\n%s", n.Message())
	}
	if id, saved := a.shellPick.remembered.Shell(); saved {
		t.Errorf("a shell that would not start was remembered as %q", id)
	}
}

// TestTheShellScanReportsAFailureAndKeepsWhatItFound checks the one
// failure the looking has: the WSL distributions could not be listed,
// and the shells that were found are still offered.
func TestTheShellScanReportsAFailureAndKeepsWhatItFound(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	var logged []error
	a.onError = func(err error) { logged = append(logged, err) }
	reason := "wsl.exe: the operation timed out"
	a.shellPick.findShells = func() ([]shells.Shell, error) {
		return testShells(), errors.New(reason)
	}

	scanShells(t, a)

	if len(logged) == 0 {
		t.Error("the failure was swallowed")
	}
	n, ok := a.root.Modal().(*ui.Notice)
	if !ok {
		t.Fatalf("top dialog = %T, want a notice about the shells", a.root.Modal())
	}
	if !strings.Contains(n.Message(), reason) {
		t.Errorf("the notice does not hold the reason:\n%s", n.Message())
	}
	if _, ok := a.root.Commands.Lookup(shellCommandID("pwsh")); !ok {
		t.Error("the shells that were found were thrown away with the failure")
	}
}
