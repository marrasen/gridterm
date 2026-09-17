// Package shells finds which shells a pane on this machine can run, and translates a Windows
// directory for WSL.
//
// A shell is offered only when it was found, never because the platform usually has it. Listing the
// WSL distributions can fail on its own, and Find says why rather than leave them quietly out.
package shells

import (
	"errors"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
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

	// Path is the program, and Args the arguments it always takes, such as the -d that picks a WSL
	// distribution.
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

// Find returns the shells this machine can open a pane on, best first, and why the WSL distributions
// could not be listed. It runs wsl.exe to list them, which takes a moment, so a caller that only needs
// one remembered id should use Named.
//
// wsl.exe being missing and wsl.exe exiting non-zero are both answers rather than failures: it ships
// with every Windows, and it exits non-zero when the feature is off or nothing is installed. A WSL
// that is half broken therefore offers no distributions and says nothing about why, which is the
// price of not putting a notice in front of every Windows user who never had WSL.
func Find() ([]Shell, error) {
	return find(thisMachine())
}

func find(p probe) ([]Shell, error) {
	if p.goos != "windows" {
		if s, ok := p.login(); ok {
			return []Shell{s}, nil
		}
		return nil, nil
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
		// wsl.exe was asked and said it has none.
		var exited *exec.ExitError
		if errors.Is(err, exec.ErrNotFound) || errors.As(err, &exited) {
			return list, nil
		}
		return list, err
	}
	for _, name := range names {
		if s, ok := p.wsl(name); ok {
			list = append(list, s)
		}
	}
	return list, nil
}

// Named returns the shell an id names, and whether this machine could run it.
//
// It never lists the WSL distributions, so a window can resolve the shell a user last chose before
// the first pane opens. A distribution that has been removed shows up when the pane fails to start.
func Named(id string) (Shell, bool) {
	return named(thisMachine(), id)
}

func named(p probe, id string) (Shell, bool) {
	if p.goos != "windows" {
		if id == loginID {
			return p.login()
		}
		return Shell{}, false
	}
	if distro, ok := strings.CutPrefix(id, wslPrefix); ok {
		return p.wsl(distro)
	}
	return p.windows(id)
}

// Running returns the shell an argv runs, and whether the list holds one.
// The whole argv, because every WSL distribution runs wsl.exe and only
// the arguments tell them apart. Windows ignores case in a path, so this
// does too.
func Running(list []Shell, argv []string) (Shell, bool) {
	for _, s := range list {
		if slices.EqualFunc(s.Command(""), argv, strings.EqualFold) {
			return s, true
		}
	}
	return Shell{}, false
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
		// COMSPEC names cmd.exe when the file it names is there, and the PATH answers when it is not.
		if id == "cmd" {
			if comspec := p.getenv("COMSPEC"); comspec != "" {
				if found, err := p.lookPath(comspec); err == nil {
					prog = found
				}
			}
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

// login returns the shell $SHELL names, or the first of /bin/bash and /bin/sh that is installed, and
// whether it found one.
func (p probe) login() (Shell, bool) {
	sh := p.getenv("SHELL")
	if sh == "" {
		for _, try := range []string{"/bin/bash", "/bin/sh"} {
			if _, err := p.lookPath(try); err == nil {
				sh = try
				break
			}
		}
	}
	if sh == "" {
		return Shell{}, false
	}
	return Shell{ID: loginID, Title: path.Base(sh), Path: sh}, true
}

// Command is the argv for a pane on this shell. A WSL pane starts in dir, and leaves it out when dir
// is empty or WSL cannot reach it; every other shell ignores dir, so the caller starts the pane in a
// directory itself, through session.LocalConfig.Dir.
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

// UnixPath translates a Windows path for WSL, turning a drive letter into a mount under /mnt, so
// C:\Workspace is /mnt/c/Workspace. Anything else, a UNC path and a relative path included, gives "".
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
