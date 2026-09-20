package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/shells"
)

// freshSettings is a settings file nobody has written to.
func freshSettings(t *testing.T) *settings.Settings {
	t.Helper()
	set, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("load the settings: %v", err)
	}
	return set
}

// A window that has never been told either way sets its shells up.
// There is nothing to install, and without it a path a compiler
// printed is not clickable.
func TestAFreshWindowSetsItsShellsUp(t *testing.T) {
	if !freshSettings(t).ShellSetup() {
		t.Error("a window nobody has told either way leaves its shells alone")
	}
}

// A pane on this machine is taught to say what it is doing, without
// anybody installing anything.
func TestAPaneHereIsTaughtToSayWhatItIsDoing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	if err := a.shellSetup.set(true); err != nil {
		t.Fatalf("turn it on: %v", err)
	}

	if _, err := a.localTerminalOn(nil); err != nil {
		t.Fatalf("open a pane: %v", err)
	}

	got := a.shells[len(a.shells)-1].sentText()
	if !strings.Contains(got, "]7;") && !strings.Contains(got, "]9;9;") {
		t.Errorf("the shell was not told to say where it is:\n%q", got)
	}
	if !strings.HasSuffix(got, "\r") {
		t.Errorf("what was typed does not end with a return:\n%q", got)
	}
}

// Each shell is told in its own words. The Command Prompt builds its
// prompt out of what cmd.exe substitutes and cannot make a URL, so it
// says where it is as a plain path and marks nothing.
func TestEachShellIsToldInItsOwnWords(t *testing.T) {
	for _, tc := range []struct {
		id    string
		wants []string
		not   []string
	}{
		{"pwsh", []string{"]7;file://", "]133;A", "]133;C", "]133;D"}, nil},
		{"cmd", []string{"]9;9;"}, []string{"]133;"}},
		{"wsl:Ubuntu", []string{"]7;file://", "]133;A", "]133;C", "]133;D"}, nil},
	} {
		a := newTestApp(t, 80, 24)
		withPanel(t, a)
		withDialogs(t, a)
		a.commands()
		if err := a.shellSetup.set(true); err != nil {
			t.Fatalf("turn it on: %v", err)
		}
		sh, ok := shells.Lookup(testShells(), tc.id)
		if !ok {
			t.Fatalf("no test shell called %q", tc.id)
		}

		if err := a.openPaneOn(sh); err != nil {
			t.Fatalf("open a pane on %s: %v", tc.id, err)
		}

		got := a.shells[len(a.shells)-1].sentText()
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s was not told %s:\n%q", tc.id, want, got)
			}
		}
		for _, unwanted := range tc.not {
			if strings.Contains(got, unwanted) {
				t.Errorf("%s was told %s, which it cannot do:\n%q", tc.id, unwanted, got)
			}
		}
	}
}

// Turned off, nothing is typed into the shell at all.
func TestWithTheSetupOffNothingIsTypedIn(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()

	if _, err := a.localTerminalOn(nil); err != nil {
		t.Fatalf("open a pane: %v", err)
	}

	if got := a.shells[len(a.shells)-1].sentText(); got != "" {
		t.Errorf("%q was typed into a shell nobody asked to set up", got)
	}
}

// A pane running one command is left alone. It has no prompt to hook,
// and the line would land in that command's input.
func TestAPaneRunningACommandIsNotTaught(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	was := len(a.shells)

	if err := a.runCommandHere([]string{"cmd.exe", "/c", "echo hi"}, "", nil); err != nil {
		t.Fatalf("run it: %v", err)
	}

	if len(a.shells) <= was {
		t.Fatal("no session was opened for the command")
	}
	if got := a.shells[len(a.shells)-1].sentText(); got != "" {
		t.Errorf("%q was typed into a pane that runs one command", got)
	}
}

// A server is left alone unless its row says otherwise: the line goes
// into whatever login shell that account has, and only the user knows
// what that is.
func TestAServerIsNotTaughtUnlessItsRowSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	if err := a.book.Put(remote.Host{Name: "margit", Address: "margit.example"}, ""); err != nil {
		t.Fatalf("save the server: %v", err)
	}

	if a.setupOn("margit") {
		t.Error("a server nobody ticked would have the setup typed into it")
	}

	on, _ := a.book.Lookup("margit")
	on.Setup = true
	if err := a.book.Put(on, "margit"); err != nil {
		t.Fatalf("save it again: %v", err)
	}
	if !a.setupOn("margit") {
		t.Error("a server that was ticked would not have it")
	}
}

// Another gridterm is left alone whatever its row says. The window
// over there starts the shell and applies its own answer.
func TestAnotherGridtermIsNeverTaughtFromHere(t *testing.T) {
	a := newTestApp(t, 80, 24)
	if err := a.book.Put(remote.Host{
		Name: "laptop", Address: "laptop.example", Window: true, Setup: true,
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}

	if a.setupOn("laptop") {
		t.Error("this window would set up a shell the other window started")
	}
}

// A machine the window reached by a typed target was never saved, so
// there is no row to read and nothing is typed.
func TestAMachineThatWasNeverSavedIsNotTaught(t *testing.T) {
	a := newTestApp(t, 80, 24)

	if a.setupOn("someone@nowhere.example") {
		t.Error("a machine with no row would have the setup typed into it")
	}
}
