package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// handover is one pane the user has handed to an agent.
//
// The code is the whole of what lets anything in. It is made fresh, it
// lives here and nowhere else, and taking the pane back throws it away.
type handover struct {
	pane *term.Terminal

	// id names this handover rather than the pane.
	//
	// Handing the same pane over again makes a new one, so an agent
	// still holding the old id is holding something that no longer
	// exists: taking a pane back has to mean something even when the
	// user hands it over again afterwards.
	id string

	code string

	// working counts the agents that have used the code, so the row can
	// say the pane is being worked in rather than merely offered. More
	// than one is unusual and is said plainly rather than hidden.
	working int

	// screen is the last screen read, and said is how much the program
	// had said when it was read. A wait asks over and over, and a pane
	// that has said nothing since has the same screen as last time.
	screen *string
	said   uint64
}

// handPane hands a pane to an agent and shows the user the code.
//
// Nothing listens until this is asked for, and what an agent can do
// with the code is read this one pane, type into it, and wait. It
// cannot open a connection, start a shell, reach the files, or touch
// any other pane.
func (a *app) handPane(pane *term.Terminal) error {
	if pane == nil {
		return errors.New("there is no pane here to hand over")
	}
	if pane.Exited() {
		return errors.New("the program in this pane has finished, so there is nothing to work in")
	}
	if have := a.handedBy[pane]; have != nil {
		// Already handed over. The code is shown again rather than a
		// second one made: two codes for one pane is two things to take
		// back.
		a.showCode(have)
		return nil
	}
	if err := a.listenForAgents(); err != nil {
		return err
	}
	code, err := agent.NewCode(a.agents.Port())
	if err != nil {
		return err
	}
	a.handedNext++
	h := &handover{pane: pane, id: strconv.FormatUint(a.handedNext, 10), code: code}
	a.handedBy[pane] = h
	a.markDirty()
	a.showCode(h)
	return nil
}

// takeBackPane stops an agent working in a pane.
//
// At once: the code stops naming anything and the next thing the agent
// asks fails. It does not wait for the agent to notice.
func (a *app) takeBackPane(pane *term.Terminal) error {
	h := a.handedBy[pane]
	if h == nil {
		return errors.New("no agent has been given this pane")
	}
	delete(a.handedBy, pane)
	a.markDirty()
	return a.stopListeningIfDone()
}

// forgetHandover takes a pane's handover away, for one that has closed.
func (a *app) forgetHandover(pane *term.Terminal) error {
	if a.handedBy[pane] == nil {
		return nil
	}
	delete(a.handedBy, pane)
	return a.stopListeningIfDone()
}

// listenForAgents starts listening, if it is not already.
func (a *app) listenForAgents() error {
	if a.agents != nil {
		return nil
	}
	s, err := agent.Listen(agent.Config{
		Window:  agentWindow{a: a},
		OnError: func(err error) { a.pump.post(func() { a.logError(err) }) },
		OnUse:   func(id string) { a.pump.post(func() { a.agentCame(id) }) },
		OnGone:  func(id string) { a.pump.post(func() { a.agentWent(id) }) },
	})
	if err != nil {
		return err
	}
	a.agents = s
	return nil
}

// stopListeningIfDone stops listening once nothing is handed over.
//
// Nothing to reach means nothing to listen for, and a port that is open
// for no reason is a port that should not be open.
func (a *app) stopListeningIfDone() error {
	if a.agents == nil || len(a.handedBy) > 0 {
		return nil
	}
	s := a.agents
	a.agents = nil
	return s.Close()
}

// closeAgents stops listening, for a window that is shutting down.
func (a *app) closeAgents() error {
	if a.agents == nil {
		return nil
	}
	s := a.agents
	a.agents = nil
	clear(a.handedBy)
	return s.Close()
}

// agentCame and agentWent say when an agent starts and stops working in
// a pane, so its row can say so.
func (a *app) agentCame(id string) { a.markAgent(id, 1) }
func (a *app) agentWent(id string) { a.markAgent(id, -1) }

// markAgent counts an agent in or out of a handover.
//
// A handover that has gone is not found, which is what an agent leaving
// after the user took the pane back looks like. There is nothing to say
// about it: the row it would have changed has gone too.
func (a *app) markAgent(id string, by int) {
	for _, h := range a.handedBy {
		if h.id != id {
			continue
		}
		h.working = max(h.working+by, 0)
		a.markDirty()
		return
	}
}

// note is what a pane's row says about the agent it was handed to.
func (h *handover) note() string {
	switch h.working {
	case 0:
		return agentOffered
	case 1:
		return agentAt
	}
	return strconv.Itoa(h.working) + " agents are working here"
}

// The two things a pane's row says about an agent. Offered is not the
// same as working in it: a code the user has copied and not yet given
// anybody is a pane nothing is touching.
const (
	agentAt      = "an agent is working here"
	agentOffered = "offered to an agent"
)

// isAgentNote reports whether a note is one of ours.
func isAgentNote(note string) bool {
	return note == agentAt || note == agentOffered ||
		strings.HasSuffix(note, " agents are working here")
}

