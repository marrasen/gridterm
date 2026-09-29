package app

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/agent"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/settings"
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
	line, want := saysAnswer("back")
	a.terminal(id).Paste(line + "\r")
	waitFor(t, a, "the new shell to answer", func() bool { return strings.Contains(a.terminal(id).Text(), want) })
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

// A pane opened on a shell picked by name starts that shell again, not
// the usual one.
func TestAPaneStartsItsOwnShellAgain(t *testing.T) {
	a, _ := agentApp(t)
	id := a.st.Panes[0].ID
	a.argvs[id] = shellSaying("the-picked-one")
	shellEnds(t, a, id, "0")
	if err := a.startAgain(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the picked shell", func() bool { return strings.Contains(a.terminal(id).Text(), "the-picked-one") })
}

// A pane on a server whose connection has gone connects again when
// started again, and says so when the server is at another address
// than the pane was opened at.
func TestAPaneReconnectsWhenStartedAgain(t *testing.T) {
	testhome.New(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	s := sshtest.New(t)
	host, port := s.Host()
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		for _, id := range a.machines.Connected() {
			c := a.machines.Get(id).Conn
			_ = c.Close()
		}
	})
	// Answers whatever is asked: the host key, and the password.
	answering := func() {
		for _, q := range a.st.Asks {
			ans := AskAnswered{ID: q.ID, Yes: true}
			if len(q.Prompts) > 0 {
				ans.Answers = []string{sshtest.Password}
			}
			a.handle(ans)
		}
	}
	target := "tester@" + net.JoinHostPort(host, strconv.Itoa(port))
	a.handle(ConnectTo{Target: target})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	id, name := a.st.Panes[0].ID, a.st.Panes[0].Machine
	if a.paneAt[id] == "" {
		t.Fatal("the pane's address was not written down")
	}
	// As if the saved server had moved since the pane opened.
	a.paneAt[id] = "tester@elsewhere:22"

	_ = a.machines.Get(name).Conn.Close()
	waitFor(t, a, "the pane to end", func() bool { return a.st.Panes[0].Ended && a.machines.Get(name).Conn == nil })
	if err := a.startAgain(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the pane to run again", func() bool { answering(); return !a.st.Panes[0].Ended })
	waitFor(t, a, "the line saying it moved", func() bool {
		// The line wraps at the screen's edge.
		said := strings.ReplaceAll(a.terminal(id).Text(), "\n", "")
		return strings.Contains(said, "is "+a.machines.Name(name)+" now. This pane was on tester@elsewhere:22")
	})
}

// An agent never makes the window dial: a pane whose machine has gone
// is not started again for it, and a pane that runs one command opens
// no shell beside it.
func TestAnAgentMakesTheWindowDialNothing(t *testing.T) {
	a, code := agentApp(t)
	id := a.st.Panes[0].ID
	c, sh := dial(t, a, code)
	a.handle(SetAgentMay{Pane: id, May: settings.AgentMay{Restart: true, OpenMore: true}})
	shellEnds(t, a, id, "0")
	a.setPane(id, func(p *Pane) { p.Machine = "gone" })
	var err error
	asAgent(t, a, func() { _, err = c.Restart(sh.Panes[0].ID) })
	if err == nil || !strings.Contains(err.Error(), "opening connections is the user's") {
		t.Fatalf("restarting on a machine let go of said %v", err)
	}
	if a.terminal(id).Asking() == "" {
		t.Fatal("refused, the pane's question went")
	}
}

func TestClearFinishedClosesEndedPanes(t *testing.T) {
	a, _ := agentApp(t)
	if err := a.open("", Placement{}); err != nil {
		t.Fatal(err)
	}
	ended := a.st.Panes[0].ID
	shellEnds(t, a, ended, "0")
	a.handle(ClearFinished{})
	waitFor(t, a, "the ended pane to close", func() bool { return !a.has(ended) })
	if len(a.st.Panes) != 1 {
		t.Fatalf("cleared, the panes are %+v", a.st.Panes)
	}
}

// A command's question names it, cut short when long, and says how it
// ended: finished with a status, cut off, never run, or just stopped.
func TestACommandsQuestionSaysHowItEnded(t *testing.T) {
	short := []string{"make", "test"}
	for _, c := range []struct {
		status        int
		known, notRun bool
		cutOff        bool
		want          string
	}{
		{0, true, false, false, "make test finished. Exit 0. Run it again?"},
		{0, false, false, true, "The connection went while make test was running. Run it again?"},
		{0, false, true, false, "The connection was not made, so make test did not run. Run it again?"},
		{0, false, false, false, "make test has stopped. Run it again?"},
	} {
		if got := commandQuestion(short, c.status, c.known, c.notRun, c.cutOff); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
	long := strings.Fields("rsync -avz --delete /home/me/projects/kakel/ backup.example:/srv/backups/kakel/")
	if got := commandQuestion(long, 0, true, false, false); !strings.HasPrefix(got, "rsync -avz --delete /home/me/projects/k") || !strings.Contains(got, "…") {
		t.Errorf("a long command reads %q", got)
	}
}
