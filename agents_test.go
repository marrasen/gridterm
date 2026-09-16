package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// handedOver hands the window's one pane to an agent and gives back the
// pane, the code, and an agent already connected with it.
func handedOver(t *testing.T, a *testApp) (*term.Terminal, string, *agent.Client) {
	t.Helper()
	pane := onlyPaneOn(t, a)
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	h := a.agents.of(pane)
	if h == nil {
		t.Fatal("the window did not record the handover")
	}
	c, err := agent.Dial(h.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return pane, h.code, c
}

// An agent given a code reads the pane and types into it.
//
// This is the whole of what a handover is for: the user sets something
// up -- through however many machines, as whichever user -- and then
// lets an agent work in it while watching.
func TestAnAgentWorksInThePaneItWasHanded(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = c.Use(a.agents.of(pane).code)
		return err
	})
	if got.Cols != pane.Size().Cols || got.Rows != pane.Size().Rows {
		t.Errorf("it was told the pane is %dx%d", got.Cols, got.Rows)
	}

	// What is on the screen.
	a.shells[0].out <- []byte("root@margit:~# \r\n")
	waitFor(t, a, "the pane to show what the shell said", func() bool {
		return strings.Contains(paneText(pane), "root@margit")
	})

	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(got.ID)
		return err
	})
	if !strings.Contains(look.Screen, "root@margit") {
		t.Errorf("it read %q", look.Screen)
	}
	if look.Gone {
		t.Error("it thinks the program has finished")
	}

	// And what it types reaches the program.
	offWindow(t, a, "the window to answer the agent", func() error { return c.Send(got.ID, "uptime\r") })
	waitFor(t, a, "the shell to be sent what the agent typed", func() bool {
		return strings.Contains(a.shells[0].sentText(), "uptime")
	})
}

// Without a code an agent can do nothing, even having reached the
// window.
func TestAnAgentWithoutACodeCanDoNothing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, c := handedOver(t, a)

	// The name of the pane, which an agent could guess.
	a.refreshPanel(panelNow)
	id := a.panes[pane].ID()
	if id == "" {
		t.Fatal("the pane has no name")
	}

	if _, err := c.Read(id); err == nil {
		t.Error("it read a pane it had given no code for")
	}
	if err := c.Send(id, "rm -rf /\r"); err == nil {
		t.Error("it typed into a pane it had given no code for")
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
}

// Taking the pane back stops the agent at once.
func TestTakingThePaneBackStopsTheAgent(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = c.Use(code)
		return err
	})

	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}

	if _, err := c.Read(got.ID); err == nil {
		t.Error("it read a pane the user had taken back")
	}
	if err := c.Send(got.ID, "x"); err == nil {
		t.Error("it typed into a pane the user had taken back")
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
	// And nothing is listening any more, because nothing is handed
	// over.
	if a.agents.listening() {
		t.Error("the window is still listening for agents")
	}
}

// Nothing listens until the user hands a pane over.
func TestNothingListensForAgentsUntilAPaneIsHandedOver(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	if a.agents.listening() {
		t.Fatal("it is listening for agents with nothing handed over")
	}

	pane := onlyPaneOn(t, a)
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	if !a.agents.listening() {
		t.Fatal("it is not listening after a pane was handed over")
	}
	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	if a.agents.listening() {
		t.Error("it is still listening with nothing handed over")
	}
}

// Handing the same pane over twice gives back the code it already has,
// rather than a second one to take back separately.
func TestHandingOnePaneOverTwiceKeepsOneCode(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, _ := handedOver(t, a)

	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over again: %v", err)
	}
	if got := a.agents.of(pane).code; got != code {
		t.Errorf("it made a second code: %q then %q", code, got)
	}
}

// A pane that has closed cannot be handed over, and one handed over
// that closes takes the handover with it.
func TestAPaneThatClosesTakesItsHandoverWithIt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, _ := handedOver(t, a)

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	if a.agents.of(pane) != nil {
		t.Error("the handover outlived the pane")
	}
	if a.agents.listening() {
		t.Error("the window is still listening with nothing handed over")
	}
}

// The pane's row says an agent has been given it, and says the
// difference between offered and being worked in.
func TestTheRowSaysAnAgentHasThePane(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	a.refreshPanel(panelNow)
	if got := a.panes[pane].Note; got != agentOffered {
		t.Errorf("the row says %q", got)
	}

	offWindow(t, a, "the window to answer the agent", func() error {
		_, err := c.Use(code)
		return err
	})
	waitFor(t, a, "the row to say an agent is working here", func() bool {
		a.refreshPanel(panelNow)
		return a.panes[pane].Note == agentAt
	})

	if err := c.Close(); err != nil {
		t.Fatalf("the agent leaving: %v", err)
	}
	waitFor(t, a, "the row to say it is only offered again", func() bool {
		a.refreshPanel(panelNow)
		return a.panes[pane].Note == agentOffered
	})
}

