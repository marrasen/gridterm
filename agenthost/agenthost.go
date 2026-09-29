// Package agenthost knows the agent programs kakel hands panes to:
// how to add kakel's MCP server to each, the prompt that hands a share
// over, and where each reads the skill that says how to work in panes.
package agenthost

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/marrasen/kakel/mcp"
	"github.com/marrasen/kakel/settings"
)

// The agent programs the prompt and setup are written for, by name.
const (
	ClaudeCode = "Claude Code"
	Codex      = "Codex"
	Cursor     = "Cursor"
	Other      = "Another host"
)

// Host is an agent program: Name is what the list calls it, and Called
// what a sentence does. Cmd adds an MCP server from the command line,
// and ConfigAt is where one without a command keeps its servers.
type Host struct {
	Name, Called, Cmd, ConfigAt string
	// skillIn is where under home it reads skills from, and skillEnv
	// the setting that moves that.
	skillIn  []string
	skillEnv string
}

// All are the agent programs kakel knows how to set up.
var All = []Host{
	{Name: ClaudeCode, Called: ClaudeCode, Cmd: "claude", skillIn: []string{".claude", "skills", "kakel"}, skillEnv: "CLAUDE_CONFIG_DIR"},
	{Name: Codex, Called: Codex, Cmd: "codex"},
	{Name: Cursor, Called: Cursor, ConfigAt: "~/.cursor/mcp.json"},
	{Name: Other, Called: "the host"},
}

// Named is the agent program called name, or the first for a name it
// does not know.
func Named(name string) Host {
	for _, h := range All {
		if h.Name == name {
			return h
		}
	}
	return All[0]
}

// Names are the agent programs' names, in order.
func Names() []string {
	var out []string
	for _, h := range All {
		out = append(out, h.Name)
	}
	return out
}

// ExePath is this program's path, for the MCP server's command line,
// or its bare name when the system will not say.
func ExePath() string {
	if exe, ok := Exe(); ok {
		return exe
	}
	return "kakel"
}

// Exe is where this program is, and false when the system will not say:
// then only a bare name is left, which works only on the PATH.
func Exe() (string, bool) {
	exe, err := os.Executable()
	return exe, err == nil && exe != ""
}

func quotedPath(path string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return `'` + strings.ReplaceAll(path, `'`, `'\''`) + `'`
}

func mcpConfig(exe string) string {
	inJSON := strings.ReplaceAll(strings.ReplaceAll(exe, `\`, `\\`), `"`, `\"`)
	return `{"mcpServers": {"kakel": {` + "\n" + `  "command": "` + inJSON + `",` + "\n" + `  "args": ["-mcp"]}}}`
}

// SetupToCopy is what adds the MCP server to h.
func (h Host) SetupToCopy(exe string) string {
	if h.Cmd != "" {
		return h.Cmd + " mcp add kakel -- " + quotedPath(exe) + " -mcp"
	}
	return mcpConfig(exe)
}

// setupForAgent is the part of the prompt that says how to add the
// server, for an agent that does not have its tools.
func (h Host) setupForAgent(exe string) string {
	if h.Cmd != "" {
		return "Ask the user to run this line and then start you again:\n\n  " + h.SetupToCopy(exe)
	}
	where := h.ConfigAt
	if where == "" {
		where = "its MCP config"
	}
	lines := strings.Split(mcpConfig(exe), "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return "Ask the user to put this in " + where + " and then start " + h.Called + " again:\n\n" + strings.Join(lines, "\n")
}

// Prompt is what the user pastes into the agent program h, with the
// share's code.
func (h Host) Prompt(code, exe string) string {
	return fmt.Sprintf(`The user has shared terminal panes with you in kakel, a terminal
running on this machine. You work in those panes through kakel's MCP
server, and the user watches everything you do.

That server runs on this machine, on standard input and output (stdio), because
the port inside the code is on the loopback address. If you do not have
kakel's tools, it has not been added here yet.

%s

This code is the only credential and it came from the user. Call
use_session_code with it before anything else. The answer lists the panes, and
every other tool takes a pane's name.

  %s

%s

The server's own instructions say how the tools work, and say this again.
`, h.setupForAgent(exe), code, mcp.Short)
}

// skillFile is what a skill's file is called.
const skillFile = "SKILL.md"

// Skill is the skill for the agent program h.
func (h Host) Skill(exe string) string { return mcp.Skill(h.setupForAgent(exe)) }

// SkillPath is where the agent program's skill goes: where it reads
// skills from, or beside the settings for one that has no such place.
func (h Host) SkillPath() (string, error) {
	if len(h.skillIn) > 0 {
		if dir := os.Getenv(h.skillEnv); h.skillEnv != "" && dir != "" {
			dir, err := fromHome(dir)
			if err != nil {
				return "", err
			}
			return filepath.Join(append(append([]string{dir}, h.skillIn[1:]...), skillFile)...), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(append(append([]string{home}, h.skillIn...), skillFile)...), nil
	}
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills", "kakel", skillFile), nil
}

// fromHome is a directory an agent program's own setting named, as an
// absolute path: a leading ~, and a relative path, are read from home,
// as the program reads its setting from its own home and not from
// wherever this window was started.
func fromHome(dir string) (string, error) {
	if filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(dir, "~"), string(filepath.Separator))
	rest = strings.TrimPrefix(rest, "/")
	return filepath.Join(home, rest), nil
}

// ReadsSkills reports whether the agent program reads skills from a
// place of its own; for one that does not, SkillPath is beside kakel's
// settings, for the user to copy from.
func (h Host) ReadsSkills() bool { return len(h.skillIn) > 0 }
