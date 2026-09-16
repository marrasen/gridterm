package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/mcp"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// agents are the panes handed to agents and the listener that lets
// those agents in.
//
// Only the goroutine that draws touches it.
type agents struct {
	// server is nil while nothing has been handed over. Nothing listens
	// until the user asks.
	server *agent.Server

	// by says which panes the user has handed to an agent, and the code
	// that names each. The codes live here and nowhere else.
	by map[*term.Terminal]*handover

	// next is the last name given to a handover. It only goes up, so a
	// name never comes round again and an agent holding an old one is
	// holding nothing.
	next uint64
}

// newAgents builds an agents with nothing listening and nothing handed
// over.
func newAgents() *agents {
	return &agents{by: make(map[*term.Terminal]*handover)}
}

// of is the handover for a pane, or nil when the pane has not been
// handed over.
func (g *agents) of(pane *term.Terminal) *handover { return g.by[pane] }

// hand records a pane as handed over and gives the handover its name.
func (g *agents) hand(pane *term.Terminal, code string) *handover {
	g.next++
	h := &handover{pane: pane, id: strconv.FormatUint(g.next, 10), code: code}
	g.by[pane] = h
	return h
}

// forget drops a pane's handover and reports whether there was one.
func (g *agents) forget(pane *term.Terminal) bool {
	if g.by[pane] == nil {
		return false
	}
	delete(g.by, pane)
	return true
}

// withCode is the handover a code names, or nil when no handover holds
// it.
func (g *agents) withCode(code string) *handover {
	for _, h := range g.by {
		if h.code == code {
			return h
		}
	}
	return nil
}

// named is the handover an id names, or nil when no handover holds it.
func (g *agents) named(id string) *handover {
	for _, h := range g.by {
		if h.id == id {
			return h
		}
	}
	return nil
}

// mark counts an agent in or out of a handover and reports whether the
// handover was still there.
func (g *agents) mark(id string, by int) bool {
	for _, h := range g.by {
		if h.id != id {
			continue
		}
		h.working = max(h.working+by, 0)
		return true
	}
	return false
}

// listening reports whether the window is listening for agents.
func (g *agents) listening() bool { return g.server != nil }

// listen starts listening, if it is not already.
func (g *agents) listen(cfg agent.Config) error {
	if g.server != nil {
		return nil
	}
	s, err := agent.Listen(cfg)
	if err != nil {
		return err
	}
	g.server = s
	return nil
}

// port is the port agents are listened for on, zero when nothing is
// listening.
func (g *agents) port() int {
	if g.server == nil {
		return 0
	}
	return g.server.Port()
}

// stopIfDone stops listening once nothing is handed over.
//
// Nothing to reach means nothing to listen for, and a port that is open
// for no reason is a port that should not be open.
func (g *agents) stopIfDone() error {
	if g.server == nil || len(g.by) > 0 {
		return nil
	}
	return g.stop()
}

// stop stops listening and forgets every handover.
func (g *agents) stop() error {
	if g.server == nil {
		return nil
	}
	s := g.server
	g.server = nil
	clear(g.by)
	return s.Close()
}

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
// Nothing listens until this is asked for. What the code lets an agent
// do is read this one pane, type into it, and wait.
func (a *app) handPane(pane *term.Terminal) error {
	if pane == nil {
		return errors.New("there is no pane here to hand over")
	}
	if pane.Exited() {
		return errors.New("the program in this pane has finished, so there is nothing to work in")
	}
	if have := a.agents.of(pane); have != nil {
		// Already handed over. The code is shown again rather than a
		// second one made: two codes for one pane is two things to take
		// back.
		a.showCode(have)
		return nil
	}
	if err := a.listenForAgents(); err != nil {
		return err
	}
	code, err := agent.NewCode(a.agents.port())
	if err != nil {
		return err
	}
	h := a.agents.hand(pane, code)
	a.markDirty()
	a.showCode(h)
	return nil
}

// takeBackPane stops an agent working in a pane.
//
// At once: the code stops naming anything and the next thing the agent
// asks fails. It does not wait for the agent to notice.
func (a *app) takeBackPane(pane *term.Terminal) error {
	if !a.agents.forget(pane) {
		return errors.New("no agent has been given this pane")
	}
	a.markDirty()
	return a.agents.stopIfDone()
}

// forgetHandover takes a pane's handover away, for one that has closed.
func (a *app) forgetHandover(pane *term.Terminal) error {
	if !a.agents.forget(pane) {
		return nil
	}
	return a.agents.stopIfDone()
}

