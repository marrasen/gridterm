package app

import (
	"errors"
	"slices"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/winkeys"

	"github.com/marrasen/gunim"
)

// The launcher: a key that works from any program, Shift+Win+K unless
// the settings say another, opens a small window over everything that
// finds a machine by its name as it is typed. Enter opens what was
// opened there last, a terminal at first, in the window last worked in
// or a new one; Tab shows the rest of what can be opened there.

// LauncherTopic is the key the launcher's state is published under.
const LauncherTopic = "launcher"

// DefaultLauncherKey is the launcher's key when the settings name none.
const DefaultLauncherKey = "shift+super+k"

// LaunchState is what the launcher shows: the machines, and a count of
// the times it was opened, for it to start afresh each time.
type LaunchState struct {
	Machines []LaunchMachine
	Opened   uint64
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
)

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
	last    map[machines.ID]string
}

// takeLauncherKey takes the launcher's key from every program, and says
// so when another program has it.
func (a *app) takeLauncherKey() {
	if a.hotKeys == nil {
		return
	}
	if a.launch.release != nil {
		a.launch.release()
		a.launch.release = nil
	}
	written := DefaultLauncherKey
	if a.settings != nil {
		if k := a.settings.LauncherKey(); k != "" {
			written = k
		}
	}
	if written == "none" {
		return
	}
	press, err := winkeys.Parse(written)
	if err != nil {
		a.failed("Couldn't read the launcher's key", err.Error())
		return
	}
	release, err := a.hotKeys(gunim.HotKey{Key: press.Key, Mods: press.Mods}, func() {
		a.events <- a.openLauncher
	})
	switch {
	case errors.Is(err, gunim.ErrNoHotKeys):
	case errors.Is(err, gunim.ErrHotKeyTaken):
		a.failed("Couldn't take "+written+" for the launcher",
			"Another program has it. Choose another key with Options › Launcher Key.")
	case err != nil:
		a.failed("Couldn't take "+written+" for the launcher", err.Error())
	default:
		a.launch.release = release
	}
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
			a.launch.c = &c
			a.launch.opened++
			a.publishLauncher()
			c.ToFront()
			go func() {
				for env := range c.Intents() {
					a.events <- func() { a.handleLaunch(env.Intent) }
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
	_ = a.launch.c.Publish(LauncherTopic, LaunchState{Machines: a.launchMachines(), Opened: a.launch.opened})
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
		return append(out, LaunchAction{ID: "log", Title: "Connection Log"})
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

// handleLaunch carries out what the launcher asks.
func (a *app) handleLaunch(in gunim.Intent) {
	switch in := in.(type) {
	case CloseLauncher:
		a.closeLauncher()
	case Launch:
		a.closeLauncher()
		if a.launch.last == nil {
			a.launch.last = map[machines.ID]string{}
		}
		a.launch.last[in.Machine] = in.Action
		a.toTray(func() { a.launchOn(in) })
	}
}

// launchOn opens what in asks for, in the window in front.
func (a *app) launchOn(in Launch) {
	switch {
	case in.Action == "files":
		a.handle(FilesOn{Machine: in.Machine})
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
		c.Close()
	}
}
