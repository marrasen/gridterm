package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/agent"
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
	h := a.handedBy[pane]
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
	fromAgent(t, a, func() error {
		var err error
		got, err = c.Use(a.handedBy[pane].code)
		return err
	})
	if got.Cols != pane.Size().Cols || got.Rows != pane.Size().Rows {
		t.Errorf("it was told the pane is %dx%d", got.Cols, got.Rows)
	}

	// What is on the screen.
	a.shells[0].out <- []byte("root@margit:~# \r\n")
	waitUntil(t, func() bool { return strings.Contains(paneText(pane), "root@margit") })

	var look agent.Look
	fromAgent(t, a, func() error {
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
	fromAgent(t, a, func() error { return c.Send(got.ID, "uptime\r") })
	waitUntil(t, func() bool { return strings.Contains(a.shells[0].sentText(), "uptime") })
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
	fromAgent(t, a, func() error {
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
	if a.agents != nil {
		t.Error("the window is still listening for agents")
	}
}

// Nothing listens until the user hands a pane over.
func TestNothingListensForAgentsUntilAPaneIsHandedOver(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	if a.agents != nil {
		t.Fatal("it is listening for agents with nothing handed over")
	}

	pane := onlyPaneOn(t, a)
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	if a.agents == nil {
		t.Fatal("it is not listening after a pane was handed over")
	}
	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	if a.agents != nil {
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
	if got := a.handedBy[pane].code; got != code {
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
	if a.handedBy[pane] != nil {
		t.Error("the handover outlived the pane")
	}
	if a.agents != nil {
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

	fromAgent(t, a, func() error {
		_, err := c.Use(code)
		return err
	})
	waitUntilPumped(t, a, func() bool {
		a.refreshPanel(panelNow)
		return a.panes[pane].Note == agentAt
	})

	if err := c.Close(); err != nil {
		t.Fatalf("the agent leaving: %v", err)
	}
	waitUntilPumped(t, a, func() bool {
		a.refreshPanel(panelNow)
		return a.panes[pane].Note == agentOffered
	})
}

// fromAgent runs something an agent asks, pumping the window so the
// question posted to the drawing goroutine is answered.
//
// Every question an agent asks is about something only that goroutine
// may look at, so a test that does not run it waits for ever.
func fromAgent(t *testing.T, a *testApp, do func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- do() }()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("the agent was told: %v", err)
			}
			return
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the window never answered the agent")
}

// waitUntilPumped waits for something to become true, running what the
// window has been asked to do meanwhile.
func waitUntilPumped(t *testing.T, a *testApp, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		a.pump.run()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("it never happened")
}

// Handing a pane over shows the code and puts it on the clipboard.
//
// The next thing a code is for is being pasted into a conversation, and
// it is thirty-two characters nobody should have to read off a screen.
func TestHandingAPaneOverShowsTheCodeAndCopiesIt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}

	code := a.handedBy[pane].code
	f := openDialog(t, a)
	if !strings.Contains(strings.Join(f.Lines, "\n"), code) {
		t.Errorf("the dialog does not show the code: %v", f.Lines)
	}
	// The clipboard is written from a goroutine of its own, because on
	// some systems putting something on it means running a program.
	waitUntil(t, func() bool { return a.copiedText() == code })

	// And the dialog offers to take it straight back.
	pressButton(t, a, f, "Take it back")
	if a.handedBy[pane] != nil {
		t.Error("it is still handed over")
	}
}
