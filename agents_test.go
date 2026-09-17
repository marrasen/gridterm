package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/mcp"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// handedOver hands the window's one pane to an agent and gives back the
// pane, the code, and an agent already connected with it.
func handedOver(t *testing.T, a *testApp) (*term.Terminal, string, *agent.Client) {
	t.Helper()
	pane := onlyPaneOn(t, a)
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	h := a.agents.of(pane)
	if h == nil {
		t.Fatal("the window did not record the handover")
	}
	c, err := agent.Dial(h.in.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return pane, h.in.code, c
}

// An agent given a code reads the pane and types into it.
//
// This is the whole of what a handover is for: the user sets something
// up -- through however many machines, as whichever user -- and then
// lets an agent work in it while watching.
func TestAnAgentWorksInThePaneItWasHanded(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(a.agents.code()))
		return err
	})
	if got.Cols != pane.Size().Cols || got.Rows != pane.Size().Rows {
		t.Errorf("it was told the pane is %dx%d", got.Cols, got.Rows)
	}

	// What is on the screen.
	a.shells[0].out <- []byte("root@margit:~# \r\n")
	waitFor(t, a, "the pane to show what the shell said", func() bool {
		return strings.Contains(paneText(pane), "root@margit")
	})

	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(got.ID, 0)
		return err
	})
	if !strings.Contains(look.Screen, "root@margit") {
		t.Errorf("it read %q", look.Screen)
	}
	if look.Gone {
		t.Error("it thinks the program has finished")
	}

	// And what it types reaches the program.
	offWindow(t, a, "the window to answer the agent", func() error { return c.Send(got.ID, "uptime\r", nil) })
	waitFor(t, a, "the shell to be sent what the agent typed", func() bool {
		return strings.Contains(a.shells[0].sentText(), "uptime")
	})
}

// Without a code an agent can do nothing, even having reached the
// window.
func TestAnAgentWithoutACodeCanDoNothing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, c := handedOver(t, a)

	// The name of the pane, which an agent could guess.
	a.refreshPanel(panelNow)
	id := a.panes[pane].ID()
	if id == "" {
		t.Fatal("the pane has no name")
	}

	if _, err := c.Read(id, 0); err == nil {
		t.Error("it read a pane it had given no code for")
	}
	if err := c.Send(id, "rm -rf /\r", nil); err == nil {
		t.Error("it typed into a pane it had given no code for")
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
}

// Taking the pane back stops the agent at once.
func TestTakingThePaneBackStopsTheAgent(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}

	if _, err := c.Read(got.ID, 0); err == nil {
		t.Error("it read a pane the user had taken back")
	}
	if err := c.Send(got.ID, "x", nil); err == nil {
		t.Error("it typed into a pane the user had taken back")
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
	// And nothing is listening any more, because nothing is handed
	// over.
	if a.agents.listening() {
		t.Error("the window is still listening for agents")
	}
}

// Nothing listens until the user hands a pane over.
func TestNothingListensForAgentsUntilAPaneIsHandedOver(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	if a.agents.listening() {
		t.Fatal("it is listening for agents with nothing handed over")
	}

	pane := onlyPaneOn(t, a)
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	if !a.agents.listening() {
		t.Fatal("it is not listening after a pane was handed over")
	}
	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	if a.agents.listening() {
		t.Error("it is still listening with nothing handed over")
	}
}

// Handing the same pane over twice gives back the code it already has,
// rather than a second one to take back separately.
func TestHandingOnePaneOverTwiceKeepsOneCode(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, _ := handedOver(t, a)

	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over again: %v", err)
	}
	if got := a.agents.code(); got != code {
		t.Errorf("it made a second code: %q then %q", code, got)
	}
}

// A pane that has closed cannot be handed over, and one handed over
// that closes takes the handover with it.
func TestAPaneThatClosesTakesItsHandoverWithIt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, _ := handedOver(t, a)

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	if a.agents.of(pane) != nil {
		t.Error("the handover outlived the pane")
	}
	if a.agents.listening() {
		t.Error("the window is still listening with nothing handed over")
	}
}

// The pane's row says an agent has been given it, and says the
// difference between offered and being worked in.
func TestTheRowSaysAnAgentHasThePane(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	a.refreshPanel(panelNow)
	if got := a.panes[pane].Note; got != agentOffered {
		t.Errorf("the row says %q", got)
	}

	offWindow(t, a, "the window to answer the agent", func() error {
		_, err := firstOf(c.Use(code))
		return err
	})
	waitFor(t, a, "the row to say an agent is working here", func() bool {
		a.refreshPanel(panelNow)
		return a.panes[pane].Note == agentAt
	})

	if err := c.Close(); err != nil {
		t.Fatalf("the agent leaving: %v", err)
	}
	waitFor(t, a, "the row to say it is only offered again", func() bool {
		a.refreshPanel(panelNow)
		return a.panes[pane].Note == agentOffered
	})
}

// exeHere is this program's own path, which every prompt and skill a test
// builds has to be built from.
func exeHere(t *testing.T) string {
	t.Helper()
	exe, err := exePath()
	if err != nil {
		t.Fatalf("this program's own path: %v", err)
	}
	return exe
}

// The prompt's setup lines are the picked host's, and no other host's.
//
// A prompt that offers an agent four ways of adding the server is a
// prompt it has to choose from, and the user already chose.
func TestTheHandoverPromptFollowsTheHostItIsFor(t *testing.T) {
	const code = "gt1-54321-abcdefghijklmnopqrstuvwxyz"
	// A path with a space in it, because that is where an unquoted
	// command line falls apart.
	const exe = `C:\Program Files\gridterm\gridterm.exe`
	// The line that sets each host up, and so the line no other host's
	// prompt may carry. The path is in quotes on a command line.
	setup := map[string]string{
		hostClaudeCode: `claude mcp add gridterm -- "` + exe + `" -mcp`,
		hostCodex:      `codex mcp add gridterm -- "` + exe + `" -mcp`,
		hostCursor:     "put this in ~/.cursor/mcp.json",
		hostOther:      "put this in its MCP config",
	}
	// What each prompt says has to happen after the setup, which is the
	// step an agent cannot take for itself.
	restart := map[string]string{
		hostClaudeCode: "start you again",
		hostCodex:      "start you again",
		hostCursor:     "start Cursor again",
		hostOther:      "start the host again",
	}
	// The hosts set up by a JSON config rather than by a command of
	// their own.
	byConfig := map[string]bool{hostCursor: true, hostOther: true}

	for _, host := range agentHosts {
		t.Run(host.name, func(t *testing.T) {
			prompt := handoverPrompt(host, code, exe)
			t.Logf("the prompt as generated:\n%s", prompt)

			if want := setup[host.name]; !strings.Contains(prompt, want) {
				t.Errorf("the prompt does not say %q", want)
			}
			for other, line := range setup {
				if other == host.name {
					continue
				}
				if strings.Contains(prompt, line) {
					t.Errorf("it also carries %s's %q", other, line)
				}
			}
			// The agent cannot do the setup itself. Adding the server only
			// writes config, and a session already running will not pick
			// it up. So the prompt has the agent ask the user, and say
			// what has to happen after that.
			for _, say := range []string{"Ask the user to", restart[host.name]} {
				if !strings.Contains(prompt, say) {
					t.Errorf("the prompt does not say %q", say)
				}
			}
			if got := strings.Contains(prompt, `{"mcpServers"`); got != byConfig[host.name] {
				t.Errorf("it carries a JSON config: %v, want %v", got, byConfig[host.name])
			}
			if !byConfig[host.name] {
				return
			}
			// The path in the JSON, where its backslashes have to be
			// doubled to be read back.
			var config struct {
				Servers map[string]struct {
					Command string   `json:"command"`
					Args    []string `json:"args"`
				} `json:"mcpServers"`
			}
			snippet := jsonIn(t, prompt)
			if err := json.Unmarshal([]byte(snippet), &config); err != nil {
				t.Fatalf("the JSON in the prompt does not parse: %v in %s", err, snippet)
			}
			got, there := config.Servers["gridterm"]
			if !there || got.Command != exe || len(got.Args) != 1 || got.Args[0] != "-mcp" {
				t.Errorf("the JSON says %+v", config.Servers)
			}
		})
	}
}

// The prompt gets an agent that has never heard of gridterm as far as
// the first tool call, and no further.
//
// It carries what the server cannot tell the agent itself: how to add
// the server, where it has to run, and the code. Everything past that --
// which tools there are, how to press Enter, what the rules are -- comes
// from the server's own instructions when the agent connects.
func TestTheHandoverPromptGetsTheAgentConnected(t *testing.T) {
	for _, host := range agentHosts {
		t.Run(host.name, func(t *testing.T) { promptGetsTheAgentConnected(t, host) })
	}
}

// promptGetsTheAgentConnected checks the prompt for one host. Every host
// has its own setup lines, and the file-based ones are the longest.
func promptGetsTheAgentConnected(t *testing.T, host agentHost) {
	t.Helper()
	const code = "gt1-54321-abcdefghijklmnopqrstuvwxyz"
	const exe = `C:\Users\someone\go\bin\gridterm.exe`
	prompt := handoverPrompt(host, code, exe)
	t.Logf("the prompt as generated:\n%s", prompt)

	if got := strings.Count(prompt, code); got != 1 {
		t.Errorf("the code is in the prompt %d times, want once", got)
	}

	// The one call an agent has to make before the server will say
	// anything about the pane.
	if !strings.Contains(prompt, "use_session_code") {
		t.Error("the prompt never mentions use_session_code")
	}
	// And that the server has to run here, because the port is local.
	if !strings.Contains(prompt, "loopback") && !strings.Contains(prompt, "this machine") {
		t.Error("the prompt does not say where the server has to run")
	}

	// What the prompt is for: the code is the only way in, the first
	// call is named, and the agent is told where the rest comes from.
	for _, say := range []string{
		"the only credential", "before anything else", "The server's own instructions",
	} {
		if !strings.Contains(prompt, say) {
			t.Errorf("the prompt does not say %q", say)
		}
	}

	// The rules in short, because a host is free to pass none of the
	// server's own instructions to the model and these are the part with
	// a cost.
	if mcp.Short == "" {
		t.Fatal("the short rules are empty, so this checks nothing")
	}
	if !strings.Contains(prompt, mcp.Short) {
		t.Errorf("the prompt does not carry the rules in short:\n%s", mcp.Short)
	}

	// The workflow is the server's to give. Two copies drift apart,
	// which is why the prompt stops short of it.
	if strings.Contains(prompt, mcp.Workflow) {
		t.Errorf("the prompt repeats the workflow the server's instructions carry")
	}
	for _, tool := range []string{"list_panes", "read_pane", "send_keys", "wait_for"} {
		if strings.Contains(prompt, tool) {
			t.Errorf("the prompt lists %s, which the server's instructions do", tool)
		}
	}

	// A dialog is narrow and a prompt nobody reads is a prompt nobody
	// pastes.
	for _, line := range strings.Split(prompt, "\n") {
		if len(line) > 100 {
			t.Errorf("a line is %d characters long: %q", len(line), line)
		}
	}
	// Cut to the prompt as it stands, with a line or two of room. A
	// prompt that grows back past this is the workflow creeping in again,
	// in words the checks above do not recognise.
	if lines := strings.Count(strings.TrimRight(prompt, "\n"), "\n") + 1; lines > 27 {
		t.Errorf("the prompt is %d lines long", lines)
	}
}

// jsonIn is the MCP config snippet out of a prompt, from the first brace
// to the last.
func jsonIn(t *testing.T, prompt string) string {
	t.Helper()
	from := strings.Index(prompt, `{"mcpServers"`)
	if from < 0 {
		t.Fatalf("the prompt carries no config for other hosts:\n%s", prompt)
	}
	to := strings.LastIndex(prompt, "}}}")
	if to < from {
		t.Fatalf("the config in the prompt never ends:\n%s", prompt)
	}
	return prompt[from : to+len("}}}")]
}

// handoverDialog hands a pane over and gives back the dialog that asks
// which agent it is for.
func handoverDialog(t *testing.T, a *testApp, pane *term.Terminal) *ui.Form {
	t.Helper()
	if err := a.handPane(pane); err != nil {
		t.Fatalf("share it: %v", err)
	}
	return awaitModal[*ui.Form](t, a, "the dialog with the pane's boxes on it",
		byTitle[*ui.Form](paneBoxesTitle))
}

// shareDialog puts a pane in the share and opens the share itself,
// which is where the code and what to paste to an agent live.
func shareDialog(t *testing.T, a *testApp, pane *term.Terminal) *ui.Form {
	t.Helper()
	if a.agents.of(pane) == nil {
		boxes := handoverDialog(t, a, pane)
		pressButton(t, a, boxes, "Done")
	}
	if err := a.showShare(); err != nil {
		t.Fatalf("show the share: %v", err)
	}
	return awaitModal[*ui.Form](t, a, "the share dialog", byTitle[*ui.Form](shareTitle))
}

// drawsEveryLine checks a dialog really draws every line it was given.
//
// Form drops the lines that will not fit in the window and says nothing
// about it, so a dialog that runs long stops mid-sentence.
func drawsEveryLine(t *testing.T, a *testApp, f *ui.Form) {
	t.Helper()
	joined := strings.Join(drawnLines(a, f), "\n")
	for _, line := range f.Lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// A line too wide for the box is trimmed rather than dropped, so
		// only the start of it is looked for.
		want := line
		if len(want) > 40 {
			want = want[:40]
		}
		if !strings.Contains(joined, want) {
			t.Errorf("at %dx%d the dialog stops before %q:\n%s",
				a.lastSize[0], a.lastSize[1], line, joined)
		}
	}
}

// drawsEveryButton checks a dialog really draws every button it was
// given.
//
// A form lays its buttons out from the right and drops the ones that
// will not fit, silently. pressButton works off the list rather than the
// screen, so a test that only presses buttons never sees the drop.
func drawsEveryButton(t *testing.T, a *testApp, f *ui.Form) {
	t.Helper()
	drawn := strings.Join(drawnLines(a, f), "\n")
	for _, b := range f.Buttons() {
		if !strings.Contains(drawn, b.Title) {
			t.Errorf("at %dx%d the dialog does not draw the %q button:\n%s",
				a.lastSize[0], a.lastSize[1], b.Title, drawn)
		}
	}
}

