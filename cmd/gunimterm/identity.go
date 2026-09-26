package main

import (
	"os"
	"strings"

	"github.com/marrasen/gridterm/internal/build"
	"github.com/marrasen/gridterm/session"
	shellfind "github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/shellsetup"
)

// What a shell here is started with, as gridterm starts one: in the
// folder of the pane the user is in, told which terminal it runs in,
// and, with Shell Setup on, taught to say what it is doing, so paths
// it prints can be followed and an agent can tell a command's end.

// Intents for the shell's start.
type (
	// ToggleShellSetup turns the teaching of new shells here on or off.
	ToggleShellSetup struct{}
	// SetTermProgram sets what new shells here are told the terminal
	// is called, "" for gridterm's own name.
	SetTermProgram struct{ Called string }
)

// knownTerminals are names a user may give instead, for a program that
// shows pictures only in a terminal it knows.
var knownTerminals = []string{"iTerm.app", "WezTerm", "vscode", "Apple_Terminal"}

// startLocalSession starts argv here, or the user's shell when it is
// nil, in dir, as a new shell is started. A shell, rather than one
// command, is taught when Shell Setup is on.
func (a *app) startLocalSession(argv []string, dir string, cols, rows int, shell bool) (session.Session, error) {
	called := ""
	if a.settings != nil {
		called = a.settings.TermProgram()
	}
	sess, err := session.StartLocal(session.LocalConfig{
		Command: argv, Dir: dir, Env: paneEnv(argv, called), Cols: cols, Rows: rows,
	})
	if err != nil || !shell || !a.st.ShellSetup {
		return sess, err
	}
	route := argv
	if len(route) == 0 {
		if got, err := session.DefaultShell(); err == nil {
			route = got
		}
	}
	if typed := shellsetup.Typed(shellsetup.RouteFor(route)); len(typed) > 0 {
		_, _ = sess.Write(typed)
	}
	return sess, nil
}

// teachFar teaches a server's shell to say what it is doing, when its
// saved server says to.
func (a *app) teachFar(machine string, sess session.Session) {
	for _, h := range a.st.Saved {
		if h.Name == machine && h.Setup {
			if typed := shellsetup.Typed(shellsetup.RouteFor(nil)); len(typed) > 0 {
				_, _ = sess.Write(typed)
			}
		}
	}
}

// paneEnv is what a shell here is told about the terminal it runs in.
func paneEnv(argv []string, called string) []string {
	if called == "" {
		called = build.Name
	}
	env := []string{"TERM_PROGRAM=" + called, "TERM_PROGRAM_VERSION=" + build.Version()}
	if shellfind.IsWSL(argv) {
		env = append(env, "WSLENV="+shellfind.CarryIntoWSL(os.Getenv("WSLENV"), "TERM_PROGRAM", "TERM_PROGRAM_VERSION"))
	}
	return env
}

// dirHere is the folder of the focused pane's shell, when it is on this
// machine and has said, for a new shell to start in.
func (a *app) dirHere() string {
	t := a.terminal(a.st.Focus)
	if t == nil || a.machineOf(a.st.Focus) != "" {
		return ""
	}
	dir, host := t.Dir()
	switch strings.ToLower(host) {
	case "", "localhost", "127.0.0.1", "::1":
	default:
		if h, err := os.Hostname(); err != nil || !strings.EqualFold(h, host) {
			return ""
		}
	}
	return dir
}
