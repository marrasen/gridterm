package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/settings"
)

// Running one command in a pane of its own: on this
// machine or on a server, in a folder or where the login lands. When it
// finishes, the pane asks whether to run it again. A command can be
// kept, to be run again from the palette.

// RunCommand runs Line in a pane of its own on Machine, in Dir when
// given, and keeps it for next time with Keep.
type RunCommand struct {
	Machine   machines.ID
	Line, Dir string
	Keep      bool
	// Beside puts its pane in a split beside that pane, below it with
	// Vertical, rather than on a stage of its own.
	Beside   string
	Vertical bool
	// Instead runs it in the place of that split's chooser.
	Instead string
	// Forget is a saved command picked and then unticked, which is how
	// the user says to forget it.
	Forget string
}

// mostSavedCommands is how many commands are kept.
const mostSavedCommands = 50

// command is what a command pane runs, to run it again.
type command struct {
	argv []string
	dir  string
}

// runCommand starts a command in a pane of its own.
func (a *app) runCommand(in RunCommand) error { return a.runCommandThen(in, nil) }

// runCommandThen is runCommand, telling then the pane it opened, or why
// it could not, once it has; one that fails at once is only returned.
// then runs on the program's goroutine, and may be nil.
func (a *app) runCommandThen(in RunCommand, then func(id string, err error)) error {
	if then == nil {
		then = func(string, error) {}
	}
	argv := strings.Fields(in.Line)
	if len(argv) == 0 {
		return errors.New("there is no command to run")
	}
	through, key, far := in.Machine.Far()
	if !far {
		through = in.Machine
	}
	if !far && a.machines.Get(in.Machine).Window == nil && a.machines.IsWindow(in.Machine) {
		return fmt.Errorf("%s is a kakel window that is not connected: connect to it first", a.machines.Name(in.Machine))
	}
	if in.Forget != "" && !in.Keep && a.settings != nil {
		if err := a.settings.DropCommand(in.Forget); err != nil {
			a.failed("Couldn't forget the command", err.Error())
		}
		a.st.SavedCommands = a.settings.Commands()
	}
	if in.Keep && a.settings != nil {
		saved := settings.SavedCommand{Line: strings.Join(argv, " "), Dir: strings.TrimSpace(in.Dir), Host: a.keptAs(in.Machine), HostID: a.serverID(in.Machine)}
		if err := a.settings.KeepCommand(saved, mostSavedCommands); err != nil {
			// Not run: asked to be kept, it would be lost once it ended.
			return fmt.Errorf("couldn't save the command, so it was not run: %w", err)
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
	if a.machines.Get(through).Window != nil {
		// The window runs it, in a pane of its own, which this one
		// watches: its own machine's, or one it reaches.
		return a.openThrough(through, key, cmd, id, title, Placement{Beside: in.Beside, Vertical: in.Vertical, Instead: in.Instead}, then)
	}
	return a.startCommand(in.Machine, cmd, commandStart{
		then: func(sess session.Session) {
			a.addPane(Pane{ID: id, Title: title, Machine: in.Machine, Command: true}, screen.Open(sess, a.palette, a.withLinks(a.hooks(id), id, in.Machine)), Placement{Beside: in.Beside, Vertical: in.Vertical, Instead: in.Instead})
			then(id, nil)
		},
		failed: func() { then("", errors.New("could not run "+title)) },
	})
}

// commandStart is what is to be done with a command started: then
// takes its session. still, when set, says whether it is still wanted,
// looked at once there is a connection, and failed, when set, runs when
// it could not be started after startCommand returned.
type commandStart struct {
	then   func(session.Session)
	still  func() bool
	failed func()
}

func (s commandStart) wanted() bool { return s.still == nil || s.still() }

func (s commandStart) fail() {
	if s.failed != nil {
		s.failed()
	}
}

// startCommand starts cmd on machine and hands its session to s.then,
// on the program's goroutine. A saved server not connected is connected
// to first.
func (a *app) startCommand(machine machines.ID, cmd command, s commandStart) error {
	line := strings.Join(cmd.argv, " ")
	if machine == "" {
		sess, err := a.startLocalSession(cmd.argv, cmd.dir, screen.Cols, screen.Rows, false)
		if err != nil {
			return err
		}
		s.then(sess)
		return nil
	}
	conn, ok, err := a.connOf(machine)
	if err != nil {
		return err
	}
	if _, _, far := machine.Far(); !ok && !far {
		// Not connected: connected to first, as a terminal there is.
		return a.dialAgain(machine, func(err error) {
			if err != nil {
				s.fail()
				return
			}
			if a.machines.Get(machine).Conn == nil {
				// Connected, but by another name than this one: said,
				// rather than connected to again and again.
				a.failed("Couldn't run "+line+" on "+a.machines.Name(machine), "The connection was made under another name. Open a terminal on it from the sidebar.")
				s.fail()
				return
			}
			if err := a.startCommand(machine, cmd, s); err != nil {
				a.failed("Couldn't run "+line+" on "+a.machines.Name(machine), err.Error())
				a.problem()
				s.fail()
			}
		})
	}
	if !ok {
		return fmt.Errorf("this window is not connected to %s", a.machines.Name(machine))
	}
	if !s.wanted() {
		return nil
	}
	a.starting++
	go func() {
		cfg := a.shellConfig(machine, screen.Cols, screen.Rows)
		cfg.Command, cfg.Dir = cmd.argv, cmd.dir
		sess, err := conn.Shell(a.ctx, cfg)
		a.events <- func() {
			a.starting--
			if err != nil {
				if !s.wanted() {
					return
				}
				a.failed("Couldn't run "+line+" on "+a.machines.Name(machine), err.Error())
				a.problem()
				s.fail()
				a.stayIfEmpty()
				return
			}
			if !s.wanted() {
				_ = sess.Close()
				return
			}
			s.then(sess)
		}
	}()
	return nil
}

// runSavedCommand runs a command kept from before, on the machine it
// was kept for, by that machine's name now.
func (a *app) runSavedCommand(saved settings.SavedCommand) error {
	machine, err := a.machineNow(saved.Host, saved.HostID)
	if err != nil {
		return err
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
	// The pane may close while a connection is made for it: then the
	// command is not run. One that cannot be run asks again.
	still := func() bool { return a.terminal(id) == t }
	// On a server, not started with no connection there is the
	// connection not made, and its question says so. One refused over a
	// connection that is there says its own reason.
	machine := a.machineOf(id)
	notRun := func() {
		if m := a.machines.Get(machine); machine != machines.Local && m.Conn == nil && m.Window == nil {
			a.notRun[id] = true
		}
		a.paneEnded(id)
	}
	err := a.startCommand(machine, cmd, commandStart{
		then: func(sess session.Session) {
			if err := a.restarted(id, t, sess); err != nil {
				a.failed("Couldn't run it again", err.Error())
			}
		},
		still: still,
		failed: func() {
			if still() {
				notRun()
			}
		},
	})
	if err != nil && still() {
		// It asks again, having said why.
		notRun()
	}
	return err
}

// shellConfig is how a shell on machine starts, at cols by rows: as its
// saved server says, with the TERM it names, or as any other.
func (a *app) shellConfig(machine machines.ID, cols, rows int) remote.ShellConfig {
	if h, ok := a.machines.Saved(machine); ok {
		return h.Shell(cols, rows)
	}
	return remote.ShellConfig{Cols: cols, Rows: rows}
}
