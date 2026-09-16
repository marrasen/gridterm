package main

import (
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
		var went []byte
		for _, ev := range press {
			went = input.EncodeMode(ev, input.Mode{}, went)
		}
		if len(went) == 0 {
			t.Errorf("%s presses %+v, which is no input at all", name, press)
		}
	}
}
