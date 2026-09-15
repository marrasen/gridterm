package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePanes is a window with one pane, for testing what an agent is
// told and what it may do.
type fakePanes struct {
	mu     sync.Mutex
	code   string
	screen string
	typed  string
	open   bool
	gone   bool
	gaveUp bool
	waited Until

	// waiting is closed when a wait has started, and letGo lets it
	// finish, for a test about what else can be asked meanwhile.
	waiting chan struct{}
	letGo   chan struct{}
}

func (f *fakePanes) Use(code string) (Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if code != f.code {
		return Pane{}, errors.New("that code does not name a pane this window has handed over")
	}
	f.open = true
	return Pane{ID: "pane-1", Label: "bash on margit", Cols: 80, Rows: 24}, nil
}

func (f *fakePanes) List() ([]Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open {
		return nil, nil
	}
	return []Pane{{ID: "pane-1", Label: "bash on margit", Cols: 80, Rows: 24}}, nil
}

func (f *fakePanes) Read(id string) (Screen, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open || id != "pane-1" {
		return Screen{}, errors.New("that is not a pane you have been handed")
	}
	return Screen{Screen: f.screen, Gone: f.gone}, nil
}

func (f *fakePanes) Send(id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open || id != "pane-1" {
		return errors.New("that is not a pane you have been handed")
	}
	f.typed += text
	return nil
}

func (f *fakePanes) Wait(id string, until Until) (Screen, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open || id != "pane-1" {
		return Screen{}, false, errors.New("that is not a pane you have been handed")
	}
	f.waited = until
	waiting, letGo := f.waiting, f.letGo
	screen, gone, gaveUp := f.screen, f.gone, f.gaveUp
	f.mu.Unlock()
	if waiting != nil {
		close(waiting)
		<-letGo
	}
	f.mu.Lock()
	return Screen{Screen: screen, Gone: gone}, gaveUp, nil
}

func (f *fakePanes) Close() error { return nil }

// talk has a conversation with the server, one question at a time, and
// gives back the answers in the order the questions were asked.
//
// One at a time because that is what a client does: it waits for the
// answer to a question before asking one that depends on it. The server
// answers several at once, so a test that sent them all and read
// afterwards would be testing an order nothing promises.
func talk(t *testing.T, panes Panes, messages ...string) []response {
	t.Helper()
	toServer, fromTest := io.Pipe()
	fromServer, toTest := io.Pipe()

	done := make(chan error, 1)
	go func() { done <- Serve(t.Context(), toServer, toTest, panes) }()

	in := bufio.NewReaderSize(fromServer, 4096)
	var answers []response
	for _, m := range messages {
		if _, err := io.WriteString(fromTest, m+"\n"); err != nil {
			t.Fatalf("ask: %v", err)
		}
		if !wantsAnAnswer(m) {
			continue
		}
		line, _, err := in.ReadLine()
		if err != nil {
			t.Fatalf("it never answered %s: %v", m, err)
		}
		var r response
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("it said something unreadable: %q", line)
		}
		answers = append(answers, r)
	}
	if err := fromTest.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
	if err := toTest.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return answers
}

// wantsAnAnswer reports whether a message is one the server answers. A
// notification is not.
func wantsAnAnswer(message string) bool {
	var r request
	if err := json.Unmarshal([]byte(message), &r); err != nil {
		// Something the server cannot read, which it says so about.
		return true
	}
	return len(r.ID) > 0
}

// atOnce asks everything without waiting, for a test about what the
// server does with messages it cannot match to a question.
func atOnce(t *testing.T, panes Panes, messages ...string) []response {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(t.Context(), strings.NewReader(strings.Join(messages, "\n")+"\n"), &out, panes); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var answers []response
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r response
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("it said something unreadable: %q", line)
		}
		answers = append(answers, r)
	}
	return answers
}

// byID is the answers, by the id of the question each answers.
//
// Questions are answered as they come rather than one after another, so
// where an answer lands in the stream says nothing about which question
// it belongs to. The id does, which is what it is for.
func byID(t *testing.T, answers []response) map[int]response {
	t.Helper()
	out := map[int]response{}
	for _, a := range answers {
		var id int
		if err := json.Unmarshal(a.ID, &id); err != nil {
			continue
		}
		out[id] = a
	}
	return out
}

