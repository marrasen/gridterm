package ui

import "testing"

// A command is found by a word it answers to, as well as by its title.
//
// The colour schemes are the reason: their command reads "Colour
// scheme…", so looking for "theme" in the palette found nothing at all.
func TestACommandIsFoundByAWordItAnswersTo(t *testing.T) {
	cmds := []Command{
		{ID: "view.theme", Title: "Colour scheme…", AlsoFind: []string{"theme"}},
		{ID: "pane.open", Title: "New pane"},
	}

	got := MatchCommands(cmds, "theme")

	if len(got) != 1 {
		t.Fatalf("looking for a theme found %d commands, want the one", len(got))
	}
	if got[0].Command.ID != "view.theme" {
		t.Errorf("it found %q", got[0].Command.ID)
	}
}

// A word it answers to never beats a title, so the order stays the one
// the titles give.
func TestATitleBeatsAWordACommandAnswersTo(t *testing.T) {
	cmds := []Command{
		{ID: "view.theme", Title: "Colour scheme…", AlsoFind: []string{"pane"}},
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
	cmds := []Command{{ID: "view.theme", Title: "Colour scheme…", AlsoFind: []string{"theme"}}}

	got := MatchCommands(cmds, "colour")

	if len(got) != 1 {
		t.Fatalf("looking for the colour found %d commands, want the one", len(got))
	}
	if len(got[0].At) == 0 {
		t.Error("the match is not marked on the title")
	}
}
