package agent

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWindow is a window with one pane on it, for testing what an agent
// may and may not do.
type fakeWindow struct {
	mu      sync.Mutex
	code    string
	screen  string
	typed   string
	changed uint64
	gone    bool
	taken   bool
}

func (w *fakeWindow) Use(code string) (Pane, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || code != w.code {
		return Pane{}, errors.New("that code does not name a pane this window has handed over")
	}
	return Pane{ID: "pane-1", Label: "bash", Cols: 80, Rows: 24}, nil
}

func (w *fakeWindow) Look(id string) (Look, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken {
		return Look{}, errors.New("the user has taken that pane back")
	}
	if id != "pane-1" {
		return Look{}, errors.New("no such pane")
	}
	return Look{Screen: w.screen, Gone: w.gone, Changed: w.changed}, nil
}

func (w *fakeWindow) Send(id, text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken {
		return errors.New("the user has taken that pane back")
	}
	if id != "pane-1" {
		return errors.New("no such pane")
	}
	w.typed += text
	return nil
}

func (w *fakeWindow) say(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.screen = text
	w.changed++
}

func (w *fakeWindow) takeBack() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.taken = true
}

func (w *fakeWindow) sentText() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.typed
}

// listening starts a window listening for agents, and gives back a code
// for its one pane.
func listening(t *testing.T) (*fakeWindow, *Server, string) {
	t.Helper()
	w := &fakeWindow{screen: "$ "}
	s, err := Listen(Config{Window: w, OnError: func(error) {}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	code, err := NewCode(s.Port())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	w.code = code
	return w, s, code
}

// An agent given a code reads the pane and types into it, and nothing
// else.
func TestAnAgentReadsAndTypesInThePaneItWasGiven(t *testing.T) {
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
	if pane.Label != "bash" || pane.Cols != 80 {
		t.Errorf("it was handed %+v", pane)
	}

	w.say("$ whoami\r\nmarcus\r\n$ ")
	look, err := c.Read(pane.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(look.Screen, "marcus") {
		t.Errorf("it read %q", look.Screen)
	}

	if err := c.Send(pane.ID, "uptime\r"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := w.sentText(); got != "uptime\r" {
		t.Errorf("the pane was sent %q", got)
	}
}

// Nothing reaches a pane without a code.
func TestWithoutACodeAnAgentCanDoNothing(t *testing.T) {
	w, _, code := listening(t)

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	// The pane is there and the agent knows its name, having guessed
	// it. Without a code that is not enough.
	if _, err := c.Read("pane-1"); err == nil {
		t.Error("it read a pane it was never handed")
	}
	if err := c.Send("pane-1", "rm -rf /\r"); err == nil {
		t.Error("it typed into a pane it was never handed")
	}
	if got := w.sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
	if panes, err := c.Panes(); err != nil || len(panes) != 0 {
		t.Errorf("it holds %v, %v", panes, err)
	}
}

// A code that is not the one handed out opens nothing.
func TestAWrongCodeOpensNothing(t *testing.T) {
	_, s, _ := listening(t)

	other, err := NewCode(s.Port())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	c, err := Dial(other)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if _, err := c.Use(other); err == nil {
		t.Fatal("a code nobody handed out was accepted")
	}
}

// One agent cannot reach a pane another was handed.
func TestOneAgentCannotReachAnothersPane(t *testing.T) {
	w, _, code := listening(t)

	mine, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = mine.Close() }()
	pane, err := mine.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	// A second agent on the same window, with no code of its own.
	theirs, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = theirs.Close() }()

	if _, err := theirs.Read(pane.ID); err == nil {
		t.Error("it read a pane handed to somebody else")
	}
	if err := theirs.Send(pane.ID, "x"); err == nil {
		t.Error("it typed into a pane handed to somebody else")
	}
	if got := w.sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
}

// Taking a pane back stops the agent working in it, at once.
func TestTakingAPaneBackStopsTheAgent(t *testing.T) {
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
	if _, err := c.Read(pane.ID); err != nil {
		t.Fatalf("read: %v", err)
	}

	w.takeBack()

	if _, err := c.Read(pane.ID); err == nil {
		t.Error("it read a pane the user had taken back")
	}
	if err := c.Send(pane.ID, "x"); err == nil {
		t.Error("it typed into a pane the user had taken back")
	}
	if got := w.sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
	// And the same code does not open it again.
	if _, err := c.Use(code); err == nil {
		t.Error("the code still works")
	}
}

// Waiting comes back when the pane goes quiet.
func TestWaitingComesBackWhenThePaneGoesQuiet(t *testing.T) {
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

	// Something running, then stopping.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			w.say("line " + string(rune('a'+i)))
			time.Sleep(20 * time.Millisecond)
		}
		w.say("$ ")
	}()

	look, timedOut, err := c.Wait(pane.ID, Until{QuietMS: 150, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if timedOut {
		t.Error("it gave up rather than seeing the pane go quiet")
	}
	if look.Screen != "$ " {
		t.Errorf("it came back on %q", look.Screen)
	}
	<-done
}

// Waiting for something that never comes gives up and says so, with
// what is on the screen.
func TestWaitingForSomethingThatNeverComesGivesUp(t *testing.T) {
	w, _, code := listening(t)
	w.say("still going")
	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	look, timedOut, err := c.Wait(pane.ID, Until{Contains: "never", TimeoutMS: 200})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !timedOut {
		t.Error("it said it saw what it was waiting for")
	}
	if look.Screen != "still going" {
		t.Errorf("it came back with %q", look.Screen)
	}
}

// A code says which window, so two on one machine do not answer for
// each other.
func TestACodeSaysWhichWindow(t *testing.T) {
	_, one, codeOne := listening(t)
	_, two, codeTwo := listening(t)

	if one.Port() == two.Port() {
		t.Fatal("both windows took the same port")
	}
	gotOne, err := ReadCode(codeOne)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	gotTwo, err := ReadCode(codeTwo)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if gotOne != one.Port() || gotTwo != two.Port() {
		t.Errorf("the codes name %d and %d, want %d and %d",
			gotOne, gotTwo, one.Port(), two.Port())
	}

	// And one window's code does not open the other's pane.
	c, err := Dial(codeOne)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Use(codeTwo); err == nil {
		t.Error("the other window's code was accepted")
	}
}

// Two codes are never the same.
func TestCodesAreNotGuessable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := NewCode(2222)
		if err != nil {
			t.Fatalf("code: %v", err)
		}
		if seen[code] {
			t.Fatalf("it made %q twice", code)
		}
		seen[code] = true
		// Long enough that guessing is not a way in: twenty bytes as
		// base32 is thirty-two characters.
		parts := strings.Split(code, "-")
		if len(parts) != 3 || len(parts[2]) != 32 {
			t.Fatalf("it made %q", code)
		}
	}
}

