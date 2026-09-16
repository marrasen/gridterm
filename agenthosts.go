package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/marrasen/gridterm/mcp"
	"github.com/marrasen/gridterm/settings"
)

// agentHost is one program that runs an agent: what to call it, how gridterm's MCP server is added
// to it, and where it reads skills from.
type agentHost struct {
	// name is what the dialog offers and what the settings remember.
	name string

	// called is what a sentence calls this host, which is its name unless the name is not
	// something a sentence can use.
	called string

	// cmd is the host's own command that adds an MCP server, empty for a host set up by a config
	// file instead.
	cmd string

	// configAt is the file an MCP config goes in, empty for a host gridterm knows no path for.
	configAt string

	// skillIn is where this host reads skills from, under the user's home directory. Empty means
	// gridterm knows of nowhere, so the skill goes under gridterm's own settings instead.
	skillIn []string

	// skillEnv names an environment variable holding this host's configuration directory, which
	// stands in for the first part of skillIn when it is set.
	skillEnv string
}

// The hosts the dialog offers, by name.
const (
	hostClaudeCode = "Claude Code"
	hostCodex      = "Codex"
	hostCursor     = "Cursor"
	hostOther      = "Another host"
)

// agentHosts are the hosts a prompt and a skill can be written for. Claude Code is first, and is
// what a name from another build of gridterm falls back to.
var agentHosts = []agentHost{
	{name: hostClaudeCode, called: hostClaudeCode, cmd: "claude",
		skillIn: []string{".claude", "skills", "gridterm"}, skillEnv: "CLAUDE_CONFIG_DIR"},
	{name: hostCodex, called: hostCodex, cmd: "codex"},
	{name: hostCursor, called: hostCursor, configAt: "~/.cursor/mcp.json"},
	{name: hostOther, called: "the host"},
}

// addLine is the command that adds gridterm's MCP server, for a host that has one.
func (h agentHost) addLine(exe string) string {
	return h.cmd + " mcp add gridterm -- " + quotedPath(exe) + " -mcp"
}

// configIn is what to call the file a host's MCP config goes in.
func (h agentHost) configIn() string {
	if h.configAt != "" {
		return h.configAt
	}
	return "its MCP config"
}

// dialogCols is the room a dialog has for one line of text.
const dialogCols = 72

// setupLines say how the user adds gridterm's MCP server to this host, as lines for a dialog.
//
// Wrapped by hand, because a dialog draws a line it is given and trims what will not fit. The
// path goes on a line of its own, and is broken across lines when even that will not hold it.
func (h agentHost) setupLines(exe string) []string {
	if h.cmd != "" {
		lines := []string{
			"First add gridterm's MCP server to " + h.called + ", as one command line:",
			"",
			"  " + h.cmd + " mcp add gridterm --",
		}
		lines = append(lines, wrapped("  "+quotedPath(exe)+" -mcp", "  ", dialogCols)...)
		return append(lines, "",
			"It only writes the config, so start "+h.called+" again first.")
	}
	lines := []string{"First put gridterm's MCP server in " + h.configIn() + ":", ""}
	for _, line := range strings.Split(mcpConfig(exe), "\n") {
		lines = append(lines, wrapped(line, "  ", dialogCols)...)
	}
	return append(lines, "",
		"Then start "+h.called+" again, and paste the prompt to it.")
}

// wrapped breaks a line too wide for a dialog, carrying the rest on with indent in front of it.
//
// It breaks anywhere, because a path has no words to break between, and a path the user cannot
// read at all is worse than one they have to join up.
func wrapped(line, indent string, room int) []string {
	var out []string
	for len(line) > room && room > len(indent) {
		cut := room
		// Never in the middle of a character.
		for cut > len(indent) && !utf8.RuneStart(line[cut]) {
			cut--
		}
		out = append(out, line[:cut])
		line = indent + line[cut:]
	}
	return append(out, line)
}

// setupForAgent says the same to an agent that cannot see gridterm's tools: what to ask the user
// for, and the exact thing to ask them to run or write.
func (h agentHost) setupForAgent(exe string) string {
	if h.cmd != "" {
		return "Ask the user to run this line and then start you again:\n\n  " + h.addLine(exe)
	}
	return "Ask the user to put this in " + h.configIn() + " and then start " + h.called +
		" again:\n\n" + indented(mcpConfig(exe), "  ")
}

// quotedPath is a path as one argument on a command line of this platform's shell.
//
// In quotes, because a path with a space in it would otherwise be two arguments.
func quotedPath(path string) string { return quotedPathOn(path, runtime.GOOS) }