// Handing a pane over shows the code and copies the prompt, and copying
// opens nothing on top of the dialog.
//
// At eighty columns by twenty four rows, which is the smallest window
// anybody uses: a dialog that runs longer than that is cut off without a
// word, and the part that goes is the end.
func TestHandingAPaneOverShowsTheCodeAndCopiesThePrompt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	f := shareDialog(t, a, pane)
	drawsEveryLine(t, a, f)
	drawsEveryButton(t, a, f)
	code := a.agents.code()
	// The code is on the dialog the user is already looking at.
	if drawn := strings.Join(drawnLines(a, f), "\n"); !strings.Contains(drawn, code) {
		t.Errorf("the hand-over dialog never shows the code:\n%s", drawn)
	}
	pressButton(t, a, f, "Copy the prompt")

	host := hostNamed(hostClaudeCode)
	// Under go test this is the test binary, and under go run a
	// temporary one: a real path either way, and one worth nothing
	// tomorrow.
	exe := exeHere(t)
	prompt := handoverPrompt(host, code, exe)

	// The clipboard is written from a goroutine of its own, because on
	// some systems putting something on it means running a program.
	waitFor(t, a, "the prompt to reach the clipboard", func() bool { return a.copiedText() == prompt })
	if got := a.copiedText(); !strings.Contains(got, "use_session_code") ||
		!strings.Contains(got, "-mcp") {
		t.Errorf("the clipboard holds %q, which is not a prompt an agent can act on", got)
	}

	// Copying copies and nothing else: the dialog the user was on is
	// still the one in front, with nothing stacked over it to close.
	if top := a.root.Modal(); top != ui.Widget(f) {
		t.Errorf("copying the prompt put %T over the share dialog", top)
	}

	// The instructions are a button away, and say what to run.
	pressButton(t, a, f, "Instructions")
	shownIn := awaitModal[*ui.Form](t, a, "the install instructions",
		byTitle[*ui.Form]("Adding gridterm to "+host.called))
	drawsEveryLine(t, a, shownIn)
	drawsEveryButton(t, a, shownIn)
	drawn := strings.Join(drawnLines(a, shownIn), "\n")
	for _, say := range []string{
		"claude mcp add gridterm --", "start Claude Code again", "clipboard",
	} {
		if !strings.Contains(drawn, say) {
			t.Errorf("the dialog does not draw %q:\n%s", say, drawn)
		}
	}

	// And the command line can be taken off it, which is the whole point
	// of a dialog naming a command. The line itself, not the prompt and
	// not the dialog.
	pressButton(t, a, shownIn, "Copy the command")
	waitFor(t, a, "the setup line to reach the clipboard", func() bool {
		got := a.copiedText()
		return strings.HasPrefix(got, "claude mcp add gridterm -- ") && strings.Contains(got, exe)
	})
	if got := a.copiedText(); strings.Contains(got, "use_session_code") {
		t.Errorf("the button copied the prompt, not the setup line: %q", got)
	}

	pressButton(t, a, shownIn, "Done")
	// Ending the share is one button away, on the dialog underneath, and
	// it takes every pane back.
	pressButton(t, a, f, "Stop sharing")
	if a.agents.of(pane) != nil {
		t.Error("the pane is still shared")
	}
	if a.agents.sharing() {
		t.Error("the share is still open")
	}
}

// A prompt copied without gridterm's own path says so, there and then.
//
// The prompt still works where gridterm is on the PATH, and nowhere
// else. The user need never open the instructions, so the copy is where
// this has to be said.
func TestCopyingThePromptWithoutAPathSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	was := exePath
	exePath = func() (string, error) { return "gridterm", errors.New("no path for this process") }
	t.Cleanup(func() { exePath = was })

	f := shareDialog(t, a, pane)
	pressButton(t, a, f, "Copy the prompt")

	n := awaitModal[*ui.Notice](t, a, "a notice about the path", nil)
	if !n.Failure {
		t.Error("the notice does not read as a failure")
	}
	if !strings.Contains(n.Message(), "PATH") {
		t.Errorf("the notice says %q", n.Message())
	}
	// And the prompt is on the clipboard anyway: it is worth pasting.
	waitFor(t, a, "the prompt to reach the clipboard", func() bool {
		return strings.Contains(a.copiedText(), a.agents.code())
	})
}

// The copy chord copies the setup line too, for a user who reaches for
// the chord rather than the button.
func TestTheCopyChordOnTheInstallInstructionsCopiesTheSetupLine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	f := shareDialog(t, a, pane)
	pressButton(t, a, f, "Instructions")
	host := hostNamed(hostClaudeCode)
	shown := awaitModal[*ui.Form](t, a, "the install instructions",
		byTitle[*ui.Form]("Adding gridterm to "+host.called))

	chord := copyChordOf(t, a)
	sendKey(t, a, press(chord.Key, chord.Mods))
	waitFor(t, a, "the setup line to reach the clipboard", func() bool {
		got := a.copiedText()
		return strings.HasPrefix(got, "claude mcp add gridterm -- ") &&
			strings.Contains(got, exeHere(t))
	})
	if shown.ErrorText() != "" {
		t.Errorf("the dialog is showing an error: %q", shown.ErrorText())
	}
}

