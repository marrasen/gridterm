package main

import (
	"os"
	"time"

	"github.com/marrasen/gridterm/logs"
)

// Logs: the window's own, and each server's account of how it was
// reached. Each shows in a pane that reads it as a terminal, so its
// scrollback, selection and copying are the terminal's.

// ShowLog opens a log's pane, or goes to it: the connection log of
// Machine, or the window's own when Machine is "".
type ShowLog struct{ Machine string }

// kindLog is a log's pane.
const kindLog = "log"

// windowLog is what this process logs. The lines still reach stderr;
// this keeps a copy for the pane.
var windowLog = logs.New(0, os.Stderr)

// account returns the connection log of machine, made on first use.
// A log that has one already carries on, under a line saying so.
func (a *app) account(machine string) *logs.Lines {
	l, ok := a.accounts[machine]
	if !ok {
		l = logs.New(0, nil)
		a.accounts[machine] = l
		a.st.Accounts = append(a.st.Accounts, machine)
		return l
	}
	logLine(l, "", "connecting again")
	return l
}

// logLine writes one line into a log, stamped with the time, in colour
// when it is given one: an SGR number such as "31" for red.
func logLine(l *logs.Lines, colour, line string) {
	stamp := time.Now().Format("15:04:05") + "  "
	if colour != "" {
		line = "\x1b[" + colour + "m" + line + "\x1b[0m"
	}
	_, _ = l.Write([]byte(stamp + line + "\n"))
}

// Colours for lines that went badly, and lines that went well.
const (
	badly = "31"
	well  = "32"
)

// showLog opens a log's pane, or goes to the one open.
func (a *app) showLog(machine string) {
	for _, p := range a.st.Panes {
		if p.Kind == kindLog && p.Machine == machine {
			a.st.Focus = p.ID
			return
		}
	}
	l, title := windowLog, "Window Log"
	if machine != "" {
		var ok bool
		if l, ok = a.accounts[machine]; !ok {
			a.notify("Connection logs start as a connection does", "Connect to "+machine+" first.", "")
			return
		}
		title = "Connection Log"
	}
	a.next++
	id := "p" + itoa(a.next)
	a.addPane(Pane{ID: id, Title: title, Machine: machine, Kind: kindLog}, openShell(l.Open(), a.palette, a.hooks(id)), placement{})
}

// watchDial shows a machine's connection log as the connection is made,
// as gridterm does: the dial's steps, and why it failed, where the user
// is looking. It returns the pane it opened, or "" when the log's pane
// was open already, which it goes to instead. Closing the pane gives
// the dial up.
func (a *app) watchDial(machine string) string {
	for _, p := range a.st.Panes {
		if p.Kind == kindLog && p.Machine == machine {
			a.st.Focus = p.ID
			return ""
		}
	}
	a.next++
	id := "p" + itoa(a.next)
	a.addPane(Pane{ID: id, Title: "Connecting to " + machine, Machine: machine, Kind: kindLog},
		openShell(a.accounts[machine].Open(), a.palette, a.hooks(id)), placement{})
	return id
}

// dialed puts what the connection was for in the place of the log
// pane watchDial opened: a terminal, when open says so, beside it,
// with the log folding away so the terminal takes its room. The log
// stays under the machine's menu.
func (a *app) dialed(logPane, machine string, open bool) {
	if open {
		if err := a.open(machine, placement{beside: logPane}); err != nil {
			a.notify("Couldn't open a shell on "+machine, err.Error(), "")
			return
		}
	}
	if logPane != "" {
		a.closePane(logPane)
	}
}
