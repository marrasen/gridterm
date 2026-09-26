package main

import (
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/gridterm/notify"
	"github.com/marrasen/gridterm/vt"
)

// What a program in a pane says about itself, as gridterm shows it: how
// far along it is, from OSC 9;4, and a message, from OSC 9. Both go on
// the pane's sidebar row. A new message also goes to the window log,
// and to a pop-up outside the window where the system has one, at most
// one every toastGap.

// toastGap is the least time between two pop-ups, so a program that
// says something on every line does not bury the screen.
const toastGap = 2 * time.Second

// toaster shows pop-ups outside the window: on Windows, in its
// notification area, and elsewhere nowhere, as in gridterm. It is made
// at the first pop-up, since on Windows it puts an icon there.
var toaster = sync.OnceValue(func() notify.Toaster { return notify.New(programName) })

// toasted says whether toaster was made, for closing it.
var toasted atomic.Bool

// notePrograms puts what each program says on its pane's row, and
// passes a new message on.
func (a *app) notePrograms() {
	for i := range a.st.Panes {
		p := &a.st.Panes[i]
		t := a.terminal(p.ID)
		if t == nil {
			continue
		}
		var say []string
		if note := progressNote(t.Progress()); note != "" {
			say = append(say, note)
		}
		text, num := t.Notice()
		if num != 0 && a.noticed[p.ID] != num {
			a.noticed[p.ID] = num
			if text != "" {
				from := "This computer"
				if p.Machine != "" {
					from = p.Machine
				}
				from += ": " + p.Title
				log.Printf("%s: %s", from, text)
				a.toast(from, text)
			}
		}
		if text != "" {
			say = append(say, text)
		}
		p.Note = strings.Join(say, ", ")
	}
}

// progressNote says how far along a program is, as its row puts it.
func progressNote(p vt.Progress) string {
	switch p.State {
	case vt.Working:
		return strconv.Itoa(p.Percent) + "%"
	case vt.Indeterminate:
		return "working"
	case vt.ProgressFailed:
		if p.Percent == 0 {
			return "failed"
		}
		return "failed at " + strconv.Itoa(p.Percent) + "%"
	case vt.ProgressWarning:
		if p.Percent == 0 {
			return "warning"
		}
		return strconv.Itoa(p.Percent) + "%, warning"
	}
	return ""
}

// toast pops a message up outside the window, unless one went up less
// than toastGap ago.
func (a *app) toast(title, body string) {
	now := time.Now()
	if now.Sub(a.lastToast) < toastGap {
		return
	}
	a.lastToast = now
	toasted.Store(true)
	if err := toaster().Show(title, body); err != nil {
		log.Printf("showing a pop-up: %v", err)
	}
}

// closeToaster takes away what the pop-ups hold, such as the icon in
// Windows' notification area, once the window has closed.
func closeToaster() {
	if toasted.Load() {
		_ = toaster().Close()
	}
}

// tell says something worth knowing while the window is out of sight:
// a notice in the window, a line in the log, and a pop-up outside it.
func (a *app) tell(title, body string) {
	line := title
	if body != "" {
		line += " — " + body
	}
	log.Print(line)
	a.toast(title, body)
	a.notify(title, body, "")
}
