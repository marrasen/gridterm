package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/agent"
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
	c, err := agent.Dial(h.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return pane, h.code, c
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
		got, err = c.Use(a.agents.of(pane).code)
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
		got, err = c.Use(code)
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
	if got := a.agents.of(pane).code; got != code {
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
		_, err := c.Use(code)
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

	// The workflow and the rules are the server's to give. Two copies
	// drift apart, which is why the prompt stops short of them.
	for _, said := range []string{mcp.Workflow, mcp.Rules} {
		if said == "" {
			t.Fatal("the workflow or the rules are empty, so this checks nothing")
		}
		if strings.Contains(prompt, said) {
			t.Errorf("the prompt repeats what the server's instructions say:\n%s", said)
		}
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
	if lines := strings.Count(strings.TrimRight(prompt, "\n"), "\n") + 1; lines > 22 {
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
		t.Fatalf("hand it over: %v", err)
	}
	return awaitModal[*ui.Form](t, a, "the hand-over dialog",
		byTitle[*ui.Form]("An agent may work in this pane"))
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

	f := handoverDialog(t, a, pane)
	drawsEveryLine(t, a, f)
	code := a.agents.of(pane).code
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
		t.Errorf("copying the prompt put %T over the hand-over dialog", top)
	}

	// The instructions are a button away, and say what to run.
	pressButton(t, a, f, "Install")
	shownIn := awaitModal[*ui.Form](t, a, "the install instructions",
		byTitle[*ui.Form]("Add gridterm to "+host.called))
	drawsEveryLine(t, a, shownIn)
	drawn := strings.Join(drawnLines(a, shownIn), "\n")
	for _, say := range []string{"claude mcp add gridterm --", "start Claude Code again"} {
		if !strings.Contains(drawn, say) {
			t.Errorf("the dialog does not draw %q:\n%s", say, drawn)
		}
	}

	// And the command line can be taken off it, which is the whole point
	// of a dialog naming a command.
	pressButton(t, a, shownIn, host.copyTitle())
	want := host.setupToCopy(exe)
	waitFor(t, a, "the setup line to reach the clipboard", func() bool { return a.copiedText() == want })

	pressButton(t, a, shownIn, "Done")
	// Taking it back is still one button away, on the dialog underneath.
	pressButton(t, a, f, "Take it back")
	if a.agents.of(pane) != nil {
		t.Error("it is still handed over")
	}
}

// The copy chord copies the setup line too, for a user who reaches for
// the chord rather than the button.
func TestTheCopyChordOnTheInstallInstructionsCopiesTheSetupLine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	pane := onlyPaneOn(t, a)

	f := handoverDialog(t, a, pane)
	pressButton(t, a, f, "Install")
	host := hostNamed(hostClaudeCode)
	shown := awaitModal[*ui.Form](t, a, "the install instructions",
		byTitle[*ui.Form]("Add gridterm to "+host.called))

	chord := copyChordOf(t, a)
	sendKey(t, a, press(chord.Key, chord.Mods))
	want := host.setupToCopy(exeHere(t))
	waitFor(t, a, "the setup line to reach the clipboard", func() bool { return a.copiedText() == want })
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

	f := handoverDialog(t, a, pane)
	// Nothing remembered yet, so it opens on Claude Code.
	fieldSays(t, f, "Agent", hostClaudeCode)
	stepOptions(t, a, f, "Agent")
	fieldSays(t, f, "Agent", hostCodex)
	pressButton(t, a, f, "Copy the prompt")

	code := a.agents.of(pane).code
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

	// And the next hand-over opens on it.
	pressButton(t, a, f, "Done")
	next := handoverDialog(t, a, pane)
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

	offWindow(t, a, "the window to answer the agent", func() error {
		_, err := c.Use(code)
		return err
	})

	// A wait in flight: the agent is asking, and nothing is running
	// what it asked for.
	asking := make(chan struct{})
	go func() {
		close(asking)
		_, _, _ = c.Wait("1", 0, agent.Until{QuietMS: 60000, TimeoutMS: 60000})
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
		had, err = c.Use(code)
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
	if again.code == code {
		t.Fatal("it handed out the same code again")
	}
	if again.id == had.ID {
		t.Fatal("the new handover has the name the old one had")
	}

	// The old agent is holding a name that no longer means anything.
	// Its connection was closed with the listener, so it reconnects the
	// way anything that lost a connection would.
	old, err := agent.Dial(again.code)
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

	first := a.agents.of(panes[0])
	c, err := agent.Dial(first.code)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	var had agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		had, err = c.Use(first.code)
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

			f := handoverDialog(t, a, pane)
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
	f := handoverDialog(t, a, pane)
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
	f = handoverDialog(t, a, pane)
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

	f := handoverDialog(t, a, pane)
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
			const front = "---\nname: gridterm\ndescription: Work in a terminal pane the user" +
				" handed over in gridterm, through its MCP server\n---\n"
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
			f := handoverDialog(t, a, pane)
			drawsEveryLine(t, a, f)

			// The longest it gets: a path with a space in it, and the
			// note about not having read the path at all.
			const exe = `C:\Program Files\gridterm\gridterm.exe`
			a.showSetup(host, exe, errors.New("gridterm could not read its own path"))

			shown := awaitModal[*ui.Form](t, a, "the install instructions",
				byTitle[*ui.Form]("Add gridterm to "+host.called))
			drawsEveryLine(t, a, shown)
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
		got, err = c.Use(code)
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
		got, err = c.Use(code)
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
		got, err = c.Use(code)
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

	f := handoverDialog(t, a, pane)
	code := a.agents.of(pane).code
	drawsEveryLine(t, a, f)
	if drawn := strings.Join(drawnLines(a, f), "\n"); !strings.Contains(drawn, code) {
		t.Errorf("the first dialog never shows the code:\n%s", drawn)
	}

	// The prompt, and the dialog that says how to install the server.
	pressButton(t, a, f, "Copy the prompt")
	pressButton(t, a, f, "Install")
	shown := awaitModal[*ui.Form](t, a, "the install instructions",
		byTitle[*ui.Form]("Add gridterm to "+hostNamed(hostClaudeCode).called))
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
	if h := a.agents.of(pane); h == nil || h.code != code {
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
		got, err = c.Use(code)
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
