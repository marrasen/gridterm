package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
	shellfind "github.com/marrasen/gridterm/shells"
)

func TestACommandRunsInAPaneOfItsOwnAndAgain(t *testing.T) {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.handle(RunCommand{Line: "echo command-ran", Keep: true})
	if len(a.st.Panes) != 2 || !a.st.Panes[1].Command || a.st.Panes[1].Title != "echo command-ran" {
		t.Fatalf("run, the panes are %+v", a.st.Panes)
	}
	if len(a.st.SavedCommands) != 1 || a.st.SavedCommands[0].Line != "echo command-ran" {
		t.Fatalf("kept, the commands are %+v", a.st.SavedCommands)
	}
	id := a.st.Panes[1].ID
	waitFor(t, a, "the question", func() bool {
		return a.terminal(id).Asking() == "echo command-ran finished. Exit 0. Run it again?"
	})
	if err := a.startAgain(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "it to run again", func() bool {
		return strings.Count(a.terminal(id).Text(), "command-ran") == 2 && a.terminal(id).Asking() != ""
	})
	a.handle(RunSavedCommand{Saved: a.st.SavedCommands[0]})
	if len(a.st.Panes) != 3 || a.st.Panes[2].Title != "echo command-ran" {
		t.Fatalf("run from the saved list, the panes are %+v", a.st.Panes)
	}
}

func TestAShellCanBeKeptForNewTerminals(t *testing.T) {
	was := findShells
	findShells = func() ([]shellfind.Shell, error) {
		return []shellfind.Shell{
			{ID: "login", Title: "sh", Path: "/bin/sh"},
			{ID: "echoer", Title: "Echoer", Path: "/bin/sh", Args: []string{"-c", "echo i-am-the-echoer; sleep 5"}},
		}, nil
	}
	t.Cleanup(func() { findShells = was })
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.scanShells()
	waitFor(t, a, "the shells", func() bool { return len(a.st.Shells) == 2 })
	a.handle(OpenShellNamed{ID: "echoer"})
	waitFor(t, a, "the echoer", func() bool { return strings.Contains(a.terminal(a.st.Panes[1].ID).Text(), "i-am-the-echoer") })
	a.handle(PickShell{ID: "echoer"})
	if id, ok := set.Shell(); !ok || id != "echoer" || a.st.ChosenShell != "echoer" {
		t.Fatalf("kept, the settings say %q, %v", id, ok)
	}
	a.handle(NewTerminal{})
	waitFor(t, a, "the echoer again", func() bool { return strings.Contains(a.terminal(a.st.Panes[2].ID).Text(), "i-am-the-echoer") })
	a.handle(PickShell{})
	if _, ok := set.Shell(); ok {
		t.Fatal("back to the default, a shell is still kept")
	}
}

func TestASavedCommandPickedAndUntickedIsForgotten(t *testing.T) {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.handle(RunCommand{Line: "echo kept", Keep: true})
	a.handle(RunCommand{Line: "echo kept", Forget: "echo kept"})
	if len(a.st.SavedCommands) != 0 {
		t.Fatalf("unticked, the commands are %+v", a.st.SavedCommands)
	}
}

func TestThingsSavedBeforeServersHadIDsAreGivenThem(t *testing.T) {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	if err := set.KeepCommand(settings.SavedCommand{Line: "top", Host: "desk"}, mostSavedCommands); err != nil {
		t.Fatal(err)
	}
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "desk", Address: "desk.example"}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	a.giveSavedIDs()
	h, _ := book.Lookup("desk")
	if got := set.Commands()[0].HostID; got == "" || got != h.ID {
		t.Fatalf("the command is on server id %q, want %q", got, h.ID)
	}
}
