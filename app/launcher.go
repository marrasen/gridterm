package app

import (
	"errors"
	"runtime"
	"slices"
	"strings"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/winkeys"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
)

// The launcher: a key that works from any program, Shift+Win+K on
// Windows and Ctrl+Alt+K elsewhere unless the settings say another, opens a small window over everything that
// finds a machine by its name as it is typed. Enter opens what was
// opened there last, a terminal at first, in the window last worked in
// or a new one; Tab shows the rest of what can be opened there.

// LauncherTopic is the key the launcher's state is published under.
const LauncherTopic = "launcher"

// DefaultLauncherKey is the launcher's key when the settings name none:
// Shift+Win+K on Windows, and Ctrl+Alt+K elsewhere, where the window
// manager keeps the keys with Super for itself, as Cinnamon and GNOME
// do.
var DefaultLauncherKey = map[bool]string{true: "shift+super+k", false: "ctrl+alt+k"}[runtime.GOOS == "windows"]

// LaunchState is what the launcher shows: the machines, the things it
// finds besides them once something is typed, and a count of the times
// it was opened, for it to start afresh each time.
type LaunchState struct {
	Machines []LaunchMachine
	Things   []LaunchThing
	Opened   uint64
}

// LaunchThing is one thing the launcher opens straight from what is
// typed: a shell here, files somewhere, a saved command, a window. Note
// says where, Also are other words it is found by, and Kind picks its
// icon.
type LaunchThing struct {
	Title, Note string
	Also        []string
	Kind        string
	Machine     machines.ID
	Action      string
}

// LaunchMachine is a machine the launcher offers, what can be opened on
// it, and which of that Enter opens.
type LaunchMachine struct {
	ID      machines.ID
	Name    string
	Note    string
	Actions []LaunchAction
	Default int
}

// LaunchAction is a thing the launcher can open on a machine.
type LaunchAction struct {
	ID, Title string
}

// Intents from the launcher.
type (
	// Launch opens Action on Machine.
	Launch struct {
		Machine machines.ID
		Action  string
	}
	// CloseLauncher closes the launcher, as Escape does, or a click
	// elsewhere.
	CloseLauncher struct{}
	// OpenLauncher opens the launcher, as its key does.
	OpenLauncher struct{}
	// SetLauncherKey makes Key the launcher's, written as a shortcut
	// is, "none" for none, or "" for the one kakel comes with.
	SetLauncherKey struct{ Key string }
)

// setLauncherKey takes the launcher's key, and keeps it once it has:
// one that cannot be taken leaves the one held before as it was.
func (a *app) setLauncherKey(key string) error {
	key = strings.TrimSpace(key)
	switch {
	case isNone(key):
		key = "none"
		a.letLauncherKeyGo()
	default:
		written := key
		if written == "" {
			written = DefaultLauncherKey
		}
		k, err := LauncherHotKey(written)
		if err != nil {
			return err
		}
		held := a.launch.release != nil && a.launch.key == k
		if a.hotKeys != nil && a.opts.OneOfMany() && !held {
			if err := a.takeKey(k); err != nil {
				return err
			}
		}
	}
	if a.settings != nil {
		return a.settings.PutLauncherKey(key)
	}
	return nil
}

// launcherKey is the launcher's key, as written.
func (a *app) launcherKey() string {
	if a.settings != nil {
		if k := a.settings.LauncherKey(); k != "" {
			return k
		}
	}
	return DefaultLauncherKey
}

// LauncherOpener opens the launcher's window, with its view mounted.
type LauncherOpener func() (gunim.Client, error)

// HotKeys takes a key from every program: see gunim's App.RegisterHotKey.
type HotKeys func(k gunim.HotKey, fn func()) (release func(), err error)

// launchState is the launcher as it is: its window while it is open,
// the key that opens it, and what was opened on each machine last.
type launchState struct {
	c       *gunim.Client
	opening bool
	opened  uint64
	release func()
	key     gunim.HotKey
	last    map[machines.ID]string
}

// takeLauncherKey takes the launcher's key from every program, and says
// so when another program has it. A kakel of its own, as for
// screenshots, leaves it to the one running.
func (a *app) takeLauncherKey() {
	if a.hotKeys == nil || !a.opts.OneOfMany() {
		return
	}
	written := a.launcherKey()
	if isNone(written) {
		a.letLauncherKeyGo()
		return
	}
	k, err := LauncherHotKey(written)
	if err != nil {
		a.failed("Couldn't read the launcher's key", err.Error())
		return
	}
	a.letLauncherKeyGo()
	if err := a.takeKey(k); err != nil {
		a.failed("Couldn't take "+written+" for the launcher", err.Error())
	}
}

// takeKey takes k for the launcher, which the key held before gives
// way to once it has.
func (a *app) takeKey(k gunim.HotKey) error {
	release, err := a.hotKeys(k, func() { a.events <- a.openLauncher })
	switch {
	case errors.Is(err, gunim.ErrNoHotKeys):
		return nil
	case errors.Is(err, gunim.ErrHotKeyTaken):
		return errors.New("another program has it. Choose another key with Options › Launcher Key")
	case err != nil:
		return err
	}
	a.letLauncherKeyGo()
	a.launch.release, a.launch.key = release, k
	return nil
}