// agentWindow is what an agent may do with the panes handed to it.
//
// Every method is called from a goroutine serving one agent, and every
// one of them hands the work to the goroutine that draws: the panes and
// what has been handed over are that goroutine's, like everything else
// the window holds.
type agentWindow struct{ a *app }

func (w agentWindow) Use(code string) (agent.Pane, error) {
	return onDrawing(w.a, func() (agent.Pane, error) {
		for pane, h := range w.a.handedBy {
			if h.code != code {
				continue
			}
			e := w.a.panes[pane]
			if e == nil {
				return agent.Pane{}, errors.New("that pane is no longer open")
			}
			size := pane.Size()
			return agent.Pane{
				ID:    h.id,
				Label: agentLabel(e),
				Cols:  size.Cols,
				Rows:  size.Rows,
			}, nil
		}
		return agent.Pane{}, errors.New(
			"that code does not name a pane this window has handed over")
	})
}

func (w agentWindow) Look(id string) (agent.Look, error) {
	return onDrawing(w.a, func() (agent.Look, error) {
		pane, err := w.a.handedPane(id)
		if err != nil {
			return agent.Look{}, err
		}
		h := w.a.handedBy[pane]
		said := pane.Said()
		// Read again only when the program has said something. A wait
		// asks twenty times a second, and reading a screen means
		// rendering the whole of it; a pane that is sitting there would
		// have it rendered afresh each time for the same answer.
		if h.screen == nil || h.said != said {
			text := pane.Text()
			h.screen, h.said = &text, said
		}
		return agent.Look{
			Screen:  *h.screen,
			Gone:    pane.Exited(),
			Changed: said,
		}, nil
	})
}

func (w agentWindow) Send(id, text string) error {
	_, err := onDrawing(w.a, func() (struct{}, error) {
		pane, err := w.a.handedPane(id)
		if err != nil {
			return struct{}{}, err
		}
		if pane.Exited() {
			return struct{}{}, errors.New(
				"the program in that pane has finished, so nothing is left to type into")
		}
		pane.Send([]byte(text))
		return struct{}{}, nil
	})
	return err
}

// handedPane is the pane an id names, and only while that handover is
// still the one in force.
//
// An id names one handover. Taking the pane back ends it, and handing
// the same pane over again starts another with a name of its own, so an
// agent holding the old name gets nothing.
func (a *app) handedPane(id string) (*term.Terminal, error) {
	for pane, h := range a.handedBy {
		if h.id == id {
			return pane, nil
		}
	}
	return nil, fmt.Errorf("%q is not a pane you have been handed any more", id)
}

// agentLabel is what to call a pane, for an agent holding more than
// one.
func agentLabel(e *conns.Entry) string {
	where := e.Host
	if where == conns.Local {
		where = "this machine"
	}
	if e.Label == "" {
		return where
	}
	return e.Label + " on " + where
}

// onDrawing runs something on the goroutine that draws and waits for it.
//
// Nothing the window holds may be touched from anywhere else, and every
// question an agent asks is about something the window holds.
func onDrawing[T any](a *app, f func() (T, error)) (T, error) {
	type answer struct {
		v   T
		err error
	}
	back := make(chan answer, 1)
	a.pump.post(func() {
		v, err := f()
		back <- answer{v: v, err: err}
	})
	select {
	case got := <-back:
		return got.v, got.err
	case <-a.ctx.Done():
		// The window is closing and nothing will run what was posted.
		// Waiting for it would park this goroutine for the rest of the
		// process.
		var zero T
		return zero, errors.New("this window is closing")
	}
}

// handHere hands the pane the user is looking at to an agent.
func (a *app) handHere() error { return a.handPane(a.focusedTerminal()) }

// takeBackHere stops an agent working in the pane the user is looking
// at.
func (a *app) takeBackHere() error { return a.takeBackPane(a.focusedTerminal()) }

// showCode shows the code for a handed-over pane, and puts it on the
// clipboard.
//
// On the clipboard because the next thing it is for is being pasted
// into a conversation with an agent, and a code is thirty-two
// characters nobody should have to read off a screen.
func (a *app) showCode(h *handover) {
	a.clip.set(h.code)
	f := a.newConfirm("An agent may work in this pane", []string{
		"Give this code to the agent. It is on the clipboard already.",
		"",
		"  " + h.code,
		"",
		"With it the agent can read this pane, type into it, and wait",
		"for it to settle. It reaches no other pane and nothing else of",
		"this window's.",
		"",
		"What it types goes into the shell running here, as whoever you",
		"set it up as, so it does whatever that shell does.",
		"",
		"You see everything it does, as it does it. Take the pane back",
		"from the sidebar or with this window's menu, and the code stops",
		"working at once.",
	})
	f.AddButton(ui.Button{Title: "Done"})
	f.AddButton(ui.Button{Title: "Take it back", Do: func() error {
		return a.takeBackPane(h.pane)
	}})
	a.showForm(f, nil)
}
