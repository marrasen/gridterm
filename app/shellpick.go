package app

import (
	"slices"
	"strings"

	shellfind "github.com/marrasen/kakel/shells"
)

// Choosing the shell: the shells on this machine are
// found as the window opens, and where there is more than one, as on
// Windows with its Command Prompt, its two PowerShells and its WSL
// distributions, the palette opens a terminal with any of them and
// keeps one as the shell new terminals start.

// ShellChoice is a shell on this machine, as the palette offers it.
type ShellChoice struct {
	ID, Title string
	// Folder is where Windows reaches a WSL distribution's files, empty
	// for any other shell.
	Folder string
}

// Intents for the shell.
type (
	// OpenShellNamed opens a terminal with the shell ID, once.
	OpenShellNamed struct{ ID string }
	// OpenDefaultShell opens a terminal with this machine's default
	// shell, and goes back to it for new terminals.
	OpenDefaultShell struct{}
	// PickShell keeps the shell ID as the one new terminals start, or
	// with ID empty, goes back to the default.
	PickShell struct{ ID string }
)

// findShells finds this machine's shells. A variable, so a test can
// describe a machine.
var findShells = shellfind.Find

// scanShells finds the shells in the background, and publishes them.
func (a *app) scanShells() {
	go func() {
		found, err := findShells()
		a.events <- func() {
			if err != nil {
				a.failed("Couldn't list the shells here", err.Error())
			}
			a.found, a.scanned = found, err == nil
			a.st.Shells = nil
			for _, s := range found {
				choice := ShellChoice{ID: s.ID, Title: s.Title}
				if s.Distro != "" {
					choice.Folder = shellfind.WSLRoot(s.Distro)
				}
				a.st.Shells = append(a.st.Shells, choice)
			}
		}
	}()
}

// localShell is the command a new terminal here starts, when the pane
// in front runs no shell to start another of (see likeHere): the shell
// kept, or nil for the user's own.
func (a *app) localShell() []string {
	if a.settings == nil {
		return nil
	}
	id, chosen := a.settings.Shell()
	if !chosen {
		return nil
	}
	argv := a.shellCommand(id)
	if argv == nil && !a.shellGoneSaid {
		// The default shell opens instead, and the user is told once.
		a.shellGoneSaid = true
		a.failed("Shell not found", strings.TrimPrefix(id, "wsl:")+" is no longer installed. The default shell was opened. Choose another in the palette, under Start … in New Terminals.")
	}
	return argv
}

// shellCommand is the command that starts shell id, or nil when this
// machine has no such shell. Before the shells have been looked for,
// a shell is taken as it is named; after, only one that was found.
func (a *app) shellCommand(id string) []string {
	i := slices.IndexFunc(a.found, func(s shellfind.Shell) bool { return s.ID == id })
	if i < 0 {
		if s, ok := shellfind.Named(id); ok && !a.scanned {
			return s.Command("")
		}
		return nil
	}
	return a.found[i].Command("")
}

// pickShell keeps a shell for new terminals.
func (a *app) pickShell(id string) error {
	if a.settings == nil {
		return nil
	}
	a.st.ChosenShell = id
	if id == "" {
		return a.settings.ForgetShell()
	}
	return a.settings.PutShell(id)
}

// likeHere has the next terminal here start the shell the focused pane
// runs, in its folder: another Command Prompt from a Command Prompt, the
// same WSL distribution from a WSL shell. A shell picked for it already,
// a pane on a server, and one running a command rather than a shell
// leave it as it is.
func (a *app) likeHere() {
	id := a.st.Focus
	if a.nextShell != nil || a.machineOf(id) != "" {
		return
	}
	if _, ok := a.commands[id]; ok {
		return
	}
	argv := a.argvs[id]
	if len(argv) == 0 {
		return
	}
	if s, ok := shellfind.Running(a.found, argv); ok {
		a.nextShell = s.Command(a.dirHere())
	}
}

// shellTitle is the title a local pane's program gave, with the
// program's own path, which the Command Prompt and PowerShell name
// themselves by, given as the shell's name: "Command Prompt", and
// "Command Prompt - ping host" while that runs. What Windows puts in
// front for a program run as administrator stays in front.
func (a *app) shellTitle(id, title string) string {
	if a.machineOf(id) != "" {
		return title
	}
	s, ok := shellfind.Running(a.found, a.localArgv(id))
	if !ok || s.Path == "" {
		return title
	}
	admin, rest := "", title
	if r, cut := strings.CutPrefix(title, "Administrator: "); cut {
		admin, rest = "Administrator: ", r
	}
	switch {
	case strings.EqualFold(rest, s.Path):
		return admin + s.Title
	case len(rest) > len(s.Path) && strings.EqualFold(rest[:len(s.Path)], s.Path) && strings.HasPrefix(rest[len(s.Path):], " - "):
		return admin + s.Title + rest[len(s.Path):]
	}
	return title
}

// withoutFolder is argv without the folder a WSL shell was told to start
// in, which is where the pane started, not where it is now: starting it
// again goes to the folder the shell says then, or its home.
func withoutFolder(argv []string) []string {
	if n := len(argv); n >= 2 && argv[n-2] == "--cd" {
		return argv[: n-2 : n-2]
	}
	return argv
}