// letLauncherKeyGo lets the launcher's key go.
func (a *app) letLauncherKeyGo() {
	if a.launch.release != nil {
		a.launch.release()
		a.launch.release = nil
	}
}

// isNone reports whether a key written is none.
func isNone(written string) bool { return strings.EqualFold(strings.TrimSpace(written), "none") }

// LauncherHotKey reads a launcher's key as written, refusing one a
// launcher cannot take: one with no Ctrl, Alt or Win, which would take
// a letter from typing everywhere, or a key no platform lends.
func LauncherHotKey(written string) (gunim.HotKey, error) {
	press, err := winkeys.Parse(strings.TrimSpace(written))
	if err != nil {
		return gunim.HotKey{}, err
	}
	if press.Mods&(gi.ModControl|gi.ModAlt|gi.ModSuper) == 0 {
		return gunim.HotKey{}, errors.New("the key needs Ctrl, Alt or Win with it, or it would be taken from typing everywhere")
	}
	k := press.Key
	switch {
	case k >= gi.KeyA && k <= gi.KeyZ, k >= gi.Key0 && k <= gi.Key9, k >= gi.KeyF1 && k <= gi.KeyF12,
		k == gi.KeySpace, k == gi.KeyEnter, k == gi.KeyEscape, k == gi.KeyTab:
	default:
		return gunim.HotKey{}, errors.New("that key cannot be taken from every program; use a letter, a digit, F1 to F12, Space, Enter, Escape or Tab")
	}
	return gunim.HotKey{Key: k, Mods: press.Mods}, nil
}

// openLauncher opens the launcher, or brings it to the front.
func (a *app) openLauncher() {
	if a.gone || a.openLaunch == nil || a.launch.opening {
		return
	}
	if c := a.launch.c; c != nil {
		c.ToFront()
		return
	}
	a.launch.opening = true
	open := a.openLaunch
	go func() {
		c, err := open()
		a.events <- func() {
			a.launch.opening = false
			if err != nil {
				a.failed("Couldn't open the launcher", err.Error())
				return
			}
			if a.gone {
				c.Close()
				return
			}
			a.launch.c = &c
			a.launch.opened++
			_ = c.SetTheme(a.st.Theme)
			a.publishLauncher()
			c.ToFront()
			go func() {
				for env := range c.Intents() {
					a.events <- func() {
						// Only this launcher's, not a closed one's late word.
						if a.launch.c != nil && *a.launch.c == c {
							a.handleLaunch(env.Intent)
						}
					}
				}
				a.events <- func() {
					if a.launch.c != nil && *a.launch.c == c {
						a.launch.c = nil
					}
				}
			}()
		}
	}()
}

// publishLauncher shows the launcher the machines, while it is open.
func (a *app) publishLauncher() {
	if a.launch.c == nil {
		return
	}
	ms := a.launchMachines()
	_ = a.launch.c.Publish(LauncherTopic, LaunchState{Machines: ms, Things: a.launchThings(ms), Opened: a.launch.opened})
}

// launchMachines are the machines the launcher offers: this computer,
// then each saved server, the connected ones first.
func (a *app) launchMachines() []LaunchMachine {
	actions := func(m machines.ID) []LaunchAction {
		out := []LaunchAction{{ID: "terminal", Title: "Terminal"}, {ID: "files", Title: "Files"}}
		if m == machines.Local {
			for _, sh := range a.st.Shells {
				out = append(out, LaunchAction{ID: "shell:" + sh.ID, Title: sh.Title})
			}
			return out
		}
		if slices.Contains(a.machines.Connected(), m) {
			// A log is of a connection: there is none to show before.
			out = append(out, LaunchAction{ID: "log", Title: "Connection Log"})
		}
		return out
	}
	withDefault := func(lm LaunchMachine) LaunchMachine {
		lm.Actions = actions(lm.ID)
		if last := a.launch.last[lm.ID]; last != "" {
			if i := slices.IndexFunc(lm.Actions, func(x LaunchAction) bool { return x.ID == last }); i >= 0 {
				lm.Default = i
			}
		}
		return lm
	}
	out := []LaunchMachine{withDefault(LaunchMachine{ID: machines.Local, Name: "This computer"})}
	connected := a.machines.Connected()
	var rest []LaunchMachine
	for _, h := range a.st.Saved {
		if h.Window {
			continue
		}
		m := machines.ID(h.ID)
		lm := LaunchMachine{ID: m, Name: h.Name}
		if slices.Contains(connected, m) {
			lm.Note = "connected"
			out = append(out, withDefault(lm))
			continue
		}
		rest = append(rest, withDefault(lm))
	}
	return append(out, rest...)
}

