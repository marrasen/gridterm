package main

import (
	"log"
	"strconv"
	"time"

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

// toastGap is the shortest time between two pop-ups.
//
// Anything that can write to a pane can ask for one, so a program
// sending them one after another would otherwise bury the screen. The
// ones left out are still on the row and in the log.
const toastGap = 2 * time.Second

// logNewNotice writes a message to the window's log the first time it
// arrives, and puts it up outside the window, so one that goes by
// while the user is looking elsewhere is still seen.
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
	name := a.noticeFrom(pane)
	log.Printf("%s: %s", name, text)
	a.toast(name, text)
}

// noticeFrom names the pane a message came from, for the log line and
// the pop-up: what the row calls it, or the machine it runs on.
func (a *app) noticeFrom(pane *term.Terminal) string {
	e := a.panes[pane]
	if e == nil {
		return groupName(conns.Local)
	}
	if e.Label != "" {
		return groupName(e.Host) + ": " + e.Label
	}
	return groupName(e.Host)
}

// toast puts a message up outside the window, and gives up on one that
// came too soon after the last.
func (a *app) toast(title, body string) {
	if a.toasts == nil {
		return
	}
	now := time.Now()
	if now.Sub(a.lastToast) < toastGap {
		return
	}
	a.lastToast = now
	if err := a.toasts.Show(title, body); err != nil {
		// Logged and carried on. The message is on the row and in the
		// log already, so a pop-up that would not show has lost
		// nothing, and a window that stopped working over one would be
		// worse than the notification is worth.
		a.logError(err)
	}
}

// tell says that something the window did in the background has
// finished, the way a program's message is said: a line in the log, a
// pop-up outside the window, and a line on the bottom row.
//
// Not a dialog. It is news, not a question, and a dialog that turns up
// when a copy lands takes the keys from whatever the user moved on to.
// The bottom row is there for a machine with no pop-ups, and for a
// pop-up that came too soon after the last one.
func (a *app) tell(title, body string) {
	line := title
	if body != "" {
		line += " — " + body
	}
	log.Print(line)
	a.toast(title, body)
	a.say(line)
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