// listenForAgents starts listening, if it is not already.
func (a *app) listenForAgents() error {
	return a.agents.listen(agent.Config{
		Window:  agentWindow{a: a},
		OnError: func(err error) { a.pump.post(func() { a.logError(err) }) },
		OnUse:   func(id string) { a.pump.post(func() { a.agentCame(id) }) },
		OnGone:  func(id string) { a.pump.post(func() { a.agentWent(id) }) },
	})
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
	if a.agents.mark(id, by) {
		a.markDirty()
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
		h := w.a.agents.withCode(code)
		if h == nil {
			return agent.Pane{}, errors.New(
				"that code does not name a pane this window has handed over")
		}
		e := w.a.panes[h.pane]
		if e == nil {
			return agent.Pane{}, errors.New("that pane is no longer open")
		}
		size := h.pane.Size()
		return agent.Pane{
			ID:    h.id,
			Label: agentLabel(e),
			Cols:  size.Cols,
			Rows:  size.Rows,
		}, nil
	})
}

func (w agentWindow) Look(id string) (agent.Look, error) {
	return onDrawing(w.a, func() (agent.Look, error) {
		h, err := w.a.handedPane(id)
		if err != nil {
			return agent.Look{}, err
		}
		said := h.pane.Said()
		// Read again only when the program has said something. A wait
		// asks twenty times a second, and reading a screen means
		// rendering the whole of it; a pane that is sitting there would
		// have it rendered afresh each time for the same answer.
		if h.screen == nil || h.said != said {
			text := h.pane.Text()
			h.screen, h.said = &text, said
		}
		return agent.Look{
			Screen:  *h.screen,
			Gone:    h.pane.Exited(),
			Changed: said,
		}, nil
	})
}

func (w agentWindow) Send(id, text string) error {
	_, err := onDrawing(w.a, func() (struct{}, error) {
		h, err := w.a.handedPane(id)
		if err != nil {
			return struct{}{}, err
		}
		if h.pane.Exited() {
			return struct{}{}, errors.New(
				"the program in that pane has finished, so nothing is left to type into")
		}
		h.pane.Send([]byte(text))
		return struct{}{}, nil
	})
	return err
}

// handedPane is the handover an id names, and only while that handover
// is still the one in force.
//
// An id names one handover. Taking the pane back ends it, and handing
// the same pane over again starts another with a name of its own, so an
// agent holding the old name gets nothing.
func (a *app) handedPane(id string) (*handover, error) {
	if h := a.agents.named(id); h != nil {
		return h, nil
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

// showCode shows the prompt for a handed-over pane, and puts the whole
// of it on the clipboard: the next thing it is for is being pasted into
// a conversation.
func (a *app) showCode(h *handover) {
	prompt := handoverPrompt(h.code, exePath())
	a.clip.set(prompt)
	lines := []string{
		"Paste this to the agent. The whole of it is on the clipboard",
		"already. It starts:",
		"",
	}
	for _, line := range strings.Split(promptOpening(prompt), "\n") {
		lines = append(lines, "  "+line)
	}
	lines = append(lines,
		"",
		"Then it says how to run gridterm -mcp as an MCP server, gives",
		"the agent this code, and says what to do with it:",
		"",
		"  "+h.code,
		"",
		"The agent can read this pane, type into it, and wait for it to",
		"settle. It reaches no other pane and nothing else of this",
		"window's. What it types goes into the shell running here, as",
		"whoever you set it up as, so it does whatever that shell does.",
		"",
		"You see everything it does, as it does it. The pane's row says",
		"when an agent is working in it.",
		"",
		"Take it back from the Servers menu, and the code stops working",
		"at once.",
	)
	f := a.newConfirm("An agent may work in this pane", lines)
	f.AddButton(ui.Button{Title: "Done"})
	f.AddButton(ui.Button{Title: "Take it back", Do: func() error {
		return a.takeBackPane(h.pane)
	}})
	a.showForm(f, nil)
}

// handoverPrompt is what the user pastes to an agent that has never
// heard of gridterm: what it has been handed, how to reach the MCP
// server, the code, and what to do with it.
func handoverPrompt(code, exe string) string {
	// A path inside JSON needs its backslashes doubled.
	inJSON := strings.ReplaceAll(exe, `\`, `\\`)
	inJSON = strings.ReplaceAll(inJSON, `"`, `\"`)
	return fmt.Sprintf(`The user has handed you one terminal pane in gridterm, a terminal
running on this machine. You work in that pane through gridterm's MCP
server, and the user watches everything you do.

Start that server with the command below. It speaks MCP on standard input and
output (stdio), and it has to run on this machine, because the port inside the
code is on the loopback address. In Claude Code, one line adds it:

  claude mcp add gridterm -- %s -mcp
For any other host, put this in its MCP config:
  {"mcpServers": {"gridterm": {
    "command": "%s", "args": ["-mcp"]}}}

This code is the only credential and it came from the user. Call
use_session_code with it before anything else. The answer names the pane, and
every other tool takes that name.

  %s

%s

%s
`, exe, inJSON, code, mcp.Workflow, mcp.Rules)
}

// promptOpening is the prompt's first paragraph, which is what the
// dialog shows of it.
func promptOpening(prompt string) string {
	first, _, _ := strings.Cut(prompt, "\n\n")
	return first
}

// exePath is this program's own path, for the lines that say how to
// start the MCP server. A system that will not say gives the plain
// name, which works wherever gridterm is on the PATH.
func exePath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "gridterm"
	}
	return exe
}
