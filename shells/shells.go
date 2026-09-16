// Package shells finds which shells a pane on this machine can run, and translates a Windows
// directory for WSL.
//
// A shell is offered only when it was found, never because the platform usually has it.
package shells

import (
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
)

// wslPrefix starts the id of every WSL shell, and the rest of the id is the distribution's name.
const wslPrefix = "wsl:"

// loginID is the id of the login shell, which is the only shell offered away from Windows.
const loginID = "login"

// Shell is a shell a pane on this machine can run.
type Shell struct {
	// ID names the shell in the settings file and in a command id. It is stable across runs:
	// "cmd", "powershell", "pwsh", "wsl:Ubuntu".
	ID string

	// Title is what a menu line says.
	Title string

	// Path is the program, and Args what goes before the working directory.
	Path string
	Args []string

	// Distro is the WSL distribution this runs, empty for the rest.
	Distro string
}

// windowsShells are the shells that come with Windows, in the order a menu offers them.
var windowsShells = []struct {
	id, title, file string
}{
	{"cmd", "Command Prompt", "cmd.exe"},
	{"powershell", "Windows PowerShell", "powershell.exe"},
	{"pwsh", "PowerShell", "pwsh.exe"},
}

// probe is where the lookup gets its answers, so a test can describe a machine rather than depend on
// the one it runs on.
type probe struct {
	goos     string
	getenv   func(name string) string
	lookPath func(file string) (string, error)

	// distros returns the installed WSL distributions, or why they could not be listed.
	distros func() ([]string, error)
}

// thisMachine asks the machine the program is running on.
func thisMachine() probe {
	return probe{
		goos:     runtime.GOOS,
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		distros:  wslDistros,
	}
}

// Find returns the shells this machine can open a pane on, best first.
//
// It runs wsl.exe to list the distributions, which takes a moment, so a caller that only needs to
// resolve one remembered id should use Named.
func Find() []Shell {
	return find(thisMachine())
}

func find(p probe) []Shell {
	if p.goos != "windows" {
		return []Shell{p.login()}
	}

	var list []Shell
	for _, w := range windowsShells {
		if s, ok := p.windows(w.id); ok {
			list = append(list, s)
		}
	}

	// One line per installed distribution, in the order wsl.exe lists them.
	names, err := p.distros()
	if err != nil {
		return list
	}
	for _, name := range names {
		if s, ok := p.wsl(name); ok {
			list = append(list, s)
		}
	}
	return list
}

// Named returns the shell an id names, and whether it is still installed.
//
// It never lists the WSL distributions, so a window can resolve the shell a user last chose before
// the first pane opens. A distribution that has been removed shows up when the pane fails to start.
func Named(id string) (Shell, bool) {
	return named(thisMachine(), id)
}

func named(p probe, id string) (Shell, bool) {
	if p.goos != "windows" {
		if id == loginID {
			return p.login(), true
		}
		return Shell{}, false
	}
	if distro, ok := strings.CutPrefix(id, wslPrefix); ok {
		return p.wsl(distro)
	}
	return p.windows(id)
}

// Lookup returns the shell in list that id names, for the case where a remembered shell has gone.
func Lookup(list []Shell, id string) (Shell, bool) {
	for _, s := range list {
		if s.ID == id {
			return s, true
		}
	}
	return Shell{}, false
}

// windows returns one of the shells Windows comes with, and whether it is installed.
func (p probe) windows(id string) (Shell, bool) {
	for _, w := range windowsShells {
		if w.id != id {
			continue
		}
		prog := ""
		if id == "cmd" {
			prog = p.getenv("COMSPEC")
		}
		if prog == "" {
			found, err := p.lookPath(w.file)
			if err != nil {
				return Shell{}, false
			}
			prog = found
		}
		return Shell{ID: w.id, Title: w.title, Path: prog}, true
	}
	return Shell{}, false
}

// wsl returns the shell that runs a WSL distribution, and whether wsl.exe is installed.
func (p probe) wsl(distro string) (Shell, bool) {
	if distro == "" {
		return Shell{}, false
	}
	found, err := p.lookPath("wsl.exe")
	if err != nil {
		return Shell{}, false
	}
	return Shell{
		ID:     wslPrefix + distro,
		Title:  distro + " (WSL)",
		Path:   found,
		Args:   []string{"-d", distro},
		Distro: distro,
	}, true
}

// login returns the user's login shell, which is the only shell a machine that is not Windows offers.
func (p probe) login() Shell {
	sh := p.getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
		if _, err := p.lookPath("/bin/bash"); err == nil {
			sh = "/bin/bash"
		}
	}
	return Shell{ID: loginID, Title: path.Base(sh), Path: sh}
}

// Command is the argv for a pane on this shell, starting in dir. An empty dir leaves the working
// directory to the caller.
func (s Shell) Command(dir string) []string {
	argv := make([]string, 0, len(s.Args)+3)
	argv = append(argv, s.Path)
	argv = append(argv, s.Args...)

	// WSL has its own idea of the working directory, and only a path it can reach will do.
	if s.Distro != "" && dir != "" {
		if unix := UnixPath(dir); unix != "" {
			argv = append(argv, "--cd", unix)
		}
	}
	return argv
}

// UnixPath translates a Windows path for WSL, and returns "" for one WSL cannot reach.
//
// A drive letter becomes a mount under /mnt, so C:\Workspace is /mnt/c/Workspace. A UNC path and a
// relative path both give "".
func UnixPath(win string) string {
	if len(win) < 3 {
		return ""
	}
	drive := win[0]
	switch {
	case drive >= 'a' && drive <= 'z':
	case drive >= 'A' && drive <= 'Z':
		drive += 'a' - 'A'
	default:
		return ""
	}
	if win[1] != ':' || (win[2] != '\\' && win[2] != '/') {
		return ""
	}
	return "/mnt/" + string(drive) + strings.ReplaceAll(win[2:], `\`, "/")
}
