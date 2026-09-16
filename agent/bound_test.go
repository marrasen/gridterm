package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
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
		if _, err := readLine(in, LongestRequest); err != nil {
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

// A read of a big screen comes back whole rather than closing the
// connection.
//
// The most a read gives, on the widest pane anybody has, in characters
// JSON writes as six bytes each: an answer the wire refused would leave
// the agent holding nothing but "that window has gone", and the user
// would have to make a new code. It is well over what a request may be,
// which is the point of the two limits differing.
func TestABigAnswerReachesTheAgent(t *testing.T) {
	w, _, code := listening(t)

	var wide strings.Builder
	for i := 0; i < MostLines; i++ {
		if i > 0 {
			wide.WriteByte('\n')
		}
		// A screenful of XML, where every character is one JSON escapes.
		wide.WriteString(strings.Repeat("<", 800))
	}
	w.say(wide.String())
	onTheWire, err := json.Marshal(said{Look: &Look{Screen: wide.String()}})
	if err != nil {
		t.Fatalf("measure the answer: %v", err)
	}
	if len(onTheWire) <= LongestRequest {
		t.Fatalf("that answer is %d bytes on the wire, which is inside what a request may be",
			len(onTheWire))
	}

	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	pane, err := c.Use(code)
	if err != nil {
		t.Fatalf("use: %v", err)
	}

	look, err := c.Read(pane.ID, MostLines)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if look.Screen != wide.String() {
		t.Errorf("it read %d bytes, want %d", len(look.Screen), wide.Len())
	}
	if c.Gone() {
		t.Error("the connection closed over an answer the window meant to send")
	}
}

// An answer longer than an answer can be reaches the agent as what went
// wrong, and not only as a window that has gone.
//
// The two are not the same thing to whoever is watching: a window that
// hung up wants looking at, and one that sent too much is a build of
// gridterm that disagrees with this one about how big an answer may be.
func TestAnAnswerTooLongToReadIsSaidInWords(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		in := bufio.NewReaderSize(conn, 4096)
		// The greeting, the question, and then more than one line may
		// ever carry back.
		if _, err := readLine(in, LongestRequest); err != nil {
			return
		}
		if err := json.NewEncoder(conn).Encode(said{OK: true}); err != nil {
			return
		}
		if _, err := readLine(in, LongestRequest); err != nil {
			return
		}
		_, _ = conn.Write(append([]byte(strings.Repeat("x", longestAnswer+1)), '\n'))
	}()

	code, err := NewCode(ln.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	c, err := Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if _, err := c.Read("pane-1", 0); err == nil {
		t.Fatal("an answer longer than the wire allows was read as an answer")
	} else {
		if !errors.Is(err, ErrGone) {
			t.Errorf("it said %v, want it to say the window is no longer answering", err)
		}
		if !strings.Contains(err.Error(), "too long to read") {
			t.Errorf("it said %q, and not what went wrong", err)
		}
	}
}

// A request longer than a request can be is refused in words, and
// nothing in it reaches the pane.
func TestARequestTooLongToBeARequestIsRefused(t *testing.T) {
	w, s, code := listening(t)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	out := json.NewEncoder(conn)
	in := bufio.NewReaderSize(conn, 4096)

	// The greeting and a code, so what refuses the request below is its
	// length and nothing else.
	for _, want := range []ask{{Do: "hello", Protocol: hello}, {Do: "use", Code: code}} {
		if err := out.Encode(want); err != nil {
			t.Fatalf("ask: %v", err)
		}
		if _, err := readLine(in, longestAnswer); err != nil {
			t.Fatalf("answer: %v", err)
		}
	}

	go func() {
		_ = out.Encode(ask{Do: "send", Pane: "pane-1", Text: strings.Repeat("x", LongestRequest)})
	}()
	line, err := readLine(in, longestAnswer)
	if err != nil {
		t.Fatalf("it never said why: %v", err)
	}
	var got said
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("it said something unreadable: %q", line)
	}
	if !strings.Contains(got.Error, "too long to read") {
		t.Errorf("it said %q", got.Error)
	}
	if typed := w.sentText(); typed != "" {
		t.Errorf("%d bytes of it reached the pane", len(typed))
	}
}

// A connection speaking another version of the protocol is told which
// of the two builds is the older one.
func TestAnotherProtocolVersionIsToldWhichBuildIsOlder(t *testing.T) {
	for _, tc := range []struct {
		spoke string
		want  string
	}{
		{"gridterm-agent-1", "the gridterm serving this mcp server is an older build"},
		{"gridterm-agent-9", "this window is an older build"},
		{"GET / HTTP/1.1", "not one of gridterm"},
	} {
		_, s, _ := listening(t)
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port()))
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		out := json.NewEncoder(conn)
		if err := out.Encode(ask{Do: "hello", Protocol: tc.spoke}); err != nil {
			t.Fatalf("greet: %v", err)
		}
		line, err := readLine(bufio.NewReaderSize(conn, 4096), longestAnswer)
		if err != nil {
			t.Fatalf("it never answered %s: %v", tc.spoke, err)
		}
		var got said
		if err := json.Unmarshal(line, &got); err != nil {
			t.Fatalf("it said something unreadable: %q", line)
		}
		if !strings.Contains(strings.ToLower(got.Error), tc.want) {
			t.Errorf("%s was told %q, want it to say %q", tc.spoke, got.Error, tc.want)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}
