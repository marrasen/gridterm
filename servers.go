package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// openServer asks which machine to connect to.
func (a *app) openServer() error {
	if a.connecting {
		return errors.New("a connection is already being made")
	}
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
		if a.connecting {
			return errors.New("a connection is already being made")
		}
		if a.prepare != nil {
			cfg = a.prepare(cfg)
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
// The dial runs on its own goroutine because it can stop to ask the user
// something, and the dialog it asks with is drawn by this one. A
// "Connecting" dialog holds the place until it is done, and cancelling
// that gives up.
//
// One connection at a time, for now. Each wants a dialog of its own to
// wait in, the modal stack is ordered, and closing a dialog takes
// anything above it — so a connection that finished would tear down the
// dialog another was still waiting in. The connections panel is where
// several at once will live.
func (a *app) connect(cfg remote.Config) {
	if a.connecting {
		a.reportError("Could not connect", errors.New("a connection is already being made"))
		return
	}
	cfg.Ask = &askUser{app: a}
	cfg.Ring = a.keys

	ctx, cancel := context.WithCancel(a.ctx)
	waiting := a.newConfirm("Connecting", []string{cfg.Host})
	waiting.AddButton(ui.Button{Title: "Cancel", Do: func() error {
		cancel()
		return nil
	}})
	// Closing it any other way means the same thing.
	a.connecting = true
	dismiss := a.showForm(waiting, cancel)

	cols, rows := a.lastSize[0], a.lastSize[1]
	go func() {
		sh, err := remote.StartShell(ctx, cfg, remote.ShellConfig{Cols: cols, Rows: rows})
		a.pump.post(func() {
			// The waiting dialog goes whichever way it turned out, and
			// before anything else is shown over it.
			a.connecting = false
			dismiss()
			cancel()
			if err != nil {
				a.reportError(fmt.Sprintf("Could not connect to %s", cfg.Host), err)
				return
			}
			if err := a.openSessionTab(sh); err != nil {
				_ = sh.Close()
				a.reportError("Could not open a terminal", err)
			}
		})
	}()
}

// openSessionTab puts a session in a tab of its own.
func (a *app) openSessionTab(sess session.Session) error {
	t, err := a.newTerminalOn(sess)
	if err != nil {
		return err
	}
	return a.placeTab(t)
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

// newTerminalOn puts a widget on a session that is already open.
func (a *app) newTerminalOn(sess session.Session) (*term.Terminal, error) {
	t, err := term.New(term.Config{
		Session:        sess,
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
	a.panes[t] = struct{}{}
	return t, nil
}
