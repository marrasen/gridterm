package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// What agents typed, kept for each pane, as gridterm keeps it: Typing
// History shows it in a reader, as the agent sent it, edits included,
// so the user can see what went into a pane that has since scrolled.

// ShowTyped opens what agents typed into Pane in a reader beside it.
type ShowTyped struct{ Pane string }

// mostTyped and mostTypedBytes bound what is kept for one pane.
const (
	mostTyped      = 2000
	mostTypedBytes = 256 << 10
)

type agentSend struct {
	at   time.Time
	text string
	keys []string
}

// typedLog is what agents typed into one pane.
type typedLog struct {
	sends          []agentSend
	bytes, dropped int
}

func (l *typedLog) add(s agentSend) {
	weigh := func(s agentSend) int {
		n := len(s.text)
		for _, k := range s.keys {
			n += len(k)
		}
		return n
	}
	l.sends = append(l.sends, s)
	l.bytes += weigh(s)
	for len(l.sends) > mostTyped || (l.bytes > mostTypedBytes && len(l.sends) > 1) {
		l.bytes -= weigh(l.sends[0])
		l.sends = l.sends[1:]
		l.dropped++
	}
}

// agentTyped writes down what an agent sent a pane.
func (a *app) agentTyped(pane, text string, keys []string) {
	if text == "" && len(keys) == 0 {
		return
	}
	l := a.typed[pane]
	if l == nil {
		l = &typedLog{}
		a.typed[pane] = l
	}
	l.add(agentSend{at: time.Now(), text: text, keys: slices.Clone(keys)})
}

// showTyped opens what agents typed into a pane in a reader beside it.
func (a *app) showTyped(pane string) error {
	l := a.typed[pane]
	if l == nil || len(l.sends) == 0 {
		return errors.New("no agent has typed in this pane")
	}
	lines := []string{"Input as the agent sent it, edits included.", ""}
	if l.dropped > 0 {
		lines = append(lines, fmt.Sprintf("The first %d are no longer kept.", l.dropped), "")
	}
	for _, s := range l.sends {
		var b strings.Builder
		b.WriteString(showInvisible(s.text))
		for _, k := range s.keys {
			b.WriteString("<" + k + ">")
		}
		lines = append(lines, s.at.Format("15:04:05")+"  "+b.String())
	}
	a.next++
	id := "p" + itoa(a.next)
	title := "Typing History of " + a.titleOf(pane)
	a.addPane(Pane{ID: id, Title: title, Machine: a.machineOf(pane), Kind: kindReader}, nil, placement{beside: pane})
	m := maps.Clone(a.st.Readers)
	if m == nil {
		m = map[string]Reader{}
	}
	m[id] = Reader{Path: title, Lines: lines, Seq: 1, Line: len(lines)}
	a.st.Readers = m
	return nil
}

// showInvisible writes what does not print as escapes, so a return and
// a tab can be told from a space.
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
