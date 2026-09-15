package main

import (
	"errors"
	"fmt"

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
	code string

	// working says an agent has used the code, so the row can say the
	// pane is being worked in rather than merely offered.
	working bool
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
	h := &handover{pane: pane, code: code}
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
func (a *app) agentCame(id string) { a.markAgent(id, true) }
func (a *app) agentWent(id string) { a.markAgent(id, false) }

func (a *app) markAgent(id string, working bool) {
	for pane, h := range a.handedBy {
		if a.panes[pane] != nil && a.panes[pane].ID() == id {
			h.working = working
			a.markDirty()
			return
		}
	}
}

// agentNote is what a pane's row says about the agent it was handed to.
func (h *handover) note() string {
	if h.working {
		return agentAt
	}
	return agentOffered
}

// The two things a pane's row says about an agent. Offered is not the
// same as working in it: a code the user has copied and not yet given
// anybody is a pane nothing is touching.
const (
	agentAt      = "an agent is working here"
	agentOffered = "offered to an agent"
)

// isAgentNote reports whether a note is one of ours.
func isAgentNote(note string) bool { return note == agentAt || note == agentOffered }

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
				ID:    e.ID(),
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
		return agent.Look{
			Screen:  pane.Text(),
			Gone:    pane.Exited(),
			Changed: pane.Said(),
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
				"the program in that pane has finished, so there is nothing to read it")
		}
		pane.Send([]byte(text))
		return struct{}{}, nil
	})
	return err
}

// handedPane is the pane an id names, and only while it is still handed
// over.
func (a *app) handedPane(id string) (*term.Terminal, error) {
	for pane, h := range a.handedBy {
		e := a.panes[pane]
		if e != nil && e.ID() == id {
			_ = h
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
		"for it to settle. It can do nothing else: no other pane, no",
		"connection of its own, no files.",
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