// The setup lines fit the dialog for a path of any length, so the exact
// command the user has to run is not trimmed at the edge of the box.
func TestTheSetupLinesFitTheDialog(t *testing.T) {
	// formMaxCols less the blank column each side, which is the widest a
	// dialog line is ever drawn.
	const room = 72
	paths := []string{
		`C:\Program Files\gridterm\gridterm.exe`,
		`C:\` + strings.Repeat("d", 47) + `\gridterm.exe`,
		`C:\` + strings.Repeat("d", 67) + `\gridterm.exe`,
	}
	for _, exe := range paths {
		for _, host := range agentHosts {
			lines := host.setupLines(exe)
			for _, line := range lines {
				if len(line) > room {
					t.Errorf("%s with a path of %d: a setup line is %d characters, over %d: %q",
						host.name, len(exe), len(line), room, line)
				}
			}
			// And the path is all there, however it had to be broken up.
			// A host set up by a file shows it inside JSON, where a
			// backslash is doubled.
			//
			// Only the line breaks are taken out, not the spaces: the
			// user copies this and an indent at a break would go inside
			// the path.
			want := exe
			if host.cmd == "" {
				want = strings.ReplaceAll(exe, `\`, `\\`)
			}
			if joined := strings.Join(lines, ""); !strings.Contains(joined, want) {
				t.Errorf("%s with a path of %d does not show the whole path unbroken: %q",
					host.name, len(exe), lines)
			}
		}
	}
}

// wrapped breaks a line to fit and puts nothing in front of what carries
// on, so the path the user copies out of the dialog joins back up as it
// was.
//
// A run of bytes that begins no character has nowhere to break at all,
// and is broken where it does not fit rather than looked at for ever.
func TestWrappedBreaksWithoutInsertingAnything(t *testing.T) {
	const room = 20
	for _, tc := range []struct{ what, line string }{
		{"a quoted path", `  "C:\` + strings.Repeat("d", 90) + `\gridterm.exe" -mcp`},
		{"bytes that begin no character", strings.Repeat("\x80", 30)},
		{"one that already fits", "  claude mcp add"},
	} {
		lines := wrapped(tc.line, room)
		for _, line := range lines {
			if len(line) > room {
				t.Errorf("%s: a line is %d characters, over %d: %q",
					tc.what, len(line), room, line)
			}
		}
		if joined := strings.Join(lines, ""); joined != tc.line {
			t.Errorf("%s: it broke into %q, which joins back up as %q", tc.what, lines, joined)
		}
	}
}

// The prompt is written for the agent the user picked, and the next
// hand-over opens on that agent.
//
// Whoever hands panes to Codex today will hand panes to Codex tomorrow,
// and picking it again every time is a question already answered.
func TestTheHandoverDialogWritesForThePickedAgentAndRemembersIt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	path := filepath.Join(t.TempDir(), "settings.json")
	set, err := withSettings(t, a, path)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	pane := onlyPaneOn(t, a)

	f := shareDialog(t, a, pane)
	// Nothing remembered yet, so it opens on Claude Code.
	fieldSays(t, f, "Agent", hostClaudeCode)
	stepOptions(t, a, f, "Agent")
	fieldSays(t, f, "Agent", hostCodex)
	pressButton(t, a, f, "Copy the prompt")

	code := a.agents.code()
	want := handoverPrompt(hostNamed(hostCodex), code, exeHere(t))
	waitFor(t, a, "the prompt for Codex to reach the clipboard",
		func() bool { return a.copiedText() == want })

	// The pick is written down, in the file as well as in this window.
	waitFor(t, a, "the pick to be remembered", func() bool {
		got, saved := set.AgentHost()
		return saved && got == hostCodex
	})
	again, err := settings.Load(path)
	if err != nil {
		t.Fatalf("load the settings again: %v", err)
	}
	if got, saved := again.AgentHost(); !saved || got != hostCodex {
		t.Errorf("the file remembered %q, %v; want %q", got, saved, hostCodex)
	}

	// The instructions follow the pick too, rather than the agent the
	// dialog opened on.
	pressButton(t, a, f, "Instructions")
	shown := awaitModal[*ui.Form](t, a, "the install instructions for Codex",
		byTitle[*ui.Form]("Adding gridterm to "+hostNamed(hostCodex).called))
	if drawn := strings.Join(drawnLines(a, shown), "\n"); !strings.Contains(drawn, "codex mcp add") {
		t.Errorf("the instructions are not Codex's:\n%s", drawn)
	}
	pressButton(t, a, shown, "Copy the command")
	waitFor(t, a, "Codex's setup line to reach the clipboard", func() bool {
		return strings.HasPrefix(a.copiedText(), "codex mcp add gridterm -- ")
	})
	pressButton(t, a, shown, "Done")

	// And the next hand-over opens on it.
	pressButton(t, a, f, "Done")
	next := shareDialog(t, a, pane)
	fieldSays(t, next, "Agent", hostCodex)
}

// Taking a pane back while an agent is waiting on it comes back.
//
// Every question an agent asks is answered by the goroutine that draws,
// and taking a pane back happens on that same goroutine. One waiting
// for the other is a window that never draws again, and the action that
// wedges it is the one the user reaches for when they want the agent to
// stop.
func TestTakingAPaneBackWhileAnAgentIsAskingComesBack(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// A wait in flight: the agent is asking, and nothing is running
	// what it asked for.
	asking := make(chan struct{})
	go func() {
		close(asking)
		_, _, _ = c.Wait(got.ID, 0, agent.Until{QuietMS: 60000, TimeoutMS: 60000})
	}()
	<-asking
	waitUntil(t, "work to be queued for the window", func() bool { return a.pump.pending() > 0 })

	done := make(chan error, 1)
	go func() { done <- a.takeBackPane(pane) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("taking it back gave %v", err)
		}
	case <-time.After(waitBudget):
		t.Fatal("taking the pane back waited for the agent it was taking it from")
	}
}

// Handing a pane over again does not let the agent that had it back in.
//
// Taking a pane back has to mean something even when the user changes
// their mind afterwards. The old agent still holds what it was given;
// what it was given has to have stopped naming anything.
func TestHandingAPaneOverAgainShutsOutTheAgentThatHadIt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var had agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		had, err = firstOf(c.Use(code))
		return err
	})

	// The user takes it back, thinks better of it, and hands it over
	// again -- to somebody else, with a new code.
	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over again: %v", err)
	}
	again := a.agents.of(pane)
	if a.agents.code() == code {
		t.Fatal("it handed out the same code again")
	}
	if again.name() == had.ID {
		t.Fatal("the new share gave the pane the name the old one had")
	}

	// The old agent is holding a name that no longer means anything.
	// Its connection was closed with the listener, so it reconnects the
	// way anything that lost a connection would.
	old, err := agent.Dial(a.agents.code())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = old.Close() }()

	done := make(chan error, 1)
	go func() {
		_, err := old.Read(had.ID, 0)
		done <- err
	}()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("the name it was given before still reads the pane")
	}

	// And typing with it reaches nothing.
	go func() {
		done <- old.Send(had.ID, "rm -rf /\r", nil)
	}()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("the name it was given before still types into the pane")
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
}

// A code the window never handed out opens nothing.
//
// The window has its own check, and it is the one that matters: the
// stand-in used elsewhere has one of its own, so a test against that
// says nothing about this.
func TestACodeTheWindowNeverHandedOutOpensNothing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	// A letter the code does not already end in, because a code is
	// random and one in thirty-two of them ends in any given letter.
	swap := "z"
	if strings.HasSuffix(code, swap) {
		swap = "y"
	}
	wrong := code[:len(code)-1] + swap
	if wrong == code {
		t.Fatal("the test did not change the code")
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Use(wrong)
		done <- err
	}()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("a code the window never handed out was taken")
	}
	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the pane was sent %q", got)
	}
}

// Taking one pane back stops the agent in it, with another still handed
// over.
//
// Taking the last one back stops the listener, which hides whether the
// window checks anything: the connection simply goes. With another pane
// still out, the listener stays up and the check is the only thing
// standing between the agent and the pane.
func TestTakingOnePaneBackWithAnotherStillOut(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	// Two panes, both handed over.
	if err := a.openTab(); err != nil {
		t.Fatalf("a second pane: %v", err)
	}
	panes := make([]*term.Terminal, 0, 2)
	for pane := range a.panes {
		panes = append(panes, pane)
	}
	if len(panes) != 2 {
		t.Fatalf("%d panes, want two", len(panes))
	}
	for _, pane := range panes {
		if err := a.handPane(pane); err != nil {
			t.Fatalf("hand it over: %v", err)
		}
	}

	c, err := agent.Dial(a.agents.code())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	var had agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		had, err = firstOf(c.Use(a.agents.code()))
		return err
	})

	if err := a.takeBackPane(panes[0]); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	if !a.agents.listening() {
		t.Fatal("the listener stopped with a pane still handed over")
	}

	// The connection is still open, so what refuses the agent is the
	// window.
	done := make(chan error, 1)
	go func() {
		_, err := c.Read(had.ID, 0)
		done <- err
	}()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("it read a pane the user had taken back")
	}

	go func() { done <- c.Send(had.ID, "rm -rf /\r", nil) }()
	waitFor(t, a, "the window to answer", func() bool { return len(done) > 0 })
	if err := <-done; err == nil {
		t.Error("it typed into a pane the user had taken back")
	}
	for i, shell := range a.shells {
		if got := shell.sentText(); got != "" {
			t.Errorf("shell %d was sent %q", i, got)
		}
	}
}

// The dialog says where to take the pane back from, and that place
// exists.
//
// A dialog that names a command the window does not have is worse than
// one that says nothing: the user goes looking.
func TestTheDialogNamesSomewhereTheCommandsReallyAre(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	pane := onlyPaneOn(t, a)
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}

	f := awaitModal[*ui.Form](t, a, "a dialog", nil)
	lines := strings.Join(f.Lines, " ")
	if !strings.Contains(lines, "Servers menu") {
		t.Errorf("the dialog says %q", lines)
	}

	// And the Servers menu really offers it.
	a.refreshServers()
	var offered bool
	for _, menu := range a.bar.Menus {
		if menu.Title != "Servers" {
			continue
		}
		for _, item := range menu.Items {
			if item.Command == "agent.take" {
				offered = true
			}
		}
	}
	if !offered {
		t.Error("the Servers menu does not offer taking the pane back")
	}
}

// withHome points os.UserHomeDir and os.UserConfigDir at a directory the
// test owns, and returns it.
//
// The real ones belong to whoever is running the tests, and a test that
// wrote a skill into them would replace a skill that person wrote.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	config := filepath.Join(home, "config")
	// HOME and XDG_CONFIG_HOME on Unix, USERPROFILE and APPDATA on
	// Windows. Setting all four keeps the test the same on either.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("APPDATA", config)
	return home
}

// gridtermSkillPath is where a skill goes for a host gridterm knows no
// skill directory for: under gridterm's own settings.
func gridtermSkillPath(t *testing.T) string {
	t.Helper()
	path, err := settings.Path()
	if err != nil {
		t.Fatalf("where the settings live: %v", err)
	}
	return filepath.Join(filepath.Dir(path), "skills", "gridterm", "SKILL.md")
}

// The dialog writes the skill where the picked host reads skills from,
// and says where that was.
func TestWritingTheSkillPutsItWhereTheHostLooks(t *testing.T) {
	for _, host := range agentHosts {
		t.Run(host.name, func(t *testing.T) {
			home := withHome(t)
			a := newTestApp(t, 90, 30)
			withDialogs(t, a)
			withPanel(t, a)
			pane := onlyPaneOn(t, a)

			want := gridtermSkillPath(t)
			if len(host.skillIn) > 0 {
				want = filepath.Join(append([]string{home}, append(host.skillIn, "SKILL.md")...)...)
			}

			f := shareDialog(t, a, pane)
			f.Field("Agent").SetText(host.name)
			pressButton(t, a, f, "Write the skill")

			n := awaitModal[*ui.Notice](t, a, "a notice saying where the skill went", nil)
			if n.Failure {
				t.Fatalf("writing the skill failed: %s", n.Message())
			}
			if !strings.Contains(n.Message(), want) {
				t.Errorf("the notice says %q, and not where the skill went", n.Message())
			}
			// A host gridterm knows no skill directory for has to be
			// told where to find it, or the file sits there unread.
			if len(host.skillIn) == 0 && !strings.Contains(n.Message(), "Copy it") {
				t.Errorf("the notice does not say to copy it: %q", n.Message())
			}

			got, err := os.ReadFile(want)
			if err != nil {
				t.Fatalf("read the skill back: %v", err)
			}
			if string(got) != skillFor(host, exeHere(t)) {
				t.Errorf("the file holds something else:\n%s", got)
			}
		})
	}
}

// A skill already there that says something else is not written over
// without the user's word.
//
// The file is the user's to edit once it is written, and a second press
// of the same button must not throw those edits away.
func TestWritingTheSkillWillNotLoseYourOwnEdits(t *testing.T) {
	home := withHome(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	path := filepath.Join(home, ".claude", "skills", "gridterm", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("make the skill directory: %v", err)
	}
	const mine = "---\nname: gridterm\n---\n\nMy own notes.\n"
	if err := os.WriteFile(path, []byte(mine), 0o600); err != nil {
		t.Fatalf("write my own skill: %v", err)
	}

	// Asked, and told to keep what is there.
	f := shareDialog(t, a, pane)
	pressButton(t, a, f, "Write the skill")
	ask := awaitModal[*ui.Form](t, a, "the dialog asking before it replaces the skill",
		byTitlePrefix[*ui.Form]("Replace"))
	if lines := strings.Join(ask.Lines, "\n"); !strings.Contains(lines, path) {
		t.Errorf("the question does not say which file: %v", ask.Lines)
	}
	pressButton(t, a, ask, "Keep")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the skill back: %v", err)
	}
	if string(got) != mine {
		t.Fatalf("my own skill was written over:\n%s", got)
	}

	// Asked again, and told to replace it.
	f = shareDialog(t, a, pane)
	pressButton(t, a, f, "Write the skill")
	ask = awaitModal[*ui.Form](t, a, "the dialog asking before it replaces the skill",
		byTitlePrefix[*ui.Form]("Replace"))
	pressButton(t, a, ask, "Replace")
	want := skillFor(hostNamed(hostClaudeCode), exeHere(t))
	waitFor(t, a, "the skill to be written", func() bool {
		got, err := os.ReadFile(path)
		return err == nil && string(got) == want
	})
}

// A skill that could not be written is said, not worked around.
func TestASkillThatCannotBeWrittenIsSaid(t *testing.T) {
	home := withHome(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	// A directory where the file has to go.
	path := filepath.Join(home, ".claude", "skills", "gridterm", "SKILL.md")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("make a directory in the way: %v", err)
	}

	f := shareDialog(t, a, pane)
	pressButton(t, a, f, "Write the skill")

	n := awaitModal[*ui.Notice](t, a, "the failure", nil)
	if !n.Failure {
		t.Errorf("it was not shown as a failure: %q", n.Message())
	}
	if !strings.Contains(n.Message(), path) {
		t.Errorf("the failure does not say which file: %q", n.Message())
	}
}

// The skill says what gridterm is, how to reach the server for this
// host, and carries the workflow and the rules as the server states them.
func TestTheSkillSaysHowToWorkInAHandedOverPane(t *testing.T) {
	const exe = `C:\Users\someone\go\bin\gridterm.exe`
	for _, host := range agentHosts {
		t.Run(host.name, func(t *testing.T) {
			skill := skillFor(host, exe)
			t.Logf("the skill as generated:\n%s", skill)

			// The front matter, which is how a host finds the skill at
			// all.
			const front = "---\nname: gridterm\ndescription: Work in the terminal panes the" +
				" user shared with you in gridterm, through its MCP server\n---\n"
			if !strings.HasPrefix(skill, front) {
				t.Errorf("the skill does not start with the front matter")
			}
			// The workflow and the rules word for word, so the skill and
			// the server's own instructions cannot drift apart.
			if !strings.Contains(skill, mcp.Workflow) {
				t.Error("the skill does not carry the workflow")
			}
			if !strings.Contains(skill, mcp.Rules) {
				t.Error("the skill does not carry the rules")
			}
			// This host's setup, and how to get a code.
			if !strings.Contains(skill, host.setupForAgent(exe)) {
				t.Errorf("the skill does not say how to get the server added to %s", host.name)
			}
			if !strings.Contains(skill, "use_session_code") {
				t.Error("the skill does not say what to do with a session code")
			}
			if !strings.Contains(skill, "gridterm is a terminal") {
				t.Error("the skill does not say what gridterm is")
			}
		})
	}
}

// The install instructions read whole in a small window for every host,
// and with the line more they have to say when gridterm could not read
// its own path.
func TestTheInstallInstructionsReadWholeForEveryHost(t *testing.T) {
	for _, host := range agentHosts {
		t.Run(host.name, func(t *testing.T) {
			a := newTestApp(t, 80, 24)
			withDialogs(t, a)
			withPanel(t, a)
			pane := onlyPaneOn(t, a)
			f := shareDialog(t, a, pane)
			drawsEveryLine(t, a, f)

			// The longest it gets: a path with a space in it, and the
			// note about not having read the path at all.
			const exe = `C:\Program Files\gridterm\gridterm.exe`
			a.showSetup(host, exe, errors.New("gridterm could not read its own path"))

			shown := awaitModal[*ui.Form](t, a, "the install instructions",
				byTitle[*ui.Form]("Adding gridterm to "+host.called))
			drawsEveryLine(t, a, shown)
			drawsEveryButton(t, a, shown)
			for _, line := range shown.Lines {
				if len(line) > dialogCols {
					t.Errorf("a line is %d characters and the dialog draws %d: %q",
						len(line), dialogCols, line)
				}
			}

			// What the button says, and what it copies, for this host.
			// Spelled out rather than asked of the code being tested.
			copies := map[string][2]string{
				hostClaudeCode: {"Copy the command", `claude mcp add gridterm -- "` + exe + `" -mcp`},
				hostCodex:      {"Copy the command", `codex mcp add gridterm -- "` + exe + `" -mcp`},
				hostCursor:     {"Copy the config", `{"mcpServers": {"gridterm": {`},
				hostOther:      {"Copy the config", `{"mcpServers": {"gridterm": {`},
			}
			want := copies[host.name]
			pressButton(t, a, shown, want[0])
			waitFor(t, a, "the setup to reach the clipboard", func() bool {
				return strings.Contains(a.copiedText(), want[1])
			})
			// And it says the path is a guess, rather than showing a line
			// that will not work and leaving the user to find out.
			drawn := strings.Join(drawnLines(a, shown), "\n")
			if !strings.Contains(drawn, "could not read its own path") {
				t.Errorf("the dialog does not say the path is unknown:\n%s", drawn)
			}
		})
	}
}

// An agent is told where the cursor is and whether a full-screen
// program is drawing.
//
// A screen as plain text says neither. Where the cursor sits is what
// tells an agent that the shell is at a prompt rather than part way
// through a line, and the alternate screen is why asking for more lines
// than the screen holds gives the screen.
func TestAnAgentSeesTheCursorAndTheAlternateScreen(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// A shell at a prompt: the cursor is where the next character would
	// go, which is the end of the prompt on the second row.
	a.shells[0].out <- []byte("last login: never\r\n$ ")
	waitFor(t, a, "the pane to show the prompt", func() bool {
		return strings.Contains(paneText(pane), "$ ")
	})

	var look agent.Look
	read := func() {
		t.Helper()
		offWindow(t, a, "the window to answer the agent", func() error {
			var err error
			look, err = c.Read(got.ID, 0)
			return err
		})
	}
	read()
	if look.Row != 1 || look.Col != 2 {
		t.Errorf("the agent was told the cursor is at row %d, column %d, want row 1, column 2",
			look.Row, look.Col)
	}
	if look.Alt {
		t.Error("it was told an ordinary screen is a full-screen program")
	}

	// And a full-screen program, which draws on the alternate screen.
	a.shells[0].out <- []byte("\x1b[?1049h\x1b[H~")
	waitFor(t, a, "the pane to move to the alternate screen", func() bool {
		_, _, alt := pane.Cursor()
		return alt
	})
	read()
	if !look.Alt {
		t.Error("the agent was not told a full-screen program is drawing")
	}
}

// Two reads a line apart on a pane that has said nothing render once.
//
// Rendering is the expensive half of a read and it happens under the
// pane's lock, so an agent alternating two counts could keep the window
// from drawing. A read of fewer lines than the last one is cut from what
// was already read.
func TestTwoReadsOfAlmostTheSameLengthRenderOnce(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	a.shells[0].out <- []byte("root@margit:~# \r\n")
	waitFor(t, a, "the pane to show what the shell said", func() bool {
		return strings.Contains(paneText(pane), "root@margit")
	})

	// Two counts a line apart and both inside what one read gives, so it
	// is the reading kept here that answers the second and not the cap.
	var first, second agent.Look
	offWindow(t, a, "the window to read 400 lines", func() error {
		var err error
		first, err = c.Read(got.ID, 400)
		return err
	})
	offWindow(t, a, "the window to read 399 lines", func() error {
		var err error
		second, err = c.Read(got.ID, 399)
		return err
	})

	if h := a.agents.of(pane); h.rendered != 1 {
		t.Errorf("two reads rendered the pane %d times, want once", h.rendered)
	}
	if first.Screen != second.Screen {
		t.Errorf("the second read says something else:\n%q\n%q", first.Screen, second.Screen)
	}
	// And both say this is everything the pane has kept, because a pane
	// that has said one line has nothing like four hundred.
	if !first.All || !second.All {
		t.Errorf("a read of the whole of a short pane did not say so: %v, %v",
			first.All, second.All)
	}

	// The screen itself is not the whole of what has been kept, so it
	// says nothing of the sort.
	var screen agent.Look
	offWindow(t, a, "the window to read the screen", func() error {
		var err error
		screen, err = c.Read(got.ID, 0)
		return err
	})
	if screen.All {
		t.Error("a read of the screen was called everything the pane has kept")
	}
	if got := countLines(screen.Screen); got != pane.Size().Rows {
		t.Errorf("the screen is %d lines, want %d", got, pane.Size().Rows)
	}
}

// A read taken after the pane is resized is of the pane as it is now.
//
// The reading kept here is answered from again when the pane has said
// nothing since, and a resize says nothing. An agent served from the old
// one is given rows of a screen that has gone and a cursor below the
// bottom of the one it has.
func TestAReadAfterAResizeIsOfTheScreenAsItIsNow(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// Enough lines to fill the screen, so the cursor sits on its last
	// row and a shorter screen has no such row.
	a.shells[0].out <- []byte(strings.Repeat("filling up\r\n", 40) + "root@margit:~# ")
	waitFor(t, a, "the pane to fill up", func() bool {
		return strings.Contains(paneText(pane), "root@margit")
	})

	// A read of the whole of what the pane has kept, which is what the
	// reading here is left holding.
	offWindow(t, a, "the window to read what the pane has kept", func() error {
		_, err := c.Read(got.ID, 500)
		return err
	})
	rendered := a.agents.of(pane).rendered

	was := pane.Size()
	a.setGridSize(60, 14)
	waitFor(t, a, "the pane to take the new size", func() bool { return pane.Size() != was })
	rows := pane.Size().Rows

	var look agent.Look
	offWindow(t, a, "the window to read the screen", func() error {
		var err error
		look, err = c.Read(got.ID, 0)
		return err
	})
	if a.agents.of(pane).rendered == rendered {
		t.Error("the read after the resize was cut from the reading of the screen before it")
	}
	if lines := countLines(look.Screen); lines != rows {
		t.Errorf("the agent was given %d lines, want the pane's %d rows", lines, rows)
	}
	if look.Row >= rows {
		t.Errorf("the cursor is on row %d of a screen %d rows tall", look.Row, rows)
	}
}

// Codex's skill goes under gridterm's own settings, with the notice that
// says to move it.
//
// gridterm has never checked where Codex reads skills from, and a file
// written into a directory a host does not read is a skill nobody will
// ever see.
func TestTheSkillForCodexGoesUnderGridtermsOwnSettings(t *testing.T) {
	withHome(t)
	host := hostNamed(hostCodex)
	if len(host.skillIn) > 0 {
		t.Errorf("gridterm claims Codex reads skills from %v", host.skillIn)
	}

	path, ownPlace, err := skillPathFor(host)
	if err != nil {
		t.Fatalf("where the skill goes: %v", err)
	}
	if ownPlace {
		t.Error("it thinks that is where Codex looks")
	}
	if want := gridtermSkillPath(t); path != want {
		t.Errorf("the skill goes to %q, want %q", path, want)
	}
	if said := skillWritten(host, path); !strings.Contains(said, "Copy it") ||
		!strings.Contains(said, "does not know where") {
		t.Errorf("the notice says %q, and not that gridterm is guessing", said)
	}
}

// Claude Code's own setting for where its configuration lives takes the
// skill with it.
func TestClaudeConfigDirMovesTheSkill(t *testing.T) {
	home := withHome(t)
	host := hostNamed(hostClaudeCode)

	// Unset, it is the ordinary place under the home directory.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	path, ownPlace, err := skillPathFor(host)
	if err != nil {
		t.Fatalf("where the skill goes: %v", err)
	}
	if want := filepath.Join(home, ".claude", "skills", "gridterm", skillFile); path != want {
		t.Errorf("the skill goes to %q, want %q", path, want)
	}
	if !ownPlace {
		t.Error("it does not think that is where Claude Code looks")
	}

	// Set, it goes there instead, and the skill really lands there.
	dir := filepath.Join(t.TempDir(), "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path, ownPlace, err = skillPathFor(host)
	if err != nil {
		t.Fatalf("where the skill goes: %v", err)
	}
	want := filepath.Join(dir, "skills", "gridterm", skillFile)
	if path != want {
		t.Errorf("the skill goes to %q, want %q", path, want)
	}
	if !ownPlace {
		t.Error("it does not think that is where Claude Code looks")
	}
	if _, _, err := writeSkill(host, `C:\gridterm.exe`, true); err != nil {
		t.Fatalf("write the skill: %v", err)
	}
	if _, err := os.ReadFile(want); err != nil {
		t.Fatalf("read the skill back: %v", err)
	}
}

// A setting naming a directory under the home directory is read from
// there, not from wherever gridterm was started.
//
// Claude Code reads that setting the same way. A skill written beside
// gridterm's working directory is a skill the host never finds.
func TestARelativeClaudeConfigDirIsReadFromTheHomeDirectory(t *testing.T) {
	host := hostNamed(hostClaudeCode)
	for _, tc := range []struct {
		what, set string
		want      []string
	}{
		{"a relative directory", "claude-config", []string{"claude-config"}},
		{"one under a tilde", "~/elsewhere/.claude", []string{"elsewhere", ".claude"}},
		{"a tilde on its own", "~", nil},
	} {
		t.Run(tc.what, func(t *testing.T) {
			home := withHome(t)
			t.Setenv("CLAUDE_CONFIG_DIR", tc.set)

			path, ownPlace, err := skillPathFor(host)
			if err != nil {
				t.Fatalf("where the skill goes: %v", err)
			}
			parts := append([]string{home}, tc.want...)
			want := filepath.Join(append(parts, "skills", "gridterm", skillFile)...)
			if path != want {
				t.Errorf("the skill goes to %q, want %q", path, want)
			}
			if !ownPlace {
				t.Error("it does not think that is where Claude Code looks")
			}
		})
	}
}

// A setting naming a directory from the root of a disk is left where it
// is, spelt either way.
//
// Windows calls neither /c/Users/me/.claude nor \opt\claude absolute, and joining
// one onto the home directory would write the skill somewhere nobody
// named.
func TestARootedClaudeConfigDirIsLeftAlone(t *testing.T) {
	host := hostNamed(hostClaudeCode)
	for _, tc := range []struct{ what, set string }{
		{"with forward slashes", "/c/Users/agent/.claude"},
		{"with backslashes", `\opt\claude`},
	} {
		t.Run(tc.what, func(t *testing.T) {
			home := withHome(t)
			t.Setenv("CLAUDE_CONFIG_DIR", tc.set)

			path, ownPlace, err := skillPathFor(host)
			if err != nil {
				t.Fatalf("where the skill goes: %v", err)
			}
			if want := filepath.Join(tc.set, "skills", "gridterm", skillFile); path != want {
				t.Errorf("the skill goes to %q, want %q", path, want)
			}
			if strings.HasPrefix(path, home) {
				t.Errorf("the skill goes under the home directory %q: %q", home, path)
			}
			if !ownPlace {
				t.Error("it does not think that is where Claude Code looks")
			}
		})
	}
}

// A path is quoted for the shell of the platform it will be typed into.
func TestAPathIsQuotedForTheShellItIsTypedInto(t *testing.T) {
	for _, tc := range []struct {
		goos, path, want string
	}{
		{"windows", `C:\Program Files\gridterm.exe`, `"C:\Program Files\gridterm.exe"`},
		{"windows", `C:\say "hi"\gridterm.exe`, `"C:\say \"hi\"\gridterm.exe"`},
		{"linux", "/usr/local/bin/grid term", `'/usr/local/bin/grid term'`},
		{"darwin", "/Users/o'brien/gridterm", `'/Users/o'\''brien/gridterm'`},
	} {
		if got := quotedPathOn(tc.path, tc.goos); got != tc.want {
			t.Errorf("on %s %q quotes as %q, want %q", tc.goos, tc.path, got, tc.want)
		}
	}
	// And this platform gets its own.
	got := quotedPath("/tmp/grid term")
	if runtime.GOOS == "windows" {
		if got != `"/tmp/grid term"` {
			t.Errorf("here a path quotes as %q", got)
		}
	} else if got != `'/tmp/grid term'` {
		t.Errorf("here a path quotes as %q", got)
	}
}

// The hand-over dialog does both of its offers in one visit, and shows
// the code without being asked.
//
// A user who presses "Done" has a live code they were never shown, and
// one who copies the prompt should not have to hand the pane over again
// to write the skill.
func TestTheHandoverDialogDoesBothInOneVisit(t *testing.T) {
	withHome(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	f := shareDialog(t, a, pane)
	code := a.agents.code()
	drawsEveryLine(t, a, f)
	if drawn := strings.Join(drawnLines(a, f), "\n"); !strings.Contains(drawn, code) {
		t.Errorf("the first dialog never shows the code:\n%s", drawn)
	}

	// The prompt, and the dialog that says how to install the server.
	pressButton(t, a, f, "Copy the prompt")
	pressButton(t, a, f, "Instructions")
	shown := awaitModal[*ui.Form](t, a, "the install instructions",
		byTitle[*ui.Form]("Adding gridterm to "+hostNamed(hostClaudeCode).called))
	pressButton(t, a, shown, "Done")

	// And then the skill, without handing the pane over again.
	pressButton(t, a, f, "Write the skill")
	n := awaitModal[*ui.Notice](t, a, "a notice saying where the skill went", nil)
	if n.Failure {
		t.Fatalf("writing the skill failed: %s", n.Message())
	}
	if !strings.Contains(n.Message(), skillFile) {
		t.Errorf("the notice says %q", n.Message())
	}

	// Still the one hand-over, with the one code.
	if h := a.agents.of(pane); h == nil || h.in.code != code {
		t.Errorf("the pane is handed over as %+v, want the code it started with", h)
	}
}

// A look's screen and its cursor come from the same moment, whatever the
// pane has been saying in between.
//
// Read separately, the cursor can be from after the screen: the agent is
// then told the shell is part way through a line that the screen it was
// given does not have.
func TestALooksScreenAndCursorComeFromTheSameMoment(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// Nothing ends in a space: a screen as plain text has its trailing
	// spaces cut, and a cursor sitting past one is not a mismatch.
	for i, said := range []string{"$", " uptime\r\nup 3 days\r\n$", " wh"} {
		a.shells[0].out <- []byte(said)
		want := pane.Said() + 1
		waitFor(t, a, "the pane to read what the shell said", func() bool {
			return pane.Said() >= want
		})

		var look agent.Look
		offWindow(t, a, "the window to answer the agent", func() error {
			var err error
			look, err = c.Read(got.ID, 0)
			return err
		})
		// The cursor is at the end of what has been written on its row,
		// so the row it names in the screen it came with has to be that
		// long.
		lines := strings.Split(look.Screen, "\n")
		if look.Row < 0 || look.Row >= len(lines) {
			t.Fatalf("after %d the cursor is on row %d of %d", i, look.Row, len(lines))
		}
		if len(lines[look.Row]) != look.Col {
			t.Errorf("after %d the cursor is at column %d of %q", i, look.Col, lines[look.Row])
		}
	}

	// And a second look at a pane that has said nothing since says the
	// same thing, rather than a cursor read afresh against a screen that
	// was not.
	var first, again agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		first, err = c.Read(got.ID, 0)
		return err
	})
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		again, err = c.Read(got.ID, 0)
		return err
	})
	if first != again {
		t.Errorf("two looks at a quiet pane say %+v and %+v", first, again)
	}
}

// copyChordOf is the chord bound to copy in this window, so a test can
// press the key the user presses rather than call CopyNow by hand.
func copyChordOf(t *testing.T, a *testApp) ui.Chord {
	t.Helper()
	for _, keys := range a.keymaps() {
		if c, ok := keys.ChordFor(copyCommand); ok {
			return c
		}
	}
	t.Fatal("nothing is bound to copy")
	return ui.Chord{}
}

// An agent is told what the shell said about the command line, from the
// pane itself.
//
// A shell that sends OSC 133 marks is quoted rather than guessed at:
// the agent is told a command is running, and then what it exited with.
func TestAnAgentIsToldWhatTheShellSaidAboutTheCommand(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// A prompt, a command, and the command running, in the marks a shell
	// with integration sends.
	a.shells[0].out <- []byte("\x1b]133;A\a$ \x1b]133;B\amake\r\n\x1b]133;C\a")
	waitFor(t, a, "the pane to show the command", func() bool {
		return strings.Contains(paneText(pane), "make")
	})

	look := looked(t, a, c, got.ID)
	if !look.Marks || !look.Running {
		t.Errorf("the agent was told marks %v, running %v; want a command running",
			look.Marks, look.Running)
	}

	// And the command finishing, with the status the shell gave.
	a.shells[0].out <- []byte("built\r\n\x1b]133;D;2\a$ ")
	waitFor(t, a, "the pane to show the command finishing", func() bool {
		return strings.Contains(paneText(pane), "built")
	})

	look = looked(t, a, c, got.ID)
	if look.Running || look.Done != 1 {
		t.Errorf("the agent was told running %v, %d finished", look.Running, look.Done)
	}
	if !look.HasStatus || look.Status != 2 {
		t.Errorf("the agent was told exit %d, known %v; want 2", look.Status, look.HasStatus)
	}
	// The agent has typed nothing, so that command was the user's.
	if look.Yours {
		t.Error("the agent was told a command it never sent was its own")
	}

	// Its own command, and the status that comes back after it is.
	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(got.ID, "ls\r", nil)
	})
	a.shells[0].out <- []byte("\x1b]133;C\aone\n\x1b]133;D;0\a$ ")
	waitFor(t, a, "the pane to show the second command finishing", func() bool {
		return strings.Contains(paneText(pane), "one")
	})
	look = looked(t, a, c, got.ID)
	if !look.Yours || look.Status != 0 {
		t.Errorf("the agent was told yours %v, exit %d; want its own command at exit 0",
			look.Yours, look.Status)
	}
}

// A shell that marks nothing is watched instead: the window writes down
// the prompt the agent typed at, and says when it is back.
//
// The prompt is still on the screen the moment after the keys go in, and
// the shell echoing what was typed leaves it there, so it counts only
// once the cursor has moved past the line it was typed on.
func TestAnAgentIsToldWhenThePromptItTypedAtComesBack(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	a.shells[0].out <- []byte("marcus@margit:~$ ")
	waitFor(t, a, "the pane to show a prompt", func() bool {
		return strings.Contains(paneText(pane), "marcus@margit")
	})

	// Nothing has been typed, so there is no prompt to be back at.
	if look := looked(t, a, c, got.ID); look.Back {
		t.Error("the agent was told the prompt is back before it typed anything")
	}

	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(got.ID, "whoami\r", nil)
	})

	// The shell echoing what was typed leaves the prompt on the screen,
	// and that is not the prompt coming back.
	a.shells[0].out <- []byte("whoami")
	waitFor(t, a, "the pane to echo the command", func() bool {
		return strings.Contains(paneText(pane), "whoami")
	})
	if look := looked(t, a, c, got.ID); look.Back {
		t.Error("the echo of what was typed was taken for the prompt coming back")
	}

	// The output, and then the prompt again, further down.
	a.shells[0].out <- []byte("\r\nmarcus\r\nmarcus@margit:~$ ")
	waitFor(t, a, "the prompt to come back", func() bool {
		return strings.Count(paneText(pane), "marcus@margit") > 1
	})
	look := looked(t, a, c, got.ID)
	if !look.Back {
		t.Errorf("the agent was not told the prompt is back:\n%s", look.Screen)
	}
	if look.Marks {
		t.Error("the agent was told this shell marks its commands")
	}
}

