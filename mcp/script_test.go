package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/agent"
)

// steps runs a list of steps in the fake pane and gives back what the
// agent was told.
func runList(t *testing.T, panes *fakePanes, list ...string) (string, bool) {
	t.Helper()
	args, err := json.Marshal(map[string]any{"pane": "1.1", "steps": list})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	answers := talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"`+panes.code+`"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":`+string(args)+`}}`)
	return textOf(t, answers[1])
}

// A list is typed and pressed in the order it was given, in one call.
//
// The whole point is what does not happen between the steps: a command,
// the program it starts, and what is typed into that program are one
// call rather than three with a gap in each.
func TestAListIsWorkedInOrder(t *testing.T) {
	panes := &fakePanes{
		code: "gt1-2222-abc",
		// The editor is up, which is what the list waits for.
		screen:  "\"notes.md\" [New] 0L, 0C",
		because: "the shell said so",
	}

	text, failed := runList(t, panes,
		"type:vim notes.md", "key:Enter", "until:[New", "type:ihello",
		"key:Escape", "type::wq", "key:Enter", "until")

	if failed {
		t.Fatalf("the list failed: %q", text)
	}
	panes.mu.Lock()
	typed, pressed, waited := panes.typed, panes.pressed, panes.waited
	panes.mu.Unlock()
	if want := "vim notes.mdihello:wq"; typed != want {
		t.Errorf("it typed %q, want %q", typed, want)
	}
	if want := []string{"Enter", "Escape", "Enter"}; strings.Join(pressed, ",") != strings.Join(want, ",") {
		t.Errorf("it pressed %v, want %v", pressed, want)
	}
	// The last wait is the bare one, which watches the pane rather than
	// any words.
	if waited.Contains != "" || waited.SinceKeys {
		t.Errorf("the last wait was %+v, want a wait on the pane", waited)
	}
	// And the answer is the pane once the waiting is over, so there is
	// nothing to call after it.
	if !strings.Contains(text, "8 steps ran") {
		t.Errorf("the answer does not say the list ran: %q", text)
	}
	if !strings.Contains(text, "[New") {
		t.Errorf("the answer does not carry the screen: %q", text)
	}
}

// A wait for text inside a list waits for that text to arrive.
//
// The text a list waits for is usually a word it has just typed, and a
// pane echoes what is typed.
func TestAWaitInsideAListWaitsForTheTextToArrive(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ echo done\ndone\n$ ",
		because: "the shell said the command had finished"}

	if text, failed := runList(t, panes, "type:echo done", "key:Enter", "until:done"); failed {
		t.Fatalf("the list failed: %q", text)
	}

	panes.mu.Lock()
	waited := panes.waited
	panes.mu.Unlock()
	if waited.Contains != "done" || !waited.SinceKeys {
		t.Errorf("it waited for %+v, want the text to arrive", waited)
	}
}

// A step that does not do what it says stops the list, and the answer
// says which one and what the pane looked like.
func TestAListStopsAtTheStepThatFailed(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ ", gaveUp: true}

	text, failed := runList(t, panes, "type:sleep 60", "key:Enter", "until:never", "type:rm -rf /")

	if !failed {
		t.Fatalf("a list whose wait ran out was answered as though it worked: %q", text)
	}
	if !strings.Contains(text, "step 3") || !strings.Contains(text, "until:never") {
		t.Errorf("the answer does not name the step that stopped it: %q", text)
	}
	// And nothing after it was typed, which is the whole reason to stop.
	panes.mu.Lock()
	typed := panes.typed
	panes.mu.Unlock()
	if strings.Contains(typed, "rm -rf /") {
		t.Errorf("it went on typing after the wait failed: %q", typed)
	}
}

// A list that cannot be read is refused before anything is typed.
func TestAListThatCannotBeReadTypesNothing(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}

	text, failed := runList(t, panes, "type:ls", "key:Enter", "press:Enter")

	if !failed {
		t.Fatalf("a list with a step that is not one was taken: %q", text)
	}
	if !strings.Contains(text, "step 3") {
		t.Errorf("the answer does not say which step: %q", text)
	}
	panes.mu.Lock()
	typed := panes.typed
	panes.mu.Unlock()
	if typed != "" {
		t.Errorf("it typed %q before refusing the list", typed)
	}
}

// A key nobody has a name for stops the list where it stands.
func TestAListWithAKeyThatIsNotOneStops(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}

	text, failed := runList(t, panes, "type:ls", "key:Meta+q")

	if !failed {
		t.Fatalf("a key nobody names was pressed: %q", text)
	}
	if !strings.Contains(text, "step 2") {
		t.Errorf("the answer does not say which step: %q", text)
	}
}

