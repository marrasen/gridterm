package mcp

import (
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

	// lines is what the last Look was asked for, and pressed is every
	// key name a Send has carried.
	lines   int
	pressed []string

	// cmd is what this window says about the command line, which every
	// Look carries.
	cmd agent.Look

	// output is what this window says the last command printed.
	output string
}

func (w *oneWindow) Use(code string) (agent.Pane, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || code != w.code {
		return agent.Pane{}, errors.New("that code does not name a pane this window has handed over")
	}
	return agent.Pane{ID: "pane-1", Label: "bash on this machine", Cols: 80, Rows: 24}, nil
}

func (w *oneWindow) Look(id string, lines int) (agent.Look, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || id != "pane-1" {
		return agent.Look{}, errors.New("that is not a pane you have been handed")
	}
	w.lines = lines
	look := w.cmd
	look.Screen = w.screen
	return look, nil
}

func (w *oneWindow) Output(id string, most int) (agent.Look, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || id != "pane-1" {
		return agent.Look{}, errors.New("that is not a pane you have been handed")
	}
	look := w.cmd
	look.Screen = w.output
	look.Note = "this is what the last command printed"
	return look, nil
}

func (w *oneWindow) Send(id, text string, keys []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || id != "pane-1" {
		return errors.New("that is not a pane you have been handed")
	}
	w.typed += text
	w.pressed = append(w.pressed, keys...)
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

	// What the window calls the pane, which is what the agent is given
	// and has to hand back.
	pane, err := panes.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	answers := talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"`+code+`"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":`+
			`{"name":"read_pane","arguments":{"pane":"`+pane.ID+`"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"`+pane.ID+`","text":"uptime\r"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":`+
			`{"name":"list_panes","arguments":{}}}`)

	said := byID(t, answers)
	told, failed := textOf(t, said[2])
	if failed || !strings.Contains(told, "bash on this machine") {
		t.Errorf("it was not told what it has: %q", told)
	}
	screen, failed := textOf(t, said[3])
	if failed || !strings.Contains(screen, "root@margit") {
		t.Errorf("the screen never reached the agent: %q", screen)
	}
	if got := win.sentText(); got != "uptime\r" {
		t.Errorf("the window was sent %q", got)
	}
	// And the listing says what it holds, which is the one pane and its
	// size -- checked here rather than left to something an earlier
	// answer happened to mention too.
	listed, failed := textOf(t, said[5])
	if failed {
		t.Fatalf("listing failed: %q", listed)
	}
	if !strings.Contains(listed, pane.ID) || !strings.Contains(listed, "80x24") {
		t.Errorf("it listed %q", listed)
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
	pane, err := panes.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	win.takeBack()

	if _, err := panes.Read(pane.ID, 0); err == nil {
		t.Error("it read a pane the user had taken back")
	}
	if err := panes.Send(pane.ID, "x", nil); err == nil {
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

// A code for another window opens that window, not the one already
// reached.
//
// Two windows on one machine hand out codes of the same shape, and a
// code carries the port for exactly this reason. Offered to the wrong
// window a code names nothing, and the user would be told their code
// was refused by the window that never had it.
func TestACodeForAnotherWindowReachesThatWindow(t *testing.T) {
	first := &oneWindow{screen: "the first window"}
	one, err := agent.Listen(agent.Config{Window: first, OnError: func(error) {}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = one.Close() }()
	first.code, err = agent.NewCode(one.Port())
	if err != nil {
		t.Fatalf("code: %v", err)
	}

	second := &oneWindow{screen: "the second window"}
	two, err := agent.Listen(agent.Config{Window: second, OnError: func(error) {}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = two.Close() }()
	second.code, err = agent.NewCode(two.Port())
	if err != nil {
		t.Fatalf("code: %v", err)
	}

	panes := NewWindow()
	defer func() { _ = panes.Close() }()

	onOne, err := panes.Use(first.code)
	if err != nil {
		t.Fatalf("the first window: %v", err)
	}
	onTwo, err := panes.Use(second.code)
	if err != nil {
		t.Fatalf("the second window: %v", err)
	}
	if onOne.ID == onTwo.ID {
		t.Fatalf("both panes are called %q", onOne.ID)
	}

	// Each reads its own window, and the first is not lost by the
	// second arriving.
	for _, c := range []struct {
		pane Pane
		want string
	}{{onOne, "the first window"}, {onTwo, "the second window"}} {
		screen, err := panes.Read(c.pane.ID, 0)
		if err != nil {
			t.Fatalf("read %s: %v", c.pane.ID, err)
		}
		if screen.Screen != c.want {
			t.Errorf("%s read %q, want %q", c.pane.ID, screen.Screen, c.want)
		}
	}

	// And both are listed.
	listed, err := panes.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Errorf("it holds %v", listed)
	}
}

// Being asked what you have when you have nothing is an answer, not a
// failure.
func TestListingNothingIsNotAFailure(t *testing.T) {
	panes := NewWindow()
	defer func() { _ = panes.Close() }()

	got, err := panes.List()

	if err != nil {
		t.Errorf("it gave %v", err)
	}
	if len(got) != 0 {
		t.Errorf("it holds %v", got)
	}
}

// A fresh code for a window whose connection broke is dialled again
// rather than answered with the dead one.
//
// A code arriving after the connection broke is the user handing a pane
// over again. Holding on to the corpse would mean this process had to
// be restarted before their code would work.
func TestAFreshCodeAfterTheConnectionBrokeIsDialledAgain(t *testing.T) {
	win := &oneWindow{screen: "still here"}
	s, err := agent.Listen(agent.Config{Window: win, OnError: func(error) {}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	win.code, err = agent.NewCode(s.Port())
	if err != nil {
		t.Fatalf("code: %v", err)
	}

	panes := NewWindow()
	defer func() { _ = panes.Close() }()
	pane, err := panes.Use(win.code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := panes.Read(pane.ID, 0); err == nil {
		t.Fatal("it read a window that had gone")
	}

	// The same code again. Nothing is listening now, so what says the
	// old connection was let go of is that this tried to dial at all.
	_, err = panes.Use(win.code)
	if err == nil {
		t.Fatal("it opened a pane on a window that has gone")
	}
	if !strings.Contains(err.Error(), "is listening on port") {
		t.Errorf("it answered with the old connection rather than dialling: %v", err)
	}
}

// What the shell said about the command line reaches the agent through
// every layer.
//
// The window says it, the wire carries it, and the tool answer words it.
// Nothing between the window and the agent's text is tested anywhere
// else: the fakes on either side of it agree with each other by
// construction.
func TestWhatTheShellSaidReachesTheAgent(t *testing.T) {
	win := &oneWindow{screen: "marcus@margit:~$ "}
	win.cmd = agent.Look{
		Marks: true, Done: 3, Status: 1, HasStatus: true, Yours: true,
	}
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
	pane, err := panes.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	answers := talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"`+code+`"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"read_pane","arguments":{"pane":"`+pane.ID+`"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":`+
			`{"name":"wait_for","arguments":{"pane":"`+pane.ID+`","quiet_ms":30,`+
			`"timeout_ms":5000}}}`)

	said := byID(t, answers)
	screen, failed := textOf(t, said[2])
	if failed {
		t.Fatalf("reading failed: %q", screen)
	}
	for _, want := range []string{"exit status 1", "That is what you sent"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the read does not say %q:\n%s", want, screen)
		}
	}

	// And the wait says why it ended, which is the window's word for it
	// carried the whole way.
	waited, failed := textOf(t, said[3])
	if failed {
		t.Fatalf("waiting failed: %q", waited)
	}
	if !strings.Contains(waited, agent.EndedOnMarks) {
		t.Errorf("the wait does not say why it ended:\n%s", waited)
	}
}