// quotedPathOn is quotedPath for a named platform, so both kinds can be tested anywhere.
//
// Windows shells take double quotes, and a backslash in a path there is an ordinary character.
// Everywhere else a single-quoted string ends at the first quote, so an inner quote closes the
// string, adds an escaped quote and opens it again.
func quotedPathOn(path, goos string) string {
	if goos == "windows" {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return `'` + strings.ReplaceAll(path, `'`, `'\''`) + `'`
}

// mcpConfig is the MCP config that starts gridterm's server, for a host set up by a file.
func mcpConfig(exe string) string {
	// A path inside JSON needs its backslashes doubled.
	inJSON := strings.ReplaceAll(exe, `\`, `\\`)
	inJSON = strings.ReplaceAll(inJSON, `"`, `\"`)
	return `{"mcpServers": {"gridterm": {` + "\n" +
		`  "command": "` + inJSON + `",` + "\n" +
		`  "args": ["-mcp"]}}}`
}

// indented puts a prefix in front of every line of a block.
func indented(block, prefix string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// agentHostNames are the hosts the dialog offers, in order.
func agentHostNames() []string {
	names := make([]string, 0, len(agentHosts))
	for _, h := range agentHosts {
		names = append(names, h.name)
	}
	return names
}

// hostNamed is the host a name picks out. A name this build does not know falls back to Claude Code.
func hostNamed(name string) agentHost {
	for _, h := range agentHosts {
		if h.name == name {
			return h
		}
	}
	return agentHosts[0]
}

// skillFile is what a skill is called wherever it goes.
const skillFile = "SKILL.md"

// skillFor is the skill that tells a host what gridterm is and how to work in a pane it handed over.
func skillFor(host agentHost, exe string) string {
	return fmt.Sprintf(`---
name: gridterm
description: Work in a terminal pane the user handed over in gridterm, through its MCP server
---

# Working in a gridterm pane

gridterm is a terminal on the user's machine. The user sets a session up -- through whatever
machines, as whatever user -- and hands you one pane of it. You work in that pane through
gridterm's MCP server, and the user watches everything you do.

## Reaching the server

The server runs on the user's machine, on standard input and output (stdio), because the port
inside a session code is on the loopback address. If you do not have gridterm's tools, it has not
been added here yet.

%s

## Getting a pane

The user hands a pane over in gridterm and gets a session code for that one pane. Ask the user for
the code if you have not been given one. Call use_session_code with it before anything else. The
answer names the pane, and every other tool takes that name.

## Working in the pane

%s

## Rules

%s
`, host.setupForAgent(exe), mcp.Workflow, mcp.Rules)
}

// printSkill writes the skill for Claude Code, for -mcp-skill.
func printSkill(w io.Writer) error {
	// Refused rather than written with the bare name, because a file sits there saying the wrong
	// thing long after the failure is forgotten, where the dialog is read once and warns as it goes.
	exe, err := exePath()
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, skillFor(hostNamed(hostClaudeCode), exe))
	return err
}

// skillPathFor is where a host's skill goes, and whether that is where the host reads skills from.
func skillPathFor(host agentHost) (path string, ownPlace bool, err error) {
	if len(host.skillIn) > 0 {
		// The host's own setting for where its configuration lives, which moves its skills with it.
		if dir := os.Getenv(host.skillEnv); host.skillEnv != "" && dir != "" {
			parts := append([]string{dir}, host.skillIn[1:]...)
			return filepath.Join(append(parts, skillFile)...), true, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", true, fmt.Errorf("there is no home directory to write a skill in: %w", err)
		}
		parts := make([]string, 0, len(host.skillIn)+2)
		parts = append(parts, home)
		parts = append(parts, host.skillIn...)
		parts = append(parts, skillFile)
		return filepath.Join(parts...), true, nil
	}
	path, err = settings.Path()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(filepath.Dir(path), "skills", "gridterm", skillFile), false, nil
}

// writeSkill writes a host's skill and says where it went.
//
// A file already there saying something else is left alone and reported as needing the user's word,
// unless over says to write over it.
func writeSkill(host agentHost, exe string, over bool) (path string, ask bool, err error) {
	path, _, err = skillPathFor(host)
	if err != nil {
		return "", false, err
	}
	body := skillFor(host, exe)
	if !over {
		was, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return path, false, fmt.Errorf("read %s: %w", path, err)
		case string(was) == body:
			// Already says what this would write, so there is nothing to write and nothing to ask.
			return path, false, nil
		default:
			return path, true, nil
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return path, false, fmt.Errorf("make %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return path, false, fmt.Errorf("write %s: %w", path, err)
	}
	return path, false, nil
}
