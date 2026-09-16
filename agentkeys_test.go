package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/input"
)

// An agent presses keys by name, and the pane encodes each the way the
// program running in it asks for.
//
// This is why the names go in rather than bytes: the same Up is two
// different sequences depending on what the program has asked for, and
// the pane is the only thing that knows which.
func TestTheNamedKeysAnAgentPressesAreEncodedByThePane(t *testing.T) {
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

	// wants presses keys and says what the shell was sent for them.
	wants := func(want string, keys ...string) {
		t.Helper()
		was := a.shells[0].sentText()
		offWindow(t, a, "the window to press "+strings.Join(keys, " and "), func() error {
			return c.Send(got.ID, "", keys)
		})
		waitFor(t, a, "the shell to be sent "+strings.Join(keys, " and "), func() bool {
			return len(a.shells[0].sentText()) >= len(was)+len(want)
		})
		if sent := strings.TrimPrefix(a.shells[0].sentText(), was); sent != want {
			t.Errorf("%v reached the shell as %q, want %q", keys, sent, want)
		}
	}

	wants("\x1b\r", "Escape", "Enter")
	wants("\x03", "Ctrl+C")
	wants("\x1b[15~", "F5")
	wants("\t", "Tab")
	wants("\x1b[Z", "Shift+Tab")
	wants(" ", "Space")
	wants("\x1bf", "Alt+F")
	wants("\x1b[A", "Up")

	// The program asks for the cursor keys in application mode, as vim
	// and less do, and the same name goes in as what it asked for.
	a.shells[0].out <- []byte("\x1b[?1h")
	waitFor(t, a, "the pane to read the mode the program set", func() bool {
		return pane.Said() > 0
	})
	wants("\x1bOA", "Up")

	// Text and keys in one call: the text first, then the keys.
	was := a.shells[0].sentText()
	offWindow(t, a, "the window to type and press", func() error {
		return c.Send(got.ID, ":q!", []string{"Enter"})
	})
	waitFor(t, a, "the shell to be sent the line and Enter", func() bool {
		return strings.HasSuffix(a.shells[0].sentText(), ":q!\r")
	})
	if sent := strings.TrimPrefix(a.shells[0].sentText(), was); sent != ":q!\r" {
		t.Errorf("it typed %q", sent)
	}
}

// A key name the window does not have is refused, with the names it
// does have, and nothing goes into the pane at all.
func TestAKeyNameTheWindowDoesNotHaveTypesNothing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = c.Use(code)
		return err
	})

	failed := make(chan error, 1)
	go func() { failed <- c.Send(got.ID, "ls", []string{"Enter", "Ecsape"}) }()
	var err error
	waitFor(t, a, "the window to refuse a key it has no name for", func() bool {
		select {
		case err = <-failed:
			return true
		default:
			return false
		}
	})
	if err == nil {
		t.Fatal("the window pressed a key it has no name for")
	}
	// The names that exist, so the agent can fix the call itself.
	for _, want := range []string{"Ecsape", "Escape", "F12", "Ctrl+<letter>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("it said %q, which does not mention %s", err.Error(), want)
		}
	}
	// And the text in the same call never went in: a call refused is a
	// call that did nothing.
	if sent := a.shells[0].sentText(); sent != "" {
		t.Errorf("the pane was sent %q", sent)
	}
}

