package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/marrasen/gridterm/ui/term"
)

// typedCommand shows what an agent has typed in a pane, and typedTitle
// heads both the line and the dialog.
const (
	typedCommand = "agent.typed"
	typedTitle   = "Typing History"
)

// mostTyped is how many sends one pane keeps, and mostTypedBytes how
// much they weigh. The oldest go first and the dialog says how many
// went, except that one send heavier than the whole cap is kept: a
// record saying nothing was sent would be worse.
const (
	mostTyped      = 2000
	mostTypedBytes = 256 << 10
)

// agentSend is one thing an agent typed into a pane.
type agentSend struct {
	At   time.Time
	Text string
	Keys []string
}

// typedLog is what an agent has typed in one pane, oldest first.
//
// It holds what the agent sent, not what the shell ran: Backspace, Tab
// completion and Up through the history all change a line before the
// shell sees it. What the user types themselves is not in it, including
// a secret they type at the agent's asking.
type typedLog struct {
	sends []agentSend

	// bytes is how much text the sends hold, and dropped how many went
	// to keep within the cap.
	bytes   int
	dropped int
}

// add writes down one send, dropping the oldest to stay within the cap.
func (l *typedLog) add(s agentSend) {
	l.sends = append(l.sends, s)
	l.bytes += weigh(s)
	for len(l.sends) > mostTyped || (l.bytes > mostTypedBytes && len(l.sends) > 1) {
		l.bytes -= weigh(l.sends[0])
		// Let go of it as well as forgetting it: a reslice alone leaves
		// the text in the array behind the slice, and a megabyte of it
		// would sit there until the next append moved everything.
		l.sends[0] = agentSend{}
		l.sends = l.sends[1:]
		l.dropped++
	}
}

// weigh is how much of the cap one send takes: its text and its key
// names, both of which an agent chooses the size of.
func weigh(s agentSend) int {
	n := len(s.Text)
	for _, key := range s.Keys {
		n += len(key)
	}
	return n
}

// agentTyped writes down what an agent typed in a pane.
//
// Kept beside the panes rather than on the handover, so taking a pane
// out of the share leaves the record of what was done in it. It goes
// when the pane does.
func (a *app) agentTyped(pane *term.Terminal, text string, keys []string) {
	if pane == nil || (text == "" && len(keys) == 0) {
		return
	}
	if a.typed == nil {
		a.typed = make(map[*term.Terminal]*typedLog)
	}
	log := a.typed[pane]
	if log == nil {
		log = &typedLog{}
		a.typed[pane] = log
	}
	log.add(agentSend{At: a.clock(), Text: text, Keys: slices.Clone(keys)})
}

// forgetTyped drops a pane's record, for a pane that has closed.
func (a *app) forgetTyped(pane *term.Terminal) { delete(a.typed, pane) }

// showTyped opens the record of what an agent typed in the focused pane.
func (a *app) showTyped() error {
	pane := a.focusedTerminal()
	if pane == nil {
		return errors.New("no pane is focused")
	}
	log := a.typed[pane]
	if log.count() == 0 {
		return errors.New("no agent has typed in this pane")
	}
	n := a.newNotice(typedTitle, typedText(log))
	// A column of times against what was sent, which the dialog would
	// otherwise re-wrap at the spaces.
	n.Preformatted = true
	a.presentNotice(n)
	return nil
}

// typedText is the record as the dialog shows it: a line per send, the
// time it was sent and what was sent.
func typedText(log *typedLog) string {
	var b strings.Builder
	b.WriteString("Input as the agent sent it, edits included.\n\n")
	if log.dropped > 0 {
		fmt.Fprintf(&b, "The first %d are no longer kept.\n\n", log.dropped)
	}
	for _, s := range log.sends {
		fmt.Fprintf(&b, "%s  %s\n", s.At.Format("15:04:05"), sendText(s))
	}
	return b.String()
}

// sendText is one send written out: the text with anything invisible
// shown, and the keys named after it in angle brackets.
func sendText(s agentSend) string {
	var b strings.Builder
	b.WriteString(showInvisible(s.Text))
	for _, key := range s.Keys {
		b.WriteString("<" + key + ">")
	}
	if b.Len() == 0 {
		return "(nothing)"
	}
	return b.String()
}

// showInvisible writes a string with every character that does not print
// shown, so a record of a tab or a return is not a blank.
func showInvisible(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\\':
			b.WriteString(`\\`)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		case r < 0x80:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteString(`\u` + strconv.FormatInt(int64(r), 16))
		}
	}
	return b.String()
}

// count is how many sends a record holds, and none for a pane that has
// no record at all.
func (l *typedLog) count() int {
	if l == nil {
		return 0
	}
	return len(l.sends)
}
