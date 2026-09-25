package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/settings"
)

// shellEnds types exit into pane id's shell and waits for the pane to
// say so.
func shellEnds(t *testing.T, a *app, id, status string) {
	t.Helper()
	a.terminal(id).Paste("exit " + status + "\r")
	waitFor(t, a, "the shell to end", func() bool {
		for _, p := range a.st.Panes {
			if p.ID == id {
				return p.Ended
			}
		}
		return false
	})
}

func TestAPaneStaysWhenItsShellEndsAndStartsAgain(t *testing.T) {
	a, _ := agentApp(t)
	id := a.st.Panes[0].ID
	shellEnds(t, a, id, "3")
	// The status comes in after the output ends, and the question
	// takes it in when it does.
	waitFor(t, a, "the question with the status", func() bool {
		return a.terminal(id).Asking() == "The shell has finished. Exit 3."
	})
	if err := a.startAgain(id); err != nil {
		t.Fatal(err)
	}
	if a.st.Panes[0].Ended || a.terminal(id).Exited() {
		t.Fatal("started again, the pane still says it ended")
	}
	a.terminal(id).Paste("echo back-$((1+1))\r")
	waitFor(t, a, "the new shell to answer", func() bool { return strings.Contains(a.terminal(id).Text(), "back-2") })
}

func TestEnterClosesAPaneWhoseShellEnded(t *testing.T) {
	a, _ := agentApp(t)
	id := a.st.Panes[0].ID
	shellEnds(t, a, id, "0")
	if got := a.terminal(id).Asking(); got != "The shell has finished." {
		t.Fatalf("ended cleanly, the pane asks %q", got)
	}
	_, _ = a.terminal(id).HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter})
	waitFor(t, a, "the pane to close", func() bool { return !a.has(id) })
}

func TestAnAgentRestartsAPaneOnlyWhenAllowed(t *testing.T) {
	a, code := agentApp(t)
	id := a.st.Panes[0].ID
	c, sh := dial(t, a, code)
	shellEnds(t, a, id, "0")
	var err error
	asAgent(t, a, func() { _, err = c.Restart(sh.Panes[0].ID) })
	if err == nil || !strings.Contains(err.Error(), agent.BoxRestart) {
		t.Fatalf("restarting without the box said %v", err)
	}
	a.handle(SetAgentMay{Pane: id, May: settings.AgentMay{Restart: true}})
	var p agent.Pane
	asAgent(t, a, func() { p, err = c.Restart(sh.Panes[0].ID) })
	if err != nil || p.Ended {
		t.Fatalf("restarting said %+v, %v", p, err)
	}
}
