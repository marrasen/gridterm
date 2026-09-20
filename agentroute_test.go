package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/remote"
)

// A connection the window is already holding, registered without
// dialling one, so a test can say what it was reached with.
func machineHeld(t *testing.T, a *testApp, name string, forwarding bool) {
	t.Helper()
	m := &machine{at: step{
		name: name,
		cfg:  remote.Config{Host: name + ".example", User: "tester", ForwardAgent: forwarding},
	}}
	a.machines.take(m)
	// Taken out again before the window closes, which would close a
	// connection this one never made.
	t.Cleanup(func() { a.machines.drop(m) })
}

// Turning the SSH agent off for a machine that is connected has to stop
// the keys going. The far end keeps its way to the agent while the
// connection is up, so a new pane on that connection is refused and the
// message says to close it.
func TestUntickingTheAgentRefusesTheLiveConnection(t *testing.T) {
	a := newTestApp(t, 80, 24)
	machineHeld(t, a, "build", true)

	// What the saved server says now: the tick is off.
	off := step{name: "build", cfg: remote.Config{
		Host: "build.example", User: "tester", ForwardAgent: false,
	}}

	_, _, err := a.plan([]step{off})

	if err == nil {
		t.Fatal("a pane was opened on a connection still carrying the agent after it was turned off")
	}
	if !strings.Contains(err.Error(), "close that connection first") {
		t.Errorf("it says %q, want it to say to close the connection", err)
	}
	if !strings.Contains(err.Error(), "stays reachable") {
		t.Errorf("it says %q, want it to say why closing is the only way", err)
	}
}

// Ticking it on while connected does not quietly do nothing either.
func TestTickingTheAgentOnRefusesTheLiveConnection(t *testing.T) {
	a := newTestApp(t, 80, 24)
	machineHeld(t, a, "build", false)

	on := step{name: "build", cfg: remote.Config{
		Host: "build.example", User: "tester", ForwardAgent: true,
	}}

	if _, _, err := a.plan([]step{on}); err == nil {
		t.Fatal("a pane opened carrying no agent on a machine now ticked to carry one")
	}
}

// A machine whose tick has not changed is reused, which is what every
// second pane depends on.
func TestAnUnchangedAgentTickReusesTheConnection(t *testing.T) {
	for _, forwarding := range []bool{false, true} {
		a := newTestApp(t, 80, 24)
		machineHeld(t, a, "build", forwarding)

		same := step{name: "build", cfg: remote.Config{
			Host: "build.example", User: "tester", ForwardAgent: forwarding,
		}}

		through, missing, err := a.plan([]step{same})

		if err != nil {
			t.Fatalf("the agent tick %v: plan: %v", forwarding, err)
		}
		if through == nil {
			t.Errorf("the agent tick %v: the connection already open was not used", forwarding)
		}
		if len(missing) != 0 {
			t.Errorf("the agent tick %v: it wants to dial %v, want nothing", forwarding, missing)
		}
	}
}