// Something pasted by mistake is turned away by name.
func TestSomethingThatIsNotACodeIsTurnedAway(t *testing.T) {
	for _, not := range []string{
		"", "hello", "gt1", "gt1-abc-xyz", "gt1-0-xyz", "gt1-99999-xyz",
		"gt2-2222-xyz", "gt1-2222-", "gt1-2222-xyz-extra",
	} {
		if _, err := ReadCode(not); err == nil {
			t.Errorf("%q was taken for a code", not)
		}
	}
	if _, err := ReadCode("gt1-2222-abcdef"); err != nil {
		t.Errorf("a code was turned away: %v", err)
	}
}

// Closing the window hangs up on every agent.
func TestClosingHangsUpOnEveryAgent(t *testing.T) {
	_, s, code := listening(t)
	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Use(code); err != nil {
		t.Fatalf("use: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := c.Read("pane-1"); err == nil {
		t.Error("it went on working in a pane of a window that has gone")
	}
}

// Closing twice is not a failure.
func TestClosingTwiceIsFine(t *testing.T) {
	_, s, _ := listening(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("closing again gave %v", err)
	}
}

// An agent is told when the user hands a pane over and when it lets go.
func TestTheWindowIsToldWhenAnAgentComesAndGoes(t *testing.T) {
	w := &fakeWindow{screen: "$ "}
	used := make(chan string, 4)
	gone := make(chan string, 4)
	s, err := Listen(Config{
		Window:  w,
		OnError: func(error) {},
		OnUse:   func(id string) { used <- id },
		OnGone:  func(id string) { gone <- id },
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.Close() }()
	code, err := NewCode(s.Port())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	w.code = code

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := c.Use(code); err != nil {
		t.Fatalf("use: %v", err)
	}
	select {
	case id := <-used:
		if id != "pane-1" {
			t.Errorf("it was told about %q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the window was never told an agent had arrived")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case id := <-gone:
		if id != "pane-1" {
			t.Errorf("it was told about %q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the window was never told the agent had gone")
	}
}
