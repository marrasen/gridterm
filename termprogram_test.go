package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/internal/build"
	"github.com/marrasen/gridterm/ui"
)

// valueOf is what an environment list says a name is, and whether it
// says anything.
func valueOf(env []string, name string) (string, bool) {
	for _, entry := range env {
		if got, value, ok := strings.Cut(entry, "="); ok && got == name {
			return value, true
		}
	}
	return "", false
}

// A pane is told this is gridterm, because TERM names a kind of
// terminal and says nothing about which one.
func TestAPaneIsToldWhichTerminalThisIs(t *testing.T) {
	env := paneEnv([]string{`C:\WINDOWS\system32\cmd.exe`}, "")

	if got, ok := valueOf(env, "TERM_PROGRAM"); !ok || got != build.Name {
		t.Errorf("TERM_PROGRAM is %q, want %q", got, build.Name)
	}
	if got, ok := valueOf(env, "TERM_PROGRAM_VERSION"); !ok || got == "" {
		t.Errorf("TERM_PROGRAM_VERSION is %q, want a version", got)
	}
}

// A user can have the window call itself a terminal that the program
// they are running has heard of, which is the only way to reach one
// that will never hear of gridterm.
func TestTheWindowCanCallItselfSomethingElse(t *testing.T) {
	env := paneEnv([]string{`C:\WINDOWS\system32\cmd.exe`}, "iTerm.app")

	if got, _ := valueOf(env, "TERM_PROGRAM"); got != "iTerm.app" {
		t.Errorf("TERM_PROGRAM is %q, want the name the user chose", got)
	}
}

// A pane in WSL needs the names listed in WSLENV as well. Without that
// they stop at the Windows side and the program in the distribution
// sees none of it, which is the case this whole change came from.
func TestAWSLPaneCarriesTheNamesIn(t *testing.T) {
	t.Setenv("WSLENV", "")

	env := paneEnv([]string{`C:\WINDOWS\system32\wsl.exe`, "-d", "Ubuntu"}, "")

	carried, ok := valueOf(env, "WSLENV")
	if !ok {
		t.Fatalf("a WSL pane carries nothing in: %v", env)
	}
	for _, want := range []string{"TERM_PROGRAM/u", "TERM_PROGRAM_VERSION/u"} {
		if !strings.Contains(carried, want) {
			t.Errorf("WSLENV is %q, want %q in it", carried, want)
		}
	}
}

// A pane on this machine needs no WSLENV, and writing one would carry
// the window's own names into every distribution the user opens later.
func TestAPaneOnThisMachineCarriesNothingIn(t *testing.T) {
	env := paneEnv([]string{`C:\WINDOWS\system32\cmd.exe`}, "")

	if got, ok := valueOf(env, "WSLENV"); ok {
		t.Errorf("a pane on this machine sets WSLENV to %q, want it left alone", got)
	}
}

// What the user already carries into WSL is kept.
func TestAWSLPaneKeepsWhatTheUserCarries(t *testing.T) {
	t.Setenv("WSLENV", "MY_TOOL/p")

	env := paneEnv([]string{"wsl.exe"}, "")

	carried, _ := valueOf(env, "WSLENV")
	if !strings.HasPrefix(carried, "MY_TOOL/p") {
		t.Errorf("WSLENV is %q, want what the user carries kept", carried)
	}
}

// The window falls back to gridterm's own name rather than to nothing,
// so a pane is never told the terminal is called "".
func TestAnUnsetNameIsGridtermsOwn(t *testing.T) {
	var none *termProgram

	if got := none.name(); got != "" {
		t.Errorf("a window with no settings calls itself %q, want nothing yet", got)
	}
	env := paneEnv([]string{"cmd.exe"}, none.name())
	if got, _ := valueOf(env, "TERM_PROGRAM"); got != build.Name {
		t.Errorf("it starts a pane calling itself %q, want %q", got, build.Name)
	}
}

// Nothing else is put in the environment. A pane inherits the window's,
// and a name added here reaches every program the user runs.
func TestOnlyTheTerminalsNameIsAdded(t *testing.T) {
	env := paneEnv([]string{"wsl.exe"}, "")

	var names []string
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		names = append(names, name)
	}
	slices.Sort(names)
	want := []string{"TERM_PROGRAM", "TERM_PROGRAM_VERSION", "WSLENV"}
	if !slices.Equal(names, want) {
		t.Errorf("a pane is started with %v, want %v", names, want)
	}
}

// The name is reachable from the window, not only from the settings
// file, and what is typed is what a new pane is started with.
func TestTheWindowCanBeToldWhatToCallItself(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	settingsFor(t, a)

	if err := a.openTermProgram(); err != nil {
		t.Fatalf("openTermProgram: %v", err)
	}
	f := awaitModal(t, a, "the name dialog", byTitle[*ui.Form]("What this window calls itself"))
	typeIntoField(t, a, f, "TERM_PROGRAM", "iTerm.app")
	pressButton(t, a, f, "Save")

	if f.Error() != nil {
		t.Fatalf("Save: %v", f.Error())
	}
	if got := a.called.name(); got != "iTerm.app" {
		t.Errorf("the window calls itself %q, want what was typed", got)
	}
	env := paneEnv([]string{"cmd.exe"}, a.called.name())
	if got, _ := valueOf(env, "TERM_PROGRAM"); got != "iTerm.app" {
		t.Errorf("a pane is told %q, want what was typed", got)
	}
}

// Clearing the field goes back to gridterm's own name rather than
// telling a pane the terminal is called nothing.
func TestClearingTheNameGoesBackToGridterm(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	settingsFor(t, a)
	if err := a.called.set("iTerm.app"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err := a.openTermProgram(); err != nil {
		t.Fatalf("openTermProgram: %v", err)
	}
	f := awaitModal(t, a, "the name dialog", byTitle[*ui.Form]("What this window calls itself"))
	retypeField(t, a, f, "TERM_PROGRAM", "")
	pressButton(t, a, f, "Save")

	if got := a.called.name(); got != "" {
		t.Errorf("the window calls itself %q, want gridterm's own", got)
	}
	if got, _ := valueOf(paneEnv([]string{"cmd.exe"}, a.called.name()), "TERM_PROGRAM"); got != build.Name {
		t.Errorf("a pane is told %q, want %q", got, build.Name)
	}
}

// XTVERSION answers with the same name a pane is told, so the two ways
// of asking never disagree.
func TestTheVersionAnswerMatchesTheName(t *testing.T) {
	a := newTestApp(t, 80, 24)
	settingsFor(t, a)
	if err := a.called.set("iTerm.app"); err != nil {
		t.Fatalf("set: %v", err)
	}

	answer := a.called.version()

	if !strings.HasPrefix(answer, "iTerm.app ") {
		t.Errorf("XTVERSION would answer %q, want it to start with the name a pane is told", answer)
	}
	if !strings.Contains(answer, build.Version()) {
		t.Errorf("XTVERSION would answer %q, want the version in it", answer)
	}
}

// settingsFor gives a test window somewhere of its own to keep what it
// remembers between runs.
func settingsFor(t *testing.T, a *testApp) {
	t.Helper()
	if _, err := withSettings(t, a, filepath.Join(t.TempDir(), "settings.json")); err != nil {
		t.Fatalf("settings: %v", err)
	}
}
