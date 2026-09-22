package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
)

// currentHost returns the machine the user is looking at: the one the
// panel has selected while the panel has the keys, and otherwise the one
// the focused pane is running on.
//
// The panel only counts while it is focused. Its selection outlives
// being looked at -- it is still there when the panel is hidden -- and a
// command that opened a terminal on a machine the user chose ten minutes
// ago would be opening it somewhere they are not looking.
func (a *app) currentHost() string {
	// A menu dropped from a machine's row beats everything else: the
	// user named the machine by clicking it. A menu dropped from a
	// machine of a window taken over is not a name at all, and machine
	// says so: a.current is what answers for those.
	if host, up := a.hostMenus.machine(); up {
		return host
	}
	if a.panel != nil && a.panel.Focused() {
		if e, ok := a.selectedConnection(); ok {
			return e.Host
		}
	}
	if t := a.focusedTerminal(); t != nil {
		if m := a.machines.runningOn(t); m != nil {
			return m.at.name
		}
		if e := a.panes[t]; e != nil {
			return e.Host
		}
	}
	// A file pane is not a terminal, and the one with the keys is on a
	// machine like anything else.
	switch pane := ui.FocusedLeaf(a.root.Widget()).(type) {
	case *files.Pane:
		return a.hostOf(pane.FS())
	case *files.Reader:
		// The machine the file is on, which is the one its row was
		// filed under when it was opened.
		if held := a.readers[pane]; held != nil {
			return held.row.Host
		}
	}
	// Nothing in front to read it off, so it is wherever a new pane
	// would open: the machine -ssh named, or this one.
	return a.newPaneHost()
}

// current is the machine the user is looking at, as facts rather than as
// a name.
//
// A menu dropped from a machine of a window taken over is the one case a
// name cannot carry: that machine is reached through the window, and this
// window holds nothing under any name for it. So it comes back as
// hostFar, which every command but Files refuses -- structurally, rather
// than by a name that a saved server could be called as well.
func (a *app) current() hostFacts {
	if key, on := a.hostMenus.farMachine(); on {
		return hostFacts{a: a, name: key.host, kind: hostFar, far: key}
	}
	return a.about(a.currentHost())
}

// here is what the commands that act on the machine in front of the user
// work from: the facts, or the refusal for a machine reached through a
// window taken over.
func (a *app) here() (hostFacts, error) {
	h := a.current()
	if h.kind == hostFar {
		return h, farRefusal(h.far)
	}
	return h, nil
}

// farRefusal says a command cannot act on a machine of a window taken
// over, naming the machine and the window.
//
// One sentence for every command, because there is one reason: the shells
// and the tunnels on that machine belong to the window, and a pane
// reading its files is all this window can open over there.
func farRefusal(key remoteHostKey) error {
	return fmt.Errorf("%s is reached through %s: only its files can be opened from here",
		key.host, key.window.name)
}

// disconnectHere closes the connection to the machine the user is
// looking at, and everything riding on it.
func (a *app) disconnectHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	switch h.kind {
	case hostHere:
		return errors.New("this is the machine gridterm is running on, not one it connected to")
	case hostWindow:
		return a.dropWindow(h.name)
	case hostMachine, hostConnecting:
		return a.dropMachine(h.name)
	}
	// Said rather than done quietly. A command that reports success and
	// changes nothing is how a connection that would not close looked
	// like a window that had stopped listening.
	return fmt.Errorf("nothing is connected to %s", h.name)
}

// openTerminalHere opens another terminal on the machine the user is
// looking at.
func (a *app) openTerminalHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	// The same shell as the pane the user is in, when that is what
	// "here" means. A window whose last pick was PowerShell would
	// otherwise answer a cmd.exe pane with PowerShell, which is not
	// what "another one of these" means.
	if argv := a.shellLikeThePaneHere(h); argv != nil {
		dir := a.dirOfThePaneHere()
		return a.openPaneWith(func() (*term.Terminal, error) {
			return a.localTerminalIn(argv, dir)
		})
	}
	return a.openTerminalOn(h.name, nil)
}

// dirOfThePaneHere is where the focused pane says it is, for a new
// terminal opened beside it. Empty when nothing said.
//
// A shell only says through OSC 7, which most send once they are set
// up for it and none send by default on Windows. Empty means the new
// pane starts where a new pane starts, which is what it did before
// any of this.
//
// The name in the URL has to be this machine. A shell that has been
// ssh'd somewhere from inside the pane goes on sending OSC 7, and the
// path it sends is a path over there.
func (a *app) dirOfThePaneHere() string {
	t := a.focusedTerminal()
	if t == nil {
		return ""
	}
	dir, host := t.Dir()
	if dir == "" || !isThisMachine(host) {
		return ""
	}
	return dir
}

