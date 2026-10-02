package app

import (
	"log"
	"slices"

	"github.com/marrasen/gunim"
)

// A question that wants something typed, a password or a passphrase,
// opens in a small window of its own, over the others, where the user
// is working, instead of as a dialog in one of kakel's windows: such a
// question often comes while the user works in a file manager window,
// and a dialog in a window behind it would take the keyboard there.
//
// One is open at a time, for the oldest such question; the next opens
// once it is answered. Questions to choose an answer to stay dialogs in
// the window in front. Where no window can open, the question is a
// dialog after all.

// PromptOpener opens a window of its own for q, with its view mounted,
// in the theme named, centred over near, the kakel window in front, or
// on the main display when near is nil. It runs on a goroutine of its
// own.
type PromptOpener func(q Ask, theme string, near *gunim.Window) (gunim.Client, error)

// promptState is the prompt window as it is: the question it shows,
// or 0 for none, and the window once it is open.
type promptState struct {
	id      uint64
	c       *gunim.Client
	opening bool
}

// pose adds q to the questions asked: in a window of its own when it
// wants something typed and such a window can open, and otherwise in
// the window in front.
func (a *app) pose(q Ask) {
	q.win = a.frontID()
	q.alone = len(q.Prompts) > 0 && a.openPrompt != nil
	a.st.Asks = append(a.st.Asks, q)
}

// prompted reports whether a question waits in a window of its own, or
// for one to open.
func (a *app) prompted() bool {
	return slices.ContainsFunc(a.st.Asks, func(q Ask) bool { return q.alone })
}

// showPrompt opens the window for the oldest question that wants one,
// and closes the window of one answered or dropped.
func (a *app) showPrompt() {
	if a.gone || a.openPrompt == nil {
		return
	}
	p := &a.prompt
	if p.id != 0 && !slices.ContainsFunc(a.st.Asks, func(q Ask) bool { return q.ID == p.id }) {
		a.closePrompt()
	}
	if p.id != 0 {
		return
	}
	i := slices.IndexFunc(a.st.Asks, func(q Ask) bool { return q.alone })
	if i < 0 {
		return
	}
	q := a.st.Asks[i]
	p.id, p.opening = q.ID, true
	open, theme := a.openPrompt, a.st.Theme
	var near *gunim.Window
	if a.cur != nil && !a.cur.gone {
		near = a.cur.gw
	}
	go func() {
		c, err := open(q, theme, near)
		a.events <- func() { a.promptOpened(q.ID, c, err) }
	}()
}

// promptOpened takes the window opened for question id, which shows it
// from now on, or which closes again when the question went meanwhile.
// A window that could not open leaves the question to a dialog.
func (a *app) promptOpened(id uint64, c gunim.Client, err error) {
	p := &a.prompt
	if err != nil {
		log.Printf("couldn't open a window to ask in: %v", err)
		for i := range a.st.Asks {
			if a.st.Asks[i].ID == id {
				a.st.Asks[i].alone = false
			}
		}
		if p.id == id {
			*p = promptState{}
		}
		return
	}
	if a.gone || p.id != id || !p.opening {
		c.Close()
		return
	}
	p.opening, p.c = false, &c
	_ = c.SetTheme(a.st.Theme)
	c.ToFront()
	go func() {
		for env := range c.Intents() {
			a.events <- func() { a.fromPrompt(id, env.Intent) }
		}
		a.events <- func() { a.promptClosed(id, c) }
	}()
}

// fromPrompt does what the window asking question id asks: answers it,
// or does what one of its buttons that keeps it open does.
func (a *app) fromPrompt(id uint64, in gunim.Intent) {
	switch in := in.(type) {
	case AskAnswered:
		if in.ID == id {
			a.handle(in)
		}
	case AskAction:
		if in.ID == id {
			a.handle(in)
		}
	}
}

// promptClosed hears that window c, asking question id, closed. Closed
// by the user before an answer, as by Alt+F4, it answers no.
func (a *app) promptClosed(id uint64, c gunim.Client) {
	if p := &a.prompt; p.c != nil && *p.c == c {
		*p = promptState{}
	}
	if _, ok := a.replies[id]; ok {
		a.handle(AskAnswered{ID: id})
	}
}

// closePrompt closes the prompt window, at once as kakel leaves.
func (a *app) closePrompt() {
	if c := a.prompt.c; c != nil {
		if a.gone {
			c.Close()
		} else {
			c.Leave()
		}
	}
	a.prompt = promptState{}
}
