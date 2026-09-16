package agent

import (
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
