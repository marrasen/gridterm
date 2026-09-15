package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/conns"
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
	if name == "" {
		name = cfg.Host
	}
	// A window the server list holds is taken over rather than logged
	// in to, whichever way the user asked for it. Checked here because
	// this is the one place every connection by name or by address goes
	// through: a path that missed it would log in to the serve port and
	// be refused, which is a long way round to find out.
	if h, ok := a.savedWindowFor(name, cfg); ok {
		if a.windows[h.Name] != nil {
			if err := a.openOnWindow(h.Name, nil); err != nil {
				a.reportError("Could not open a terminal on "+h.Name, err)
			}
			return
		}
		if err := a.takeOver(h.ServeAddr(), h.KeyFile()); err != nil {
			a.reportError("Could not take over "+h.Name, err)
		}
		return
	}
	a.openRoute(name, []step{{name: name, cfg: cfg}}, nil, nil)
}

// savedWindowFor is the saved window a request names, by the name it
// was asked for or by the address it would have been dialled at.
//
// By address as well as by name, because "connect to a server" takes a
// typed address and knows nothing about the list.
func (a *app) savedWindowFor(name string, cfg remote.Config) (remote.Host, bool) {
	if h, ok := a.book.Lookup(name); ok && h.Window {
		return h, true
	}
	addr := cfg.Host
	if addr == "" {
		return remote.Host{}, false
	}
	if cfg.Port != 0 {
		addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	}
	for _, h := range a.book.Hosts() {
		if !h.Window {
			continue
		}
		if strings.EqualFold(h.ServeAddr(), addr) ||
			(cfg.Port == 0 && strings.EqualFold(h.Address, addr)) {
			return h, true
		}
	}
	return remote.Host{}, false
}

// openSessionTab puts a session where it was asked to go: dividing a
// pane when one was named, and in a tab of its own otherwise.
func (a *app) openSessionTab(sess session.Session, host string, kind conns.Kind,
	label string, at *spot) (*term.Terminal, error) {

	t, err := a.newTerminalOn(sess, host, kind, label)
	if err != nil {
		return nil, err
	}
	if err := a.place(t, at); err != nil {
		// Nowhere to put it, so nothing is told about it. Closing the
		// terminal closes the session with it.
		delete(a.panes, t)
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
// waiting for it, so a message they never see is no message at all.
func (a *app) reportError(title string, err error) {
	if errors.Is(err, context.Canceled) {
		// They cancelled it themselves and know what happened.
		return
	}
	f := a.newConfirm(title, wrapLines(err.Error(), errorLineWidth))
	f.AddButton(ui.Button{Title: "Close"})
	a.showForm(f, nil)
}

// errorLineWidth is how wide a wrapped error message is allowed to get.
// It matches what a dialog will show without being trimmed.
const errorLineWidth = 52

// wrapLines breaks a message at spaces so a long error reads as a
// paragraph rather than being cut off at the edge of the box.
func wrapLines(s string, width int) []string {
	var lines []string
	line := ""
	for _, word := range splitWords(s) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
		// A single word longer than the box is cut rather than pushing
		// the dialog wider than the window.
		for len(line) > width {
			lines = append(lines, line[:width])
			line = line[width:]
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
func (a *app) lockKeys() error {
	a.keys.Lock()
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
		Palette:        &a.colours,
		ReadClipboard:  clipboardRead,
		WriteClipboard: a.clip.set,
		OnExit:         a.paneExited,
		OnError:        a.logError,
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
