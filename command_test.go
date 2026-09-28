package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
)

func TestACommandRunsInAPaneOfItsOwnAndAgain(t *testing.T) {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	line := echoCommand("command-ran")
	a.handle(RunCommand{Line: line, Keep: true})
	if len(a.st.Panes) != 2 || !a.st.Panes[1].Command || a.st.Panes[1].Title != line {
		t.Fatalf("run, the panes are %+v; notices %+v", a.st.Panes, a.st.Notices)
	}
	if len(a.st.SavedCommands) != 1 || a.st.SavedCommands[0].Line != line {
		t.Fatalf("kept, the commands are %+v", a.st.SavedCommands)
	}
	id := a.st.Panes[1].ID
	question := line + " finished. Exit 0. Run it again?"
	waitFor(t, a, "the question", func() bool { return a.terminal(id).Asking() == question })
	if err := a.startAgain(id); err != nil {
		t.Fatal(err)
	}
	// Run again, the pane asks nothing until the command has ended
	// again, so the question coming back is the second run's end.
	// Counting the output twice failed on Windows, where the first
	// run's output can be gone from the screen by then.
	if got := a.terminal(id).Asking(); got != "" {
		t.Fatalf("run again, the pane still asks %q", got)
	}
	waitFor(t, a, "it to run again", func() bool {
		return a.terminal(id).Asking() == question && strings.Contains(a.terminal(id).Text(), "command-ran")
	})
	a.handle(RunSavedCommand{Saved: a.st.SavedCommands[0]})
	if len(a.st.Panes) != 3 || a.st.Panes[2].Title != line {
		t.Fatalf("run from the saved list, the panes are %+v", a.st.Panes)
	}
}

func TestAShellCanBeKeptForNewTerminals(t *testing.T) {
	was := findShells
	plain, echoer := plainShell(), shellSaying("i-am-the-echoer")
	findShells = func() ([]shellfind.Shell, error) {
		return []shellfind.Shell{
			{ID: "login", Title: "Plain", Path: plain[0]},
			{ID: "echoer", Title: "Echoer", Path: echoer[0], Args: echoer[1:]},
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
	if len(a.st.Panes) != 2 {
		t.Fatalf("opened, the panes are %+v; notices %+v", a.st.Panes, a.st.Notices)
	}
	waitFor(t, a, "the echoer", func() bool { return strings.Contains(a.terminal(a.st.Panes[1].ID).Text(), "i-am-the-echoer") })
	a.handle(PickShell{ID: "echoer"})
	if id, ok := set.Shell(); !ok || id != "echoer" || a.st.ChosenShell != "echoer" {
		t.Fatalf("kept, the settings say %q, %v", id, ok)
	}
	a.handle(NewTerminal{})
	if len(a.st.Panes) != 3 {
		t.Fatalf("a new terminal, the panes are %+v; notices %+v", a.st.Panes, a.st.Notices)
	}
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
