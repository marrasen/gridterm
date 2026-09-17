package mcp

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/agent"
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
			`{"name":"send_keys","arguments":{"pane":"1.1","keys":["Escape"]}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"1.1","text":":q!",`+
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
			`{"name":"send_keys","arguments":{"pane":"1.1","text":"ls",`+
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
			`{"name":"send_keys","arguments":{"pane":"1.1"}}}`)

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
		{"read_pane", `{"pane":"1.1"}`, 0},
		{"read_pane", `{"pane":"1.1","lines":400}`, 400},
		{"read_pane", `{"pane":"1.1","lines":100000}`, mostLines},
		{"wait_for", `{"pane":"1.1","contains":"$","lines":250}`, 250},
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
			if lines.Type != "integer" || !strings.Contains(lines.Description, strconv.Itoa(mostLines)) {
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

// The rules say what the pane is and who is watching, and stop there.
//
// They are not a list of prohibitions. What the tools refuse is refused
// in the tools; what is left is the part an agent cannot work out for
// itself, which is that this is somebody's live machine and that some of
// what a shell does cannot be taken back.
func TestTheRulesSayWhatThePaneIsAndStopThere(t *testing.T) {
	for _, want := range []string{
		"trusted with a live machine", "cannot undo", "take the pane back",
	} {
		if !strings.Contains(Rules, want) {
			t.Errorf("the rules do not say %q: %q", want, Rules)
		}
	}
	// Short enough to be read rather than skimmed past.
	if lines := strings.Count(strings.TrimRight(Rules, "\n"), "\n") + 1; lines > 6 {
		t.Errorf("the rules are %d lines long", lines)
	}
	// The short form the pasted prompt carries says the same, in less.
	for _, want := range []string{"trusted with a live machine", "cannot undo"} {
		if !strings.Contains(Short, want) {
			t.Errorf("the short rules do not say %q: %q", want, Short)
		}
	}
	if lines := strings.Count(strings.TrimRight(Short, "\n"), "\n") + 1; lines > 3 {
		t.Errorf("the short rules are %d lines long", lines)
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

// read_pane and wait_for say that every answer carries what is known
// about the command line, and that one of the two sources is a guess.
//
// It is the only place an agent is told those fields exist at all.
func TestTheToolsSayWhatIsKnownAboutTheCommand(t *testing.T) {
	want := map[string][]string{
		"read_pane": {"command line", "shell integration", "guess"},
		// And what ends a wait, including the thing that ends it whatever
		// else it was told to watch for.
		"wait_for": {"command line", "shell integration", "guess",
			"ends when the command you sent finishes", "the quiet is switched off",
			"a command that finishes still ends it"},
	}
	for _, tl := range toolList() {
		for _, say := range want[tl.Name] {
			if !strings.Contains(tl.Description, say) {
				t.Errorf("%s does not say %q: %q", tl.Name, say, tl.Description)
			}
		}
	}
}

// The workflow says a command needs a carriage return at the end, which
// no agent can guess and nothing else it reads says.
//
// The hand-over prompt used to say it as well. It stopped, so this is
// the only telling left.
func TestTheWorkflowSaysWhatSendsACommand(t *testing.T) {
	for _, want := range []string{`\r`, "Enter"} {
		if !strings.Contains(Workflow, want) {
			t.Errorf("the workflow does not say %q: %q", want, Workflow)
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
		{"read_pane", `{"pane":"1.1","lines":100000}`, true},
		{"read_pane", `{"pane":"1.1","lines":400}`, false},
		{"read_pane", `{"pane":"1.1"}`, false},
		{"wait_for", `{"pane":"1.1","contains":"$","lines":100000}`, true},
		{"wait_for", `{"pane":"1.1","contains":"$","lines":400}`, false},
	} {
		panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
		answers := talk(t, panes, use,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
				`{"name":"`+tc.what+`","arguments":`+tc.args+`}}`)

		told, failed := textOf(t, byID(t, answers)[2])
		if failed {
			t.Fatalf("%s with %s failed: %q", tc.what, tc.args, told)
		}
		if got := strings.Contains(told, "more lines than a read gives"); got != tc.clamped {
			t.Errorf("%s with %s said %q", tc.what, tc.args, told)
		}
	}
}

// The tools say that a read of more lines than one read gives comes back
// cut to the most a read gives.
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
		if !strings.Contains(said, strconv.Itoa(mostLines)) ||
			!strings.Contains(said, "the answer says so") {
			t.Errorf("%s never says what happens past %d lines: %q", name, mostLines, said)
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
		{"read_pane", `{"pane":"1.1"}`},
		{"wait_for", `{"pane":"1.1","contains":"$"}`},
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
		if !strings.Contains(told, "Cursor at row 7, column 2 of the screen.") {
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

// A message too long to be a message is refused here, before it is
// turned into a request the window would hang up over.
//
// This server's limit and the wire's have to agree: a message this takes
// and the window will not read leaves the agent's connection dead with
// nothing said about why.
func TestAMessageTooLongIsRefusedBeforeTheWindowSeesIt(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	answers := talk(t, panes, use,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"1.1","text":"`+
			strings.Repeat("x", 2<<20)+`"}}}`)

	if len(answers) < 2 || answers[1].Error == nil {
		t.Fatalf("a message of two megabytes was taken: %+v", answers)
	}
	if !strings.Contains(answers[1].Error.Message, "too long to read") {
		t.Errorf("it said %q", answers[1].Error.Message)
	}
	panes.mu.Lock()
	defer panes.mu.Unlock()
	if panes.typed != "" {
		t.Errorf("%d bytes of it reached the window", len(panes.typed))
	}
}

// More key names than one call presses is refused, with the number it
// takes, and nothing is sent.
func TestMoreKeysThanOneCallPressesIsRefused(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	many := make([]string, agent.MostKeys+1)
	for i := range many {
		many[i] = `"Enter"`
	}
	answers := talk(t, panes, use,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"1.1","keys":[`+
			strings.Join(many, ",")+`]}}}`)

	text, failed := textOf(t, byID(t, answers)[2])
	if !failed {
		t.Errorf("it pressed %d keys: %q", len(many), text)
	}
	if !strings.Contains(text, strconv.Itoa(agent.MostKeys)) {
		t.Errorf("it said %q, which does not say how many it takes", text)
	}
	panes.mu.Lock()
	defer panes.mu.Unlock()
	if len(panes.pressed) > 0 {
		t.Errorf("the window was told to press %v", panes.pressed)
	}

	// And the tool says the cap, so an agent need not find it by being
	// refused.
	listed := talk(t, &fakePanes{}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, err := json.Marshal(listed[0].Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Tools []tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("that is not a tool list: %s", raw)
	}
	for _, have := range got.Tools {
		if have.Name != "send_keys" {
			continue
		}
		if said := have.InputSchema.Properties["keys"].Description; !strings.Contains(
			said, strconv.Itoa(agent.MostKeys)) {
			t.Errorf("send_keys does not say how many keys it takes: %q", said)
		}
	}
}

// A read that reached the top of what the pane has kept says so, rather
// than leaving the agent to ask for the same lines again.
func TestAReadOfEverythingThereIsSaysSo(t *testing.T) {
	for _, tc := range []struct {
		all, alt bool
		says     bool
	}{
		{all: true, says: true},
		{all: false, says: false},
		// A full-screen program gets the note about that instead, which
		// says the same thing for a different reason.
		{all: true, alt: true, says: false},
	} {
		panes := &fakePanes{code: "gt1-2222-abc", screen: "$ ", all: tc.all, alt: tc.alt}
		answers := talk(t, panes, use,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
				`{"name":"read_pane","arguments":{"pane":"1.1","lines":400}}}`)

		told, failed := textOf(t, byID(t, answers)[2])
		if failed {
			t.Fatalf("the read failed: %q", told)
		}
		if got := strings.Contains(told, "everything the pane has kept"); got != tc.says {
			t.Errorf("all=%v alt=%v said %q", tc.all, tc.alt, told)
		}
	}
}

// The screen ends at a marker, and the tools say so.
//
// Everything after it is gridterm talking. An agent that compares two
// reads, or quotes a line back as contains, has to be able to tell the
// pane's own text from what was added to it.
func TestTheNotesComeAfterAMarkerTheToolsName(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ ls\nfile\n$ "}
	answers := talk(t, panes, use,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"read_pane","arguments":{"pane":"1.1"}}}`)

	told, failed := textOf(t, byID(t, answers)[2])
	if failed {
		t.Fatalf("the read failed: %q", told)
	}
	screen, notes, marked := strings.Cut(told, "\n\n"+notesMarker+"\n")
	if !marked {
		t.Fatalf("the answer has no marker: %q", told)
	}
	if screen != panes.screen {
		t.Errorf("the screen came back as %q, want %q", screen, panes.screen)
	}
	if !strings.Contains(notes, "Cursor at row") {
		t.Errorf("the notes are %q", notes)
	}

	// And the tools name the marker, so the agent is not left to notice
	// it.
	listed := talk(t, &fakePanes{}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, err := json.Marshal(listed[0].Result)
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
				said = have.Description
			}
		}
		if !strings.Contains(said, notesMarker) {
			t.Errorf("%s never names the marker: %q", name, said)
		}
	}
}