// isThisMachine reports whether the name in an OSC 7 URL is the
// machine gridterm is running on. An empty name means the sender did
// not say, which every shell on the local machine does.
func isThisMachine(host string) bool {
	switch strings.ToLower(host) {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	name, err := os.Hostname()
	if err != nil {
		return false
	}
	// The short name as well: a shell sends what hostname gives it,
	// which may or may not carry the domain.
	short, _, _ := strings.Cut(strings.ToLower(name), ".")
	asked, _, _ := strings.Cut(strings.ToLower(host), ".")
	return asked == short
}

// shellLikeThePaneHere is the shell the focused pane is running, when
// it is a terminal on this machine and the user is in it.
//
// Nil for everything else: for a pane on another machine, where the
// shell is that machine's business; for a command pane, because
// another one of those is a rerun rather than a new terminal; and
// while the sidebar has the keys, because then the user named a
// machine rather than pointing at a pane.
func (a *app) shellLikeThePaneHere(h hostFacts) []string {
	if h.kind != hostHere {
		return nil
	}
	if a.panel != nil && a.panel.Focused() {
		return nil
	}
	if _, up := a.hostMenus.machine(); up {
		return nil
	}
	t := a.focusedTerminal()
	if t == nil {
		return nil
	}
	if e := a.panes[t]; e == nil || e.Kind != conns.Terminal || e.Host != conns.Local {
		return nil
	}
	was := a.started[t]
	if was == nil || len(was.argv) == 0 {
		return nil
	}
	return slices.Clone(was.argv)
}

// openCommandHere asks for a command to run on the machine the user is
// looking at, connecting to it if the connection has since been closed.
func (a *app) openCommandHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	if h.kind == hostWindow || h.kind == hostSavedWindow {
		return fmt.Errorf(
			"%s is a gridterm window: it has no shell, so there is nothing to run a command in. "+
				"Open a terminal on it instead.",
			h.name)
	}
	a.askCommandOn(h.name, nil)
	return nil
}

