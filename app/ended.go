package app

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/words"
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
	a.countEnding(id)
	t := a.terminal(id)
	switch {
	case t == nil:
		a.closePane(id)
		return
	case !t.Exited():
		// A notice from before the pane was started again.
		return
	case a.restarting[id]:
		// The second word of the end, landing while the pane starts
		// again on another goroutine. Asked now, the question would
		// say a start that is on its way had failed; one that fails
		// asks it again.
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
		start, question = "Run Again", commandQuestion(cmd.argv, status, known, a.notRun[id], a.cutOff(id, t))
	}
	// Told twice: as the output ends, and again once the program's
	// status is in. The second asks again only when it knows more.
	if t.Asking() == question {
		return
	}
	// Once, as the status comes in: the first telling knows none.
	if i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == id }); known && i >= 0 {
		log.Printf("%s ended, exit %d", a.paneForLog(a.st.Panes[i]), status)
	}
	// Ended out of sight, with its status in: an echo says how it went.
	if w := a.ownerOf(id); known {
		// In front of its window, an echo while the user is elsewhere,
		// as for a long command.
		front := w != nil && a.focusIn(w) == id
		p := a.pingsIn(w)
		switch {
		case front && status == 0:
			p.FrontDones++
		case front:
			p.FrontProblems++
		case status == 0:
			p.Dones++
		default:
			p.Problems++
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
func (a *app) startAgain(id string) error { return a.startAgainOr(id, true) }

// startAgainOr is startAgain, dialling a connection that has gone only
// with dial: without it, as for an agent, which may not open
// connections, it refuses one this window, or the window the pane is
// through, would have to dial.
func (a *app) startAgainOr(id string, dial bool) error {
	t := a.terminal(id)
	if t == nil {
		return errors.New("that pane is no longer open")
	}
	if !t.Exited() {
		return nil
	}
	machine := a.machineOf(id)
	if !dial && machine != "" && a.machines.Get(machine).Conn == nil && a.machines.Get(machine).Window == nil {
		return fmt.Errorf("this window is not connected to %s any more, and opening connections is the user's to do: ask them to connect to it", a.machines.Name(machine))
	}
	if cmd, ok := a.commands[id]; ok && a.machines.Get(machine).Window == nil {
		return a.runAgain(id, cmd)
	}
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
	if w := a.machines.Get(machine).Window; w != nil {
		// The window started it: it is asked to start it again, in its
		// own pane, what ran there, and the pane here watches that as
		// before. A window that cannot, or a pane it no longer knows,
		// gets a shell of its own there.
		farID := ""
		for fid, pane := range w.Bound {
			if pane == id {
				farID = fid
			}
		}
		// Where it ran, and what, for a window that has to open it anew.
		key, cmd := a.farHostOf(id), a.commands[id]
		a.restarting[id] = true
		go func() {
			var sess session.Session
			var err error
			if farID != "" {
				// Ended, it is not listed there, and is asked for by the
				// ID the window gave it; one on a machine beyond it is.
				open, ok := w.Serve.OpenNamed(farID)
				if !ok {
					open = serve.Open{ID: farID, Kind: "Terminal"}
				}
				// Only over a connection that window holds: one it would
				// have to dial is for someone at that window to make,
				// where its questions are asked.
				err = w.Serve.StartAgainConnected(serve.Attached{ID: open.ID, Host: open.Key(), Kind: open.Kind})
				if err == nil {
					sess, err = w.Serve.Attach(open, size.Cols, size.Rows)
				}
				// A window that cannot, or one that closed it: a shell of
				// its own there, in its place.
				if errors.Is(err, serve.ErrCannotStartAgain) || errors.Is(err, serve.ErrNotOpen) {
					err = nil
				}
			}
			bind := func(n serve.Attached) {
				go func() { a.events <- func() { a.bindFar(w, n.ID, id) } }()
			}
			if sess == nil && err == nil && (key != "" || len(cmd.argv) > 0) {
				// Opened anew where it ran: the machine beyond, or the
				// command again.
				sess, err = w.Serve.OpenOn(key, strings.Join(cmd.argv, " "), cmd.dir, size.Cols, size.Rows, bind)
				if errors.Is(err, serve.ErrCannotOpenOn) {
					sess, err = nil, nil
				}
			}
			if sess == nil && err == nil {
				// A window of a build before that: a shell on its own
				// machine, in its place.
				moved := key != ""
				sess, err = w.Serve.Open(size.Cols, size.Rows, func(n serve.Attached) {
					if moved {
						go func() { a.events <- func() { a.onWindowsOwn(id) } }()
					}
					bind(n)
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
						a.askAgain(id)
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
		a.restarting[id] = true
		err := a.dialAgain(machine, func(err error) {
			if err == nil {
				err = a.startAgain(id)
			}
			if err != nil {
				// The question goes back up, to be answered again.
				a.askAgain(id)
			}
		})
		if err != nil {
			delete(a.restarting, id)
		}
		return err
	}
	a.sayIfMoved(id, t, machine)
	a.restarting[id] = true
	go func() {
		sess, err := conn.Shell(a.ctx, a.shellConfig(machine, size.Cols, size.Rows))
		a.events <- func() {
			if err == nil {
				// Taught again, as the shell is new: its paths and the
				// ends of its commands are said again.
				a.teachFar(machine, sess)
				err = a.restarted(id, t, sess)
			}
			if err != nil {
				a.failed("Couldn't start it again", err.Error())
				// The question goes back up, to be answered again.
				if a.terminal(id) == t {
					a.askAgain(id)
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
func (a *app) sayIfMoved(id string, t *uiterm.Terminal, machine machines.ID) {
	was, now := a.paneAt[id], a.machines.Get(machine).Reached
	if was == "" || now == "" || was == now {
		return
	}
	t.Say("-- kakel: " + a.machines.Name(machine) + " is " + now + " now. This pane was on " + was + " --")
	a.paneAt[id] = now
}

// countEnding counts the end of the run pane id holds, once, if it has
// ended. The terminal says each end twice, and a window asked to start
// a pane again can hear of its end, from the window that asked, before
// it has itself. Counted at each end once, and by the run that ended, a
// late word of the run before is no new end: one counted after a start
// again said the new run had failed, when it had not.
func (a *app) countEnding(id string) {
	if t := a.terminal(id); t != nil && t.Exited() && !a.endCounted[id] {
		a.endings[id]++
		a.endCounted[id] = true
	}
}

// askAgain puts a pane's question back up once starting it again has
// failed.
func (a *app) askAgain(id string) {
	delete(a.restarting, id)
	a.paneEnded(id)
}

// restarted puts a new session in a pane.
func (a *app) restarted(id string, t *uiterm.Terminal, sess session.Session) error {
	if sh := a.shells.Get(id); sh != nil {
		// Counted on the sidebar as the first was.
		sess = sh.Counted(sess)
	}
	if err := t.Restart(sess); err != nil {
		return errors.Join(err, sess.Close())
	}
	a.setPane(id, func(p *Pane) { p.Ended = false })
	a.restarts[id]++
	delete(a.endCounted, id)
	delete(a.restarting, id)
	delete(a.notRun, id)
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
	}
	return session.Status(why)
}

// clearFinished closes the panes whose programs have ended and clears
// the tunnels that stopped.
func (a *app) clearFinished() {
	for _, id := range a.machines.Dropped() {
		a.machines.At(id).Dropped = false
	}
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
type ClearMachine struct{ ID machines.ID }

// clearMachine takes a machine whose connection went off the sidebar.
// Its panes go too, ended or still ending: the connection under them
// has gone. A reconnect's log on its way stays.
func (a *app) clearMachine(name machines.ID) {
	dropped := a.machines.Get(name).Dropped
	a.machines.At(name).Dropped = false
	a.withdrawLost(name)
	for _, p := range slices.Clone(a.st.Panes) {
		if p.Machine == name && (p.Ended || dropped && p.Kind != KindLog) {
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
	a.worked("Server list read again", words.Count(len(a.st.Saved), "saved server")+".", "")
	return nil
}

// bindFar has pane watch what window w calls farID, so the sidebar does
// not list it under w as well.
func (a *app) bindFar(w *machines.Window, farID, pane string) {
	if farID == "" || !a.has(pane) {
		return
	}
	// What it watched before is not this pane's any more.
	for f, p := range w.Bound {
		if p == pane {
			delete(w.Bound, f)
		}
	}
	w.Bound[farID] = pane
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
	if where := a.linksAt[id]; where != nil {
		own := a.machineOf(id)
		where.Store(&own)
	}
	if t := a.terminal(id); t != nil && was != "" {
		t.Say("It ran on " + a.machines.Name(machines.FarID(a.machineOf(id), was)) + ", where it is not open any more: this one is on the window's own machine.")
	}
}

// commandRoom is how much of a command its pane's question names: a
// long one is cut short, the way the old window cut it.
const commandRoom = 40

// commandQuestion words the question on a pane that ran one command,
// naming it: how it ended is said rather than assumed. A command cut
// off did not finish, and saying it had is as wrong as an exit of 0.
func commandQuestion(argv []string, status int, known, notRun, cutOff bool) string {
	what := grid.TrimTail(strings.Join(argv, " "), commandRoom)
	if what == "" {
		what = "The command"
	}
	switch {
	case notRun:
		return "The connection was not made, so " + what + " did not run. Run it again?"
	case known:
		return what + " finished. Exit " + strconv.Itoa(status) + ". Run it again?"
	case cutOff:
		return "The connection went while " + what + " was running. Run it again?"
	}
	return what + " has stopped. Run it again?"
}

// cutOff reports whether the connection pane id's program ran over went
// while it ran: the session says it ended with no word of how, or the
// connection itself has gone.
func (a *app) cutOff(id string, t *uiterm.Terminal) bool {
	why, over := t.Ending()
	var missing *ssh.ExitMissingError
	if over && (errors.As(why, &missing) || errors.Is(why, serve.ErrNoEnding)) {
		return true
	}
	machine := a.machineOf(id)
	if machine == machines.Local {
		return false
	}
	m := a.machines.Get(machine)
	return m.Conn == nil && m.Window == nil
}
