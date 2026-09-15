package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// mute is a window that accepts an agent, greets it, and then says
// nothing at all.
//
// Not a window that has gone: the socket stays open, so nothing on it
// ever reports a failure. It is a window wedged behind a dialog, or one
// whose machine went to sleep.
func mute(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		in := bufio.NewReaderSize(conn, 4096)
		if _, err := readLine(in); err != nil {
			return
		}
		// The greeting, and then silence.
		if err := json.NewEncoder(conn).Encode(said{}); err != nil {
			return
		}
		<-done
	}()

	code, err := NewCode(ln.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	return code
}

// A window that greets an agent and then stops answering is given up on
// rather than waited for.
//
// The process running an agent has nothing that could close the
// connection from the side, so a question with no bound on it parks that
// agent for the life of the process.
func TestAWindowThatStopsAnsweringIsGivenUpOn(t *testing.T) {
	code := mute(t)

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	c.patience = 200 * time.Millisecond

	start := time.Now()
	_, err = c.Use(code)
	if !errors.Is(err, ErrGone) {
		t.Fatalf("Use = %v, want it to say the window is gone", err)
	}
	// And which ending it was: a window that ran out of time is not one
	// that hung up, and the user does something different about each.
	if !strings.Contains(err.Error(), "did not answer within") {
		t.Errorf("it said %q, want it to say the window ran out of time", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("it waited %v for a window that said nothing", took)
	}
	if !c.Gone() {
		t.Error("the connection is still open to a window that stopped answering")
	}
}

// A wait is the one question meant to take a while, and it gets the time
// it asked for.
func TestAWaitIsGivenTheTimeItAskedFor(t *testing.T) {
	c := &Client{patience: promptly}

	// A client with no patience of its own still bounds its questions.
	if got := (&Client{}).answerWithin(ask{Do: "read"}); got != promptly {
		t.Errorf("a client built without a patience gets %v, want %v", got, promptly)
	}
	if got := c.answerWithin(ask{Do: "read"}); got != promptly {
		t.Errorf("an ordinary question gets %v, want %v", got, promptly)
	}
	minute := ask{Do: "wait", Until: wait{TimeoutMS: 60000}}
	if got := c.answerWithin(minute); got < time.Minute {
		t.Errorf("a one-minute wait gets %v", got)
	}
	// One that asks for longer than the window will ever wait gets the
	// window's own limit and no more.
	forever := ask{Do: "wait", Until: wait{TimeoutMS: 1 << 30}}
	if got := c.answerWithin(forever); got > longestWait+promptly {
		t.Errorf("a wait with no sensible limit gets %v", got)
	}
}
