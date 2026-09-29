package main

import (
	"errors"
	"log"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/settings"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// A terminal pane whose program ends stays open, with what it printed,
// and asks in the pane whether to start the program again or close the
// pane. Enter closes it, so typing exit and Enter
// still leaves nothing behind.

// paneEnded marks a terminal pane whose program has ended, and asks
// what next. Other panes, which read a log, close.
//
// The terminal says so twice, once as the program's output ends and
// again once its exit status is in.
func (a *app) paneEnded(id string) {
	a.endings[id]++
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
	status, known := exitStatus(t.Ending())
	if known && status != 0 {
		question += " Exit " + strconv.Itoa(status) + "."
	}
	if cmd, ok := a.commands[id]; ok {
		start, question = "Run Again", strings.Join(cmd.argv, " ")+" has stopped. Run it again?"
		if known {
			question = strings.Join(cmd.argv, " ") + " finished. Exit " + strconv.Itoa(status) + ". Run it again?"
		}
	}
	// Told twice: as the output ends, and again once the program's
	// status is in. The second asks again only when it knows more.
	if t.Asking() == question {
		return
	}
	// Ended out of sight, with its status in: an echo says how it went.
	if known && a.st.Focus != id {
		if status == 0 {
			a.done()
		} else {
			a.problem()
		}
	}
	// The choices are made on the window's goroutine, where the pane
	// takes its keys, and carried out on the program's.
	post := func(f func()) { go func() { a.events <- f }() }
	t.Ask(question,
		uiterm.Choice{Label: start, Do: func() error {
			post(func() {
				if err := a.startAgain(id); err != nil {
					a.failed("Couldn't start it again", err.Error())
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
	if cmd, ok := a.commands[id]; ok {
		return a.runAgain(id, cmd)
	}
	machine := a.machineOf(id)
	if machine == "" {
		argv := a.argvs[id]
		if argv == nil {
			argv = a.localShell()
		}
		sess, err := a.startLocalSession(argv, "", t.Size().Cols, t.Size().Rows, true)
		if err != nil {
			return err
		}
		return a.restarted(id, t, sess)
	}
	size := t.Size()
	if w, ok := a.windows[machine]; ok {
		// The window started it: it is asked to start it again, in its
		// own pane, what ran there, and the pane here watches that as
		// before. A window that cannot, or a pane it no longer knows,
		// gets a shell of its own there.
		farID := ""
		for fid, pane := range w.bound {
			if pane == id {
				farID = fid
			}
		}
		go func() {
			var sess session.Session
			var err error
			if farID != "" {
				// Ended, it is not listed there, and is asked for by the
				// ID the window gave it; one on a machine beyond it is.
				open, ok := w.win.OpenNamed(farID)
				if !ok {
					open = serve.Open{ID: farID, Kind: "Terminal"}
				}
				err = w.win.StartAgain(serve.Attached{ID: open.ID, Host: open.Key(), Kind: open.Kind})
				if err == nil {
					sess, err = w.win.Attach(open, size.Cols, size.Rows)
				}
				// A window that cannot, or one that closed it: a shell of
				// its own there, in its place.
				if errors.Is(err, serve.ErrCannotStartAgain) || errors.Is(err, serve.ErrNotOpen) {
					err = nil
				}
			}
			if sess == nil && err == nil {
				moved := farID != "" && a.farHostOf(id) != ""
				sess, err = w.win.Open(size.Cols, size.Rows, func(n serve.Attached) {
					if moved {
						go func() { a.events <- func() { a.onWindowsOwn(id) } }()
					}
					go func() { a.events <- func() { a.bindFar(w, n.ID, id) } }()
				})
			}
			a.events <- func() {
				if err == nil {
					err = a.restarted(id, t, sess)
				}
				if err != nil {
					a.failed("Couldn't start it again", err.Error())
					// The question goes back up, to be answered again.
					if a.terminal(id) == t {
						a.paneEnded(id)
					}
				}
			}
		}()
		return nil
	}
	conn, ok, err := a.connOf(machine)
	if err != nil {
		// Said, and asked again once it can be.
		a.paneEnded(id)
		return err
	}
	if !ok {
		// The connection has gone: dial it again, and
		// start the pane once it is back.
		return a.dialAgain(machine, func(err error) {
			if err == nil {
				err = a.startAgain(id)
			}
			if err != nil {
				// The question goes back up, to be answered again.
				a.paneEnded(id)
			}
		})
	}
	a.sayIfMoved(id, t, machine)
	go func() {
		sess, err := conn.Shell(a.ctx, remote.ShellConfig{Cols: size.Cols, Rows: size.Rows})
		a.events <- func() {
			if err == nil {
				err = a.restarted(id, t, sess)
			}
			if err != nil {
				a.failed("Couldn't start it again", err.Error())
				// The question goes back up, to be answered again.
				if a.terminal(id) == t {
					a.paneEnded(id)
				}
			}
		}
	}()
	return nil
}

// sayIfMoved writes a line into a pane whose server is at another
// address than the pane was opened at, which a saved server edited
// since leaves it. The new run goes under the old transcript, and the
// two would otherwise read as one machine.
func (a *app) sayIfMoved(id string, t *uiterm.Terminal, machine string) {
	was, now := a.paneAt[id], a.reached[machine]
	if was == "" || now == "" || was == now {
		return
	}
	t.Say("-- kakel: " + a.nameOf(machine) + " is " + now + " now. This pane was on " + was + " --")
	a.paneAt[id] = now
}

// restarted puts a new session in a pane.
func (a *app) restarted(id string, t *uiterm.Terminal, sess session.Session) error {
	if err := t.Restart(sess); err != nil {
		return errors.Join(err, sess.Close())
	}
	a.setPane(id, func(p *Pane) { p.Ended = false })
	a.restarts[id]++
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

// clearFinished closes the panes whose programs have ended and clears
// the tunnels that stopped.
func (a *app) clearFinished() {
	clear(a.dropped)
	a.clearJobs(true)
	a.showJobs()
	for _, p := range slices.Clone(a.st.Panes) {
		if p.Ended {
			a.closePane(p.ID)
		}
	}
	for _, t := range slices.Clone(a.st.Tunnels) {
		if !t.Live {
			_ = a.closeTunnel(t.ID)
		}
	}
}

// giveSavedIDs gives the commands, tunnels and copies saved before
// servers had ids the ids of the servers their names stand for now.
func (a *app) giveSavedIDs() {
	if a.settings == nil || a.book == nil {
		return
	}
	err := a.settings.FillServerIDs(func(name string) string {
		if h, saved := a.book.Lookup(name); saved {
			return h.ID
		}
		return ""
	})
	// Settings that could not be written are asked for again next time.
	if err != nil && !errors.Is(err, settings.ErrUnsaveable) {
		log.Printf("giving saved things their servers' ids: %v", err)
	}
}

// ClearMachine takes a machine whose connection went off the sidebar,
// with the ended panes on it.
type ClearMachine struct{ ID string }

// clearMachine takes a machine whose connection went off the sidebar.
func (a *app) clearMachine(name string) {
	delete(a.dropped, name)
	for _, p := range slices.Clone(a.st.Panes) {
		if p.Machine == name && p.Ended {
			a.closePane(p.ID)
		}
	}
}

// reloadServers reads the saved servers again.
func (a *app) reloadServers() error {
	path, err := remote.BookPath()
	if err != nil {
		return err
	}
	b, err := remote.LoadBook(path)
	if err != nil {
		return err
	}
	a.book = b
	a.st.Saved = b.Hosts()
	a.giveSavedIDs()
	a.worked("Server list read again", count(len(a.st.Saved), "saved server")+".", "")
	return nil
}

// bindFar has pane watch what window w calls farID, so the sidebar does
// not list it under w as well.
func (a *app) bindFar(w *remoteWin, farID, pane string) {
	if farID == "" || !a.has(pane) {
		return
	}
	// What it watched before is not this pane's any more.
	for f, p := range w.bound {
		if p == pane {
			delete(w.bound, f)
		}
	}
	w.bound[farID] = pane
	a.showWindows()
}

// farHostOf is the machine beyond a window pane id ran on, "" for the
// window's own. Read on the program's goroutine only.
func (a *app) farHostOf(id string) string { return a.farHost[id] }

// onWindowsOwn marks pane id as on its window's own machine now, having
// started again there in place of one on a machine beyond it, which
// had closed: its files and links go there, and it says so.
func (a *app) onWindowsOwn(id string) {
	was := a.farHost[id]
	delete(a.farHost, id)
	a.setPane(id, func(p *Pane) { p.On = "" })
	if t := a.terminal(id); t != nil && was != "" {
		t.Say("It ran on " + a.nameOf(a.machineOf(id)+farSep+was) + ", where it is not open any more: this one is on the window's own machine.")
	}
}