// textOf is what a tool call answered, and whether it failed.
func textOf(t *testing.T, r response) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(r.Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got result
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("that is not a tool answer: %s", raw)
	}
	var b strings.Builder
	for _, c := range got.Content {
		b.WriteString(c.Text)
	}
	return b.String(), got.IsError
}

// A server says what it is and what it can do.
func TestItSaysWhatItIsAndWhatItCanDo(t *testing.T) {
	answers := talk(t, &fakePanes{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	if len(answers) != 2 {
		t.Fatalf("it gave %d answers", len(answers))
	}
	raw, _ := json.Marshal(answers[0].Result)
	if !strings.Contains(string(raw), protocolVersion) {
		t.Errorf("it did not say which protocol: %s", raw)
	}
	if !strings.Contains(string(raw), "gridterm") {
		t.Errorf("it did not say what it is: %s", raw)
	}

	raw, _ = json.Marshal(answers[1].Result)
	for _, want := range []string{
		"use_session_code", "list_panes", "read_pane", "send_keys", "wait_for",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("it does not offer %s", want)
		}
	}
	// And nothing else. What an agent may do is the whole of the
	// argument for handing it a pane at all.
	var listed struct {
		Tools []tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(listed.Tools) != 5 {
		var names []string
		for _, tl := range listed.Tools {
			names = append(names, tl.Name)
		}
		t.Errorf("it offers %v", names)
	}
}

// A notification is acted on and never answered.
func TestANotificationIsNotAnswered(t *testing.T) {
	answers := talk(t, &fakePanes{},
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`)

	if len(answers) != 1 {
		t.Fatalf("it gave %d answers, want one", len(answers))
	}
	var id int
	if err := json.Unmarshal(answers[0].ID, &id); err != nil || id != 1 {
		t.Errorf("it answered %s", answers[0].ID)
	}
}

// A code opens a pane, and the agent is told what it has.
func TestACodeOpensAPane(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	answers := talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"gt1-2222-abc"}}}`)

	text, failed := textOf(t, answers[0])
	if failed {
		t.Fatalf("it failed: %s", text)
	}
	if !strings.Contains(text, "bash on margit") || !strings.Contains(text, "80x24") {
		t.Errorf("it said %q", text)
	}
	panes.mu.Lock()
	defer panes.mu.Unlock()
	if !panes.open {
		t.Error("it did not open the pane")
	}
}

// Without a code nothing works, and what an agent is told says what to
// do about it.
func TestWithoutACodeNothingWorks(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc"}
	answers := talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"read_pane","arguments":{"pane":"pane-1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"pane-1","text":"rm -rf /\r"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":`+
			`{"name":"list_panes","arguments":{}}}`)

	for _, i := range []int{0, 1} {
		text, failed := textOf(t, answers[i])
		if !failed {
			t.Errorf("it worked without a code: %q", text)
		}
	}
	panes.mu.Lock()
	sent := panes.typed
	panes.mu.Unlock()
	if sent != "" {
		t.Errorf("the pane was sent %q", sent)
	}
	text, failed := textOf(t, answers[2])
	if failed {
		t.Errorf("listing nothing failed: %q", text)
	}
	if !strings.Contains(text, "session code") {
		t.Errorf("it does not say how to get a pane: %q", text)
	}
}

// A wrong code opens nothing, and says so as an answer rather than as a
// protocol failure: the agent asked a fair question.
func TestAWrongCodeIsAnAnswerNotACrash(t *testing.T) {
	answers := talk(t, &fakePanes{code: "gt1-2222-abc"},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"gt1-2222-wrong"}}}`)

	if answers[0].Error != nil {
		t.Fatalf("it answered with a protocol failure: %v", answers[0].Error)
	}
	text, failed := textOf(t, answers[0])
	if !failed {
		t.Errorf("a wrong code was taken: %q", text)
	}
	if !strings.Contains(text, "does not name a pane") {
		t.Errorf("it said %q", text)
	}
}

// Typing goes in exactly as given.
func TestTypingGoesInExactlyAsGiven(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"gt1-2222-abc"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"pane-1","text":"uptime\r"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"pane-1","text":"\u0003"}}}`)

	panes.mu.Lock()
	defer panes.mu.Unlock()
	if panes.typed != "uptime\r\x03" {
		t.Errorf("the pane was sent %q", panes.typed)
	}
}

// Waiting passes on what was asked for, and says when it gave up.
func TestWaitingSaysWhenItGaveUp(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "still going", gaveUp: true}
	answers := talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"gt1-2222-abc"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"wait_for","arguments":{"pane":"pane-1","contains":"done",`+
			`"quiet_ms":250,"timeout_ms":9000}}}`)

	panes.mu.Lock()
	waited := panes.waited
	panes.mu.Unlock()
	if waited != (Until{Contains: "done", QuietMS: 250, TimeoutMS: 9000}) {
		t.Errorf("it waited for %+v", waited)
	}
	text, failed := textOf(t, answers[1])
	if failed {
		t.Fatalf("waiting failed: %s", text)
	}
	if !strings.Contains(text, "still going") {
		t.Errorf("it did not give back the screen: %q", text)
	}
	if !strings.Contains(text, "time ran out") {
		t.Errorf("it did not say it gave up: %q", text)
	}
}

