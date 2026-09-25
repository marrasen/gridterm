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
