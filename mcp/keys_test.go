package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// use is the call that opens the one pane a fakePanes has.
const use = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` +
	`{"name":"use_session_code","arguments":{"code":"gt1-2222-abc"}}}`

// Keys reach the window as the names the agent wrote, for the window to
// encode: this server does not turn a name into bytes.
func TestKeysReachTheWindowByName(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	talk(t, panes, use,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"pane-1","keys":["Escape"]}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"pane-1","text":":q!",`+
			`"keys":["Enter"]}}}`)

	panes.mu.Lock()
	defer panes.mu.Unlock()
	if got := strings.Join(panes.pressed, ","); got != "Escape,Enter" {
		t.Errorf("the window was told to press %q", got)
	}
	if panes.typed != ":q!" {
		t.Errorf("the pane was sent %q", panes.typed)
	}
}

// A key name this does not have is refused, with the names it has, and
// nothing is sent.
func TestAKeyNameThisDoesNotHaveIsRefusedWithTheOnesItHas(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	answers := talk(t, panes, use,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"pane-1","text":"ls",`+
			`"keys":["Ecsape"]}}}`)

	text, failed := textOf(t, answers[1])
	if !failed {
		t.Errorf("it pressed a key it has no name for: %q", text)
	}
	for _, want := range []string{"Ecsape", "Escape", "PageUp", "F12", "Ctrl+<letter>"} {
		if !strings.Contains(text, want) {
			t.Errorf("it said %q, which does not mention %s", text, want)
		}
	}
	panes.mu.Lock()
	defer panes.mu.Unlock()
	if panes.typed != "" || len(panes.pressed) > 0 {
		t.Errorf("the pane was sent %q and %v", panes.typed, panes.pressed)
	}
}

// A call with neither text nor keys says so: there is nothing in it to
// send.
func TestSendingNeitherTextNorKeysIsRefused(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	answers := talk(t, panes, use,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"pane-1"}}}`)

	if answers[1].Error == nil {
		t.Fatalf("a call with nothing to send was taken: %+v", answers[1].Result)
	}
	if !strings.Contains(answers[1].Error.Message, "keys") {
		t.Errorf("it said %q", answers[1].Error.Message)
	}
}

// A read or a wait passes lines on to the window, bounded by what one
// answer carries.
func TestHowManyLinesToReadReachesTheWindow(t *testing.T) {
	for _, tc := range []struct {
		what  string
		args  string
		lines int
	}{
		{"read_pane", `{"pane":"pane-1"}`, 0},
		{"read_pane", `{"pane":"pane-1","lines":400}`, 400},
		{"read_pane", `{"pane":"pane-1","lines":100000}`, mostLines},
		{"wait_for", `{"pane":"pane-1","contains":"$","lines":250}`, 250},
	} {
		panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
		talk(t, panes, use,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
				`{"name":"`+tc.what+`","arguments":`+tc.args+`}}`)

		panes.mu.Lock()
		got := panes.lines
		panes.mu.Unlock()
		if got != tc.lines {
			t.Errorf("%s with %s asked the window for %d lines, want %d",
				tc.what, tc.args, got, tc.lines)
		}
	}
}