// A pane whose program has finished says so, because nothing the agent
// types will be read.
func TestAFinishedProgramIsSaidPlainly(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "logout", gone: true}
	answers := talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"gt1-2222-abc"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"read_pane","arguments":{"pane":"pane-1"}}}`)

	text, _ := textOf(t, answers[1])
	if !strings.Contains(text, "has finished") {
		t.Errorf("it said %q", text)
	}
}

// Something that is not a message, or is not a method this has, is
// turned away by name.
func TestSomethingItCannotAnswerIsTurnedAway(t *testing.T) {
	answers := atOnce(t, &fakePanes{},
		`not json at all`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":2}`,
		`[{"jsonrpc":"2.0","id":3,"method":"ping"}]`)

	if len(answers) != 4 {
		t.Fatalf("it gave %d answers", len(answers))
	}
	// Two of them carry a null id, because the question did not say
	// what it wanted or was not a message at all. Null rather than
	// nothing: an answer that named no question at all is one the agent
	// cannot tell from a message of some other kind.
	var noID []response
	for _, a := range answers {
		if a.Error == nil {
			t.Errorf("it answered %v rather than saying what was wrong", a.Result)
		}
		if len(a.ID) == 0 {
			t.Errorf("an answer carried no id at all: %+v", a)
		}
		if string(a.ID) == "null" {
			noID = append(noID, a)
		}
	}
	said := byID(t, answers)
	if got := said[1].Error; got == nil || got.Code != codeMethodNotFound {
		t.Errorf("a method it does not have gave %v", got)
	}
	if got := said[2].Error; got == nil || got.Code != codeInvalidRequest {
		t.Errorf("a message that wants nothing gave %v", got)
	}
	// The unreadable line and the batch, in either order.
	var codes []int
	for _, a := range noID {
		codes = append(codes, a.Error.Code)
	}
	sort.Ints(codes)
	if len(codes) != 2 || codes[0] != codeParse || codes[1] != codeInvalidRequest {
		t.Errorf("the two it could not match gave %v", codes)
	}
}

// A tool this server does not have, and arguments it cannot read, are
// failures of the message rather than of what a tool did.
//
// The two are different things to tell an agent. A tool that ran and
// could not do what it was asked is an answer it should read; a call
// this server could not make sense of is a mistake in the asking.
func TestACallItCannotMakeSenseOfIsAFailureOfTheMessage(t *testing.T) {
	answers := talk(t, &fakePanes{},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"open_shell"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"read_pane","arguments":{"pane":42}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":`+
			`{"name":"read_pane","arguments":{}}}`)

	said := byID(t, answers)
	for id, what := range map[int]string{1: "open_shell", 2: "arguments", 3: "no pane"} {
		got := said[id].Error
		if got == nil {
			t.Errorf("%s was answered rather than refused: %v", what, said[id].Result)
			continue
		}
		if got.Code != codeInvalidParams {
			t.Errorf("%s gave %d, want %d", what, got.Code, codeInvalidParams)
		}
	}
	if got := said[1].Error; got != nil && !strings.Contains(got.Message, "open_shell") {
		t.Errorf("it said %q", got.Message)
	}
}

