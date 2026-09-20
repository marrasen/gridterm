package main

import (
	"log"
	"strconv"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// programNote is what a pane's row says on the program's own behalf:
// how far along it says it is, and the last message it asked to have
// shown.
//
// On the row rather than in a dialog. A dialog takes the keyboard, and
// anything that can write to a pane can send one of these, so a program
// sending them one after another would stop the window.
func (a *app) programNote(pane *term.Terminal) []string {
	var say []string
	if note := progressNote(pane.Progress()); note != "" {
		say = append(say, note)
	}
	text, num := pane.Notice()
	a.logNewNotice(pane, text, num)
	if text != "" {
		say = append(say, text)
	}
	return say
}

// progressNote is how far along a program says it is, in a few words.
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

// logNewNotice writes a message to the window's log the first time it
// arrives, so one that goes by while the user is looking elsewhere is
// still there to read.
func (a *app) logNewNotice(pane *term.Terminal, text string, num uint64) {
	if num == 0 || a.noticed[pane] == num {
		return
	}
	if a.noticed == nil {
		a.noticed = map[*term.Terminal]uint64{}
	}
	a.noticed[pane] = num
	if text == "" {
		return
	}
	where := conns.Local
	if e := a.panes[pane]; e != nil {
		where = e.Host
	}
	log.Printf("%s: %s", groupName(where), text)
}

// forgetNotes drops the notes remembered for rows that have gone.
func (a *app) forgetNotes() {
	for e := range a.wrote {
		if _, live := a.paneRows[e]; !live {
			delete(a.wrote, e)
		}
	}
}

// forgetNotices drops what is remembered about panes that have closed.
func (a *app) forgetNotices() {
	for pane := range a.noticed {
		if _, live := a.panes[pane]; !live {
			delete(a.noticed, pane)
		}
	}
}