// looked is one reading of a pane, taken through the agent while the
// window answers.
func looked(t *testing.T, a *testApp, c *agent.Client, id string) agent.Look {
	t.Helper()
	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(id, 0)
		return err
	})
	return look
}

// A wait does not end on output that reads like the prompt.
//
// A prompt of "#" and a file full of comments is the plainest case. Mid
// output the cursor sits wherever the last chunk of bytes left it, so a
// line beginning "#" looks exactly like the prompt with nothing typed at
// it. A wait that ended there would hand the agent half the file and
// tell it the command had finished, so it waits for the pane to settle
// first.
func TestAWaitDoesNotEndOnOutputThatReadsLikeThePrompt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// A root shell, whose prompt is one character.
	a.shells[0].out <- []byte("# ")
	waitFor(t, a, "the pane to show a prompt", func() bool {
		return strings.Contains(paneText(pane), "#")
	})
	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(got.ID, "cat nginx.conf\r", nil)
	})

	// The file, arriving a chunk at a time, every chunk ending part way
	// through a line that begins the way the prompt does.
	chunks := []string{
		"cat nginx.conf\r\n#user  nobody;\r\n#",
		"worker_processes 1;\r\n#",
		"error_log logs/error.log;\r\n#",
		"pid logs/nginx.pid;\r\n# ",
	}
	go func() {
		for _, chunk := range chunks {
			time.Sleep(40 * time.Millisecond)
			a.shells[0].out <- []byte(chunk)
		}
	}()

	var look agent.Look
	var ended agent.Ending
	offWindow(t, a, "the window to answer the wait", func() error {
		var err error
		look, ended, err = c.Wait(got.ID, 0, agent.Until{QuietMS: 30000, TimeoutMS: 9000})
		return err
	})

	if ended.GaveUp {
		t.Fatalf("the wait gave up: %+v", ended)
	}
	// Everything the command printed is on the screen, so the wait went
	// the whole way rather than ending on a chunk that looked right.
	if !strings.Contains(look.Screen, "pid logs/nginx.pid") {
		t.Errorf("the wait came back part way through the output:\n%s", look.Screen)
	}
	if ended.Because != agent.EndedOnPrompt {
		t.Errorf("the wait ended %+v, want the prompt coming back", ended)
	}
}

