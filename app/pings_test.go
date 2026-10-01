package app

import "testing"

// A long command that finishes out of sight counts as a problem or as
// done, by its status; in the pane in front of its window it counts
// apart, for the window to send only while the user is in another
// program. Each counts in the window its pane is in.
func TestALongCommandFinishingCountsForAnEcho(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(one)
	a.focus("p2")
	a.commandDone("p1", 0)
	a.commandDone("p1", 2)
	a.commandDone("p2", 0)
	a.commandDone("p2", 1)
	a.commandDone("p2", 1)
	want := Pings{Dones: 1, Problems: 1, FrontDones: 1, FrontProblems: 2}
	if one.pings != want {
		t.Fatalf("the first window's counts are %+v, want %+v", one.pings, want)
	}
	// p3 is in front of the second window, which is behind: its own.
	a.commandDone("p3", 0)
	if two.pings != (Pings{FrontDones: 1}) || one.pings != want {
		t.Fatalf("a command in the window behind counted %+v there and %+v in front", two.pings, one.pings)
	}
	if st := a.stateFor(two, a.st); st.Pings != two.pings {
		t.Fatalf("the window behind is shown %+v", st.Pings)
	}
}