// The prompt tells an agent that has never heard of gridterm everything
// it needs.
//
// A bare code is no use on its own: the agent has to know what it has
// been given, how to start the MCP server, and which tools to call.
func TestTheHandoverPromptStandsOnItsOwn(t *testing.T) {
	const code = "gt1-54321-abcdefghijklmnopqrstuvwxyz"
	const exe = `C:\Users\someone\go\bin\gridterm.exe`
	prompt := handoverPrompt(code, exe)
	t.Logf("the prompt as generated:\n%s", prompt)

	if got := strings.Count(prompt, code); got != 1 {
		t.Errorf("the code is in the prompt %d times, want once", got)
	}
	// The one line for Claude Code, with this program's own path.
	if want := "claude mcp add gridterm -- " + exe + " -mcp"; !strings.Contains(prompt, want) {
		t.Errorf("the prompt does not say %q", want)
	}
	// And the same path in the JSON for any other host, where its
	// backslashes have to be doubled to be read back.
	var config struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	snippet := jsonIn(t, prompt)
	if err := json.Unmarshal([]byte(snippet), &config); err != nil {
		t.Fatalf("the JSON in the prompt does not parse: %v in %s", err, snippet)
	}
	got, there := config.Servers["gridterm"]
	if !there || got.Command != exe || len(got.Args) != 1 || got.Args[0] != "-mcp" {
		t.Errorf("the JSON says %+v", config.Servers)
	}

	for _, tool := range []string{
		"use_session_code", "list_panes", "read_pane", "send_keys", "wait_for",
	} {
		if !strings.Contains(prompt, tool) {
			t.Errorf("the prompt never mentions %s", tool)
		}
	}
	// How to press Enter, which no agent can guess.
	if !strings.Contains(prompt, `\r`) {
		t.Error("the prompt does not say what sends a command")
	}
	// And that the server has to run here, because the port is local.
	if !strings.Contains(prompt, "loopback") && !strings.Contains(prompt, "this machine") {
		t.Error("the prompt does not say where the server has to run")
	}

	// A dialog is narrow and a prompt nobody reads is a prompt nobody
	// pastes.
	for _, line := range strings.Split(prompt, "\n") {
		if len(line) > 100 {
			t.Errorf("a line is %d characters long: %q", len(line), line)
		}
	}
	if lines := strings.Count(strings.TrimRight(prompt, "\n"), "\n") + 1; lines > 32 {
		t.Errorf("the prompt is %d lines long", lines)
	}
}

// jsonIn is the MCP config snippet out of a prompt, from the first brace
// to the last.
func jsonIn(t *testing.T, prompt string) string {
	t.Helper()
	from := strings.Index(prompt, `{"mcpServers"`)
	if from < 0 {
		t.Fatalf("the prompt carries no config for other hosts:\n%s", prompt)
	}
	to := strings.LastIndex(prompt, "}}}")
	if to < from {
		t.Fatalf("the config in the prompt never ends:\n%s", prompt)
	}
	return prompt[from : to+len("}}}")]
}

// Handing a pane over shows the code and puts the whole prompt on the
// clipboard.
//
// The next thing the prompt is for is being pasted into a conversation,
// and it is more than anybody should have to read off a screen.
func TestHandingAPaneOverShowsTheCodeAndCopiesThePrompt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}

	code := a.agents.of(pane).code
	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	shown := strings.Join(f.Lines, "\n")
	if !strings.Contains(shown, code) {
		t.Errorf("the dialog does not show the code: %v", f.Lines)
	}
	// The dialog indents what it quotes of the prompt.
	opening := "  " + strings.ReplaceAll(promptOpening(handoverPrompt(code, exePath())), "\n", "\n  ")
	if !strings.Contains(shown, opening) {
		t.Errorf("the dialog does not show how the prompt starts: %v", f.Lines)
	}
	if !strings.Contains(shown, "clipboard") {
		t.Errorf("the dialog does not say the prompt is on the clipboard: %v", f.Lines)
	}
	// The clipboard is written from a goroutine of its own, because on
	// some systems putting something on it means running a program.
	want := handoverPrompt(code, exePath())
	waitFor(t, a, "the prompt to reach the clipboard", func() bool { return a.copiedText() == want })
	if got := a.copiedText(); !strings.Contains(got, "use_session_code") ||
		!strings.Contains(got, "-mcp") {
		t.Errorf("the clipboard holds %q, which is not a prompt an agent can act on", got)
	}

	// And the dialog offers to take it straight back.
	pressButton(t, a, f, "Take it back")
	if a.agents.of(pane) != nil {
		t.Error("it is still handed over")
	}
}

