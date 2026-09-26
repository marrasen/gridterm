package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/settings"
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