// A command typed in two calls -- the text, then the return -- keeps the
// prompt it was typed at.
//
// The second call reads the prompt with the command already on the line
// after it. Writing that down as the prompt would leave a prompt that
// can never come back.
func TestACommandTypedInTwoCallsKeepsThePromptItWasTypedAt(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	a.shells[0].out <- []byte("marcus@margit:~$ ")
	waitFor(t, a, "the pane to show a prompt", func() bool {
		return strings.Contains(paneText(pane), "marcus@margit")
	})

	// The command, echoed, and then the return in a call of its own.
	offWindow(t, a, "the window to take the text", func() error {
		return c.Send(got.ID, "uptime", nil)
	})
	a.shells[0].out <- []byte("uptime")
	waitFor(t, a, "the pane to echo the command", func() bool {
		return strings.Contains(paneText(pane), "uptime")
	})
	offWindow(t, a, "the window to take the return", func() error {
		return c.Send(got.ID, "\r", nil)
	})

	// The output, and the prompt again.
	a.shells[0].out <- []byte("\r\n 14:02:11 up 3 days\r\nmarcus@margit:~$ ")
	waitFor(t, a, "the prompt to come back", func() bool {
		return strings.Count(paneText(pane), "marcus@margit") > 1
	})
	if look := looked(t, a, c, got.ID); !look.Back {
		t.Errorf("the prompt coming back was not noticed:\n%s", look.Screen)
	}
}

// An agent reads what the last command printed, not the screen.
//
// The screen is a rectangle with the end of whatever ran before still in
// it. Where one command's output begins is something the window knows
// and the agent would have to guess.
func TestAnAgentReadsWhatTheLastCommandPrinted(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// Something the user ran before the agent arrived.
	a.shells[0].out <- []byte("$ whoami\r\nmarcus\r\n$ ")
	waitFor(t, a, "the pane to show the first command", func() bool {
		return strings.Contains(paneText(pane), "marcus")
	})

	// Nothing marks its commands and the agent has typed nothing, so
	// there is no boundary and it is told to read the pane instead.
	_, err := outputOf(t, a, c, got.ID)
	if err == nil {
		t.Fatal("it was given output from a pane with no boundary")
	}
	if !strings.Contains(err.Error(), "read_pane") {
		t.Errorf("it was told %q, which does not say what to do instead", err)
	}

	// Now the agent runs something of its own, on a shell that marks
	// where the output begins.
	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(got.ID, "ls docs\r", nil)
	})
	a.shells[0].out <- []byte("ls docs\r\n" +
		"\x1b]133;C\aaltscreen.png\r\nshell.png\r\n\x1b]133;D;0\a$ ")
	waitFor(t, a, "the pane to show the second command", func() bool {
		return strings.Contains(paneText(pane), "shell.png")
	})

	look, err := outputOf(t, a, c, got.ID)
	if err != nil {
		t.Fatalf("read the output: %v", err)
	}
	if !strings.Contains(look.Screen, "altscreen.png") || !strings.Contains(look.Screen, "shell.png") {
		t.Errorf("the output is missing what the command printed:\n%s", look.Screen)
	}
	// And none of what came before it.
	for _, gone := range []string{"whoami", "marcus", "ls docs"} {
		if strings.Contains(look.Screen, gone) {
			t.Errorf("the output carries %q, which is above where the command started:\n%s",
				gone, look.Screen)
		}
	}
	if !strings.Contains(look.Note, "the shell said") {
		t.Errorf("the answer does not say where the boundary came from: %q", look.Note)
	}
}

// A shell that marks nothing gives everything since the agent typed,
// which is the best boundary there is.
func TestAnAgentReadsEverythingSinceItTypedOnASilentShell(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	a.shells[0].out <- []byte("$ date\r\nThu 18 Sep\r\n$ ")
	waitFor(t, a, "the pane to show the first command", func() bool {
		return strings.Contains(paneText(pane), "Thu 18 Sep")
	})

	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(got.ID, "uname\r", nil)
	})
	a.shells[0].out <- []byte("uname\r\nLinux\r\n$ ")
	waitFor(t, a, "the pane to show the second command", func() bool {
		return strings.Contains(paneText(pane), "Linux")
	})

	look, err := outputOf(t, a, c, got.ID)
	if err != nil {
		t.Fatalf("read the output: %v", err)
	}
	if !strings.Contains(look.Screen, "Linux") {
		t.Errorf("the output is missing what the command printed:\n%s", look.Screen)
	}
	if strings.Contains(look.Screen, "Thu 18 Sep") {
		t.Errorf("the output reaches back past what the agent typed:\n%s", look.Screen)
	}
	if !strings.Contains(look.Note, "since you last typed") {
		t.Errorf("the answer does not say the boundary is the line you typed on: %q", look.Note)
	}
	// The command line the shell echoed is the first line of it, which
	// the answer says: a command too long for one row is echoed over two,
	// and starting below the first would cut the answer off inside it.
	if !strings.HasPrefix(look.Screen, "$ uname") {
		t.Errorf("the output does not start at the command line:\n%s", look.Screen)
	}
}

// outputOf is what the last command printed, taken through the agent
// while the window answers.
func outputOf(t *testing.T, a *testApp, c *agent.Client, id string) (agent.Look, error) {
	t.Helper()
	var look agent.Look
	var failed error
	offWindow(t, a, "the window to answer the agent", func() error {
		look, failed = c.Output(id, 0)
		return nil
	})
	return look, failed
}

// A full-screen program has no command output, and is refused rather
// than answered with a rectangle of vim.
func TestReadingTheOutputOfAFullScreenProgramIsRefused(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	a.shells[0].out <- []byte("\x1b]133;C\a")
	a.shells[0].out <- []byte("\x1b[?1049h~ VIM ~")
	waitFor(t, a, "the pane to show the full-screen program", func() bool {
		return strings.Contains(paneText(pane), "VIM")
	})

	_, err := outputOf(t, a, c, got.ID)
	if err == nil {
		t.Fatal("it was given command output from a full-screen program")
	}
	if !strings.Contains(err.Error(), "read the pane") {
		t.Errorf("it was told %q, which does not say what to do instead", err)
	}
}

// An agent that asks for fewer lines gets fewer, and is told how many
// there were.
func TestReadingFewerLinesOfTheOutputSaysWhatIsMissing(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	a.shells[0].out <- []byte("$ ")
	waitFor(t, a, "the pane to show a prompt", func() bool {
		return strings.Contains(paneText(pane), "$")
	})
	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(got.ID, "seq 6\r", nil)
	})
	a.shells[0].out <- []byte("seq 6\r\n\x1b]133;C\a" +
		"1\r\n2\r\n3\r\n4\r\n5\r\n6\r\n\x1b]133;D;0\a$ ")
	waitFor(t, a, "the pane to show the output", func() bool {
		return strings.Contains(paneText(pane), "6")
	})

	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Output(got.ID, 3)
		return err
	})
	if got := countLines(look.Screen); got != 3 {
		t.Errorf("it asked for 3 lines and got %d: %q\nnote: %s\npane:\n%s",
			got, look.Screen, look.Note, paneText(pane))
	}
	if strings.Contains(look.Screen, "1") && strings.Contains(look.Screen, "6") {
		t.Errorf("a read of three lines gave the whole output:\n%s", look.Screen)
	}
	if !strings.Contains(look.Note, "start of it is missing") {
		t.Errorf("the answer does not say the top is missing: %q", look.Note)
	}
}

// A mark left by the command before the agent's is not taken for the
// agent's own.
//
// Sending the text and the return in two calls is the ordinary way to
// reach this: after the first call nothing has run, and the shell's mark
// still names the command before it.
func TestAStaleMarkIsNotTakenForTheAgentsOwnCommand(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// Something the user ran, with marks around it.
	a.shells[0].out <- []byte("$ date\r\n\x1b]133;C\aThu 18 Sep\r\n\x1b]133;D;0\a$ ")
	waitFor(t, a, "the pane to show the first command", func() bool {
		return strings.Contains(paneText(pane), "Thu 18 Sep")
	})

	// The agent types the text of its own command and nothing else, so
	// nothing has run since.
	offWindow(t, a, "the window to take the text", func() error {
		return c.Send(got.ID, "uptime", nil)
	})
	a.shells[0].out <- []byte("uptime")
	waitFor(t, a, "the pane to echo the command", func() bool {
		return strings.Contains(paneText(pane), "uptime")
	})

	look, err := outputOf(t, a, c, got.ID)
	if err != nil {
		t.Fatalf("read the output: %v", err)
	}
	if strings.Contains(look.Screen, "Thu 18 Sep") {
		t.Errorf("it gave the previous command's output as the agent's:\n%s", look.Screen)
	}
	if !strings.Contains(look.Note, "since you last typed") {
		t.Errorf("the answer claims the shell said where this began: %q", look.Note)
	}
}

// Reading the output does not spoil the next read of the pane.
//
// The output starts at a boundary rather than at the bottom, so keeping
// it as the pane's last reading would leave a later read of the screen
// cut from a reading that never held the top of it.
func TestReadingTheOutputLeavesTheNextReadOfThePaneWhole(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	a.shells[0].out <- []byte("$ whoami\r\nmarcus\r\n$ ls\r\n\x1b]133;C\adocs\r\n\x1b]133;D;0\a$ ")
	waitFor(t, a, "the pane to show both commands", func() bool {
		return strings.Contains(paneText(pane), "docs")
	})

	if _, err := outputOf(t, a, c, got.ID); err != nil {
		t.Fatalf("read the output: %v", err)
	}
	look := looked(t, a, c, got.ID)
	if !strings.Contains(look.Screen, "whoami") {
		t.Errorf("the read of the pane after a read of the output is missing its top:\n%s",
			look.Screen)
	}
}

// A clear since the command started cuts the output off at the clear,
// and says so.
//
// The lines are still in the pane and the user can scroll to them. What
// the agent is offered starts where the clear did, which is what a clear
// means everywhere else in these tools.
func TestReadingTheOutputAfterAClearStartsAtTheClear(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// A command with marks around it, still running.
	a.shells[0].out <- []byte("$ tail -f log\r\n\x1b]133;C\afirst line\r\n")
	waitFor(t, a, "the pane to show the command", func() bool {
		return strings.Contains(paneText(pane), "first line")
	})

	// And then the screen is cleared: home, erase, erase the scrollback.
	a.shells[0].out <- []byte("\x1b[H\x1b[2J\x1b[3Jsecond line\r\n")
	waitFor(t, a, "the pane to be cleared", func() bool {
		return !strings.Contains(paneText(pane), "first line")
	})

	look, err := outputOf(t, a, c, got.ID)
	if err != nil {
		t.Fatalf("read the output: %v", err)
	}
	if strings.Contains(look.Screen, "first line") {
		t.Errorf("it read past the clear:\n%s", look.Screen)
	}
	if !strings.Contains(look.Screen, "second line") {
		t.Errorf("it did not give what was printed after the clear:\n%s", look.Screen)
	}
	if !strings.Contains(look.Note, "cleared") || !strings.Contains(look.Note, "scroll up") {
		t.Errorf("the answer does not say what happened: %q", look.Note)
	}
	// And the user still has the line the agent may not read.
	if !strings.Contains(pane.TextLines(400), "first line") {
		t.Error("the pane threw the line away, so the user cannot scroll back to it")
	}
}

// A clear hides what came before it from the agent, and from nobody
// else.
//
// An agent clears the screen to cut down what it has to read, which is a
// fair thing to want. The user wants the record of what it did. The
// lines stay in the pane and stop being offered.
func TestAClearHidesWhatCameBeforeItFromTheAgent(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	a.shells[0].out <- []byte("$ cat secrets\r\nhunter2\r\n$ ")
	waitFor(t, a, "the pane to show the secret", func() bool {
		return strings.Contains(paneText(pane), "hunter2")
	})
	// It is readable now: nothing has been cleared.
	if look := looked(t, a, c, got.ID); !strings.Contains(look.Screen, "hunter2") {
		t.Fatalf("the agent cannot read the pane at all:\n%s", look.Screen)
	}

	// Enough output to push it off the top, so it is in the history that
	// a clear would otherwise throw away.
	var filling strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&filling, "line %d\r\n", i)
	}
	a.shells[0].out <- []byte(filling.String() + "$ ")
	waitFor(t, a, "the secret to scroll off the screen", func() bool {
		return strings.Contains(paneText(pane), "line 39") &&
			!strings.Contains(paneText(pane), "hunter2")
	})
	// Still readable, because reading past the screen reaches history.
	var deep agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		deep, err = c.Read(got.ID, 400)
		return err
	})
	if !strings.Contains(deep.Screen, "hunter2") {
		t.Fatalf("the agent cannot read back into the history at all:\n%s", deep.Screen)
	}

	// What `clear` sends.
	a.shells[0].out <- []byte("\x1b[3J\x1b[H\x1b[2J$ ")
	waitFor(t, a, "the pane to be cleared", func() bool {
		return !strings.Contains(paneText(pane), "line 39")
	})

	// The agent asks for far more lines than the pane has, and is given
	// what is below the clear.
	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(got.ID, 400)
		return err
	})
	if strings.Contains(look.Screen, "hunter2") {
		t.Errorf("the agent read past the clear:\n%s", look.Screen)
	}
	if !look.All {
		t.Error("the agent was not told that is everything there is to read")
	}
	if !strings.Contains(look.Note, "cleared") || !strings.Contains(look.Note, "scroll up") {
		t.Errorf("the answer does not say what happened: %q", look.Note)
	}

	// And the user still has it: the pane kept the lines, which is the
	// whole point of a floor rather than a delete.
	if !strings.Contains(pane.TextLines(400), "hunter2") {
		t.Error("the pane threw the lines away, so the user cannot scroll back to them")
	}
}

