package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// atTime is a clock that does not move, so what a log says is the same
// every run.
func atTime(c *connLog) *connLog {
	at := time.Date(2026, 9, 15, 9, 41, 2, 0, time.UTC)
	c.clock = func() time.Time { return at }
	c.started = at
	return c
}

// readAll reads a log until it ends or says what was wanted.
func readLog(t *testing.T, c *connLog, want string) string {
	t.Helper()
	var got strings.Builder
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		if strings.Contains(got.String(), want) {
			return got.String()
		}
		b, err := readLogOnce(t, c)
		got.Write(b)
		if err != nil {
			if strings.Contains(got.String(), want) {
				return got.String()
			}
			t.Fatalf("it ended with %v, having said %q", err, got.String())
		}
	}
	t.Fatalf("waited for %q, got %q", want, got.String())
	return ""
}

// readLogOnce reads once, failing the test rather than blocking for ever.
func readLogOnce(t *testing.T, c *connLog) ([]byte, error) {
	t.Helper()
	type got struct {
		b   []byte
		err error
	}
	back := make(chan got, 1)
	go func() {
		p := make([]byte, 4096)
		n, err := c.Read(p)
		back <- got{b: p[:n], err: err}
	}()
	select {
	case g := <-back:
		return g.b, g.err
	case <-time.After(waitBudget):
		t.Fatal("the read never came back")
		return nil, nil
	}
}

// What a connection is doing is readable as it happens, before there is
// anything to connect to.
func TestAConnectionSaysWhatItIsDoingAsItHappens(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Say("finding a key to offer")
	got := readLog(t, c, "finding a key")
	if !strings.Contains(got, "09:41:02") {
		t.Errorf("it said %q, with no time on it", got)
	}
	if !strings.HasSuffix(got, "\r\n") {
		t.Errorf("it said %q, which a terminal will not put on its own line", got)
	}

	c.Say("connecting")
	readLog(t, c, "connecting")
}

// What the far end says is written in full, on its own lines, marked as
// its words.
//
// This is where a sign-in link ends up. A link that is cut off, or that
// cannot be selected and copied, is a link nobody can use.
func TestWhatTheFarEndSaysIsWrittenInFull(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	const link = "https://login.tailscale.com/a/0123456789abcdef0123456789abcdef"
	c.Quote("marcus@picard", "To authenticate, visit:\r\n\r\n"+link+"\r\n")

	got := readLog(t, c, link)
	if !strings.Contains(got, "marcus@picard says:") {
		t.Errorf("it does not say who said it: %q", got)
	}
	if !strings.Contains(got, "To authenticate") {
		t.Errorf("it dropped what came before the link: %q", got)
	}
	// Whole, on one line, with nothing cut off it.
	for line := range strings.SplitSeq(got, "\r\n") {
		if !strings.Contains(line, link) {
			continue
		}
		if strings.TrimSpace(line) != link {
			t.Errorf("the link shares its line with %q", line)
		}
		return
	}
	t.Errorf("the link is not on a line of its own: %q", got)
}

// Nothing the far end did not say is written for it.
func TestNothingIsWrittenForAFarEndThatSaidNothing(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Quote("margit", "   \r\n  \n")
	c.Say("connecting")

	got := readLog(t, c, "connecting")
	if strings.Contains(got, "margit says") {
		t.Errorf("it made something up: %q", got)
	}
}

