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

// Typing a word puts the thing named after that word on top, and marks
// the letters of that word rather than letters scattered across others.
//
// Marcus typed "WSL" and got "Write a starting keyboard shortcuts file"
// above "New pane on Ubuntu (WSL)", which was eighth. The W of "Write",
// the s of "starting" and an l further along scored three word starts;
// the three letters of "WSL" together scored almost nothing. Worse, the
// row that did match marked the w of "New" and then the S and L of
// "WSL", because the matcher took the first letter it saw.
func TestAWordTypedWholeComesFirst(t *testing.T) {
	cmds := []Command{
		{ID: "keys.write", Title: "Write a starting keyboard shortcuts file"},
		{ID: "pane.wsl", Title: "New pane on Ubuntu (WSL)"},
		{ID: "win.select", Title: "Show all panes"},
	}

	got := MatchCommands(cmds, "WSL")

	if len(got) == 0 {
		t.Fatal("it found nothing")
	}
	if got[0].Command.ID != "pane.wsl" {
		t.Errorf("the first is %q, want the one named after WSL: %v", got[0].Command.ID, matchIDs(got))
	}
	// And the three letters of WSL are what is marked.
	title := []rune("New pane on Ubuntu (WSL)")
	for _, at := range got[0].At {
		if at < 0 || at >= len(title) {
			t.Fatalf("it marked rune %d of a title %d long", at, len(title))
		}
	}
	marked := make([]rune, 0, len(got[0].At))
	for _, at := range got[0].At {
		marked = append(marked, title[at])
	}
	if string(marked) != "WSL" {
		t.Errorf("it marked %q, want the letters of WSL", string(marked))
	}
	// The marked letters are next to each other, which is what makes
	// them read as the word rather than as three hits.
	for i := 1; i < len(got[0].At); i++ {
		if got[0].At[i] != got[0].At[i-1]+1 {
			t.Errorf("it marked %v, want three letters in a row", got[0].At)
			break
		}
	}
}

// A word that is part of a longer one is found, and still loses to the
// same word standing on its own.
func TestAWholeWordBeatsAWordInsideAnother(t *testing.T) {
	cmds := []Command{
		{ID: "a.inside", Title: "Reconnect the panel"},
		{ID: "b.whole", Title: "Open the pane"},
	}

	got := MatchCommands(cmds, "pane")

	if got[0].Command.ID != "b.whole" {
		t.Errorf("the first is %q, want the one where pane is a word: %v",
			got[0].Command.ID, matchIDs(got))
	}
}

// The first letters of words still find a command, which is the other
// way people use the palette.
func TestTheFirstLettersOfWordsStillMatch(t *testing.T) {
	cmds := []Command{
		{ID: "pane.new", Title: "New pane here"},
		{ID: "other", Title: "Number of panes, hidden"},
	}

	got := MatchCommands(cmds, "nph")

	if len(got) == 0 {
		t.Fatal("the first letters of the words found nothing")
	}
	if got[0].Command.ID != "pane.new" {
		t.Errorf("the first is %q, want the one whose words start with them: %v",
			got[0].Command.ID, matchIDs(got))
	}
}