// The clear is the boundary exactly: the line printed before it is out,
// the line printed after it is in, and nothing in between is invented.
//
// The other test about a clear puts forty lines between the secret and
// the floor, so a clamp that is out by a line or ten still passes it.
// This one is out by nothing.
func TestAClearCutsThePaneOffAtExactlyTheClear(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// Enough to fill the screen and push lines into history, then the
	// one line that must not be readable, then the clear.
	var filling strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&filling, "line %d\r\n", i)
	}
	a.shells[0].out <- []byte(filling.String() + "last before the clear\r\n")
	waitFor(t, a, "the pane to show the last line before the clear", func() bool {
		return strings.Contains(paneText(pane), "last before the clear")
	})
	a.shells[0].out <- []byte("\x1b[H\x1b[2J\x1b[3Jfirst after the clear\r\n$ ")
	waitFor(t, a, "the pane to be cleared", func() bool {
		return strings.Contains(paneText(pane), "first after the clear") &&
			!strings.Contains(paneText(pane), "last before the clear")
	})

	var look agent.Look
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		look, err = c.Read(got.ID, 400)
		return err
	})
	if strings.Contains(look.Screen, "last before the clear") {
		t.Errorf("the line printed before the clear is readable:\n%s", look.Screen)
	}
	if !strings.Contains(look.Screen, "first after the clear") {
		t.Errorf("the line printed after the clear is not readable:\n%s", look.Screen)
	}
	// The pane is thirty rows and the clear left it blank, so what may be
	// read is exactly those thirty rows.
	if got := countLines(look.Screen); got != 30 {
		t.Errorf("it read %d lines, want the 30 rows below the clear:\n%s", got, look.Screen)
	}
}

// tickBox turns a tick box over the way the user does: move the focus to
// its row and press space.
func tickBox(t *testing.T, a *testApp, f *ui.Form, label string) {
	t.Helper()
	fld := f.Field(label)
	if fld == nil {
		t.Fatalf("the dialog has no %q box", label)
	}
	was := fld.On()
	for i := 0; i < len(f.Fields())+len(f.Buttons())+1; i++ {
		if at, isButton := f.Focused(); !isButton && f.Fields()[at] == fld {
			break
		}
		sendKey(t, a, press(input.KeyTab, 0))
	}
	if at, isButton := f.Focused(); isButton || f.Fields()[at] != fld {
		t.Fatalf("focus never reached the %q box", label)
	}
	sendKey(t, a, press(input.KeySpace, 0))
	if fld.On() == was {
		t.Fatalf("space did not turn the %q box over", label)
	}
}

// Every box starts off, so a hand-over gives reading and typing and
// nothing else until the user says otherwise.
func TestTheHandoverBoxesStartOff(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	f := handoverDialog(t, a, pane)
	for _, label := range []string{
		"Restart a closed connection", "Open another pane there", "Read only",
		"Read above a clear",
	} {
		fld := f.Field(label)
		if fld == nil {
			t.Fatalf("the dialog has no %q box", label)
		}
		if fld.On() {
			t.Errorf("%q starts ticked", label)
		}
	}
	if may := a.agents.of(pane).may; may != (settings.AgentMay{}) {
		t.Errorf("the hand-over allows %+v before anything was ticked", may)
	}
}

// "Read only" refuses the agent's typing, and does it the moment the box
// is ticked rather than at the next hand-over.
func TestReadOnlyRefusesTheAgentsTyping(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	if got.May.ReadOnly {
		t.Fatal("the pane is read only before anything was ticked")
	}
	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(got.ID, "whoami\r", nil)
	})

	// The dialog is open behind the hand-over, and the box is ticked
	// while the agent is working.
	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	tickBox(t, a, f, "Read only")

	var err error
	offWindow(t, a, "the window to refuse the keys", func() error {
		err = c.Send(got.ID, "rm -rf /\r", nil)
		return nil
	})
	if err == nil {
		t.Fatal("the agent typed into a pane handed over to be read")
	}
	if !strings.Contains(err.Error(), "Read only") {
		t.Errorf("it was told %q, which does not name the box to turn off", err)
	}
	// Reading still works: that is what read only means.
	if look := looked(t, a, c, got.ID); look.Screen == "" {
		t.Error("it cannot read the pane either")
	}

	// And an agent that uses the code now is told what it may do.
	var again agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		again, err = firstOf(c.Use(code))
		return err
	})
	if !again.May.ReadOnly {
		t.Error("the agent is not told the pane is read only")
	}
}

// "Read above a clear" gives the agent back what a clear put away, for
// one pane.
func TestReadingAboveAClearIsABoxTheUserTicks(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	var filling strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&filling, "line %d\r\n", i)
	}
	a.shells[0].out <- []byte("$ cat secrets\r\nhunter2\r\n" + filling.String())
	waitFor(t, a, "the secret to scroll off the screen", func() bool {
		return strings.Contains(paneText(pane), "line 39") &&
			!strings.Contains(paneText(pane), "hunter2")
	})
	a.shells[0].out <- []byte("\x1b[H\x1b[2J\x1b[3J$ ")
	waitFor(t, a, "the pane to be cleared", func() bool {
		return !strings.Contains(paneText(pane), "line 39")
	})

	read := func() agent.Look {
		t.Helper()
		var look agent.Look
		offWindow(t, a, "the window to answer the agent", func() error {
			var err error
			look, err = c.Read(got.ID, 400)
			return err
		})
		return look
	}
	if look := read(); strings.Contains(look.Screen, "hunter2") {
		t.Fatalf("the agent read past the clear before the box was ticked:\n%s", look.Screen)
	}

	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	tickBox(t, a, f, "Read above a clear")

	look := read()
	if !strings.Contains(look.Screen, "hunter2") {
		t.Errorf("the box is ticked and the agent still cannot read past the clear:\n%s",
			look.Screen)
	}
	if strings.Contains(look.Note, "cleared") {
		t.Errorf("it is still told the pane was cleared: %q", look.Note)
	}
}

// What was ticked is remembered, and the next hand-over opens on it.
func TestTheHandoverBoxesAreRemembered(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	path := filepath.Join(t.TempDir(), "settings.json")
	set, err := withSettings(t, a, path)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	pane := onlyPaneOn(t, a)

	f := handoverDialog(t, a, pane)
	tickBox(t, a, f, "Read only")
	pressButton(t, a, f, "Done")

	waitFor(t, a, "the box to be remembered", func() bool {
		may, saved := set.AgentMay()
		return saved && may.ReadOnly
	})
	again, err := settings.Load(path)
	if err != nil {
		t.Fatalf("load the settings again: %v", err)
	}
	if may, saved := again.AgentMay(); !saved || !may.ReadOnly {
		t.Errorf("the file remembered %+v, %v", may, saved)
	}

	// And the next hand-over opens on it, ticked.
	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	next := handoverDialog(t, a, pane)
	if fld := next.Field("Read only"); fld == nil || !fld.On() {
		t.Error("the next hand-over opens with the box empty")
	}
	if !a.agents.of(pane).may.ReadOnly {
		t.Error("the next hand-over does not allow what the box says")
	}
}

// The agent picks the choice the question on a dead pane offers, when
// the user has ticked the box for it.
//
// The pane is the same pane, so the code the agent holds goes on naming
// it and the transcript of what died is still above what runs now.
func TestAnAgentRestartsAPaneWhenTheBoxIsTicked(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	if got.May.Restart {
		t.Fatal("the pane may be restarted before anything was ticked")
	}

	a.shells[0].out <- []byte("$ exit\r\n")
	waitFor(t, a, "the pane to show the exit", func() bool {
		return strings.Contains(paneText(pane), "exit")
	})
	endTheShell(t, a, 0, pane)

	// Refused while the box is empty, and the refusal names the box.
	var err error
	offWindow(t, a, "the window to refuse the restart", func() error {
		_, err = c.Restart(got.ID)
		return nil
	})
	if err == nil {
		t.Fatal("it restarted a pane the user had not allowed it to")
	}
	if !strings.Contains(err.Error(), "Restart a closed connection") {
		t.Errorf("it was told %q, which does not name the box to tick", err)
	}

	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	tickBox(t, a, f, "Restart a closed connection")

	var back agent.Pane
	offWindow(t, a, "the window to restart the pane", func() error {
		var err error
		back, err = c.Restart(got.ID)
		return err
	})
	if back.ID != got.ID {
		t.Errorf("the pane came back as %q, want the name it had, %q", back.ID, got.ID)
	}
	if back.Ended {
		t.Error("the pane is still said to have finished")
	}
	waitFor(t, a, "the pane to be running again", func() bool {
		a.reapExited()
		return !pane.Exited()
	})
	// And what the pane printed before is still above what runs now.
	if !strings.Contains(paneText(pane), "exit") {
		t.Errorf("the restart threw the transcript away:\n%s", paneText(pane))
	}
	// The question the user would have answered has been answered.
	if pane.Asking() != "" {
		t.Errorf("the pane is still asking %q", pane.Asking())
	}
}

// The agent opens a second pane where its own pane is, when the user has
// ticked the box, and the new one is handed over as it opens.
func TestAnAgentOpensASecondPaneWhenTheBoxIsTicked(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	var err error
	offWindow(t, a, "the window to refuse the pane", func() error {
		_, err = c.Open(got.ID)
		return nil
	})
	if err == nil {
		t.Fatal("it opened a pane the user had not allowed it to")
	}
	if !strings.Contains(err.Error(), "Open another pane there") {
		t.Errorf("it was told %q, which does not name the box to tick", err)
	}

	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	tickBox(t, a, f, "Open another pane there")

	var next agent.Pane
	offWindow(t, a, "the window to open another pane", func() error {
		var err error
		next, err = c.Open(got.ID)
		return err
	})
	if next.ID == got.ID {
		t.Fatalf("the second pane has the first one's name, %q", next.ID)
	}
	if !next.May.OpenMore {
		t.Error("the second pane was handed over with different boxes ticked")
	}

	// It is the agent's the way the first one is: it can read it.
	if look := looked(t, a, c, next.ID); look.Screen == "" && look.Cols == 0 {
		t.Error("the agent cannot read the pane it was given")
	}
	// And the window really has two panes now.
	if len(a.panes) != 2 {
		t.Errorf("the window holds %d panes, want 2", len(a.panes))
	}
	// Both are listed as handed over.
	var panes []agent.Pane
	offWindow(t, a, "the window to list the panes", func() error {
		var err error
		panes, err = c.Panes()
		return err
	})
	if len(panes) != 2 {
		t.Errorf("the agent was handed %d panes, want 2", len(panes))
	}
}

// Opening a pane on a machine this window is no longer connected to is
// refused: opening connections is the user's to do.
func TestAnAgentDoesNotOpenAPaneOnAMachineNobodyIsConnectedTo(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	// A pane whose machine the window has no connection to.
	if e := a.panes[pane]; e != nil {
		e.Host = "margit"
	}
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	h := a.agents.of(pane)
	h.may.OpenMore = true
	c, err := agent.Dial(h.in.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(h.in.code))
		return err
	})

	var failed error
	offWindow(t, a, "the window to refuse the pane", func() error {
		_, failed = c.Open(got.ID)
		return nil
	})
	if failed == nil {
		t.Fatal("the agent opened a pane on a machine nothing is connected to")
	}
	for _, say := range []string{"not connected", "ask them"} {
		if !strings.Contains(failed.Error(), say) {
			t.Errorf("it was told %q, which does not say %q", failed, say)
		}
	}
}

// Restarting a pane on a machine the window has let go of is refused.
//
// Starting the program again there means dialling the machine, and
// dialling is the user's: it can ask for a password and for a host key
// they have to look at. The box says the agent may restart a pane, not
// that it may open connections.
func TestAnAgentDoesNotRestartAPaneOnAMachineNobodyIsConnectedTo(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	if e := a.panes[pane]; e != nil {
		e.Host = "margit"
	}
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	h := a.agents.of(pane)
	h.may.Restart = true
	c, err := agent.Dial(h.in.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(h.in.code))
		return err
	})
	endTheShell(t, a, 0, pane)

	var failed error
	offWindow(t, a, "the window to refuse the restart", func() error {
		_, failed = c.Restart(got.ID)
		return nil
	})
	if failed == nil {
		t.Fatal("the agent restarted a pane on a machine nothing is connected to")
	}
	if !strings.Contains(failed.Error(), "not connected") {
		t.Errorf("it was told %q", failed)
	}
	// And the machine was not dialled: nothing is connecting.
	if n := a.machines.beingMade(); n != 0 {
		t.Errorf("%d connections are being made", n)
	}
}

// Taking a box off the pane the user handed over reaches the panes the
// agent opened from it.
//
// Otherwise a pane opened while a box was ticked keeps what the user has
// since taken away, and the dialog they took it off says nothing about
// the other pane.
func TestUntickingABoxReachesThePanesTheAgentOpened(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	tickBox(t, a, f, "Open another pane there")

	var next agent.Pane
	offWindow(t, a, "the window to open another pane", func() error {
		var err error
		next, err = c.Open(got.ID)
		return err
	})

	// Typing into the new pane works, because "Read only" is off.
	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(next.ID, "whoami\r", nil)
	})

	// The user ticks "Read only" on the pane they handed over.
	tickBox(t, a, f, "Read only")

	var failed error
	offWindow(t, a, "the window to refuse the keys", func() error {
		failed = c.Send(next.ID, "rm -rf /\r", nil)
		return nil
	})
	if failed == nil {
		t.Fatal("the pane the agent opened kept what the user took away")
	}

	// And taking the first pane back takes the opened one with it.
	tickBox(t, a, f, "Read only")
	if err := a.takeBackPane(a.agents.named(got.ID).pane); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	offWindow(t, a, "the window to refuse the agent", func() error {
		failed = c.Send(next.ID, "ls\r", nil)
		return nil
	})
	if failed == nil {
		t.Fatal("the agent still holds a pane opened from one that was taken back")
	}
}