// The tools say what keys and lines do, because an agent has nothing
// else to go on.
func TestTheToolsSayWhatKeysAndLinesAreFor(t *testing.T) {
	answers := talk(t, &fakePanes{}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, err := json.Marshal(answers[0].Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var listed struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Properties map[string]struct {
					Type        string `json:"type"`
					Description string `json:"description"`
					Items       *struct {
						Type string `json:"type"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, tl := range listed.Tools {
		switch tl.Name {
		case "send_keys":
			if !strings.Contains(tl.Description, "keys") {
				t.Errorf("send_keys does not mention keys: %q", tl.Description)
			}
			// The example, which is the shortest way to say what a key
			// name looks like in a call.
			if !strings.Contains(tl.Description, `{"keys": ["Escape"]}`) {
				t.Errorf("send_keys shows no example: %q", tl.Description)
			}
			keys, there := tl.InputSchema.Properties["keys"]
			if !there {
				t.Fatalf("send_keys does not take keys: %+v", tl.InputSchema.Properties)
			}
			if keys.Type != "array" || keys.Items == nil || keys.Items.Type != "string" {
				t.Errorf("keys is %+v, want an array of strings", keys)
			}
			for _, want := range []string{"Escape", "Tab", "F5", "Ctrl+<letter>"} {
				if !strings.Contains(keys.Description, want) {
					t.Errorf("keys does not list %s: %q", want, keys.Description)
				}
			}
		case "read_pane", "wait_for":
			if !strings.Contains(tl.Description, "lines") {
				t.Errorf("%s does not mention lines: %q", tl.Name, tl.Description)
			}
			lines, there := tl.InputSchema.Properties["lines"]
			if !there {
				t.Fatalf("%s does not take lines: %+v", tl.Name, tl.InputSchema.Properties)
			}
			if lines.Type != "integer" || !strings.Contains(lines.Description, "2000") {
				t.Errorf("lines is %+v, and does not say the cap", lines)
			}
		}
	}

	// And a program that never goes quiet, which is what a bare wait
	// waits for.
	for _, tl := range listed.Tools {
		if tl.Name != "wait_for" {
			continue
		}
		for _, want := range []string{"never goes quiet", "top", "contains"} {
			if !strings.Contains(tl.Description, want) {
				t.Errorf("wait_for does not say %q: %q", want, tl.Description)
			}
		}
	}
}

// The rules say a password is the user's to type, because an agent that
// types one has taken a credential it was never given.
func TestTheRulesLeaveAPasswordToTheUser(t *testing.T) {
	for _, want := range []string{"password", "wait_for", "never type a password yourself"} {
		if !strings.Contains(Rules, want) {
			t.Errorf("the rules do not say %q: %q", want, Rules)
		}
	}
}

// The workflow says what keys and lines are, so an agent that only ever
// reads the instructions knows they exist.
func TestTheWorkflowMentionsKeysAndLines(t *testing.T) {
	for _, want := range []string{"keys", "lines"} {
		if !strings.Contains(Workflow, want) {
			t.Errorf("the workflow does not mention %s: %q", want, Workflow)
		}
	}
}

// A read cut down to the most lines one answer carries says so, and one
// that was not cut says nothing.
func TestAReadCutToTheMostLinesSaysSo(t *testing.T) {
	for _, tc := range []struct {
		what    string
		args    string
		clamped bool
	}{
		{"read_pane", `{"pane":"pane-1","lines":100000}`, true},
		{"read_pane", `{"pane":"pane-1","lines":400}`, false},
		{"read_pane", `{"pane":"pane-1"}`, false},
		{"wait_for", `{"pane":"pane-1","contains":"$","lines":100000}`, true},
		{"wait_for", `{"pane":"pane-1","contains":"$","lines":400}`, false},
	} {
		panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
		answers := talk(t, panes, use,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
				`{"name":"`+tc.what+`","arguments":`+tc.args+`}}`)

		told, failed := textOf(t, byID(t, answers)[2])
		if failed {
			t.Fatalf("%s with %s failed: %q", tc.what, tc.args, told)
		}
		if got := strings.Contains(told, "as many as a read gives"); got != tc.clamped {
			t.Errorf("%s with %s said %q", tc.what, tc.args, told)
		}
	}
}

// The tools say that a read of more lines than one answer carries gives
// the last 2000.
func TestTheToolsSayHowManyLinesAReadGives(t *testing.T) {
	answers := talk(t, &fakePanes{}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, err := json.Marshal(answers[0].Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Tools []tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("that is not a tool list: %s", raw)
	}
	for _, name := range []string{"read_pane", "wait_for"} {
		var said string
		for _, have := range got.Tools {
			if have.Name == name {
				said = have.Description + " " + have.InputSchema.Properties["lines"].Description
			}
		}
		if said == "" {
			t.Fatalf("there is no %s", name)
		}
		if !strings.Contains(said, "2000") || !strings.Contains(said, "the answer says so") {
			t.Errorf("%s never says what happens past 2000 lines: %q", name, said)
		}
	}
}

// An agent is told where the cursor is, and told when the screen is all
// there is because a full-screen program is drawing.
func TestTheAnswerSaysWhereTheCursorIsAndWhetherTheScreenIsAllThereIs(t *testing.T) {
	for _, tc := range []struct {
		what string
		args string
	}{
		{"read_pane", `{"pane":"pane-1"}`},
		{"wait_for", `{"pane":"pane-1","contains":"$"}`},
	} {
		// An ordinary screen: where the cursor is, and nothing about a
		// full-screen program.
		panes := &fakePanes{code: "gt1-2222-abc", screen: "$ ", row: 7, col: 2}
		answers := talk(t, panes, use,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
				`{"name":"`+tc.what+`","arguments":`+tc.args+`}}`)
		told, failed := textOf(t, byID(t, answers)[2])
		if failed {
			t.Fatalf("%s failed: %q", tc.what, told)
		}
		if !strings.Contains(told, "Cursor at row 7, column 2.") {
			t.Errorf("%s did not say where the cursor is: %q", tc.what, told)
		}
		if strings.Contains(told, "full-screen program") {
			t.Errorf("%s called an ordinary screen full-screen: %q", tc.what, told)
		}

		// And the alternate screen, where lines above the screen are
		// not there to read.
		panes = &fakePanes{code: "gt1-2222-abc", screen: "$ ", row: 0, col: 0, alt: true}
		answers = talk(t, panes, use,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
				`{"name":"`+tc.what+`","arguments":`+tc.args+`}}`)
		told, failed = textOf(t, byID(t, answers)[2])
		if failed {
			t.Fatalf("%s failed: %q", tc.what, told)
		}
		if !strings.Contains(told, "This is a full-screen program: the screen is all there is,"+
			" and lines above it cannot be read.") {
			t.Errorf("%s did not say the screen is all there is: %q", tc.what, told)
		}
	}
}
