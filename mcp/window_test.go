package mcp

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/marrasen/gridterm/agent"
)

// oneWindow is a gridterm window with a single pane handed over, for
// testing the whole way from an agent's JSON down to the window.
type oneWindow struct {
	mu     sync.Mutex
	code   string
	screen string
	typed  string
	taken  bool
}

func (w *oneWindow) Use(code string) (agent.Pane, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || code != w.code {
		return agent.Pane{}, errors.New("that code does not name a pane this window has handed over")
	}
	return agent.Pane{ID: "pane-1", Label: "bash on this machine", Cols: 80, Rows: 24}, nil
}

func (w *oneWindow) Look(id string) (agent.Look, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || id != "pane-1" {
		return agent.Look{}, errors.New("that is not a pane you have been handed")
	}
	return agent.Look{Screen: w.screen}, nil
}

func (w *oneWindow) Send(id, text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || id != "pane-1" {
		return errors.New("that is not a pane you have been handed")
	}
	w.typed += text
	return nil
}

func (w *oneWindow) takeBack() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.taken = true
}

func (w *oneWindow) sentText() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.typed
}

// An agent's JSON reaches a real window and comes back.
//
// Everything in between is here: the protocol, the connection the code
// names, and the window deciding what may be asked.
func TestAnAgentReachesARealWindow(t *testing.T) {
	win := &oneWindow{screen: "root@margit:~# "}
	s, err := agent.Listen(agent.Config{Window: win, OnError: func(error) {}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.Close() }()

	code, err := agent.NewCode(s.Port())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	win.code = code

	panes := NewWindow()
	defer func() { _ = panes.Close() }()

	var out bytes.Buffer
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":` +
			`{"name":"use_session_code","arguments":{"code":"` + code + `"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":` +
			`{"name":"read_pane","arguments":{"pane":"pane-1"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":` +
			`{"name":"send_keys","arguments":{"pane":"pane-1","text":"uptime\r"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":` +
			`{"name":"list_panes","arguments":{}}}`,
	}, "\n") + "\n"

	if err := Serve(strings.NewReader(in), &out, panes); err != nil {
		t.Fatalf("serve: %v", err)
	}
	said := out.String()
	if !strings.Contains(said, "root@margit") {
		t.Errorf("the screen never reached the agent: %s", said)
	}
	if !strings.Contains(said, "bash on this machine") {
		t.Errorf("it was not told what it has: %s", said)
	}
	if got := win.sentText(); got != "uptime\r" {
		t.Errorf("the window was sent %q", got)
	}
}

// And when the user takes the pane back, the agent is told plainly
// rather than reading a screen that has stopped being true.
func TestTakingThePaneBackReachesTheAgent(t *testing.T) {
	win := &oneWindow{screen: "root@margit:~# "}
	s, err := agent.Listen(agent.Config{Window: win, OnError: func(error) {}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.Close() }()
	code, err := agent.NewCode(s.Port())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	win.code = code

	panes := NewWindow()
	defer func() { _ = panes.Close() }()
	if _, err := panes.Use(code); err != nil {
		t.Fatalf("use: %v", err)
	}

	win.takeBack()

	if _, err := panes.Read("pane-1"); err == nil {
		t.Error("it read a pane the user had taken back")
	}
	if err := panes.Send("pane-1", "x"); err == nil {
		t.Error("it typed into a pane the user had taken back")
	}
	if got := win.sentText(); got != "" {
		t.Errorf("the window was sent %q", got)
	}
}

// Letting go of a window twice is not a failure, and letting go of one
// that was never reached is not either.
func TestLettingGoOfTheWindowIsSafeToRepeat(t *testing.T) {
	panes := NewWindow()
	if err := panes.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := panes.Close(); err != nil {
		t.Errorf("closing again gave %v", err)
	}
}

// A code that is not a code is turned away before anything is dialled.
func TestSomethingThatIsNotACodeIsTurnedAway(t *testing.T) {
	panes := NewWindow()
	defer func() { _ = panes.Close() }()

	if _, err := panes.Use("hello"); err == nil {
		t.Error("it took something that is not a code")
	}
}