// Every key name the wire allows is one this window can press.
//
// The names live in the agent package, because the tool descriptions and
// the refusal need them, and turning one into a key press lives here.
// This is what stops the two drifting apart.
func TestEveryKeyNameTheWireAllowsIsOneThisWindowPresses(t *testing.T) {
	for _, name := range append(agent.Keys(), "Ctrl+C", "Alt+F", "Shift+Tab") {
		press, err := keyPress(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if went := input.EncodeMode(press, input.Mode{}, nil); len(went) == 0 {
			t.Errorf("%s presses %+v, which is no input at all", name, press)
		}
	}
}

// Keys pressed in one call reach the shell in one write.
//
// Split across writes they arrive apart, and over a connection that is a
// round trip between them: Escape and then Enter a round trip later is
// how vim's escape timeout decides the user pressed Escape on its own.
func TestKeysInOneCallReachTheShellInOneWrite(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = c.Use(code)
		return err
	})

	was := a.shells[0].writeCount()
	offWindow(t, a, "the window to press Escape and Enter", func() error {
		return c.Send(got.ID, "", []string{"Escape", "Enter"})
	})
	waitFor(t, a, "the shell to be sent Escape and Enter", func() bool {
		return a.shells[0].sentText() == "\x1b\r"
	})
	if writes := a.shells[0].writeCount() - was; writes != 1 {
		t.Errorf("Escape and Enter went in %d writes, want one", writes)
	}

	// Text and keys together are one write too, the text first.
	was = a.shells[0].writeCount()
	offWindow(t, a, "the window to type and press", func() error {
		return c.Send(got.ID, ":q!", []string{"Enter"})
	})
	waitFor(t, a, "the shell to be sent the line and Enter", func() bool {
		return strings.HasSuffix(a.shells[0].sentText(), ":q!\r")
	})
	if writes := a.shells[0].writeCount() - was; writes != 1 {
		t.Errorf("the text and Enter went in %d writes, want one", writes)
	}
}

// An agent pressing a key leaves the user's view and their selection
// where they were.
//
// The user is watching a pane an agent is working in. Scrolling back to
// read something and having it jump to the bottom, or losing a selection
// part way through making it, is the agent taking the window over.
func TestAKeyFromAnAgentLeavesTheUsersViewAlone(t *testing.T) {
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

	// Enough output to scroll back through.
	var said strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&said, "line %d\r\n", i)
	}
	a.shells[0].out <- []byte(said.String())
	waitFor(t, a, "the pane to show what the shell said", func() bool {
		return strings.Contains(paneText(pane), "line 60")
	})

	// The user scrolls back and selects something.
	pane.ScrollView(10)
	for _, ev := range []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseLeft, Col: 0, Row: 0},
		{Kind: input.MouseMove, Button: input.MouseLeft, Col: 5, Row: 0},
		{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 5, Row: 0},
	} {
		if _, err := pane.HandleMouse(ev); err != nil {
			t.Fatalf("the mouse: %v", err)
		}
	}
	view, selected := pane.ViewOffset(), pane.SelectionText()
	if view == 0 || selected == "" {
		t.Fatalf("the test scrolled to %d and selected %q", view, selected)
	}

	offWindow(t, a, "the window to press Enter", func() error {
		return c.Send(got.ID, "", []string{"Enter"})
	})
	waitFor(t, a, "the shell to be sent Enter", func() bool {
		return strings.HasSuffix(a.shells[0].sentText(), "\r")
	})

	if now := pane.ViewOffset(); now != view {
		t.Errorf("the view is %d lines back, want %d", now, view)
	}
	if now := pane.SelectionText(); now != selected {
		t.Errorf("the selection is %q, want %q", now, selected)
	}
}

// More key names than one call presses is refused, by the window as well
// as by the MCP server, and nothing goes in.
func TestMoreKeysThanOneCallPressesIsRefusedByTheWindow(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = c.Use(code)
		return err
	})

	many := make([]string, agent.MostKeys+1)
	for i := range many {
		many[i] = "Enter"
	}
	failed := make(chan error, 1)
	go func() { failed <- c.Send(got.ID, "ls", many) }()
	var err error
	waitFor(t, a, "the window to refuse too many keys", func() bool {
		select {
		case err = <-failed:
			return true
		default:
			return false
		}
	})
	if err == nil {
		t.Fatal("the window pressed more keys than it says it will")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(agent.MostKeys)) {
		t.Errorf("it said %q, which does not say how many it takes", err)
	}
	if sent := a.shells[0].sentText(); sent != "" {
		t.Errorf("the pane was sent %q", sent)
	}
}
