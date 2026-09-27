package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/settings"
)

// agentApp is the program side with one terminal pane, p1, running
// /bin/sh, shared with an agent, and the code for the share.
func agentApp(t *testing.T) (a *app, code string) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a = newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	if err := a.open("", placement{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = a.stopSharing()
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
	})
	a.handle(SharePane{Pane: a.st.Panes[0].ID})
	if a.st.Share.Code == "" || len(a.st.Share.Panes) != 1 {
		t.Fatalf("shared, the share reads %+v", a.st.Share)
	}
	return a, a.st.Share.Code
}

// asAgent runs f on a goroutine of its own, as an agent's calls come,
// and runs the program's side until it is done.
func asAgent(t *testing.T, a *app, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	waitFor(t, a, "the agent's calls", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
}

// dial connects as an agent and uses the code.
func dial(t *testing.T, a *app, code string) (c *agent.Client, sh agent.Share) {
	t.Helper()
	var err error
	asAgent(t, a, func() {
		c, err = agent.Dial(code)
		if err == nil {
			sh, err = c.Use(code)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, sh
}

func TestAnAgentTypesIntoASharedPaneAndReadsItBack(t *testing.T) {
	a, code := agentApp(t)
	c, sh := dial(t, a, code)
	if len(sh.Panes) != 1 || sh.Panes[0].Label != "Terminal 1 on this machine" {
		t.Fatalf("the share holds %+v", sh.Panes)
	}
	id := sh.Panes[0].ID
	var err error
	asAgent(t, a, func() { err = c.Send(id, "echo hi-$((40+2))", []string{"Enter"}) })
	if err != nil {
		t.Fatal(err)
	}
	var look agent.Look
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(look.Screen, "hi-42") && time.Now().Before(deadline) {
		asAgent(t, a, func() { look, err = c.Read(id, 0) })
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(look.Screen, "hi-42") {
		t.Fatalf("the agent read %q", look.Screen)
	}
	if a.st.Share.Panes[0].Note != "an agent is working here" {
		t.Fatalf("with an agent at work, the pane's row says %q", a.st.Share.Panes[0].Note)
	}
}

func TestAWrongCodeIsRefused(t *testing.T) {
	a, code := agentApp(t)
	wrong := code[:len(code)-4] + "aaaa"
	var err error
	asAgent(t, a, func() {
		var c *agent.Client
		if c, err = agent.Dial(wrong); err == nil {
			_, err = c.Use(wrong)
			_ = c.Close()
		}
	})
	if err == nil {
		t.Fatal("a wrong code was taken")
	}
}

func TestAReadOnlyPaneRefusesTyping(t *testing.T) {
	a, code := agentApp(t)
	a.handle(SetAgentMay{Pane: a.st.Panes[0].ID, May: settings.AgentMay{ReadOnly: true}})
	c, sh := dial(t, a, code)
	var err error
	asAgent(t, a, func() { err = c.Send(sh.Panes[0].ID, "rm -rf /", []string{"Enter"}) })
	if err == nil || !strings.Contains(err.Error(), agent.BoxReadOnly) {
		t.Fatalf("typing into a read-only pane said %v", err)
	}
}

func TestTakingTheLastPaneBackEndsTheShare(t *testing.T) {
	a, code := agentApp(t)
	c, sh := dial(t, a, code)
	a.handle(UnsharePane{Pane: a.st.Panes[0].ID})
	if a.st.Share.Code != "" || a.agents.server != nil {
		t.Fatalf("with nothing shared, the share is %+v and the server %v", a.st.Share, a.agents.server)
	}
	var err error
	asAgent(t, a, func() { _, err = c.Read(sh.Panes[0].ID, 0) })
	if err == nil {
		t.Fatal("the agent still reads a pane taken back")
	}
}

func TestAnAgentsSecretIsAnsweredWhenTheUserTypesIt(t *testing.T) {
	a, code := agentApp(t)
	c, sh := dial(t, a, code)
	pane := a.st.Panes[0].ID
	answered := make(chan bool, 1)
	go func() {
		ok, err := c.Secret(sh.Panes[0].ID, "the sudo password", 10*time.Second)
		answered <- ok && err == nil
	}()
	waitFor(t, a, "the ask on the pane", func() bool { return a.terminal(pane).AskedForASecret() })
	if said := strings.ReplaceAll(a.terminal(pane).Text(), "\n", ""); !strings.Contains(said, `It asked for: "thesudo password"`) && !strings.Contains(said, `It asked for: "the sudo password"`) {
		t.Fatalf("the pane says %q", a.terminal(pane).Text())
	}
	// A return alone leaves the ask standing; something typed, then
	// return, answers it.
	_, _ = a.terminal(pane).HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true})
	_, _ = a.terminal(pane).HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter})
	var ok bool
	waitFor(t, a, "the answer", func() bool {
		select {
		case ok = <-answered:
			return true
		default:
			return false
		}
	})
	if !ok {
		t.Fatal("the user typed, and the agent was told they did not")
	}
}

func TestAnAgentOpensAPaneBesideOneItWasGiven(t *testing.T) {
	a, code := agentApp(t)
	first := a.st.Panes[0].ID
	c, sh := dial(t, a, code)
	var err error
	asAgent(t, a, func() { _, err = c.Open(sh.Panes[0].ID) })
	if err == nil || !strings.Contains(err.Error(), agent.BoxOpenMore) {
		t.Fatalf("opening without the box said %v", err)
	}
	a.handle(SetAgentMay{Pane: first, May: settings.AgentMay{OpenMore: true}})
	var opened agent.Pane
	asAgent(t, a, func() { opened, err = c.Open(sh.Panes[0].ID) })
	if err != nil {
		t.Fatal(err)
	}
	if len(a.st.Panes) != 2 || len(a.st.Share.Panes) != 2 || !opened.May.OpenMore {
		t.Fatalf("opened %+v; panes %+v, share %+v", opened, a.st.Panes, a.st.Share)
	}
	if a.st.Focus != first {
		t.Fatalf("the agent's pane took the keyboard from %s to %s", first, a.st.Focus)
	}
	// Taking the first back takes back what was opened from it.
	a.handle(UnsharePane{Pane: first})
	if len(a.st.Share.Panes) != 0 {
		t.Fatalf("taken back, the share still holds %+v", a.st.Share.Panes)
	}
}

func TestTheSkillIsWrittenAndAnEditedOneAskedAbout(t *testing.T) {
	a, _ := agentApp(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	a.handle(WriteSkill{Host: hostClaudeCode})
	path := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "gridterm", "SKILL.md")
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), "use_session_code") {
		t.Fatalf("the skill reads %q, %v", body, err)
	}
	if err := os.WriteFile(path, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.handle(WriteSkill{Host: hostClaudeCode})
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "Replace the skill?" || !q.Danger {
		t.Fatalf("the question is %+v", q)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true})
	waitFor(t, a, "the skill replaced", func() bool {
		body, _ := os.ReadFile(path)
		return strings.Contains(string(body), "use_session_code")
	})
}

