package agent

import (
	"strconv"
	"strings"
	"testing"
)

// The key names an agent may write, and how case is read.
func TestWhichKeyNamesThisTakes(t *testing.T) {
	for _, name := range []string{
		"Enter", "escape", "PAGEUP", "F12", "Space", "Ctrl+C", "ctrl+c",
		"Alt+F", "Shift+Tab", " Tab ",
	} {
		if !KnownKey(name) {
			t.Errorf("%q is not taken and should be", name)
		}
	}
	for _, name := range []string{
		"", "Ecsape", "F13", "Ctrl+Shift+C", "Super+X", "Ctrl+Up", "Shift+F1", "Alt+Enter",
	} {
		if KnownKey(name) {
			t.Errorf("%q is taken and should not be", name)
		}
	}
}

// A name this does not have comes back with the names it does, so the
// agent can fix the call itself rather than guessing again.
func TestARefusedKeyNameSaysWhichNamesThereAre(t *testing.T) {
	if err := CheckKeys([]string{"Escape", "Enter"}); err != nil {
		t.Fatalf("keys it has were refused: %v", err)
	}
	err := CheckKeys([]string{"Enter", "Ecsape"})
	if err == nil {
		t.Fatal("a key it has no name for was taken")
	}
	for _, want := range []string{"Ecsape", "Escape", "PageUp", "F12", "Shift+Tab"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("it said %q, which does not mention %s", err, want)
		}
	}
}

// Keys and a line count travel to the window as the agent sent them.
func TestKeysAndLinesReachTheWindow(t *testing.T) {
	w, _, code := listening(t)

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if err := c.Send(pane.ID, ":q!", []string{"Enter", "Ctrl+C"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := c.Read(pane.ID, 300); err != nil {
		t.Fatalf("read: %v", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.typed != ":q!" {
		t.Errorf("the pane was sent %q", w.typed)
	}
	if got := strings.Join(w.pressed, ","); got != "Enter,Ctrl+C" {
		t.Errorf("the window was told to press %q", got)
	}
	if w.lines != 300 {
		t.Errorf("the window was asked for %d lines", w.lines)
	}
}

// A call naming more keys than one call presses is refused, and says how
// many it takes.
func TestMoreKeysThanOneCallPressesIsRefused(t *testing.T) {
	many := make([]string, MostKeys)
	for i := range many {
		many[i] = "Enter"
	}
	if err := CheckKeys(many); err != nil {
		t.Fatalf("%d keys were refused: %v", MostKeys, err)
	}
	err := CheckKeys(append(many, "Enter"))
	if err == nil {
		t.Fatal("a call pressing more keys than it says it will was taken")
	}
	for _, want := range []string{strconv.Itoa(MostKeys), strconv.Itoa(MostKeys + 1)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("it said %q, which does not mention %s", err, want)
		}
	}

	// And the window refuses it too, rather than trusting whatever is on
	// the other end of the wire to have checked.
	w, _, code := listening(t)
	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if err := c.Send(pane.ID, "ls", append(many, "Enter")); err == nil {
		t.Error("the window pressed more keys than it says it will")
	} else if !strings.Contains(err.Error(), strconv.Itoa(MostKeys)) {
		t.Errorf("it said %q, which does not say how many it takes", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.typed != "" || len(w.pressed) > 0 {
		t.Errorf("the pane was sent %q and %v", w.typed, w.pressed)
	}
}

// A key name the window does not have is refused by the window as well,
// whatever reached the wire.
func TestTheWindowRefusesAKeyNameItDoesNotHave(t *testing.T) {
	w, _, code := listening(t)
	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	if err := c.Send(pane.ID, "ls", []string{"Enter", "Ecsape"}); err == nil {
		t.Error("the window pressed a key it has no name for")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.typed != "" || len(w.pressed) > 0 {
		t.Errorf("the pane was sent %q and %v", w.typed, w.pressed)
	}
}