// askCommandOn asks for a command to run on a machine and puts the pane
// it opens at a spot, or on the stage when at is nil.
func (a *app) askCommandOn(host string, at *spot) {
	where := groupName(host)
	f := a.newForm(dlgRunCommandOn + where)
	what := f.AddField(fldCommand, a.newField("", 0))
	what.Options = a.saved.lines()
	in := f.AddField(fldDirectory, a.newField("Optional", 0))
	keep := f.AddTick(fldSaveCommand, false)
	// picked is the saved command taken off the list, and "" until one
	// is. Only that one can be forgotten here, so a command typed out by
	// hand is never thrown away by a box the user did not tick.
	picked := ""
	// On a pick and not on a keystroke, or a saved command that is the
	// start of a longer one would tick the box half way through typing
	// it.
	what.OnPick = func(text string) {
		saved, have := a.saved.find(commandLine(text))
		if !have {
			return
		}
		picked = saved.Line
		keep.SetOn(true)
		// Only from the machine it was saved on, and only over an empty
		// field: a path belongs to its machine, and what the user typed
		// is theirs.
		if saved.Dir != "" && saved.Host == host && strings.TrimSpace(in.Text()) == "" {
			in.SetText(saved.Dir)
		}
	}
	f.AddButton(ui.Button{Title: btnRun, Do: func() error {
		command := strings.Fields(what.Text())
		if len(command) == 0 {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return errors.New("Enter a command")
		}
		dir := strings.TrimSpace(in.Text())
		line := commandLine(what.Text())
		switch {
		case keep.On():
			cmd := settings.SavedCommand{Line: line, Dir: dir, Host: host}
			if err := a.saved.keep(cmd); err != nil {
				return err
			}
		case picked == line:
			// Taken off the list and then unticked, which is how the
			// user says to forget it.
			if err := a.saved.forget(line); err != nil {
				return err
			}
		}
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(func() {
			if err := a.runCommandOn(host, command, dir, at); err != nil {
				a.reportError("Could not run the command on "+where, err)
			}
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, nil)
}

// runCommandOn opens a pane running a command, on this machine or on one
// the window connects to. dir is where it runs, and empty is wherever
// the shell lands.
func (a *app) runCommandOn(host string, command []string, dir string, at *spot) error {
	if host == conns.Local {
		return a.runCommandHere(command, dir, at)
	}
	return a.openOn(host, command, dir, at)
}

// runSaved runs a command the user kept, on the machine it was kept
// on and in the directory it was kept with.
//
// On that machine rather than the one in front of the user: the
// command was saved with a machine, and a directory belongs to the
// machine it was typed on.
func (a *app) runSaved(cmd settings.SavedCommand) error {
	command := strings.Fields(cmd.Line)
	if len(command) == 0 {
		return fmt.Errorf("the command kept as %q has nothing to run", cmd.Line)
	}
	return a.runCommandOn(cmd.Host, command, cmd.Dir, nil)
}

// showConnLogHere opens the account of how the machine the user is
// looking at was reached, or is being reached.
func (a *app) showConnLogHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	return a.showAccount(h.name, h.log())
}

// connLogCommand is the command that opens the account of how a machine
// was reached, and connLogName is what every line offering it says.
//
// One string, because three said it: the command, the row's menu, and
// the summary the pane writes telling the user where the account went.
// A copy in the prose is the one that goes stale without anything
// failing, and then points at a line that is not there any more.
const (
	connLogCommand = "conn.log"
	connLogName    = "Connection Log"
)

// openFilesHere puts another pane in the file manager, on the machine
// the user is looking at.
func (a *app) openFilesHere() error {
	h := a.current()
	// A machine of a window taken over: the pane reads it through the
	// window, which is the only way this one can reach it. The one
	// command hostFar does not refuse.
	if h.kind == hostFar {
		return a.openFilesFar(h.far)
	}
	return a.openFilesOn(h.name)
}

// tunnelHost returns the machine to run a tunnel over: the one the user
// is looking at, if it is one gridterm has a connection to.
func (a *app) tunnelHost() (string, error) {
	on, err := a.here()
	if err != nil {
		return "", err
	}
	if on.machine == nil {
		return "", fmt.Errorf(
			"a tunnel runs over a connection to another machine, and %s is not one",
			groupName(on.name))
	}
	return on.name, nil
}

// openTunnelHere asks for a port to forward over the connection to the
// machine the user is looking at.
func (a *app) openTunnelHere() error {
	host, err := a.tunnelHost()
	if err != nil {
		return err
	}

	f := a.newForm(dlgTunnelVia + host)
	// The direction is a field rather than a pair of buttons. Local and
	// Remote are the words every SSH client uses, the two answers say
	// which machine listens, and the paragraph that had to explain two
	// buttons is not needed. It also leaves this dialog ending the same
	// way as the SOCKS one: Open, then Cancel.
	local, remoteWay := tunnelWays(host)
	way := f.AddField(fldDirection, a.newField("", 0))
	way.Options = []string{local, remoteWay}
	way.SetText(local)
	listen := f.AddField(fldListenOn, a.newField("[address:]port", 0))
	target := f.AddField(fldForwardTo, a.newField("host:port", 0))
	keep := f.AddTick(fldSaveTunnel, false)
	// The ones kept for this machine, so one is a key away rather than
	// two ports to remember.
	listen.Options = a.savedTuns.listenOn(host)
	listen.OnPick = func(text string) {
		saved, have := a.savedTuns.findListen(host, text)
		if !have {
			return
		}
		target.SetText(saved.Target)
		// The direction it was saved with too: a saved tunnel is the
		// whole tunnel, and one that came back pointing the other way
		// would be a different tunnel under the same name.
		if t, err := asTunnel(saved); err == nil && t.Kind == remote.RemoteForward {
			way.SetText(remoteWay)
		} else {
			way.SetText(local)
		}
		keep.SetOn(true)
	}

	f.AddButton(ui.Button{Title: btnOpen, Do: func() error {
		kind := remote.LocalForward
		if way.Text() == remoteWay {
			kind = remote.RemoteForward
		}
		t := remote.Tunnel{
			Kind:   kind,
			Listen: strings.TrimSpace(listen.Text()),
			Target: strings.TrimSpace(target.Text()),
		}
		if err := t.Validate(); err != nil {
			// Returned rather than shown here, so the dialog stays
			// open with what was typed still there to correct.
			return err
		}
		if err := a.keepOrForgetTunnel(keep.On(), host, t); err != nil {
			return err
		}
		// Not from here: this dialog closes as soon as this returns,
		// and closing one takes anything stacked on top of it.
		a.pump.post(func() { a.confirmTunnel(host, t) })
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, nil)
	return nil
}

// tunnelWays are the two directions as the Direction field offers them:
// which end listens, in the words every SSH client uses for it.
func tunnelWays(host string) (local, remote string) {
	return "Local — listen here", "Remote — listen on " + host
}

// openSocksHere asks for a SOCKS5 proxy over the connection to the
// machine the user is looking at.
//
// A dialog of its own rather than a third button on the tunnel one: it
// takes no address to reach, because every stream through it says where
// it is going.
func (a *app) openSocksHere() error {
	host, err := a.tunnelHost()
	if err != nil {
		return err
	}

	f := a.newForm(dlgSocksVia + host)
	f.Lines = wrapLines("A local SOCKS port. Connections go out from "+host+".",
		errorLineWidth)
	listen := f.AddField(fldListenOn, a.newField("[address:]port", 0))
	f.AddButton(ui.Button{Title: btnOpen, Do: func() error {
		t := remote.Tunnel{
			Kind:   remote.DynamicForward,
			Listen: strings.TrimSpace(listen.Text()),
		}
		if err := t.Validate(); err != nil {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return err
		}
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(func() { a.confirmTunnel(host, t) })
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, nil)
	return nil
}