// launchThings are what the launcher finds besides the machines: each
// shell here, files and a terminal on each machine, the files of each
// WSL distribution, a connection's log, the saved commands, and kakel's
// own windows.
func (a *app) launchThings(ms []LaunchMachine) []LaunchThing {
	var out []LaunchThing
	for _, sh := range a.st.Shells {
		also := []string{sh.ID, "shell", "terminal"}
		if strings.HasPrefix(sh.ID, "wsl:") {
			also = append(also, "wsl", "linux")
		}
		out = append(out, LaunchThing{Title: sh.Title, Note: "This computer", Also: also, Kind: "terminal", Action: "shell:" + sh.ID})
		if sh.Folder != "" {
			out = append(out, LaunchThing{Title: "Files in " + sh.Title, Note: "This computer", Also: []string{"wsl", "folder", "explorer"},
				Kind: "files", Action: "files:" + sh.Folder})
		}
	}
	for _, m := range ms {
		out = append(out,
			LaunchThing{Title: "Terminal on " + m.Name, Note: m.Note, Also: []string{"shell", "ssh", "console"}, Kind: "terminal", Machine: m.ID, Action: "terminal"},
			LaunchThing{Title: "Files on " + m.Name, Note: m.Note, Also: []string{"folder", "browse", "explorer", "sftp"}, Kind: "files", Machine: m.ID, Action: "files"})
		if slices.ContainsFunc(m.Actions, func(x LaunchAction) bool { return x.ID == "log" }) {
			out = append(out, LaunchThing{Title: "Connection Log of " + m.Name, Note: m.Note, Also: []string{"log", "account"}, Kind: "log", Machine: m.ID, Action: "log"})
		}
	}
	for _, c := range a.st.SavedCommands {
		where := c.Host
		if where == "" {
			where = "This computer"
		}
		out = append(out, LaunchThing{Title: c.Line, Note: where, Also: []string{"run", "command", "saved"}, Kind: "command",
			Machine: machines.ID(c.HostID), Action: "saved:" + c.HostID + "\x00" + c.Line})
	}
	return append(out,
		LaunchThing{Title: "Servers", Note: "kakel", Also: []string{"machines", "connections"}, Kind: "servers", Action: "app:servers"},
		LaunchThing{Title: "Secrets", Note: "kakel", Also: []string{"passwords", "vault"}, Kind: "secrets", Action: "app:secrets"},
		LaunchThing{Title: "New Window", Note: "kakel", Also: []string{"terminal"}, Kind: "window", Action: "app:window"},
	)
}

// handleLaunch carries out what the launcher asks.
func (a *app) handleLaunch(in gunim.Intent) {
	if a.gone {
		a.closeLauncher()
		return
	}
	switch in := in.(type) {
	case CloseLauncher:
		a.closeLauncher()
	case Launch:
		a.closeLauncher()
		if a.launch.last == nil {
			a.launch.last = map[machines.ID]string{}
		}
		// What opens on the machine itself is what Enter opens there
		// next: not a window of kakel's, nor a command.
		if in.Action == "terminal" || in.Action == "files" || in.Action == "log" || strings.HasPrefix(in.Action, "shell:") {
			a.launch.last[in.Machine] = in.Action
		}
		if in.Action == "app:window" {
			// A window of its own: not first in the window worked in.
			a.newWindow(func() { a.handle(NewTerminal{}) })
			return
		}
		a.toTray(func() { a.launchOn(in) })
	}
}

// launchOn opens what in asks for, in the window in front.
func (a *app) launchOn(in Launch) {
	switch {
	case strings.HasPrefix(in.Action, "files:"):
		a.handle(OpenFilesOn{Machine: in.Machine, Path: strings.TrimPrefix(in.Action, "files:")})
	case strings.HasPrefix(in.Action, "saved:"):
		// By what it is, not where it stood: the list moves as
		// commands are run.
		host, line, _ := strings.Cut(strings.TrimPrefix(in.Action, "saved:"), "\x00")
		for _, c := range a.st.SavedCommands {
			if c.HostID == host && c.Line == line {
				a.handle(RunSavedCommand{Saved: c})
				break
			}
		}
	case in.Action == "app:servers":
		a.showServers()
	case in.Action == "app:secrets":
		a.showSecretsPane(func(string) {})
	case in.Action == "app:window":
		a.newWindow(func() { a.handle(NewTerminal{}) })
	case in.Action == "files":
		a.handle(OpenFilesOn{Machine: in.Machine})
	case in.Action == "log":
		a.handle(ShowLog{Machine: in.Machine})
	case len(in.Action) > len("shell:") && in.Action[:len("shell:")] == "shell:":
		a.handle(OpenShellNamed{ID: in.Action[len("shell:"):]})
	default:
		a.handle(OpenOn{Machine: in.Machine})
	}
}

// closeLauncher closes the launcher's window.
func (a *app) closeLauncher() {
	if c := a.launch.c; c != nil {
		a.launch.c = nil
		// Shrinking and fading as it goes, as kakel's windows do; at
		// once only as kakel leaves, with nothing to wait for it.
		if a.gone {
			c.Close()
		} else {
			c.Leave()
		}
	}
}
