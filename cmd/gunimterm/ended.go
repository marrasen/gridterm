package main

import (
	"errors"
	"os/exec"
	"strconv"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/session"
	uiterm "github.com/marrasen/gridterm/ui/term"
)

// A terminal pane whose program ends stays open, with what it printed,
// and asks in the pane whether to start the program again or close the
// pane, as gridterm does. Enter closes it, so typing exit and Enter
// still leaves nothing behind.

// paneEnded marks a terminal pane whose program has ended, and asks
// what next. Other panes, which read a log, close.
//
// The terminal says so twice, once as the program's output ends and
// again once its exit status is in.
func (a *app) paneEnded(id string) {
	t := a.terminal(id)
	switch {
	case t == nil:
		a.closePane(id)
		return
	case !t.Exited():
		// A notice from before the pane was started again.
		return
	}
	a.setPane(id, func(p *Pane) { p.Ended = true })
	start := "Start Again"
	question := "The shell has finished."
	if a.machineOf(id) != "" {
		start, question = "Reconnect", "Connection closed."
	}
	if status, known := exitStatus(t.Ending()); known && status != 0 {
		question += " Exit " + strconv.Itoa(status) + "."
	}
	// Told twice: as the output ends, and again once the program's
	// status is in. The second asks again only when it knows more.
	if t.Asking() == question {
		return
	}
	// The choices are made on the window's goroutine, where the pane
	// takes its keys, and carried out on the program's.
	post := func(f func()) { go func() { a.events <- f }() }
	t.Ask(question,
		uiterm.Choice{Label: start, Do: func() error {
			post(func() {
				if err := a.startAgain(id); err != nil {
					a.notify("Couldn't start it again", err.Error(), "")
				}
			})
			return nil
		}},
		uiterm.Choice{Label: "Close", Default: true, Do: func() error {
			post(func() { a.closePane(id) })
			return nil
		}})
}

// startAgain starts a new program in a pane whose program has ended:
// the user's shell here, or a shell over the connection to its server,
// which has to still be open.
func (a *app) startAgain(id string) error {
	t := a.terminal(id)
	if t == nil {
		return errors.New("that pane is no longer open")
	}
	if !t.Exited() {
		return nil
	}
	machine := a.machineOf(id)
	if machine == "" {
		sess, err := session.StartLocal(session.LocalConfig{Cols: t.Size().Cols, Rows: t.Size().Rows})
		if err != nil {
			return err
		}
		return a.restarted(id, t, sess)
	}
	size := t.Size()
	if w, ok := a.windows[machine]; ok {
		go func() {
			sess, err := w.win.Open(size.Cols, size.Rows, func(serve.Attached) {})
			a.events <- func() {
				if err == nil {
					err = a.restarted(id, t, sess)
				}
				if err != nil {
					a.notify("Couldn't start it again", err.Error(), "")
				}
			}
		}()
		return nil
	}
	conn, ok := a.conns[machine]
	if !ok {
		return errors.New("this window is not connected to " + machine + " any more. Connect to it again, then start the pane again")
	}
	go func() {
		sess, err := conn.Shell(a.ctx, remote.ShellConfig{Cols: size.Cols, Rows: size.Rows})
		a.events <- func() {
			if err == nil {
				err = a.restarted(id, t, sess)
			}
			if err != nil {
				a.notify("Couldn't start it again", err.Error(), "")
			}
		}
	}()
	return nil
}

// restarted puts a new session in a pane.
func (a *app) restarted(id string, t *uiterm.Terminal, sess session.Session) error {
	if err := t.Restart(sess); err != nil {
		return errors.Join(err, sess.Close())
	}
	a.setPane(id, func(p *Pane) { p.Ended = false })
	return nil
}

// setPane changes a pane's row.
func (a *app) setPane(id string, change func(*Pane)) {
	for i := range a.st.Panes {
		if a.st.Panes[i].ID == id {
			change(&a.st.Panes[i])
		}
	}
}

// exitStatus is the status a program ended with, and whether it is
// known.
func exitStatus(why error, over bool) (int, bool) {
	switch {
	case !over:
		return 0, false
	case why == nil:
		return 0, true
	}
	if far, ok := errors.AsType[*ssh.ExitError](why); ok {
		return far.ExitStatus(), true
	}
	if here, ok := errors.AsType[*exec.ExitError](why); ok && here.ExitCode() >= 0 {
		return here.ExitCode(), true
	}
	return 0, false
}
