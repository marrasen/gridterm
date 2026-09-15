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
	return c
}

// readAll reads a log until it ends or says what was wanted.
func readLog(t *testing.T, c *connLog, want string) string {
	t.Helper()
	var got strings.Builder
	deadline := time.Now().Add(5 * time.Second)
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
	case <-time.After(5 * time.Second):
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
	for _, line := range strings.Split(got, "\r\n") {
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

// Once the connection is made the pane carries it, and what was written
// before stays where it was.
func TestThePaneBecomesTheConnection(t *testing.T) {
	c := atTime(newConnLog(nil))
	defer func() { _ = c.Close() }()

	c.Say("connecting")
	readLog(t, c, "connecting")

	shell := newPipeSession()
	c.Became(shell)

	shell.out <- []byte("marcus@picard:~$ ")
	readLog(t, c, "marcus@picard:~$")

	// And typing reaches it.
	if _, err := c.Write([]byte("uptime\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitUntil(t, func() bool { return shell.sentText() == "uptime\r" })
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
	c.Became(shell)

	waitUntil(t, func() bool { return shell.lastSize() == [2]int{120, 40} })

	// And a resize after it reaches it too.
	if err := c.Resize(80, 24); err != nil {
		t.Fatalf("resize: %v", err)
	}
	waitUntil(t, func() bool { return shell.lastSize() == [2]int{80, 24} })
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
	c.Became(shell)

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
	if err := waitedFor(t, c); err != nil {
		t.Errorf("waiting on it gave %v", err)
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
	case <-time.After(5 * time.Second):
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
	c.Became(shell)

	waitUntil(t, func() bool { return shell.isClosed() })
}

// Closing the pane after the connection was made closes the connection.
func TestClosingThePaneClosesTheConnection(t *testing.T) {
	c := atTime(newConnLog(nil))
	shell := newPipeSession()
	c.Became(shell)

	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitUntil(t, func() bool { return shell.isClosed() })
}

// waitedFor waits on a log, failing the test rather than hanging it.
func waitedFor(t *testing.T, c *connLog) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
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

	got := readLog(t, c, "sign in here")
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

	got := readLog(t, c, "try somewhere else")
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("the pane was written %q, want nothing a terminal acts on", got)
	}
}
