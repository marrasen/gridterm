package main

import (
	"strings"
	"testing"

	"github.com/marrasen/kakel/settings"
)

// echoed types a command at pane id's prompt and waits for what it
// printed, the line after.
func echoed(t *testing.T, a *app, id, cmd, want string) {
	t.Helper()
	waitFor(t, a, "the prompt", func() bool { return strings.Contains(a.terminal(id).Text(), promptEnd()) })
	// Typed rather than pasted: bash takes a pasted return as part of
	// the line rather than as Enter.
	a.terminal(id).Send([]byte(cmd + "\r"))
	waitFor(t, a, want, func() bool { return strings.Contains(a.terminal(id).Text(), "\n"+want) })
}

func TestANewShellIsToldTheTerminalsName(t *testing.T) {
	a, _ := agentApp(t)
	echoed(t, a, a.st.Panes[0].ID, echoVar("tp=", "TERM_PROGRAM"), "tp=kakel")
	a.st.TermProgram = "WezTerm"
	set, err := settingsIn(t)
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.handle(SetTermProgram{Called: "WezTerm"})
	a.handle(NewTerminal{})
	echoed(t, a, a.st.Panes[1].ID, echoVar("tp=", "TERM_PROGRAM"), "tp=WezTerm")
}

func TestANewTerminalStartsInTheFolderOfThePaneInFront(t *testing.T) {
	a, _ := agentApp(t)
	a.st.ShellSetup = true
	// Taught with bash's PROMPT_COMMAND, which dash, the /bin/sh here,
	// lacks. Windows starts cmd.exe whatever SHELL says, and teaches it
	// through its prompt.
	t.Setenv("SHELL", "/bin/bash")
	// The first shell, taught, says where it is once it has moved.
	a.handle(NewTerminal{})
	taught := a.st.Panes[1].ID
	dir := t.TempDir()
	a.terminal(taught).Send([]byte(changeDir(dir) + "\r"))
	waitFor(t, a, "the shell to say where it is", func() bool {
		got, _ := a.terminal(taught).Dir()
		return got == dir
	})
	a.handle(NewTerminal{})
	echoed(t, a, a.st.Panes[2].ID, printDir(), dir)
}

// settingsIn is a settings file of the test's own.
func settingsIn(t *testing.T) (*settings.Settings, error) {
	t.Helper()
	return settings.Load(t.TempDir() + "/settings.json")
}