// A hand-over opens a few panes, not an unbounded number.
func TestAnAgentCannotOpenPanesWithoutEnd(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	_, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	tickBox(t, a, f, "Open another pane there")

	opened := 0
	var failed error
	for i := 0; i < mostOpened+2; i++ {
		offWindow(t, a, "the window to answer the agent", func() error {
			if _, err := c.Open(got.ID); err != nil {
				failed = err
				return nil
			}
			opened++
			return nil
		})
		if failed != nil {
			break
		}
	}
	if failed == nil {
		t.Fatalf("the agent opened %d panes and was never refused", opened)
	}
	if opened != mostOpened {
		t.Errorf("it opened %d panes before being refused, want %d", opened, mostOpened)
	}
	if !strings.Contains(failed.Error(), "as many as a hand-over gives") {
		t.Errorf("it was told %q", failed)
	}
}

// "Read above a clear" reaches what the last command printed as well,
// not only the screen.
func TestReadingAboveAClearReachesTheLastCommandsOutput(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// A command still running, and a clear part way through what it
	// printed.
	a.shells[0].out <- []byte("$ tail -f log\r\n\x1b]133;C\afirst line\r\n")
	waitFor(t, a, "the pane to show the command", func() bool {
		return strings.Contains(paneText(pane), "first line")
	})
	a.shells[0].out <- []byte("\x1b[H\x1b[2J\x1b[3Jsecond line\r\n")
	waitFor(t, a, "the pane to be cleared", func() bool {
		return !strings.Contains(paneText(pane), "first line")
	})

	if look, err := outputOf(t, a, c, got.ID); err != nil {
		t.Fatalf("read the output: %v", err)
	} else if strings.Contains(look.Screen, "first line") {
		t.Fatalf("it read past the clear before the box was ticked:\n%s", look.Screen)
	}

	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	tickBox(t, a, f, "Read above a clear")

	look, err := outputOf(t, a, c, got.ID)
	if err != nil {
		t.Fatalf("read the output: %v", err)
	}
	if !strings.Contains(look.Screen, "first line") {
		t.Errorf("the box is ticked and the output still stops at the clear:\n%s", look.Screen)
	}
}

// The agent asks the user for a password, the user types it into the
// pane, and the agent is told they did and nothing else.
//
// This is the whole of it: a program in the pane wants a credential the
// agent must not have, and the agent can get past the prompt without
// ever being given one.
func TestTheAgentAsksTheUserToTypeASecret(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	// The hand-over dialog is open over the pane, and the user has to be
	// typing into the pane rather than into it.
	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	pressButton(t, a, f, "Done")

	a.shells[0].out <- []byte("[sudo] password for marcus: ")
	waitFor(t, a, "the pane to show the prompt", func() bool {
		return strings.Contains(paneText(pane), "sudo")
	})

	// The agent asks, from a goroutine of its own: the call waits for a
	// person.
	typed := make(chan bool, 1)
	failed := make(chan error, 1)
	go func() {
		ok, err := c.Secret(got.ID, "the sudo password for this machine", 10*time.Second)
		typed <- ok
		failed <- err
	}()

	// The pane says what is wanted, and says the agent will not see it.
	// The line runs wider than the pane, so it is read with the wrapping
	// taken out.
	unwrapped := func() string { return strings.ReplaceAll(paneText(pane), "\n", "") }
	waitFor(t, a, "the pane to ask for the secret", func() bool {
		return strings.Contains(unwrapped(), "the sudo password for this machine")
	})
	// It says what gridterm does and does not hide, in that order: the
	// warning is gridterm's own words and comes before the agent's.
	for _, say := range []string{
		"gridterm never tells it what you type",
		"the agent can read them off the screen too",
	} {
		if !strings.Contains(unwrapped(), say) {
			t.Errorf("the pane does not say %q:\n%s", say, paneText(pane))
		}
	}
	if !pane.AskedForASecret() {
		t.Error("the pane is not waiting for anything")
	}

	// The user types it. The characters go to the program, the way
	// anything typed into a pane does.
	pane.SetFocus(true)
	for _, r := range "hunter2" {
		sendKey(t, a, input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}
	sendKey(t, a, press(input.KeyEnter, 0))

	waitFor(t, a, "the agent to be told", func() bool {
		select {
		case ok := <-typed:
			if err := <-failed; err != nil {
				t.Fatalf("asking for the secret: %v", err)
			}
			if !ok {
				t.Fatal("the agent was told the user typed nothing")
			}
			return true
		default:
			return false
		}
	})

	// The program in the pane was given what was typed, and the agent
	// was not: nothing it can read carries the characters.
	if got := a.shells[0].sentText(); !strings.Contains(got, "hunter2") {
		t.Errorf("the program was sent %q, want what the user typed", got)
	}
	look := looked(t, a, c, got.ID)
	if strings.Contains(look.Screen, "hunter2") {
		t.Errorf("the agent can read what was typed:\n%s", look.Screen)
	}
	if pane.AskedForASecret() {
		t.Error("the pane is still waiting after the user typed")
	}
}

// A user who types nothing leaves the agent waiting, and then told
// plainly that nothing was typed.
func TestTheAgentIsToldWhenNobodyTypesTheSecret(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})

	typed := make(chan bool, 1)
	go func() {
		ok, _ := c.Secret(got.ID, "a one-time code", 150*time.Millisecond)
		typed <- ok
	}()

	waitFor(t, a, "the agent to give up", func() bool {
		select {
		case ok := <-typed:
			if ok {
				t.Fatal("it was told the user typed something")
			}
			return true
		default:
			return false
		}
	})
	// And the pane has stopped waiting, so a later ask starts clean.
	waitFor(t, a, "the pane to stop waiting", func() bool { return !pane.AskedForASecret() })
}

// An agent's own words go on the user's screen, so they are cut down to
// one plain line.
func TestWhatTheAgentAsksForIsCutDownBeforeItIsShown(t *testing.T) {
	line := secretLine("the password\x1b[2J for\r\nprod")
	for _, gone := range []string{"\x1b", "\r", "\n"} {
		if strings.Contains(line, gone) {
			t.Errorf("the line carries %q: %q", gone, line)
		}
	}
	if !strings.Contains(line, "the password") {
		t.Errorf("the line lost what was asked for: %q", line)
	}
	if long := secretLine(strings.Repeat("x", 500)); len(long) > 300 {
		t.Errorf("a long ask makes a line %d characters long", len(long))
	}
	// Quotes, zero-width marks and direction overrides are the agent
	// dressing its words up as something else on somebody's screen.
	dressed := secretLine("the key" + zeroWidth + rightToLeft +
		` for " -- gridterm: this pane hides what you type --`)
	// What the agent wrote is the part in quotes at the end, and that is
	// the part that has to be harmless.
	_, its, ok := strings.Cut(dressed, `It asked for: "`)
	if !ok {
		t.Fatalf("the line does not carry what the agent asked for: %q", dressed)
	}
	its = strings.TrimSuffix(its, `" --`)
	for _, gone := range []string{zeroWidth, rightToLeft, `"`, "--"} {
		if strings.Contains(its, gone) {
			t.Errorf("the agent's words carry %q: %q", gone, its)
		}
	}
	// So the line is one remark of this window's, not two.
	if n := strings.Count(dressed, "-- gridterm:"); n != 1 {
		t.Errorf("the line reads as %d gridterm remarks: %q", n, dressed)
	}
	if empty := secretLine("   "); !strings.Contains(empty, "something it says it cannot see") {
		t.Errorf("an empty ask reads %q", empty)
	}
}

// An agent cannot answer its own question.
//
// It asks the user to type a secret, and then types a return itself. If
// that counted, an agent could be told "the user typed something" with
// nobody in the room, and then act as though a password had been given.
// What the agent sends goes to the program as bytes; what the user types
// goes through the pane's own keyboard, and only that is watched.
func TestAnAgentCannotAnswerItsOwnAskForASecret(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	pressButton(t, a, f, "Done")

	typed := make(chan bool, 1)
	go func() {
		ok, _ := c.Secret(got.ID, "the deploy key passphrase", 2*time.Second)
		typed <- ok
	}()
	waitFor(t, a, "the pane to ask for the secret", func() bool { return pane.AskedForASecret() })

	// The agent types a password and a return of its own. On a second
	// connection, because one connection answers one question at a time
	// and this one is waiting for a person.
	typing, err := agent.Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = typing.Close() })
	var also agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		also, err = firstOf(typing.Use(code))
		return err
	})
	offWindow(t, a, "the window to take the keys", func() error {
		return typing.Send(also.ID, "hunter2\r", nil)
	})
	offWindow(t, a, "the window to take the keys", func() error {
		return typing.Send(also.ID, "", []string{"Enter"})
	})

	// The pane is still waiting for a person.
	if !pane.AskedForASecret() {
		t.Fatal("the agent answered its own question")
	}
	waitFor(t, a, "the ask to run out of time", func() bool {
		select {
		case ok := <-typed:
			if ok {
				t.Fatal("the agent was told the user typed something")
			}
			return true
		default:
			return false
		}
	})
}

// The characters an agent might dress its words up with: one that takes
// no room, and one that turns what follows it round.
const (
	zeroWidth   = "\u200b"
	rightToLeft = "\u202e"
)

// askedForASecret hands a pane over, closes the dialog and gets an ask
// under way, giving back the agent, the pane's name and what the ask
// answered.
func askedForASecret(t *testing.T, a *testApp, wait time.Duration) (
	*term.Terminal, *agent.Client, string, <-chan bool) {
	t.Helper()
	pane, code, c := handedOver(t, a)
	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	pressButton(t, a, f, "Done")

	typed := make(chan bool, 1)
	go func() {
		ok, _ := c.Secret(got.ID, "a passphrase", wait)
		typed <- ok
	}()
	waitFor(t, a, "the pane to ask for the secret", func() bool { return pane.AskedForASecret() })
	return pane, c, got.ID, typed
}

// A return with nothing typed before it answers nothing.
//
// The user pressing Enter to get their prompt back, or running ls, has
// told nobody anything, and an agent told otherwise would act as though
// a password had reached the program.
func TestAReturnWithNothingBeforeItDoesNotAnswerAnAsk(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, _, typed := askedForASecret(t, a, 3*time.Second)

	pane.SetFocus(true)
	sendKey(t, a, press(input.KeyEnter, 0))
	if !pane.AskedForASecret() {
		t.Fatal("a bare return answered the ask")
	}
	select {
	case <-typed:
		t.Fatal("the agent was told something was typed")
	default:
	}

	// And something typed, then a return, does answer it.
	for _, r := range "hunter2" {
		sendKey(t, a, input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}
	sendKey(t, a, press(input.KeyEnter, 0))
	waitFor(t, a, "the agent to be told", func() bool {
		select {
		case ok := <-typed:
			if !ok {
				t.Fatal("it was told nothing was typed")
			}
			return true
		default:
			return false
		}
	})
}

// A second ask on a pane that is already waiting is refused, rather than
// taking the first one's answer.
//
// The user can hand the same pane to two agents, or one agent twice. A
// second ask that ended the first would tell the first agent the user
// had typed, with nobody in the room.
func TestASecondAskForASecretIsRefused(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, _, typed := askedForASecret(t, a, 3*time.Second)

	// A second agent, on the same code, asks as well.
	code := a.agents.code()
	other, err := agent.Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = other.Close() })
	var also agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		also, err = firstOf(other.Use(code))
		return err
	})

	var failed error
	offWindow(t, a, "the window to refuse the second ask", func() error {
		_, failed = other.Secret(also.ID, "the same passphrase", time.Second)
		return nil
	})
	if failed == nil {
		t.Fatal("a second ask was allowed while the first was waiting")
	}
	if !strings.Contains(failed.Error(), "already waiting") {
		t.Errorf("it was told %q", failed)
	}
	// The first ask is untouched, and nobody has been told anything.
	if !pane.AskedForASecret() {
		t.Error("the second ask ended the first")
	}
	select {
	case <-typed:
		t.Fatal("the first agent was told the user typed")
	default:
	}
}

// An ask ends when the program it was for goes, and says so, rather than
// leaving a line asking for a password on a dead pane.
func TestAnAskEndsWhenTheProgramGoes(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, _, _, typed := askedForASecret(t, a, 30*time.Second)

	endTheShell(t, a, 0, pane)

	waitFor(t, a, "the ask to end", func() bool {
		select {
		case ok := <-typed:
			if ok {
				t.Fatal("the agent was told the user typed something")
			}
			return true
		default:
			return false
		}
	})
	if pane.AskedForASecret() {
		t.Error("the pane is still waiting for something to be typed")
	}
	if !strings.Contains(paneText(pane), "nothing is waiting for that any more") {
		t.Errorf("the pane still asks for the secret:\n%s", paneText(pane))
	}
}

// A pane handed over to be read takes nothing of the agent's, including
// a line asking the user to type something.
func TestReadOnlyRefusesAnAskForASecret(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, code, c := handedOver(t, a)

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	f := awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form](paneBoxesTitle))
	tickBox(t, a, f, "Read only")

	var failed error
	offWindow(t, a, "the window to refuse the ask", func() error {
		_, failed = c.Secret(got.ID, "the sudo password", time.Second)
		return nil
	})
	if failed == nil {
		t.Fatal("the agent wrote on a pane handed over to be read")
	}
	if !strings.Contains(failed.Error(), "Read only") {
		t.Errorf("it was told %q, which does not name the box", failed)
	}
	if pane.AskedForASecret() {
		t.Error("the pane is waiting for something to be typed")
	}
}

// A pane opened to run one command does not open another pane: "another
// pane there" reads as that command run again, and this opens a shell.
func TestOpeningAnotherPaneIsRefusedOnACommandPane(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	if e := a.panes[pane]; e != nil {
		e.Kind = conns.Command
		e.Label = "make deploy"
	}
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	h := a.agents.of(pane)
	h.may.OpenMore = true
	c, err := agent.Dial(h.in.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(h.in.code))
		return err
	})

	var failed error
	offWindow(t, a, "the window to refuse the pane", func() error {
		_, failed = c.Open(got.ID)
		return nil
	})
	if failed == nil {
		t.Fatal("it opened a shell from a pane that runs one command")
	}
	if !strings.Contains(failed.Error(), "one command") {
		t.Errorf("it was told %q", failed)
	}
	if len(a.panes) != 1 {
		t.Errorf("the window holds %d panes, want the one", len(a.panes))
	}
}