// Once the connection is made the pane carries it, and what is typed
// into the pane reaches it.
func TestThePaneBecomesTheConnection(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Say("connecting")
	readLog(t, c, "connecting")

	shell := newPipeSession()
	c.Became("picard", shell)

	shell.out <- []byte("marcus@picard:~$ ")
	readLog(t, c, "marcus@picard:~$")

	// And typing reaches it.
	if _, err := c.Write([]byte("uptime\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitUntil(t, "the shell to be sent what was typed", func() bool { return shell.sentText() == "uptime\r" })
}

// A connection that was made reports what its program ended with, which
// is what lets the pane say how the run went.
func TestAConnectionThatWasMadeReportsItsProgramsEnding(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()
	shell := newPipeSession()
	c.Became("picard", shell)

	if err := shell.Close(); err != nil {
		t.Fatalf("ending the shell: %v", err)
	}

	if err := c.Wait(); err != nil {
		t.Errorf("a shell that ended cleanly reported %v", err)
	}
}

// A pane resized before the connection was made tells it how big it is
// as soon as there is one.
//
// A shell that started at one size and was told the truth afterwards
// has already drawn its first screen wrong.
func TestTheConnectionIsToldHowBigThePaneAlreadyIs(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	if err := c.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	shell := newPipeSession()
	c.Became("picard", shell)

	waitUntil(t, "the shell to be told the new size", func() bool { return shell.lastSize() == [2]int{120, 40} })

	// And a resize after it reaches it too.
	if err := c.Resize(80, 24); err != nil {
		t.Fatalf("resize: %v", err)
	}
	waitUntil(t, "the shell to be told the size again", func() bool { return shell.lastSize() == [2]int{80, 24} })
}

// Typing before there is anything to type into goes nowhere, rather
// than being kept and run later.
func TestTypingBeforeThereIsAConnectionGoesNowhere(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	if _, err := c.Write([]byte("rm -rf /\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	shell := newPipeSession()
	c.Became("picard", shell)

	// Given a moment in which it could have arrived.
	time.Sleep(50 * time.Millisecond)
	if got := shell.sentText(); got != "" {
		t.Errorf("the shell was sent %q, typed before it existed", got)
	}
}

// A connection that was not made says why and stays to be read.
func TestAConnectionThatFailedSaysWhyAndStays(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Say("connecting")
	c.Failed(errors.New("no route to host"))

	got := readLog(t, c, "no route to host")
	if !strings.Contains(got, "close it when you have read it") {
		t.Errorf("it does not say what the pane is now: %q", got)
	}

	// It ends, the way a shell that exited does, so the pane stays with
	// what it said still in it.
	for i := 0; ; i++ {
		_, err := readLogOnce(t, c)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if i > 20 {
			t.Fatal("it never ended")
		}
	}
	// And it ends with no exit status. Nil would be a program that
	// exited cleanly, and the pane over it would tell the user a run went
	// well when the machine never answered.
	if err := waitedFor(t, c); !errors.Is(err, errNeverConnected) {
		t.Errorf("waiting on it gave %v, want it to say the connection was not made", err)
	}
}

// Closing the pane before the connection is made gives up on it.
func TestClosingThePaneGivesUpOnTheConnection(t *testing.T) {
	gave := make(chan struct{})
	c := atTime(newConnLog(func() { close(gave) }))

	c.Say("connecting")
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-gave:
	case <-time.After(waitBudget):
		t.Fatal("closing the pane did not give up on the connection")
	}

	// And closing it twice is not a failure.
	if err := c.Close(); err != nil {
		t.Errorf("closing again gave %v", err)
	}
}

// A connection that arrives after the user gave up is closed rather
// than handed to a pane that has gone.
func TestAConnectionThatArrivesTooLateIsClosed(t *testing.T) {
	c := atTime(newConnLog(nil))
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	shell := newPipeSession()
	c.Became("picard", shell)

	waitUntil(t, "the shell to be closed", func() bool { return shell.isClosed() })
}

// Closing the pane after the connection was made closes the connection.
func TestClosingThePaneClosesTheConnection(t *testing.T) {
	c := atTime(newConnLog(nil))
	shell := newPipeSession()
	c.Became("picard", shell)

	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitUntil(t, "the shell to be closed", func() bool { return shell.isClosed() })
}

// waitedFor waits on a log, failing the test rather than hanging it.
func waitedFor(t *testing.T, c *connLog) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(waitBudget):
		t.Fatal("waiting never came back")
		return nil
	}
}

// The pane reads what is written to it as a terminal stream, so a
// server's own wording cannot be allowed to drive it.
//
// Left alone, a server could clear the pane, move the cursor back over
// "its host key is accepted", and write whatever it liked there in this
// window's own voice.
func TestWhatTheFarEndSaysCannotDriveThePane(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Quote("marcus@picard", "\x1b[2Jsign in here\x1b]0;owned\x07")

	got := plainly(readLog(t, c, "sign in here"))
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("the pane was written %q, want nothing a terminal acts on", got)
	}
}

