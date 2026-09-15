package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakePanes is a window with one pane, for testing what an agent is
// told and what it may do.
type fakePanes struct {
	code   string
	screen string
	typed  string
	open   bool
	gone   bool
	gaveUp bool
	waited Until
}

func (f *fakePanes) Use(code string) (Pane, error) {
	if code != f.code {
		return Pane{}, errors.New("that code does not name a pane this window has handed over")
	}
	f.open = true
	return Pane{ID: "pane-1", Label: "bash on margit", Cols: 80, Rows: 24}, nil
}

func (f *fakePanes) List() ([]Pane, error) {
	if !f.open {
		return nil, nil
	}
	return []Pane{{ID: "pane-1", Label: "bash on margit", Cols: 80, Rows: 24}}, nil
}

func (f *fakePanes) Read(id string) (Screen, error) {
	if !f.open || id != "pane-1" {
		return Screen{}, errors.New("that is not a pane you have been handed")
	}
	return Screen{Screen: f.screen, Gone: f.gone}, nil
}

func (f *fakePanes) Send(id, text string) error {
	if !f.open || id != "pane-1" {
		return errors.New("that is not a pane you have been handed")
	}
	f.typed += text
	return nil
}

func (f *fakePanes) Wait(id string, until Until) (Screen, bool, error) {
	if !f.open || id != "pane-1" {
		return Screen{}, false, errors.New("that is not a pane you have been handed")
	}
	f.waited = until
	return Screen{Screen: f.screen, Gone: f.gone}, f.gaveUp, nil
}

func (f *fakePanes) Close() error { return nil }

// talk runs a conversation through the server and gives back the
// answers, one per message that had an id.
func talk(t *testing.T, panes Panes, messages ...string) []response {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(strings.NewReader(strings.Join(messages, "\n")+"\n"), &out, panes); err != nil {
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
	if panes.typed != "" {
		t.Errorf("the pane was sent %q", panes.typed)
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

	if panes.waited != (Until{Contains: "done", QuietMS: 250, TimeoutMS: 9000}) {
		t.Errorf("it waited for %+v", panes.waited)
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
	answers := talk(t, &fakePanes{},
		`not json at all`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":2}`,
		`[{"jsonrpc":"2.0","id":3,"method":"ping"}]`)

	if len(answers) != 4 {
		t.Fatalf("it gave %d answers", len(answers))
	}
	for i, want := range []int{codeParse, codeMethodNotFound, codeInvalidRequest, codeInvalidRequest} {
		if answers[i].Error == nil {
			t.Errorf("answer %d was not a failure", i)
			continue
		}
		if answers[i].Error.Code != want {
			t.Errorf("answer %d said %d, want %d", i, answers[i].Error.Code, want)
		}
	}
}

// A tool this server does not have is an answer, not a crash.
func TestAToolItDoesNotHaveIsAnAnswer(t *testing.T) {
	answers := talk(t, &fakePanes{},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"open_shell"}}`)

	text, failed := textOf(t, answers[0])
	if !failed {
		t.Errorf("it ran something it does not have: %q", text)
	}
	if !strings.Contains(text, "open_shell") {
		t.Errorf("it said %q", text)
	}
}

// The agent closing its end ends the server, rather than leaving it
// reading a stream nobody will write to.
func TestTheAgentGoingEndsIt(t *testing.T) {
	var out bytes.Buffer
	if err := Serve(strings.NewReader(""), &out, &fakePanes{}); err != nil {
		t.Errorf("it gave %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("it said %q to nobody", out.String())
	}
}
