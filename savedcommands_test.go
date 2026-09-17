package main

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
)

// aCommandWindow is a window whose settings are in a file the test owns,
// given to it the way the program gives them: through useSettings.
func aCommandWindow(t *testing.T) (*testApp, string) {
	t.Helper()
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
	}
	return a, path
}

// commandDialog opens the dialog that runs a command on this machine.
func commandDialog(t *testing.T, a *testApp) *ui.Form {
	t.Helper()
	a.askCommandOn(conns.Local, nil)
	return awaitModal(t, a, "the command dialog", byTitle[*ui.Form]("Run a command on Local"))
}

// savedLines is what the window has kept, newest first.
func savedLines(a *testApp) []string { return a.saved.lines() }

// A command the user asked to keep is kept, with the directory it ran in
// and the machine it ran on.
func TestACommandAskedToBeKeptIsKept(t *testing.T) {
	a, _ := aCommandWindow(t)

	f := commandDialog(t, a)
	typeIntoField(t, a, f, "Command", "make deploy")
	typeIntoField(t, a, f, "Directory", "/home/marcus/src")
	tickBox(t, a, f, "Remember this command")
	pressButton(t, a, f, "Run")

	got, have := a.saved.find("make deploy")
	if !have {
		t.Fatalf("the window kept %v, want the command it was asked to keep", savedLines(a))
	}
	if got.Dir != "/home/marcus/src" {
		t.Errorf("it was kept to run in %q", got.Dir)
	}
	if got.Host != conns.Local {
		t.Errorf("it was kept as running on %q", got.Host)
	}
	// And it ran where it said, not only saved that it would.
	if len(a.dirs) == 0 || a.dirs[len(a.dirs)-1] != "/home/marcus/src" {
		t.Errorf("the command ran in %v", a.dirs)
	}
}

// A command run without asking is not kept, and nothing already kept is
// disturbed.
func TestACommandRunWithoutAskingIsNotKept(t *testing.T) {
	a, _ := aCommandWindow(t)
	if err := a.saved.keep(settings.SavedCommand{Line: "make deploy"}); err != nil {
		t.Fatalf("keep: %v", err)
	}

	f := commandDialog(t, a)
	typeIntoField(t, a, f, "Command", "ls -la")
	pressButton(t, a, f, "Run")

	if got := savedLines(a); !slices.Equal(got, []string{"make deploy"}) {
		t.Errorf("the window keeps %v, want only what it was asked to keep", got)
	}
}

// A saved command that is the start of a longer one does not tick the
// box on the way past. Typing "make deploy all" goes through "make
// deploy", and nothing may be saved or forgotten by that.
func TestTypingPastASavedCommandKeepsNothing(t *testing.T) {
	a, _ := aCommandWindow(t)
	if err := a.saved.keep(settings.SavedCommand{
		Line: "make deploy", Dir: "/saved", Host: conns.Local,
	}); err != nil {
		t.Fatalf("keep: %v", err)
	}

	f := commandDialog(t, a)
	typeIntoField(t, a, f, "Command", "make deploy all")

	if f.Field("Remember this command").On() {
		t.Error("typing past a saved command ticked the box")
	}
	if got := f.Field("Directory").Text(); got != "" {
		t.Errorf("typing past a saved command put %q in the directory", got)
	}
	pressButton(t, a, f, "Run")
	if got := savedLines(a); !slices.Equal(got, []string{"make deploy"}) {
		t.Errorf("the window keeps %v", got)
	}
}

// The dialog offers what has been kept, newest first, so a command run
// often is not retyped.
func TestTheDialogOffersTheKeptCommandsNewestFirst(t *testing.T) {
	a, _ := aCommandWindow(t)
	for _, line := range []string{"make deploy", "ls -la"} {
		if err := a.saved.keep(settings.SavedCommand{Line: line}); err != nil {
			t.Fatalf("keep %q: %v", line, err)
		}
	}

	f := commandDialog(t, a)

	what := f.Field("Command")
	if what == nil {
		t.Fatal("the dialog has no Command field")
	}
	if want := []string{"ls -la", "make deploy"}; !slices.Equal(what.Options, want) {
		t.Errorf("the field offers %v, want %v", what.Options, want)
	}
}

// Picking a saved command brings back the directory it was kept with and
// ticks the box.
func TestPickingASavedCommandBringsBackItsDirectory(t *testing.T) {
	a, _ := aCommandWindow(t)
	if err := a.saved.keep(settings.SavedCommand{
		Line: "make deploy", Dir: "/home/marcus/src", Host: conns.Local,
	}); err != nil {
		t.Fatalf("keep: %v", err)
	}

	f := commandDialog(t, a)
	stepOptions(t, a, f, "Command")

	if got := f.Field("Command").Text(); got != "make deploy" {
		t.Fatalf("the field says %q, so nothing was picked", got)
	}
	if got := f.Field("Directory").Text(); got != "/home/marcus/src" {
		t.Errorf("the directory says %q, want the one the command was kept with", got)
	}
	if !f.Field("Remember this command").On() {
		t.Error("the box does not say the command is one that is kept")
	}
}

// A directory the user typed is theirs: picking a command does not write
// over it.
func TestPickingACommandLeavesADirectoryAlreadyTyped(t *testing.T) {
	a, _ := aCommandWindow(t)
	if err := a.saved.keep(settings.SavedCommand{
		Line: "make deploy", Dir: "/saved", Host: conns.Local,
	}); err != nil {
		t.Fatalf("keep: %v", err)
	}

	f := commandDialog(t, a)
	typeIntoField(t, a, f, "Directory", "/typed")
	stepOptions(t, a, f, "Command")

	if got := f.Field("Directory").Text(); got != "/typed" {
		t.Errorf("the directory says %q, want what the user typed", got)
	}
}