// A failure often carries the far end's own words inside it, and those
// reach the same pane.
func TestAFailureFromTheFarEndCannotDriveThePane(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Failed(errors.New("refused: \x1b[2Jtry somewhere else\x07"))

	got := plainly(readLog(t, c, "try somewhere else"))
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("the pane was written %q, want nothing a terminal acts on", got)
	}
}

// plainly takes this window's own colour off what a pane was written, so
// what is left is whatever the far end got into it.
func plainly(s string) string {
	for _, sgr := range []string{sgrWent, sgrWrong, sgrWords, sgrOff} {
		s = strings.ReplaceAll(s, sgr, "")
	}
	return s
}

// The whole account is kept, in order and without the colour it was
// shown in, because the pane keeps no copy of it.
func TestTheAccountIsKeptWithoutItsColour(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Say("connecting to margit:22")
	c.Quote("margit", "sign in at https://example.test/a/1234")
	c.Say("connected to margit")
	c.Became("margit", newPipeSession())

	want := []string{
		"09:41:02  connecting to margit:22",
		"09:41:02  margit says:",
		"    sign in at https://example.test/a/1234",
		"09:41:02  connected to margit",
	}
	got := c.Lines()
	if len(got) != len(want) {
		t.Fatalf("the account is %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i, got[i], want[i])
		}
	}
	for _, line := range got {
		if strings.ContainsAny(line, "\x1b\x07") {
			t.Errorf("the account carries what a terminal acts on: %q", line)
		}
	}
}

// A line that went well is stamped in green, a line that did not in red,
// and this window's own words are greyer than a shell's output.
func TestALineIsColouredByHowItWent(t *testing.T) {
	good := atTime(newConnLog(nil))
	defer func() { _ = good.Close() }()
	good.Say("connected to margit")
	said := readLog(t, good, "connected to margit")
	if !strings.Contains(said, sgrWent+"09:41:02"+sgrOff) {
		t.Errorf("the time is not in green: %q", said)
	}
	if !strings.Contains(said, sgrWords+"connected to margit"+sgrOff) {
		t.Errorf("the words are not in grey: %q", said)
	}
	if strings.Contains(said, sgrWrong) {
		t.Errorf("a line that went well is marked as one that did not: %q", said)
	}

	bad := atTime(newConnLog(nil))
	defer func() { _ = bad.Close() }()
	bad.Failed(errors.New("no route to host"))
	said = readLog(t, bad, "no route to host")
	if !strings.Contains(said, sgrWrong+"09:41:02"+sgrOff) {
		t.Errorf("a failure is not stamped in red: %q", said)
	}

	gave := atTime(newConnLog(nil))
	defer func() { _ = gave.Close() }()
	gave.GaveUp()
	said = readLog(t, gave, "given up on")
	if !strings.Contains(said, sgrWrong+"09:41:02"+sgrOff) {
		t.Errorf("giving up is not stamped in red: %q", said)
	}
}

// Once the connection is made the pane is emptied, scrollback and all,
// and one line says how it went and where the rest of it is.
func TestTheAccountIsFoldedWhenTheConnectionIsMade(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Say("connecting to margit:22")
	readLog(t, c, "connecting to margit:22")
	c.Became("margit", newPipeSession())

	said := plainly(readLog(t, c, "How it was reached"))
	if !strings.HasPrefix(said, clearPane) {
		t.Errorf("the pane was not cleared before the summary: %q", said)
	}
	want := "Connected to margit in under a second. " +
		`"How it was reached", on the plus menu of its row, shows the account.`
	if !strings.Contains(said, want) {
		t.Errorf("the summary is %q, want %q in it", said, want)
	}
	// The summary belongs to the pane, not to the account: it says where
	// the account is.
	for _, line := range c.Lines() {
		if strings.Contains(line, "How it was reached") {
			t.Errorf("the summary went into the account: %q", line)
		}
	}
}

// A connection that took longer than a second is summed up in seconds.
func TestASlowerConnectionIsSummedUpInSeconds(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()
	was := c.clock()
	c.clock = func() time.Time { return was.Add(1500 * time.Millisecond) }

	c.Became("margit", newPipeSession())

	said := plainly(readLog(t, c, "How it was reached"))
	if !strings.Contains(said, "Connected to margit in 1.5 s.") {
		t.Errorf("the summary is %q", said)
	}
}

