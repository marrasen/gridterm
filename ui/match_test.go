package ui

import "testing"

// A command is found by a word it answers to, as well as by its title.
//
// Connecting to another window is the reason: its command reads
// "Connect to another window…", so looking for "take over" found
// nothing at all.
func TestACommandIsFoundByAWordItAnswersTo(t *testing.T) {
	cmds := []Command{
		{ID: "serve.takeOver", Title: "Connect to another window…", AlsoFind: []string{"take over"}},
		{ID: "pane.open", Title: "New pane"},
	}

	got := MatchCommands(cmds, "take over")

	if len(got) != 1 {
		t.Fatalf("looking for taking over found %d commands, want the one", len(got))
	}
	if got[0].Command.ID != "serve.takeOver" {
		t.Errorf("it found %q", got[0].Command.ID)
	}
	// Nothing marked on the title, because the match is not in it.
	if got[0].At != nil {
		t.Errorf("it marked %v on the title, and the word it answered to is not there", got[0].At)
	}
}

// A word it answers to never beats a title, so the order stays the one
// the titles give.
func TestATitleBeatsAWordACommandAnswersTo(t *testing.T) {
	cmds := []Command{
		{ID: "view.theme", Title: "Colour theme…", AlsoFind: []string{"pane"}},
		{ID: "pane.open", Title: "New pane"},
	}

	got := MatchCommands(cmds, "pane")

	if len(got) != 2 {
		t.Fatalf("it found %d commands, want both", len(got))
	}
	if got[0].Command.ID != "pane.open" {
		t.Errorf("the first is %q, want the one with it in the title", got[0].Command.ID)
	}
}

// And a command with other words is still found by its title.
func TestAWordItAnswersToDoesNotHideTheTitle(t *testing.T) {
	cmds := []Command{{ID: "view.theme", Title: "Colour theme…", AlsoFind: []string{"palette"}}}

	got := MatchCommands(cmds, "colour")

	if len(got) != 1 {
		t.Fatalf("looking for the colour found %d commands, want the one", len(got))
	}
	if len(got[0].At) == 0 {
		t.Error("the match is not marked on the title")
	}
}