// A directory saved on one machine is not offered on another: a path
// belongs to the machine it was typed on.
func TestADirectorySavedElsewhereIsNotOffered(t *testing.T) {
	a, _ := aCommandWindow(t)
	if err := a.saved.keep(settings.SavedCommand{
		Line: "make deploy", Dir: "/on/margit", Host: "margit",
	}); err != nil {
		t.Fatalf("keep: %v", err)
	}

	f := commandDialog(t, a)
	stepOptions(t, a, f, "Command")

	if got := f.Field("Directory").Text(); got != "" {
		t.Errorf("the directory says %q, and that path is on another machine", got)
	}
	if !f.Field("Remember this command").On() {
		t.Error("the command is one that is kept, whatever machine its directory is on")
	}
}

// Clearing the box on a command taken off the list forgets it. That is
// the way back out of the list.
func TestClearingTheBoxOnAPickedCommandForgetsIt(t *testing.T) {
	a, _ := aCommandWindow(t)
	if err := a.saved.keep(settings.SavedCommand{Line: "make deploy"}); err != nil {
		t.Fatalf("keep: %v", err)
	}

	f := commandDialog(t, a)
	stepOptions(t, a, f, "Command")
	if !f.Field("Remember this command").On() {
		t.Fatal("the box does not say the command is kept, so clearing it proves nothing")
	}
	tickBox(t, a, f, "Remember this command")
	pressButton(t, a, f, "Run")

	if got := savedLines(a); len(got) != 0 {
		t.Errorf("the window still keeps %v", got)
	}
}

// A command typed out by hand is never forgotten by a box the user did
// not tick, however much it looks like a saved one.
func TestTypingASavedCommandDoesNotForgetIt(t *testing.T) {
	a, _ := aCommandWindow(t)
	if err := a.saved.keep(settings.SavedCommand{Line: "make deploy"}); err != nil {
		t.Fatalf("keep: %v", err)
	}

	f := commandDialog(t, a)
	typeIntoField(t, a, f, "Command", "make deploy")
	pressButton(t, a, f, "Run")

	if got := savedLines(a); !slices.Equal(got, []string{"make deploy"}) {
		t.Errorf("the window keeps %v, want the command it was keeping", got)
	}
}

// The spacing a command was typed with does not make it a different
// command.
func TestSpacingDoesNotMakeADifferentCommand(t *testing.T) {
	a, _ := aCommandWindow(t)

	f := commandDialog(t, a)
	typeIntoField(t, a, f, "Command", "  make   deploy  ")
	tickBox(t, a, f, "Remember this command")
	pressButton(t, a, f, "Run")

	if got := savedLines(a); !slices.Equal(got, []string{"make deploy"}) {
		t.Errorf("the window keeps %v, want the command with its spacing tidied", got)
	}
}

// A command that could not be kept stops the run and says why. Running
// it anyway would leave the user thinking it had been kept.
func TestACommandThatCannotBeKeptIsNotRun(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.useSettings(settings.Unusable(errors.New("the settings file is unreadable")))
	// The window says so as it starts, which is not what this is about.
	awaitModal(t, a, "the notice about the settings",
		byTitle[*ui.Notice]("The settings could not be read"))
	dismissNotice(t, a)

	f := commandDialog(t, a)
	typeIntoField(t, a, f, "Command", "make deploy")
	tickBox(t, a, f, "Remember this command")
	pressButton(t, a, f, "Run")

	if a.root.Modal() != f {
		t.Fatal("the dialog closed although the command could not be kept")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why the command was not kept")
	}
	if len(a.panes) != 1 {
		t.Errorf("the window has %d panes, and the command should not have run", len(a.panes))
	}
}

// What is kept survives a restart, which is the whole point of keeping
// it: a second window over the same file offers it.
func TestKeptCommandsSurviveARestart(t *testing.T) {
	a, path := aCommandWindow(t)
	f := commandDialog(t, a)
	typeIntoField(t, a, f, "Command", "make deploy")
	typeIntoField(t, a, f, "Directory", "/home/marcus/src")
	tickBox(t, a, f, "Remember this command")
	pressButton(t, a, f, "Run")

	again := newTestApp(t, 80, 24)
	withDialogs(t, again)
	if _, err := withSettings(t, again, path); err != nil {
		t.Fatalf("the second window's settings: %v", err)
	}

	next := commandDialog(t, again)
	if got := next.Field("Command").Options; !slices.Equal(got, []string{"make deploy"}) {
		t.Fatalf("the second window offers %v", got)
	}
	stepOptions(t, again, next, "Command")
	if got := next.Field("Directory").Text(); got != "/home/marcus/src" {
		t.Errorf("the second window says the directory is %q", got)
	}
}

// A command is kept as running on the machine the dialog was opened
// for, so its directory is offered back only there.
func TestACommandIsKeptAgainstItsMachine(t *testing.T) {
	a, _ := aCommandWindow(t)
	a.askCommandOn("margit", nil)
	f := awaitModal(t, a, "the command dialog on margit",
		byTitle[*ui.Form]("Run a command on margit"))

	typeIntoField(t, a, f, "Command", "systemctl status nginx")
	typeIntoField(t, a, f, "Directory", "/etc/nginx")
	tickBox(t, a, f, "Remember this command")
	pressButton(t, a, f, "Run")

	got, have := a.saved.find("systemctl status nginx")
	if !have {
		t.Fatalf("the window kept %v", savedLines(a))
	}
	if got.Host != "margit" {
		t.Errorf("it was kept as running on %q, want the machine the dialog was for", got.Host)
	}
}