// The pane sees the fold without waiting on the connection, which can
// take as long as the machine carrying it.
func TestTheFoldDoesNotWaitOnTheConnection(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()
	if err := c.Resize(80, 24); err != nil {
		t.Fatalf("resize: %v", err)
	}

	// Read past everything said so far and take the wake with it, so the
	// only wake left to come is the fold's.
	c.Say("connecting")
	readLog(t, c, "connecting")
	drainLog(c)

	slow := &slowSession{pipeSession: newPipeSession(), resizing: make(chan struct{})}
	go c.Became("margit", slow)

	select {
	case <-c.wake:
	case <-time.After(waitBudget):
		t.Fatal("the pane was not woken while the resize was still out there")
	}
	close(slow.resizing)

	// And what it was woken for is the fold.
	said := plainly(readLog(t, c, "How it was reached"))
	if !strings.HasPrefix(said, clearPane) {
		t.Errorf("the pane was not cleared: %q", said)
	}
}

// drainLog throws away what a log has left to say and the wake that goes
// with it, so a test can watch for the next one.
func drainLog(c *connLog) {
	c.mu.Lock()
	c.said = nil
	c.mu.Unlock()
	select {
	case <-c.wake:
	default:
	}
}

// slowSession is a connection whose resize does not come back, for
// checking that nothing the pane needs waits behind it.
type slowSession struct {
	*pipeSession
	resizing chan struct{}
}

func (s *slowSession) Resize(cols, rows int) error {
	<-s.resizing
	return s.pipeSession.Resize(cols, rows)
}

// Nothing the far end says can colour a line or move the cursor, even
// when it arrives as one of this window's own lines.
//
// The dial passes the server's wording on through Saying: the list of
// methods it says it will accept is the server's, written into the pane
// as though the window had said it.
func TestTheFarEndCannotColourTheAccount(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Say("it will accept \x1b[32m\n09:41:02\x1b[0m  its host key is accepted\x1b[2J")

	said := readLog(t, c, "its host key is accepted")
	if strings.ContainsAny(plainly(said), "\x1b\x07") {
		t.Errorf("the pane was written %q, want nothing a terminal acts on", said)
	}
	// Nothing a terminal acts on, and no line break: one line said is one
	// line kept, so nothing it said became a line in this window's voice.
	for _, line := range c.Lines() {
		if strings.ContainsAny(line, "\x1b\x07\n") {
			t.Errorf("the account carries what a terminal acts on: %q", line)
		}
	}
	if got := len(c.Lines()); got != 1 {
		t.Errorf("%d lines in the account, want the one: %q", got, c.Lines())
	}
}

// A connection made for files alone has nothing to put in the pane, so
// closing the pane lets the account go rather than giving up on the
// connection under it.
func TestClosingThePaneAfterConnectedDoesNotGiveUp(t *testing.T) {
	gave := make(chan struct{})
	c := atTime(newConnLog(func() { close(gave) }))

	c.Say("connecting")
	c.Connected()
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-gave:
		t.Fatal("closing the pane gave up on a connection that was made")
	default:
	}
	// The account stays, because the machine's row still offers it.
	if got := c.Lines(); len(got) != 1 || !strings.Contains(got[0], "connecting") {
		t.Errorf("the account is %v, want what was said while it connected", got)
	}
}

// A machine that answers and then cannot open what was asked for on it
// is still connected, and the account says that rather than saying the
// connection was never made.
func TestTheAccountOfSomethingThatWouldNotOpen(t *testing.T) {
	c := atTime(newConnLog(nil))

	c.Say("connected to margit")
	c.Refused("margit", "the files", errors.New("subsystem request failed"))

	said := strings.Join(c.Lines(), "\n")
	if strings.Contains(said, "The connection was not made") {
		t.Errorf("it says the dial failed on a machine that answered:\n%s", said)
	}
	for _, want := range []string{"subsystem request failed", "margit is connected", "the files"} {
		if !strings.Contains(said, want) {
			t.Errorf("the account does not say %q:\n%s", want, said)
		}
	}
	// Nothing more is written after it, the way a failure ends the log.
	c.Say("later")
	if got := c.Lines(); strings.Contains(strings.Join(got, "\n"), "later") {
		t.Errorf("the account went on after it ended:\n%s", strings.Join(got, "\n"))
	}
}