// Taking a pane back while an agent is waiting on it comes back.
//
// Every question an agent asks is answered by the goroutine that draws,
// and taking a pane back happens on that same goroutine. One waiting
// for the other is a window that never draws again, and the action that
// wedges it is the one the user reaches for when they want the agent to
// stop.
func TestTakingAPaneBackWhileAnAgentIsAskingComesBack(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	offWindow(t, a, "the window to answer the agent", func() error {
		_, err := c.Use(code)
		return err
	})

	// A wait in flight: the agent is asking, and nothing is running
	// what it asked for.
	asking := make(chan struct{})
	go func() {
		close(asking)
		_, _, _ = c.Wait("1", agent.Until{QuietMS: 60000, TimeoutMS: 60000})
	}()
	<-asking
	waitUntil(t, "work to be queued for the window", func() bool { return a.pump.pending() > 0 })

	done := make(chan error, 1)
	go func() { done <- a.takeBackPane(pane) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("taking it back gave %v", err)
		}
	case <-time.After(waitBudget):
		t.Fatal("taking the pane back waited for the agent it was taking it from")
	}
}

// Handing a pane over again does not let the agent that had it back in.
//
// Taking a pane back has to mean something even when the user changes
// their mind afterwards. The old agent still holds what it was given;
// what it was given has to have stopped naming anything.
func TestHandingAPaneOverAgainShutsOutTheAgentThatHadIt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var had agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		had, err = c.Use(code)
		return err
	})

	// The user takes it back, thinks better of it, and hands it over
	// again -- to somebody else, with a new code.
	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over again: %v", err)
	}
	again := a.agents.of(pane)
	if again.code == code {
		t.Fatal("it handed out the same code again")
	}
	if again.id == had.ID {
		t.Fatal("the new handover has the name the old one had")
	}

	// The old agent is holding a name that no longer means anything.
	// Its connection was closed with the listener, so it reconnects the
	// way anything that lost a connection would.
	old, err := agent.Dial(again.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = old.Close() }()

	done := make(chan error, 1)
	go func() {
		_, err := old.Read(had.ID)
		done <- err
	}()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("the name it was given before still reads the pane")
	}

	// And typing with it reaches nothing.
	go func() {
		done <- old.Send(had.ID, "rm -rf /\r")
	}()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("the name it was given before still types into the pane")
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
}

// A code the window never handed out opens nothing.
//
// The window has its own check, and it is the one that matters: the
// stand-in used elsewhere has one of its own, so a test against that
// says nothing about this.
func TestACodeTheWindowNeverHandedOutOpensNothing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	// A letter the code does not already end in, because a code is
	// random and one in thirty-two of them ends in any given letter.
	swap := "z"
	if strings.HasSuffix(code, swap) {
		swap = "y"
	}
	wrong := code[:len(code)-1] + swap
	if wrong == code {
		t.Fatal("the test did not change the code")
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Use(wrong)
		done <- err
	}()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("a code the window never handed out was taken")
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
}

// Taking one pane back stops the agent in it, with another still handed
// over.
//
// Taking the last one back stops the listener, which hides whether the
// window checks anything: the connection simply goes. With another pane
// still out, the listener stays up and the check is the only thing
// standing between the agent and the pane.
func TestTakingOnePaneBackWithAnotherStillOut(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	// Two panes, both handed over.
	if err := a.openTab(); err != nil {
		t.Fatalf("a second pane: %v", err)
	}
	panes := make([]*term.Terminal, 0, 2)
	for pane := range a.panes {
		panes = append(panes, pane)
	}
	if len(panes) != 2 {
		t.Fatalf("%d panes, want two", len(panes))
	}
	for _, pane := range panes {
		if err := a.handPane(pane); err != nil {
			t.Fatalf("hand it over: %v", err)
		}
	}

	first := a.agents.of(panes[0])
	c, err := agent.Dial(first.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	var had agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		had, err = c.Use(first.code)
		return err
	})

	if err := a.takeBackPane(panes[0]); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	if !a.agents.listening() {
		t.Fatal("the listener stopped with a pane still handed over")
	}

	// The connection is still open, so what refuses the agent is the
	// window.
	done := make(chan error, 1)
	go func() {
		_, err := c.Read(had.ID)
		done <- err
	}()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("it read a pane the user had taken back")
	}

	go func() { done <- c.Send(had.ID, "rm -rf /\r") }()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("it typed into a pane the user had taken back")
	}
	for i, shell := range a.shells {
		if got := shell.sentText(); got != "" {
			t.Errorf("shell %d was sent %q", i, got)
		}
	}
}

// The dialog says where to take the pane back from, and that place
// exists.
//
// A dialog that names a command the window does not have is worse than
// one that says nothing: the user goes looking.
func TestTheDialogNamesSomewhereTheCommandsReallyAre(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	pane := onlyPaneOn(t, a)
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	lines := strings.Join(f.Lines, " ")
	if !strings.Contains(lines, "Servers menu") {
		t.Errorf("the dialog says %q", lines)
	}

	// And the Servers menu really offers it.
	a.refreshServers()
	var offered bool
	for _, menu := range a.bar.Menus {
		if menu.Title != "Servers" {
			continue
		}
		for _, item := range menu.Items {
			if item.Command == "agent.take" {
				offered = true
			}
		}
	}
	if !offered {
		t.Error("the Servers menu does not offer taking the pane back")
	}
}
