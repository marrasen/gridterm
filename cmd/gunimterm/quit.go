package main

import (
	"strconv"
	"strings"
)

// Closing the window, however it is asked for: the menu, the key, or
// the close button on the title bar. It asks first while anything is
// open, as gridterm does, and says what: a window holding a copy half
// done and three shells is not one to lose to a slip of the mouse.

// askToQuit asks before the window goes, and says what is still open.
func (a *app) askToQuit() {
	if a.leaving {
		// Already asking.
		return
	}
	open := a.whatIsOpen()
	if len(open) == 0 {
		a.exitNow()
		return
	}
	a.leaving = true
	go func() {
		_, err := a.ask(a.ctx, Ask{
			Title: "Exit gridterm?", Text: "Still open: " + listOf(open) + ".",
			Yes: "Exit", Danger: true,
		})
		a.events <- func() {
			a.leaving = false
			if err == nil {
				a.exitNow()
			}
		}
	}()
}

// exitNow closes every pane, which closes the window.
func (a *app) exitNow() {
	for len(a.st.Panes) > 0 {
		a.remove(a.st.Panes[0].ID)
	}
}

// whatIsOpen is what the window would take with it, worst first: file
// work part way through is the one thing that cannot be started again
// where it left off.
func (a *app) whatIsOpen() []string {
	var out []string
	jobs := 0
	for _, r := range a.running {
		if !r.job.Progress().Done {
			jobs++
		}
	}
	if jobs > 0 {
		out = append(out, manyOf(jobs, "piece of file work", "pieces of file work"))
	}
	if n := len(a.st.Panes); n > 0 {
		out = append(out, manyOf(n, "pane", "panes"))
	}
	if n := len(a.tunnels); n > 0 {
		out = append(out, manyOf(n, "tunnel", "tunnels"))
	}
	if n := len(a.conns) + len(a.windows); n > 0 {
		out = append(out, manyOf(n, "connection", "connections"))
	}
	if len(a.agents.by) > 0 {
		out = append(out, "an agent share")
	}
	if a.serving.server != nil {
		out = append(out, "this window, served")
	}
	return out
}

// manyOf writes a number and the word for it.
func manyOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// listOf writes a few things as a person would say them.
func listOf(what []string) string {
	switch len(what) {
	case 0:
		return "nothing"
	case 1:
		return what[0]
	}
	return strings.Join(what[:len(what)-1], ", ") + " and " + what[len(what)-1]
}