// A message that does not say it is JSON-RPC, or calls itself null, is
// refused.
func TestAMessageThatIsNotAQuestionIsRefused(t *testing.T) {
	answers := atOnce(t, &fakePanes{},
		`{"id":1,"method":"ping"}`,
		`{"jsonrpc":"1.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`)

	if len(answers) != 3 {
		t.Fatalf("it gave %d answers", len(answers))
	}
	for i, a := range answers {
		if a.Error == nil {
			t.Errorf("answer %d was not a refusal: %v", i, a.Result)
			continue
		}
		if a.Error.Code != codeInvalidRequest {
			t.Errorf("answer %d gave %d, want %d", i, a.Error.Code, codeInvalidRequest)
		}
	}
}

// A message too long to be a message is one bad message, not the end of
// the conversation.
func TestAMessageTooLongIsNotTheEnd(t *testing.T) {
	answers := talk(t, &fakePanes{},
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`+
			strings.Repeat("x", longestLine+16)+`"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`)

	if len(answers) != 2 {
		t.Fatalf("it gave %d answers, so it stopped listening", len(answers))
	}
	if answers[0].Error == nil || answers[0].Error.Code != codeParse {
		t.Errorf("the long one gave %+v", answers[0])
	}
	var id int
	if err := json.Unmarshal(answers[1].ID, &id); err != nil || id != 2 {
		t.Errorf("the one after it was answered as %s", answers[1].ID)
	}
}

// What it says about itself is what a client needs to talk to it.
func TestItSaysWhatAClientNeedsToKnow(t *testing.T) {
	answers := talk(t, &fakePanes{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+
			`{"protocolVersion":"1999-01-01","capabilities":{},`+
			`"clientInfo":{"name":"something","version":"1"}}}`)

	raw, err := json.Marshal(answers[0].Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var said struct {
		Protocol     string `json:"protocolVersion"`
		Capabilities *struct {
			Tools map[string]any `json:"tools"`
		} `json:"capabilities"`
		ServerInfo *struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(raw, &said); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// A version it does not speak is answered with the one it does,
	// rather than refused.
	if said.Protocol != protocolVersion {
		t.Errorf("it said it speaks %q", said.Protocol)
	}
	if said.Capabilities == nil || said.Capabilities.Tools == nil {
		t.Errorf("it did not say it has tools: %s", raw)
	}
	if said.ServerInfo == nil || said.ServerInfo.Name != "gridterm" ||
		said.ServerInfo.Version == "" {
		t.Errorf("it did not say what it is: %s", raw)
	}
	if !strings.Contains(said.Instructions, "session code") {
		t.Errorf("it did not say how to get a pane: %q", said.Instructions)
	}
}

// Every tool says what it takes, in a shape a client can check a call
// against before making it.
func TestEveryToolSaysWhatItTakes(t *testing.T) {
	answers := talk(t, &fakePanes{}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, err := json.Marshal(answers[0].Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var listed struct {
		Tools []struct {
			Name        string `json:"name"`
			Title       string `json:"title"`
			Description string `json:"description"`
			InputSchema struct {
				Type       string                    `json:"type"`
				Properties map[string]map[string]any `json:"properties"`
				Required   []string                  `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(listed.Tools) != 5 {
		t.Fatalf("it offers %d tools", len(listed.Tools))
	}
	needs := map[string][]string{
		"use_session_code": {"code"},
		"list_panes":       nil,
		"read_pane":        {"pane"},
		"send_keys":        {"pane", "text"},
		"wait_for":         {"pane"},
	}
	for _, tl := range listed.Tools {
		if tl.Title == "" || tl.Description == "" {
			t.Errorf("%s says nothing about itself", tl.Name)
		}
		if tl.InputSchema.Type != "object" {
			t.Errorf("%s takes %q, not an object", tl.Name, tl.InputSchema.Type)
		}
		want, known := needs[tl.Name]
		if !known {
			t.Errorf("it offers %s, which is not one of its tools", tl.Name)
			continue
		}
		if len(tl.InputSchema.Required) != len(want) {
			t.Errorf("%s requires %v, want %v", tl.Name, tl.InputSchema.Required, want)
		}
		for _, arg := range want {
			field, there := tl.InputSchema.Properties[arg]
			if !there {
				t.Errorf("%s does not say what %s is", tl.Name, arg)
				continue
			}
			if field["type"] == "" || field["description"] == "" {
				t.Errorf("%s says %v about %s", tl.Name, field, arg)
			}
		}
	}
}

// The agent closing its end ends the server, rather than leaving it
// reading a stream nobody will write to.
func TestTheAgentGoingEndsIt(t *testing.T) {
	var out bytes.Buffer
	if err := Serve(t.Context(), strings.NewReader(""), &out, &fakePanes{}); err != nil {
		t.Errorf("it gave %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("it said %q to nobody", out.String())
	}
}

// A question is answered while another is still being answered.
//
// A wait can be minutes long. An agent that could not ask anything else
// meanwhile -- not even whether this is still alive -- would have to
// choose between waiting and keeping in touch.
func TestAQuestionIsAnsweredWhileAWaitIsStillWaiting(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ ", waiting: make(chan struct{}), letGo: make(chan struct{})}
	toServer, fromTest := io.Pipe()
	fromServer, toTest := io.Pipe()

	done := make(chan error, 1)
	go func() { done <- Serve(t.Context(), toServer, toTest, panes) }()
	in := bufio.NewReaderSize(fromServer, 4096)

	ask := func(m string) {
		t.Helper()
		if _, err := io.WriteString(fromTest, m+"\n"); err != nil {
			t.Fatalf("ask: %v", err)
		}
	}
	answer := func() response {
		t.Helper()
		line, _, err := in.ReadLine()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var r response
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("it said something unreadable: %q", line)
		}
		return r
	}

	ask(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` +
		`{"name":"use_session_code","arguments":{"code":"gt1-2222-abc"}}}`)
	answer()

	// A wait that will not come back until this test lets it.
	ask(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":` +
		`{"name":"wait_for","arguments":{"pane":"pane-1"}}}`)
	<-panes.waiting

	// And a question asked while it is still waiting.
	ask(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	got := answer()
	var id int
	if err := json.Unmarshal(got.ID, &id); err != nil || id != 3 {
		t.Fatalf("it answered %s first, so the wait was in the way", got.ID)
	}

	close(panes.letGo)
	answer()
	if err := fromTest.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
	if err := toTest.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// deaf takes one answer and then refuses the rest, the way a stream
// that has broken part way does.
type deaf struct {
	mu    sync.Mutex
	took  int
	limit int
	why   error
}

func (d *deaf) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.took >= d.limit {
		return 0, d.why
	}
	d.took++
	return len(p), nil
}

// An answer that could not be written ends the conversation.
//
// The client is waiting for an id that will never be answered now.
// Carrying on writing into a stream that has refused one answer leaves
// it waiting for every one after it as well.
func TestAnAnswerThatCannotBeWrittenEndsTheConversation(t *testing.T) {
	boom := errors.New("the agent's stream has gone")
	out := &deaf{limit: 1, why: boom}

	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
	}, "\n") + "\n"

	err := Serve(t.Context(), strings.NewReader(in), out, &fakePanes{})
	if !errors.Is(err, boom) {
		t.Fatalf("Serve = %v, want the write that failed", err)
	}
}

// silentReader answers no read and never ends, the way a process's own
// standard input does when whatever is on the other end has stopped
// talking but not hung up.
type silentReader struct{ done chan struct{} }

func (r silentReader) Read([]byte) (int, error) { <-r.done; return 0, io.EOF }

// A write that failed ends the conversation even though the agent is
// still not saying anything.
//
// Nothing here can interrupt a read on a stream it was handed, so the
// read runs on its own goroutine and Serve stops without it.
func TestABrokenWriteEndsAConversationParkedOnARead(t *testing.T) {
	boom := errors.New("the agent's stream has gone")
	out := &deaf{why: boom}
	quiet := silentReader{done: make(chan struct{})}
	defer close(quiet.done)

	// One question, then silence. The answer to it cannot be written,
	// and nothing else will ever arrive to notice that with.
	in := io.MultiReader(
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"),
		quiet)

	done := make(chan error, 1)
	go func() { done <- Serve(t.Context(), in, out, &fakePanes{}) }()
	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Fatalf("Serve = %v, want the write that failed", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve parked on a read after the write it could not finish")
	}
}

// A cancelled context ends it too, for a caller that has decided to stop
// rather than one whose stream broke.
func TestACancelledContextEndsTheConversation(t *testing.T) {
	quiet := silentReader{done: make(chan struct{})}
	defer close(quiet.done)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, quiet, io.Discard, &fakePanes{}) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve = %v, want the cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve parked on a read after it was cancelled")
	}
}