// A second pane handed to the same agent needs only its code, so the
// dialog gives up the code on its own.
//
// Pasting the whole prompt again to say one more code is two hundred
// characters to carry forty, and the setup lines in it are about a
// server the agent already has.
func TestTheHandoverDialogCopiesTheCodeOnItsOwn(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	f := shareDialog(t, a, pane)
	code := a.agents.code()
	chord := copyChordOf(t, a)
	sendKey(t, a, press(chord.Key, chord.Mods))

	waitFor(t, a, "the code to reach the clipboard", func() bool { return a.copiedText() == code })
	// And the dialog says which key does it.
	drawn := strings.Join(drawnLines(a, f), "\n")
	if !strings.Contains(drawn, "copies the code") {
		t.Errorf("the dialog does not say the code can be copied:\n%s", drawn)
	}
}

// The user puts two panes in one share, and one code reaches both.
//
// A share is what a code names. The user adds panes to it, each with its
// own tick boxes, and the agent uses the code once: what it holds is
// every pane in the share, and list_panes is how it learns what that is
// now.
func TestAUserSharesTwoPanesUnderOneCode(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	first := onlyPaneOn(t, a)
	if err := a.openTabHere(); err != nil {
		t.Fatalf("open a second pane: %v", err)
	}
	var second *term.Terminal
	for pane := range a.panes {
		if pane != first {
			second = pane
		}
	}
	if second == nil {
		t.Fatal("the window did not open a second pane")
	}

	// The first pane starts the share; the second joins it, read only.
	f := handoverDialog(t, a, first)
	pressButton(t, a, f, "Done")
	code := a.agents.code()
	if code == "" {
		t.Fatal("sharing a pane made no code")
	}
	f = handoverDialog(t, a, second)
	tickBox(t, a, f, "Read only")
	pressButton(t, a, f, "Done")
	if got := a.agents.code(); got != code {
		t.Errorf("the second pane made a second code, %q", got)
	}

	// One code, both panes.
	c, err := agent.Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	var sh agent.Share
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		sh, err = c.Use(code)
		return err
	})
	if len(sh.Panes) != 2 {
		t.Fatalf("the code named %d panes, want 2", len(sh.Panes))
	}
	named := map[bool]agent.Pane{}
	for _, p := range sh.Panes {
		named[p.May.ReadOnly] = p
	}
	if named[true].ID == "" || named[false].ID == "" {
		t.Fatalf("the boxes did not stay with their own panes: %+v", sh.Panes)
	}
	// Both names begin with the share, which is what keeps one share's
	// panes out of another's reach.
	for _, p := range sh.Panes {
		if got, ok := agent.ShareOf(p.ID); !ok || got != sh.ID {
			t.Errorf("pane %q is not named for share %d", p.ID, sh.ID)
		}
	}

	// They are two panes, not one, and the boxes are each pane's own.
	a.shells[0].out <- []byte("first pane")
	waitFor(t, a, "the first pane to say something", func() bool {
		return strings.Contains(paneText(first), "first pane")
	})
	if look := looked(t, a, c, named[false].ID); !strings.Contains(look.Screen, "first pane") {
		t.Errorf("reading the pane that may be typed into gave:\n%s", look.Screen)
	}
	offWindow(t, a, "the window to take the keys", func() error {
		return c.Send(named[false].ID, "ls\r", nil)
	})
	var failed error
	offWindow(t, a, "the window to refuse the keys", func() error {
		failed = c.Send(named[true].ID, "ls\r", nil)
		return nil
	})
	if failed == nil {
		t.Error("it typed into the pane shared to be read")
	}
}

// A pane added while the agent works turns up the next time it asks.
//
// This is what a share is for: today's answer is not the answer for the
// rest of the session, and the agent is told to ask again rather than to
// hold the first list.
func TestAPaneAddedWhileTheAgentWorksTurnsUp(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	first := onlyPaneOn(t, a)
	f := handoverDialog(t, a, first)
	pressButton(t, a, f, "Done")
	code := a.agents.code()

	c, err := agent.Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	var sh agent.Share
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		sh, err = c.Use(code)
		return err
	})
	if len(sh.Panes) != 1 {
		t.Fatalf("the share holds %d panes, want the one", len(sh.Panes))
	}

	// The user opens another pane and adds it, while the agent holds the
	// code it already used.
	if err := a.openTabHere(); err != nil {
		t.Fatalf("open a second pane: %v", err)
	}
	var second *term.Terminal
	for pane := range a.panes {
		if pane != first {
			second = pane
		}
	}
	f = handoverDialog(t, a, second)
	pressButton(t, a, f, "Done")

	var listed []agent.Pane
	offWindow(t, a, "the window to list the panes", func() error {
		var err error
		listed, err = c.Panes()
		return err
	})
	if len(listed) != 2 {
		t.Fatalf("the agent was told it has %d panes, want 2", len(listed))
	}
	// And it can work in the new one without using another code.
	var added agent.Pane
	for _, p := range listed {
		if p.ID != sh.Panes[0].ID {
			added = p
		}
	}
	if look := looked(t, a, c, added.ID); look.Cols == 0 {
		t.Errorf("the agent cannot read the pane that was added: %+v", look)
	}

	// Taking it out again takes it off the list.
	if err := a.takeBackPane(second); err != nil {
		t.Fatalf("take it back: %v", err)
	}
	offWindow(t, a, "the window to list the panes", func() error {
		var err error
		listed, err = c.Panes()
		return err
	})
	if len(listed) != 1 {
		t.Fatalf("the agent was told it has %d panes, want the one left", len(listed))
	}
	if _, err := lookedAt(t, a, c, added.ID); err == nil {
		t.Error("it still reads the pane the user took out of the share")
	}
}

// lookedAt is a reading of a pane, and the failure when there is one.
func lookedAt(t *testing.T, a *testApp, c *agent.Client, id string) (agent.Look, error) {
	t.Helper()
	var look agent.Look
	var failed error
	offWindow(t, a, "the window to answer the agent", func() error {
		look, failed = c.Read(id, 0)
		return nil
	})
	return look, failed
}

// firstOf is the first pane of a share, for a test about one pane. A
// share with nothing in it is a failure of the test's own setup, and
// says so through the error the caller already checks.
func firstOf(sh agent.Share, err error) (agent.Pane, error) {
	if err != nil {
		return agent.Pane{}, err
	}
	if len(sh.Panes) == 0 {
		return agent.Pane{}, fmt.Errorf("share %d has no panes in it", sh.ID)
	}
	return sh.Panes[0], nil
}

// twoSharedPanes opens a second pane, puts both in the share and gives
// them back.
func twoSharedPanes(t *testing.T, a *testApp) (*term.Terminal, *term.Terminal) {
	t.Helper()
	first := onlyPaneOn(t, a)
	if err := a.openTabHere(); err != nil {
		t.Fatalf("open a second pane: %v", err)
	}
	var second *term.Terminal
	for pane := range a.panes {
		if pane != first {
			second = pane
		}
	}
	if second == nil {
		t.Fatal("the window did not open a second pane")
	}
	for _, pane := range []*term.Terminal{first, second} {
		f := handoverDialog(t, a, pane)
		pressButton(t, a, f, "Done")
	}
	return first, second
}

// The share dialog lists every pane in the share, and a row takes one
// out and puts it back.
//
// Taking a pane out is what the dialog is for, so it is one key on the
// row rather than a dialog of its own.
func TestTheShareDialogTakesAPaneOutAndPutsItBack(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	first, second := twoSharedPanes(t, a)

	// The second pane is read only, so the row says so.
	boxes := handoverDialog(t, a, second)
	tickBox(t, a, boxes, "Read only")
	pressButton(t, a, boxes, "Done")

	if err := a.showShare(); err != nil {
		t.Fatalf("show the share: %v", err)
	}
	f := awaitModal[*ui.Form](t, a, "the share dialog", byTitle[*ui.Form](shareTitle))
	if len(f.Fields()) != 3 {
		t.Fatalf("the share dialog has %d rows, want the agent and two panes", len(f.Fields()))
	}
	drawsEveryLine(t, a, f)
	drawsEveryButton(t, a, f)
	drawn := strings.Join(drawnLines(a, f), "\n")
	if !strings.Contains(drawn, a.agents.code()) {
		t.Errorf("the share dialog does not show the code:\n%s", drawn)
	}
	if !strings.Contains(drawn, "read only") {
		t.Errorf("the share dialog does not say what a pane allows:\n%s", drawn)
	}

	// The row for the second pane takes it out, at once.
	label := a.shareRowLabel(a.agents.of(second))
	tickBox(t, a, f, label)
	if a.agents.of(second) != nil {
		t.Error("the pane is still in the share")
	}
	if a.agents.of(first) == nil {
		t.Error("taking one pane out took the other with it")
	}
	// And the same key puts it back.
	tickBox(t, a, f, label)
	if a.agents.of(second) == nil {
		t.Error("the pane did not go back into the share")
	}
	// Still the one share, and the one code.
	if a.agents.code() == "" {
		t.Error("putting the pane back made a new share")
	}
}

// Taking the last pane out ends the share, and the code stops working.
func TestTakingTheLastPaneOutEndsTheShare(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)
	f := handoverDialog(t, a, pane)
	pressButton(t, a, f, "Done")
	code := a.agents.code()

	c, err := agent.Dial(code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it out: %v", err)
	}
	if a.agents.sharing() {
		t.Error("the share is still open with nothing in it")
	}
	if a.agents.listening() {
		t.Error("the window is still listening for agents")
	}
	if err := a.showShare(); err == nil {
		t.Error("the share dialog opened with no share")
	}
}

// The menu line says what pressing it does: start a share, or add to
// the one that is open.
func TestTheMenuLineSaysWhetherAShareIsOpen(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	pane := onlyPaneOn(t, a)

	a.refreshServers()
	if got := serverMenuLine(t, a, "agent.hand"); got != "Share this pane with an agent…" {
		t.Errorf("with nothing shared the line reads %q", got)
	}
	if serverMenuHas(t, a, "agent.share") {
		t.Error("the line that shows the share is there with no share")
	}

	f := handoverDialog(t, a, pane)
	pressButton(t, a, f, "Done")

	if got := serverMenuLine(t, a, "agent.hand"); got != "Add this pane to the share…" {
		t.Errorf("with a share open the line reads %q", got)
	}
	if !serverMenuHas(t, a, "agent.share") {
		t.Error("there is no line that shows the share")
	}
}

// serverMenuLine is what the Servers menu says for a command, and
// serverMenuHas whether it offers one at all.
func serverMenuLine(t *testing.T, a *testApp, command string) string {
	t.Helper()
	for _, menu := range a.bar.Menus {
		if menu.Title != serversMenu {
			continue
		}
		for _, item := range menu.Items {
			if item.Command == command {
				return item.Title
			}
		}
	}
	t.Fatalf("the Servers menu does not offer %q", command)
	return ""
}

func serverMenuHas(t *testing.T, a *testApp, command string) bool {
	t.Helper()
	for _, menu := range a.bar.Menus {
		if menu.Title != serversMenu {
			continue
		}
		for _, item := range menu.Items {
			if item.Command == command {
				return true
			}
		}
	}
	return false
}

// The bar says a share is open, and the chip opens it.
func TestTheMenuBarSaysAShareIsOpenAndOpensIt(t *testing.T) {
	a := newTestApp(t, 120, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	pane := onlyPaneOn(t, a)

	barRow(t, a)
	if got := chipsSay(a); len(got) != 0 {
		t.Fatalf("the bar says %q with nothing shared, want nothing", got)
	}

	boxes := handoverDialog(t, a, pane)
	pressButton(t, a, boxes, "Done")

	_, row := barRow(t, a)
	if got := chipsSay(a); len(got) != 1 || got[0] != shareTitle {
		t.Fatalf("the bar says %q, want %q", got, shareTitle)
	}
	if !strings.Contains(row, shareTitle) {
		t.Errorf("the bar row is %q, want %q on it", row, shareTitle)
	}
	// The code is the dialog's: the bar is read by whoever walks past.
	if strings.Contains(row, a.agents.code()) {
		t.Errorf("the bar row is %q, want the code kept off it", row)
	}

	col, at := chipColumn(t, a)

	pressChip(t, a, col, at)

	awaitModal(t, a, "the share dialog", byTitle[*ui.Form](shareTitle))

	// And the chip goes when the share does.
	if err := a.takeBackPane(pane); err != nil {
		t.Fatalf("take it out: %v", err)
	}
	barRow(t, a)
	if got := chipsSay(a); len(got) != 0 {
		t.Errorf("the bar says %q with the share over, want nothing", got)
	}
}

// A window that is both served and shared says both, and each chip opens
// its own dialog.
//
// Serving goes by the right edge, because a window too narrow for both
// drops the share first.
func TestTheMenuBarSaysBothServingAndSharing(t *testing.T) {
	a := aServedWindow(t)
	pane := onlyPaneOn(t, a)
	boxes := handoverDialog(t, a, pane)
	pressButton(t, a, boxes, "Done")

	_, row := barRow(t, a)

	if got := chipsSay(a); len(got) != 2 || got[0] != shareTitle || got[1] != "Serving" {
		t.Fatalf("the bar says %q, want the share and then serving", got)
	}
	shared, served := strings.Index(row, shareTitle), strings.Index(row, "Serving")
	if shared < 0 || served < 0 {
		t.Fatalf("the bar row is %q, want both chips on it", row)
	}
	if shared > served {
		t.Errorf("the bar row is %q, want serving drawn last", row)
	}

	// The chip by the edge is the serving one, and it opens the serving
	// dialog rather than the share.
	col, at := chipColumn(t, a)
	pressChip(t, a, col, at)
	f := awaitModal(t, a, "the serving dialog", byTitle[*ui.Form]("Serving this window"))
	pressButton(t, a, f, "Keep serving")

	// And the one beside it opens the share.
	area, _ := barRow(t, a)
	pressChip(t, a, area.X+shared, area.Y)
	awaitModal(t, a, "the share dialog", byTitle[*ui.Form](shareTitle))
}

// pressChip is a left press on one cell of the menu bar.
func pressChip(t *testing.T, a *testApp, col, row int) {
	t.Helper()
	took, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	})
	if err != nil {
		t.Fatalf("pressing the chip: %v", err)
	}
	if !took {
		t.Fatal("the press on the chip travelled on")
	}
}
