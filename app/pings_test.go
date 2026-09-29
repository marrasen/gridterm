package app

import "testing"

// A long command that finishes out of sight counts as a problem or as
// done, by its status; in the pane in front it counts apart, for the
// window to send only while the user is in another program.
func TestALongCommandFinishingCountsForAnEcho(t *testing.T) {
	a := &app{}
	a.st.Focus = "front"
	a.commandDone("back", 0)
	a.commandDone("back", 2)
	a.commandDone("front", 0)
	a.commandDone("front", 1)
	a.commandDone("front", 1)
	want := Pings{Dones: 1, Problems: 1, FrontDones: 1, FrontProblems: 2}
	if a.st.Pings != want {
		t.Fatalf("the counts are %+v, want %+v", a.st.Pings, want)
	}
}
