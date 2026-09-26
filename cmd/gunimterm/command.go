package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/settings"
)

// Running one command in a pane of its own, as gridterm does: on this
// machine or on a server, in a folder or where the login lands. When it
// finishes, the pane asks whether to run it again. A command can be
// kept, to be run again from the palette.

// RunCommand runs Line in a pane of its own on Machine, in Dir when
// given, and keeps it for next time with Keep.
type RunCommand struct {
	Machine, Line, Dir string
	Keep               bool
}

// mostSavedCommands is how many commands are kept, as gridterm keeps
// them.
const mostSavedCommands = 50

// command is what a command pane runs, to run it again.
type command struct {
	argv []string
	dir  string
}

// runCommand starts a command in a pane of its own.
func (a *app) runCommand(in RunCommand) error {
	argv := strings.Fields(in.Line)
	if len(argv) == 0 {
		return errors.New("there is no command to run")
	}
	if _, ok := a.windows[in.Machine]; ok {
		return fmt.Errorf("%s is a gunimterm window, which has no shell to run a command in: open a terminal on it instead", in.Machine)
	}
	if in.Keep && a.settings != nil {
		saved := settings.SavedCommand{Line: strings.Join(argv, " "), Dir: strings.TrimSpace(in.Dir), Host: in.Machine, HostID: a.serverID(in.Machine)}
		if err := a.settings.KeepCommand(saved, mostSavedCommands); err != nil {
			a.notify("Couldn't keep the command for next time", err.Error(), "")
		}
		a.st.SavedCommands = a.settings.Commands()
	}
	a.next++
	id := "p" + strconv.Itoa(a.next)
	cmd := command{argv: argv, dir: strings.TrimSpace(in.Dir)}
	a.commands[id] = cmd
	if in.Machine == "" {
		a.argvs[id] = argv
	}
	title := strings.Join(argv, " ")
	return a.startCommand(in.Machine, cmd, func(sess session.Session) {
		a.addPane(Pane{ID: id, Title: title, Machine: in.Machine, Command: true}, openShell(sess, a.palette, a.withLinks(a.hooks(id), in.Machine)), placement{})
	})
}

// startCommand starts cmd on machine and hands its session to then, on
// the program's goroutine.
func (a *app) startCommand(machine string, cmd command, then func(session.Session)) error {
	if machine == "" {
		sess, err := a.startLocalSession(cmd.argv, cmd.dir, shellCols, shellRows, false)
		if err != nil {
			return err
		}
		then(sess)
		return nil
	}
	conn, ok := a.conns[machine]
	if !ok {
		return fmt.Errorf("this window is not connected to %s", machine)
	}
	go func() {
		sess, err := conn.Shell(a.ctx, remote.ShellConfig{Command: cmd.argv, Dir: cmd.dir, Cols: shellCols, Rows: shellRows})
		a.events <- func() {
			if err != nil {
				a.notify("Couldn't run "+strings.Join(cmd.argv, " ")+" on "+machine, err.Error(), "")
				return
			}
			then(sess)
		}
	}()
	return nil
}

// runSavedCommand runs a command kept from before, on the machine it
// was kept for, by that machine's name now.
func (a *app) runSavedCommand(saved settings.SavedCommand) error {
	machine := saved.Host
	for _, h := range a.st.Saved {
		if saved.HostID != "" && h.ID == saved.HostID {
			machine = h.Name
		}
	}
	return a.runCommand(RunCommand{Machine: machine, Line: saved.Line, Dir: saved.Dir})
}

// runAgain runs a finished command pane's command again, in the same
// pane.
func (a *app) runAgain(id string, cmd command) error {
	t := a.terminal(id)
	if t == nil {
		return errors.New("that pane is no longer open")
	}
	return a.startCommand(a.machineOf(id), cmd, func(sess session.Session) {
		if err := a.restarted(id, t, sess); err != nil {
			a.notify("Couldn't run it again", err.Error(), "")
		}
	})
}