// Steps and the older text and keys in one call is a call that says two
// things about the same pane.
func TestAListAndTextTogetherIsRefused(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	answers := talk(t, panes,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+
			`{"name":"use_session_code","arguments":{"code":"gt1-2222-abc"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+
			`{"name":"send_keys","arguments":{"pane":"1.1","steps":["type:ls"],"text":"ls"}}}`)

	text, failed := textOf(t, answers[1])
	if !failed {
		t.Fatalf("it took both: %q", text)
	}
	panes.mu.Lock()
	typed := panes.typed
	panes.mu.Unlock()
	if typed != "" {
		t.Errorf("it typed %q", typed)
	}
}

// A list that ends without waiting says so, because the screen it would
// show has not caught up.
func TestAListThatWaitsForNothingSaysSo(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}

	text, failed := runList(t, panes, "type:ls", "key:Enter")

	if failed {
		t.Fatalf("the list failed: %q", text)
	}
	if !strings.Contains(text, "end a list with until") {
		t.Errorf("the answer does not say how to see what happened: %q", text)
	}
}

// A wait for text that ended for some other reason stops the list.
//
// The wait can end because the command finished or the pane went quiet,
// and a step that says "until the editor is up" has not done what it
// says when something else stopped the waiting. This is the case that
// matters most: "cd somewhere && vim notes.md" with the directory wrong
// finishes at once at a shell prompt, and the lines meant for the
// editor would be run as commands.
func TestAWaitThatEndedWithoutItsTextStopsTheList(t *testing.T) {
	panes := &fakePanes{
		code: "gt1-2222-abc",
		// The cd failed, so the command is over and the editor never
		// opened.
		screen:  "$ cd ~/work && vim notes.md\nbash: cd: /home/u/work: No such file or directory\n$ ",
		because: "the shell said the command had finished",
	}

	text, failed := runList(t, panes,
		"type:cd ~/work && vim notes.md", "key:Enter", "until:[New",
		"type:iline one", "key:Escape", "type::wq", "key:Enter", "until")

	if !failed {
		t.Fatalf("the list went on after the editor never opened: %q", text)
	}
	if !strings.Contains(text, "step 3") || !strings.Contains(text, "[New") {
		t.Errorf("the answer does not say which step stopped it: %q", text)
	}
	// And it says the waiting ended some other way, which is the fact
	// the agent needs to work out what to do next.
	if !strings.Contains(text, "the shell said the command had finished") {
		t.Errorf("the answer does not say how the waiting ended: %q", text)
	}
	panes.mu.Lock()
	typed := panes.typed
	panes.mu.Unlock()
	if strings.Contains(typed, "line one") {
		t.Errorf("it typed the editor's lines into the shell: %q", typed)
	}
}

// A guard stops the list when the pane is not where the list thinks it
// is, before anything is typed into it.
func TestAGuardStopsAListInTheWrongPlace(t *testing.T) {
	for _, one := range []struct {
		name string
		step string
		// screen is what the pane says when the guard runs.
		screen string
		stops  bool
	}{
		{name: "require, and it is there", step: "require:/home/u/work",
			screen: "$ cd ~/work && pwd\n/home/u/work\n$ "},
		{name: "require, and it is not", step: "require:/home/u/work",
			screen: "$ cd ~/work\nbash: cd: no such directory\n$ ", stops: true},
		{name: "fail, and it is there", step: "fail:No such file",
			screen: "$ cd ~/work\nbash: cd: No such file or directory\n$ ", stops: true},
		{name: "fail, and it is not", step: "fail:No such file",
			screen: "$ cd ~/work\n$ "},
	} {
		t.Run(one.name, func(t *testing.T) {
			panes := &fakePanes{code: "gt1-2222-abc", screen: one.screen,
				because: "the shell said the command had finished"}

			text, failed := runList(t, panes,
				"type:cd ~/work", "key:Enter", "until", one.step, "type:vim notes.md")

			if failed != one.stops {
				t.Fatalf("the list failed = %v, want %v: %q", failed, one.stops, text)
			}
			panes.mu.Lock()
			typed := panes.typed
			panes.mu.Unlock()
			if one.stops && strings.Contains(typed, "vim") {
				t.Errorf("it went on typing after the guard: %q", typed)
			}
			if !one.stops && !strings.Contains(typed, "vim") {
				t.Errorf("the guard stopped a list it should have let through: %q", typed)
			}
			if one.stops && !strings.Contains(text, "step 4") {
				t.Errorf("the answer does not say which step stopped it: %q", text)
			}
		})
	}
}

// A list that ends where the shell says a command finished answers with
// what that command printed, not with a rectangle of screen.
//
// A rectangle is what teaches an agent to clear the screen before every
// command so that the rectangle means something, and clearing throws
// away what the user had in front of them.
func TestAListAnswersWithWhatTheCommandPrinted(t *testing.T) {
	panes := &fakePanes{
		code:    "gt1-2222-abc",
		screen:  "$ ls\nnotes.md\n$ ",
		output:  "notes.md",
		because: agent.EndedOnMarks,
	}

	text, failed := runList(t, panes, "type:ls", "key:Enter", "until")

	if failed {
		t.Fatalf("the list failed: %q", text)
	}
	if !strings.Contains(text, "notes.md") {
		t.Errorf("the answer does not carry what the command printed: %q", text)
	}
	if strings.Contains(text, "$ ls") {
		t.Errorf("the answer is the screen rather than the output: %q", text)
	}
}

// A list that waits more than once answers with every wait, headed by
// the step and what that command exited with.
//
// Three checks in one call is the cheap thing to want, and the only
// other way to have it is chaining them with semicolons -- where the
// outputs run together and one exit status covers all three, which is
// what sends an agent back to clearing the screen between commands.
func TestAListSaysWhatEachWaitSaw(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ ", because: agent.EndedOnMarks,
		marks: true, hasStatus: true, status: 0, output: "first answer"}

	text, failed := runList(t, panes,
		"type:uname -r", "key:Enter", "until",
		"type:systemctl is-enabled foo", "key:Enter", "until")

	if failed {
		t.Fatalf("the list failed: %q", text)
	}
	for _, want := range []string{`step 3, "until"`, `step 6, "until"`, "exit status 0"} {
		if !strings.Contains(text, want) {
			t.Errorf("the answer does not carry %q: %q", want, text)
		}
	}
	// Both commands' output, not only the last one's.
	if got := strings.Count(text, "first answer"); got != 2 {
		t.Errorf("it carried %d of the 2 waits' output: %q", got, text)
	}
}