func TestWhatAnAgentTypedIsKept(t *testing.T) {
	a, code := agentApp(t)
	c, sh := dial(t, a, code)
	var err error
	asAgent(t, a, func() { err = c.Send(sh.Panes[0].ID, "echo one\ttwo", []string{"Enter"}) })
	if err != nil {
		t.Fatal(err)
	}
	pane := a.st.Panes[0].ID
	a.handle(ShowTyped{Pane: pane})
	if len(a.st.Panes) != 2 {
		t.Fatalf("the panes are %+v", a.st.Panes)
	}
	r := a.st.Readers[a.st.Panes[1].ID]
	if last := r.Lines[len(r.Lines)-1]; !strings.HasSuffix(last, `echo one\ttwo<Enter>`) {
		t.Fatalf("the history ends %q", last)
	}
}

// An agent's asking is cut to one plain line, as gridterm cuts it.
func TestAnAgentsAskingIsCutToOnePlainLine(t *testing.T) {
	got := secretLine("the \"db\" pass\u202eword\ue000 -- " + strings.Repeat("x", 200))
	// Only the agent's words, between the window's quotes.
	asked := got[strings.Index(got, `for: "`)+6 : strings.LastIndex(got, `" --`)]
	for _, bad := range []string{"\u202e", "\ue000", "\"", "--"} {
		if strings.Contains(asked, bad) {
			t.Errorf("the line keeps %q: %s", bad, got)
		}
	}
	if strings.Count(got, "x") > agent.MostSecretWords {
		t.Errorf("the line runs to %d characters of asking", strings.Count(got, "x"))
	}
}

// With the path to the program unknown, the skill is refused and a
// copied prompt warns.
func TestAnUnknownPathRefusesTheSkill(t *testing.T) {
	a, _ := agentApp(t)
	was := exeKnown
	exeKnown = func() (string, bool) { return "", false }
	t.Cleanup(func() { exeKnown = was })
	if err := a.writeSkill(WriteSkill{Host: hostClaudeCode}); err == nil {
		t.Fatal("with no path, the skill was written")
	}
	a.copyAgentPrompt(hostClaudeCode)
	if !slices.ContainsFunc(a.st.Notices, func(n Notice) bool { return n.Title == "gunimterm path not found" }) {
		t.Fatalf("with no path, the prompt said %+v", a.st.Notices)
	}
}

// A relative directory in an agent program's setting is read from home.
func TestASkillDirectoryIsReadFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := fromHome("conf/claude")
	if err != nil || got != filepath.Join(home, "conf", "claude") {
		t.Fatalf("read %q, %v", got, err)
	}
}
