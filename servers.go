package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// openServer asks which machine to connect to.
func (a *app) openServer() error {
	f := a.newForm("Connect to a server")
	f.Lines = []string{"A machine to open a terminal on."}
	target := f.AddField("Server", a.newField("[user@]host[:port]", 0))
	f.AddButton(ui.Button{Title: "Connect", Do: func() error {
		cfg, err := remote.ParseTarget(target.Text())
		if err != nil {
			// Returning it keeps the dialog open with what was typed
			// still there to correct.
			return err
		}
		// Not from here: this dialog closes as soon as this returns, and
		// closing a dialog takes anything stacked on top of it -- which
		// would be the one the connection had just opened. The next
		// frame starts it instead, once this form has gone.
		a.pump.post(func() { a.connect(cfg) })
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// connect opens a connection in the background and puts a terminal on it
// when it arrives.
//
// The machine is named by what was asked for rather than by the address
// alone, so two accounts on one machine are two connections rather than
// one: a terminal opened on the second must not land on the first.
func (a *app) connect(cfg remote.Config) { a.connectAs(cfg.Target(), cfg) }

// connectAs is connect, with a name for the panel: a saved server is
// known by the name the user gave it rather than by its address.
//
// The connection is kept under that name, so a second terminal on the
// machine rides on it rather than logging in again.
func (a *app) connectAs(name string, cfg remote.Config) {
	a.connectFor(name, cfg, opening{})
}

// connectFor is connectAs with what the connection is being made for,
// which for -ssh is the command it was given rather than a shell.
func (a *app) connectFor(name string, cfg remote.Config, open opening) {
	if name == "" {
		name = cfg.Host
	}
	a.openRoute(name, []step{{name: name, cfg: cfg}}, open, nil)
}

// openSessionTab puts a session where it was asked to go: dividing a
// pane when one was named, and on the stage otherwise.
func (a *app) openSessionPane(sess session.Session, host string, kind conns.Kind,
	label string, at *spot) (*term.Terminal, error) {

	t, err := a.newTerminalOn(sess, host, kind, label)
	if err != nil {
		return nil, err
	}
	if err := a.place(t, at); err != nil {
		// Nowhere to put it, so nothing is told about it. Closing the
		// terminal closes the session with it.
		delete(a.panes, t)
		delete(a.started, t)
		_ = t.Close()
		return nil, err
	}
	a.showPane(t)
	return t, nil
}

// reportError shows something that failed, for a failure that arrived
// from a goroutine with nowhere to return it.
//
// A dialog rather than a log line: the user asked for this and is
// waiting for it, so a message they never see is no message at all. The
// whole of the message goes in, because the part that was being cut off
// was twice the part that said what to do about it.
func (a *app) reportError(title string, err error) {
	if errors.Is(err, context.Canceled) {
		// They cancelled it themselves and know what happened.
		return
	}
	a.showNotice(title, err.Error(), true)
}

// errorLineWidth is how wide a line of a confirmation dialog is allowed
// to get. It matches what a dialog will show without being trimmed.
const errorLineWidth = 52

// wrapLines breaks a message at spaces so a long question reads as a
// paragraph rather than being cut off at the edge of the box.
//
// Width is counted in cells, not bytes: a line of CJK is half as many
// characters as a line of Latin and still fills the box.
func wrapLines(s string, width int) []string {
	var lines []string
	line := ""
	for _, word := range splitWords(s) {
		switch {
		case line == "":
			line = word
		case grid.StringWidth(line)+1+grid.StringWidth(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
		// A single word longer than the box is cut rather than pushing
		// the dialog wider than the window.
		for grid.StringWidth(line) > width {
			head, rest := grid.Cut(line, width)
			if head == "" {
				// A cluster wider than the whole box. It goes on a line
				// of its own rather than stopping the wrap dead.
				head = grid.Clusters(line)[0]
				rest = line[len(head):]
			}
			lines = append(lines, head)
			line = rest
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// splitWords breaks on spaces, keeping nothing empty.
func splitWords(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// lockKeys forgets every unlocked private key, so the next connection
// asks for the passphrase again.
//
// The secrets go with them: the vault is opened by one of these keys,
// so leaving it open would leave a locked window holding the thing the
// lock was for.
func (a *app) lockKeys() error {
	a.keys.Lock()
	a.lockSecrets()
	return nil
}

// newTerminalOn puts a widget on a session that is already open, and a
// row on the panel for it.
//
// host names the machine it is running on, or conns.Local for this one.
// kind and label are what the row says it is until the program in it
// names itself. Everything else the panel says about the connection is
// read from the meter the session is wrapped in, so nothing has to
// report what it is doing.
func (a *app) newTerminalOn(sess session.Session, host string, kind conns.Kind,
	label string) (*term.Terminal, error) {

	counted := &metered{Session: sess, m: meter.New()}
	t, err := term.New(term.Config{
		Session:        counted,
		Size:           ui.Size{Cols: a.lastSize[0], Rows: a.lastSize[1]},
		Scrollback:     a.scrollback,
		Program:        a.called.version(),
		Palette:        &a.colours,
		ReadClipboard:  a.pasteText,
		WriteClipboard: a.clip.set,
		OnExit:         a.paneExited,
		OnError:        a.logError,
		OnLink:         a.linkOpener(host),
		FindPath:       a.pathFinder(host),
		OnPath:         a.pathOpener(host),
		Now:            a.clock,
	})
	if err != nil {
		return nil, fmt.Errorf("start terminal: %w", err)
	}

	e := &conns.Entry{
		Host:   host,
		Kind:   kind,
		Label:  label,
		Meter:  counted.m,
		Reveal: func() { a.focus(t) },
		Close:  func() error { return a.closePane(t) },
	}
	a.panes[t] = e
	// The session, so a program that ends can be let go of with the pane
	// left open. What it was started on is filled in by whoever knows,
	// and a pane nobody tells is one the window cannot start again.
	a.started[t] = &startedAs{on: counted}
	return t, nil
}

// showPane puts a pane on the panel, once it is somewhere the panel can
// send the user.
func (a *app) showPane(t *term.Terminal) {
	if e := a.panes[t]; e != nil {
		a.registry.Add(e)
	}
}

// metered counts what moves through a session, so the panel can say
// whether the thing on the far end is doing anything.
//
// A shell printing nothing for four seconds settles; one running a build
// stays active because its output never stops for that long.
type metered struct {
	session.Session
	m *meter.Meter
}

func (s *metered) Read(p []byte) (int, error) {
	n, err := s.Session.Read(p)
	s.m.Moved(n, 0, time.Now())
	if err != nil {
		// The far end has gone. Nothing else will move, so the row says
		// closed rather than settling and looking merely quiet.
		s.m.Close()
	}
	return n, err
}

func (s *metered) Write(p []byte) (int, error) {
	n, err := s.Session.Write(p)
	s.m.Moved(0, n, time.Now())
	if err != nil {
		// A session that will not take input has ended as surely as one
		// that will not give any: the terminal treats both the same way.
		s.m.Close()
	}
	return n, err
}

func (s *metered) Close() error {
	err := s.Session.Close()
	s.m.Close()
	return err
}
