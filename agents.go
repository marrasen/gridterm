package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/mcp"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// agents are the panes shared with an agent and the listener that lets
// agents in.
//
// Only the goroutine that draws touches it.
type agents struct {
	// server is nil while nothing is shared. Nothing listens until the
	// user asks.
	server *agent.Server

	// open is the share the user is building, and nil when nothing is
	// shared. One at a time: a second would mean the menu asking which
	// share a pane joins.
	open *share

	// by says which panes are in that share. It is the share's own map,
	// kept here as well so that a pane's row can ask about itself.
	by map[*term.Terminal]*handover

	// next is the last name given to a share and to a pane in one. It
	// only goes up, so a name never comes round again and an agent
	// holding an old one is holding nothing.
	next uint64

	// remembered is which agent the share dialog opens on, kept between
	// runs. Nil until the window is given its settings.
	remembered *settings.Settings
}

// share is one code and the panes it reaches.
//
// The code is the whole of what lets anything in. It is made when the
// share starts, it lives here and nowhere else, and the share ending
// throws it away. Panes come and go while an agent works: it finds what
// it has by asking, and what it asks is answered from here.
type share struct {
	// id names this share. A pane's name carries it, which is how a
	// connection holding one share cannot reach another's panes.
	id uint64

	code string

	// panes is what is in the share.
	panes map[*term.Terminal]*handover

	// working counts the agents that have used the code, so a row can
	// say a pane is being worked in rather than merely offered. More
	// than one is unusual and is said plainly rather than hidden.
	working int
}

// newAgents builds an agents with nothing listening and nothing shared.
func newAgents() *agents {
	return &agents{by: make(map[*term.Terminal]*handover)}
}

// sharing reports whether the user has a share open.
func (g *agents) sharing() bool { return g.open != nil }

// code is the code the share is reached by, and "" when nothing is
// shared.
func (g *agents) code() string {
	if g.open == nil {
		return ""
	}
	return g.open.code
}

// shared is the panes in the open share, in the order they were added.
func (g *agents) shared() []*handover { return g.inShare(g.open) }

// inShare is the panes in one share, in the order they were added.
func (g *agents) inShare(sh *share) []*handover {
	if sh == nil {
		return nil
	}
	out := make([]*handover, 0, len(sh.panes))
	for _, h := range sh.panes {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].n < out[j].n })
	return out
}

// remember gives the window the settings the hand-over dialog opens on
// and writes itself back to.
func (g *agents) remember(set *settings.Settings) { g.remembered = set }

// startHost is the agent the hand-over dialog opens on: the one last
// picked, or Claude Code.
func (g *agents) startHost() agentHost {
	if g.remembered == nil {
		return hostNamed(hostClaudeCode)
	}
	name, saved := g.remembered.AgentHost()
	if !saved {
		return hostNamed(hostClaudeCode)
	}
	return hostNamed(name)
}

// rememberHost writes down which agent was picked, for the next run.
func (g *agents) rememberHost(name string) error {
	if g.remembered == nil {
		return nil
	}
	return g.remembered.PutAgentHost(name)
}

// of is the handover for a pane, or nil when the pane has not been
// handed over.
func (g *agents) of(pane *term.Terminal) *handover { return g.by[pane] }

// start opens a share with a code of its own, and does nothing when one
// is already open.
func (g *agents) start(code string) *share {
	if g.open != nil {
		return g.open
	}
	g.next++
	g.open = &share{id: g.next, code: code, panes: map[*term.Terminal]*handover{}}
	return g.open
}

// hand puts a pane in the open share and gives it its name.
//
// It starts on whatever the boxes were last set to, which is what the
// dialog opens showing. Nothing is allowed that the user has not ticked
// at some point.
func (g *agents) hand(pane *term.Terminal) *handover {
	if g.open == nil {
		return nil
	}
	if have := g.by[pane]; have != nil {
		return have
	}
	g.next++
	h := &handover{pane: pane, n: g.next, in: g.open, may: g.startMay()}
	g.open.panes[pane] = h
	g.by[pane] = h
	return h
}

// startMay is what the tick boxes open on: whatever was last ticked, and
// nothing at all the first time.
func (g *agents) startMay() settings.AgentMay {
	if g.remembered == nil {
		return settings.AgentMay{}
	}
	may, _ := g.remembered.AgentMay()
	return may
}

// rememberMay writes down what the boxes were set to, for the next
// hand-over.
func (g *agents) rememberMay(may settings.AgentMay) error {
	if g.remembered == nil {
		return nil
	}
	return g.remembered.PutAgentMay(may)
}

// endAsks ends anything an agent was waiting for the user to type in
// these panes, for panes that are being taken back.
func endAsks(panes ...*term.Terminal) {
	for _, pane := range panes {
		if pane != nil && pane.AskedForASecret() {
			pane.EndSecret(nil)
		}
	}
}

// forget drops a pane's handover, and with it every pane the agent
// opened from it, and reports whether there was one.
//
// The panes it opened were allowed by this one. Leaving them handed over
// after the user has taken this one back would leave the agent holding
// what they just took away.
func (g *agents) forget(pane *term.Terminal) bool {
	h := g.by[pane]
	if h == nil {
		return false
	}
	g.drop(pane)
	for other, from := range g.by {
		if g.openedBy(from, h) {
			g.drop(other)
		}
	}
	// A share with nothing in it is over, and its code stops naming
	// anything.
	if g.open != nil && len(g.open.panes) == 0 {
		g.open = nil
	}
	return true
}

// drop takes one pane out of the share.
func (g *agents) drop(pane *term.Terminal) {
	delete(g.by, pane)
	if g.open != nil {
		delete(g.open.panes, pane)
	}
	endAsks(pane)
}

// openedBy reports whether a handover was opened from another, however
// many panes along the way.
func (g *agents) openedBy(h, from *handover) bool {
	for at := h.from; at != nil; at = at.from {
		if at == from {
			return true
		}
	}
	return false
}

// withCode is the share a code names, or nil when no share holds it.
func (g *agents) withCode(code string) *share {
	if g.open != nil && g.open.code == code {
		return g.open
	}
	return nil
}

// withID is the share an id names, or nil when no share has it.
func (g *agents) withID(id uint64) *share {
	if g.open != nil && g.open.id == id {
		return g.open
	}
	return nil
}

// named is the handover an id names, or nil when no share holds it.
func (g *agents) named(id string) *handover {
	for _, h := range g.by {
		if h.name() == id {
			return h
		}
	}
	return nil
}

// mark counts an agent in or out of a share and reports whether the
// share was still there.
func (g *agents) mark(id uint64, by int) bool {
	sh := g.withID(id)
	if sh == nil {
		return false
	}
	sh.working = max(sh.working+by, 0)
	return true
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

// stopIfDone stops listening once nothing is shared.
//
// Nothing to reach means nothing to listen for, and a port that is open
// for no reason is a port that should not be open.
func (g *agents) stopIfDone() error {
	if g.server == nil || len(g.by) > 0 {
		return nil
	}
	return g.stop()
}

// stop stops listening and ends the share.
func (g *agents) stop() error {
	if g.server == nil {
		return nil
	}
	s := g.server
	g.server = nil
	clear(g.by)
	g.open = nil
	return s.Close()
}

// handover is one pane the user has handed to an agent.
//
// The code is the whole of what lets anything in. It is made fresh, it
// lives here and nowhere else, and taking the pane back throws it away.
type handover struct {
	pane *term.Terminal

	// n names this pane's place in the share, and in is the share it is
	// in. Together they are the name an agent is given.
	//
	// Adding the same pane again after taking it out makes a new one, so
	// an agent still holding the old name is holding something that no
	// longer exists: taking a pane out has to mean something even when
	// the user puts it back afterwards.
	n  uint64
	in *share

	// read is the last reading of the pane, lines is how many were asked
	// for, and size is how big the pane was. A wait asks over and over,
	// and a pane of the same size that has said nothing since has the
	// same screen as last time. A read of fewer lines than the last one
	// is cut from it rather than rendered again, so alternating counts
	// cannot make the window render on every question.
	read  *term.Reading
	lines int
	size  ui.Size

	// rendered counts the reads that went to the pane rather than to
	// the reading kept here.
	rendered int

	// typed is the prompt the agent last typed at and the line it was
	// on, for a shell that marks nothing. The prompt coming back below
	// that line is what stands in for a command finishing. Empty until
	// the agent has typed, and a pane on the alternate screen records
	// nothing: there are no lines there to count.
	typed     string
	typedLine uint64

	// typedDone is how many commands the shell had finished when the
	// agent last typed, so a finish since then is the agent's own and
	// one before it belongs to whatever ran here first.
	typedDone uint64

	// sent says the agent has typed here at all, which is what tells a
	// prompt that has not come back from a pane nobody has typed in.
	sent bool

	// may is what the user ticked when they handed this pane over. It
	// takes effect at once: turning a box off while the agent is
	// mid-call makes the next call fail, the way taking the pane back
	// does.
	//
	// A pane the agent opened has none of its own and reads the one it
	// was opened from, through from.
	may settings.AgentMay

	// from is the handover this one was opened from, and nil for a pane
	// the user handed over.
	//
	// What the user allowed there is what this pane gets, for as long as
	// they go on allowing it: unticking a box on the pane they handed
	// over reaches every pane the agent opened from it, and taking that
	// pane back takes these with it.
	from *handover
}

// allowed is what this handover lets an agent do.
//
// A pane opened from another asks that one, all the way back to the pane
// the user handed over. A chain that no longer reaches one -- the user
// took the first pane back -- allows nothing beyond reading and typing.
func (g *agents) allowed(h *handover) settings.AgentMay {
	for from := h; from != nil; from = from.from {
		if g.by[from.pane] != from {
			return settings.AgentMay{}
		}
		if from.from == nil {
			return from.may
		}
	}
	return settings.AgentMay{}
}

// openedFrom is how many panes an agent has open that were opened from
// this handover, counting the ones opened from those.
func (g *agents) openedFrom(h *handover) int {
	n := 0
	for _, other := range g.by {
		for from := other.from; from != nil; from = from.from {
			if from == h {
				n++
				break
			}
		}
	}
	return n
}

// handPane hands a pane to an agent and shows the user the code.
//
// Nothing listens until this is asked for. What the code lets an agent
// do is read this one pane, type into it, and wait.
//
// A pane whose program has finished can be handed over too: reading what
// it printed is worth something, and typing into it is refused where the
// typing happens.
func (a *app) handPane(pane *term.Terminal) error {
	if pane == nil {
		return errors.New("there is no pane here to share")
	}
	if have := a.agents.of(pane); have != nil {
		// Already in the share. What it allows is shown again rather
		// than the pane being added twice.
		a.showPaneBoxes(have)
		return nil
	}
	if err := a.listenForAgents(); err != nil {
		return err
	}
	if !a.agents.sharing() {
		code, err := agent.NewCode(a.agents.port())
		if err != nil {
			return err
		}
		a.agents.start(code)
	}
	h := a.agents.hand(pane)
	if h == nil {
		return errors.New("there is no share to add this pane to")
	}
	a.markDirty()
	a.refreshServers()
	a.showPaneBoxes(h)
	return nil
}

// takeBackPane stops an agent working in a pane.
//
// At once: the code stops naming anything and the next thing the agent
// asks fails. It does not wait for the agent to notice.
func (a *app) takeBackPane(pane *term.Terminal) error {
	if !a.agents.forget(pane) {
		return errors.New("this pane is not in a share")
	}
	a.markDirty()
	a.refreshServers()
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
		OnUse:   func(share uint64) { a.pump.post(func() { a.agentCame(share) }) },
		OnGone:  func(share uint64) { a.pump.post(func() { a.agentWent(share) }) },
	})
}

// agentCame and agentWent say when an agent starts and stops working in
// a share, so the rows of its panes can say so.
func (a *app) agentCame(share uint64) { a.markAgent(share, 1) }
func (a *app) agentWent(share uint64) { a.markAgent(share, -1) }

// markAgent counts an agent in or out of a share.
//
// A share that has ended is not found, which is what an agent leaving
// after the user ended it looks like. There is nothing to say about it:
// the rows it would have changed have gone too.
func (a *app) markAgent(id uint64, by int) {
	if a.agents.mark(id, by) {
		a.markDirty()
	}
}

// name is what an agent calls this pane: the share it is in and the pane
// inside it.
//
// The share is in the name so that a connection holding one share cannot
// reach another's panes, and so that a name from a share that has ended
// names nothing.
func (h *handover) name() string {
	return strconv.FormatUint(h.in.id, 10) + "." + strconv.FormatUint(h.n, 10)
}

// note is what a pane's row says about the agent it is shared with.
func (h *handover) note() string {
	switch h.in.working {
	case 0:
		return agentOffered
	case 1:
		return agentAt
	}
	return strconv.Itoa(h.in.working) + " agents are working here"
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

func (w agentWindow) Use(code string) (agent.Share, error) {
	return onDrawing(w.a, func() (agent.Share, error) {
		sh := w.a.agents.withCode(code)
		if sh == nil {
			return agent.Share{}, errors.New(
				"that code does not name a share this window is offering")
		}
		panes, err := w.a.told(sh)
		if err != nil {
			return agent.Share{}, err
		}
		return agent.Share{ID: sh.id, Panes: panes}, nil
	})
}

// Shared is the panes in a share as it stands now, which is how an agent
// learns that the user has added one or taken one out.
func (w agentWindow) Shared(id uint64) ([]agent.Pane, error) {
	return onDrawing(w.a, func() ([]agent.Pane, error) {
		sh := w.a.agents.withID(id)
		if sh == nil {
			return nil, errors.New("that share is over")
		}
		return w.a.told(sh)
	})
}

// told is every pane in a share, as an agent is told about them.
//
// A pane the window has since closed is left out rather than failing the
// whole answer: what the agent wants is the panes it still has.
func (a *app) told(sh *share) ([]agent.Pane, error) {
	var out []agent.Pane
	for _, h := range a.agents.inShare(sh) {
		pane, err := a.toldAbout(h)
		if err != nil {
			continue
		}
		out = append(out, pane)
	}
	return out, nil
}

// asMay is what a hand-over allows, as an agent is told it.
func asMay(may settings.AgentMay) agent.May {
	return agent.May{
		Restart:  may.Restart,
		OpenMore: may.OpenMore,
		ReadOnly: may.ReadOnly,
		ReadBack: may.ReadBack,
	}
}

func (w agentWindow) Look(id string, lines int) (agent.Look, error) {
	return onDrawing(w.a, func() (agent.Look, error) {
		h, err := w.a.handedPane(id)
		if err != nil {
			return agent.Look{}, err
		}
		// Bounded here as well as in the MCP server, because an agent
		// speaking to the wire itself does not go through that.
		want := min(max(lines, 0), agent.MostLines)
		size := h.pane.Size()
		if want == 0 {
			want = size.Rows
		}
		// Read again only when the program has said something, the pane
		// has been resized, or more lines are wanted than were read last
		// time. A wait asks twenty times a second, and reading a screen
		// means rendering the whole of it; a pane that is sitting there
		// would have it rendered afresh each time for the same answer. A
		// resize says nothing, and the reading kept here is of a screen
		// that no longer exists.
		if h.read == nil || h.read.Said != h.pane.Said() || want > h.lines || size != h.size {
			read := h.pane.ReadLines(want)
			h.read, h.lines, h.size = &read, want, size
			h.rendered++
		}
		// The cursor comes from the reading, so it says where it was on
		// the screen that came with it.
		screen := lastLines(h.read.Text, want)
		screen, atFloor, note := screen, false, ""
		if !w.a.agents.allowed(h).ReadBack {
			screen, atFloor, note = stopAtTheFloor(screen, *h.read)
		}
		status, hasStatus := h.read.Cmd.Exit()
		return agent.Look{
			Screen:    screen,
			Note:      note,
			Gone:      h.pane.Exited(),
			Changed:   h.read.Said,
			Row:       h.read.Row,
			Col:       h.read.Col,
			Alt:       h.read.Alt,
			All:       atFloor || countLines(screen) < want,
			Cols:      size.Cols,
			Rows:      size.Rows,
			Marks:     h.read.Cmd.Integrated,
			Running:   h.read.Cmd.Running,
			Done:      h.read.Cmd.Done,
			Status:    status,
			HasStatus: hasStatus,
			Back:      h.promptIsBack(*h.read),
			Watching:  h.typed != "",
			Yours:     h.sent && h.read.Cmd.Done > h.typedDone,
		}, nil
	})
}

// stopAtTheFloor cuts a reading off where the pane was last cleared, and
// says so when it cut anything.
//
// Clearing the screen means what it looks like it means: what was above
// it is not offered to an agent. It is not gone -- the person at this
// machine scrolls up and sees all of it -- which is what the note says,
// so an agent does not take a clear for a way of hiding anything.
//
// The alternate screen has no lines of the output to count, so nothing
// is cut there.
func stopAtTheFloor(screen string, read term.Reading) (string, bool, string) {
	// A floor of zero is a pane nobody has cleared. It is also a clear on
	// the very first line, where there is nothing above to hold back, so
	// the two need not be told apart.
	if read.Alt || read.Floor == 0 || read.Bottom < read.Floor {
		return screen, false, ""
	}
	below := int(read.Bottom-read.Floor) + 1
	if below > countLines(screen) {
		return screen, false, ""
	}
	// Reaching the floor is as far as this goes, whether or not a line
	// was cut off: asking for more gives no more.
	return lastLines(screen, below), true, "The pane was cleared, so the lines above the" +
		" clear are not offered here. They are still in the pane, and the user can scroll" +
		" up to them."
}

// lastLines is the last n lines of some text, and the whole of it when
// it has no more than that.
func lastLines(text string, n int) string {
	if n <= 0 {
		return ""
	}
	from := len(text)
	for left := n; left > 0; left-- {
		cut := strings.LastIndexByte(text[:from], '\n')
		if cut < 0 {
			return text
		}
		from = cut
	}
	return text[from+1:]
}

// countLines is how many lines some text has, counting an empty one at
// the end as the line it is.
func countLines(text string) int { return strings.Count(text, "\n") + 1 }

// Output is what the last command in a pane printed, without the screen
// around it.
//
// Where that output began comes from the shell when it marks its
// commands, and from the line the agent last typed on when it does not.
// A pane with neither is refused rather than answered with a rectangle
// of the screen, which is what read_pane is for.
func (w agentWindow) Output(id string, most int) (agent.Look, error) {
	return onDrawing(w.a, func() (agent.Look, error) {
		h, err := w.a.handedPane(id)
		if err != nil {
			return agent.Look{}, err
		}
		size := h.pane.Size()
		// One reading for the alternate screen and the boundary both,
		// because reading a pane renders it.
		at := h.pane.ReadLines(1)
		if at.Alt {
			return agent.Look{}, errors.New(
				"a full-screen program is drawing in that pane, so there is no command" +
					" output to read: read the pane instead")
		}
		from, note, err := h.outputFrom(at)
		if err != nil {
			return agent.Look{}, err
		}
		// A clear since the command started is where this begins instead:
		// the lines above it are not offered, whoever put them there. It
		// replaces the note rather than being added to it, because what
		// that said about where this starts is no longer true.
		if from < at.Floor && !w.a.agents.allowed(h).ReadBack {
			from = at.Floor
			note = "The pane was cleared after this output began, so this is only what has" +
				" been printed since the clear. The lines above it are still in the pane," +
				" and the user can scroll up to them."
		}
		// The cursor above where the output began is the screen having
		// been cleared or reset since: the output is not in the pane any
		// more, and the rows where it was are blank.
		if at.Line < from {
			return agent.Look{}, errors.New(
				"the screen has been cleared since that command started, so what it printed" +
					" is not in this pane any more: read the pane with read_pane instead")
		}
		// None asked for is as much as one read gives, the way the tool
		// asks for it; an agent speaking to the wire itself gets the same.
		want := agent.MostLines
		if most > 0 {
			want = min(most, agent.MostLines)
		}
		read, there := h.pane.ReadFrom(from, want)
		// Not kept as the pane's last reading: it starts at a boundary
		// rather than at the bottom, so a later read of fewer lines
		// cannot be cut from it.
		if read.Alt {
			// A full-screen program started between the two readings, so
			// what came back is its screen and not any command's output.
			return agent.Look{}, errors.New(
				"a full-screen program started drawing in that pane while this was" +
					" reading it, so there is no command output to give: read the pane instead")
		}
		status, hasStatus := read.Cmd.Exit()
		text := strings.TrimRight(read.Text, "\n")
		if countLines(read.Text) < there {
			note += fmt.Sprintf(" The start of it is missing: it printed %d lines and this"+
				" is the last %d.", there, countLines(read.Text))
		}
		if strings.TrimSpace(text) == "" {
			note += " Nothing is there: either it has printed nothing yet, or the screen" +
				" has been cleared since it started."
		}
		return agent.Look{
			// Trailing blank rows are the screen below the output, not
			// the output, and this answer is meant to be the output.
			Screen:  text,
			Gone:    h.pane.Exited(),
			Changed: read.Said,
			Row:     read.Row,
			Col:     read.Col,
			Alt:     read.Alt,
			// Not All: this read ends at a boundary the agent asked for,
			// so how much the pane has kept above it says nothing.
			Cols:      size.Cols,
			Rows:      size.Rows,
			Note:      note,
			Marks:     read.Cmd.Integrated,
			Running:   read.Cmd.Running,
			Done:      read.Cmd.Done,
			Status:    status,
			HasStatus: hasStatus,
			Back:      h.promptIsBack(read),
			Watching:  h.typed != "",
			Yours:     h.sent && read.Cmd.Done > h.typedDone,
		}, nil
	})
}

// outputFrom is the line the last command's output began on, and what to
// say about where that boundary came from. at is a reading of the pane
// taken now.
//
// The shell's own mark is taken only for a command that is the agent's:
// one running now, or one that finished since the agent typed. A mark
// from before that names the command before this one, and the line the
// agent typed on is the better boundary then. Sending the text and the
// return in two calls is the ordinary way to reach that.
func (h *handover) outputFrom(at term.Reading) (uint64, string, error) {
	from, marked := at.Cmd.Output()
	mine := at.Cmd.Running || (h.sent && at.Cmd.Done > h.typedDone)
	switch {
	case marked && (!h.sent || mine):
		return from, "This is what the last command printed, from where the shell said its" +
			" output began.", nil
	case h.sent:
		// The line typed on rather than the one after it: a command line
		// too long for the screen is echoed over two rows, and starting
		// below the first would cut the answer off inside it.
		return h.typedLine, "This shell does not say where a command's output begins, so" +
			" this is everything the pane has said since you last typed. It starts on the" +
			" line you typed at, so the first line is the prompt with your command echoed" +
			" after it.", nil
	}
	return 0, "", errors.New(
		"nothing here knows where the last command's output began: this shell does not" +
			" mark its commands, and you have typed nothing in this pane. Read the pane" +
			" with read_pane instead")
}

func (w agentWindow) Send(id, text string, keys []string) error {
	_, err := onDrawing(w.a, func() (struct{}, error) {
		h, err := w.a.handedPane(id)
		if err != nil {
			return struct{}{}, err
		}
		if w.a.agents.allowed(h).ReadOnly {
			return struct{}{}, errors.New(
				"the user handed this pane over to be read and not typed into." +
					" Ask them to turn \"Read only\" off if you need to type")
		}
		if h.pane.Exited() {
			return struct{}{}, errors.New(
				"the program in that pane has finished, so nothing is left to type into")
		}
		// Before the typing, so what is in front of the cursor is the
		// prompt rather than the prompt and what was typed at it.
		h.markPrompt()
		return struct{}{}, typeInto(h.pane, text, keys)
	})
	return err
}

// markPrompt writes down the prompt the agent is about to type at and
// the line it is on.
//
// It is what a shell that sends no marks has instead: the same prompt,
// further down the pane, is what finishing looks like from outside. A
// full-screen program has no prompt and no lines to count, so nothing is
// written down for one.
func (h *handover) markPrompt() {
	read := h.pane.ReadLines(1)
	h.sent, h.typedDone = true, read.Cmd.Done
	// A full-screen program has no prompt and no lines to count. What
	// was written down last time is left alone: the shell underneath is
	// still at the prompt it was, and clearing it would leave the agent
	// with nothing to watch for from the moment it pressed q to leave
	// less.
	if read.Alt {
		return
	}
	// A command typed in two calls -- the text in one, the return in the
	// next -- comes through here twice, and the second time what is in
	// front of the cursor is the prompt with the command after it.
	// Whatever was written down first on that line is the prompt.
	if h.typed != "" && read.Line == h.typedLine && strings.HasPrefix(read.Before, h.typed) {
		return
	}
	h.typed, h.typedLine = read.Before, read.Line
}

// promptIsBack reports whether the prompt the agent last typed at is
// back, further down the pane than it was typed at.
//
// The whole of what is in front of the cursor has to be the prompt and
// nothing else. A prefix would take any line of output that starts the
// way the prompt does -- a file of shell script read with cat under a
// prompt of "$", a diff under one of ">" -- for the prompt coming back,
// and tell the agent a command had finished half way through its output.
//
// Further down the pane, because the prompt is still on the screen the
// moment after the keys go in and the shell echoing what was typed
// leaves it there.
func (h *handover) promptIsBack(read term.Reading) bool {
	if h.typed == "" || read.Alt {
		return false
	}
	return read.Line > h.typedLine && read.Before == h.typed
}

// Restart starts a pane's program again, which is what the question on a
// dead pane offers the user.
//
// The same pane, so the code the agent holds goes on naming it and what
// the pane printed before is still above what runs now. On a pane that
// ran one command this runs that command again, which is why it is a box
// the user ticks and not a rule in the tool.
func (w agentWindow) Restart(id string) (agent.Pane, error) {
	return onDrawing(w.a, func() (agent.Pane, error) {
		h, err := w.a.handedPane(id)
		if err != nil {
			return agent.Pane{}, err
		}
		if !w.a.agents.allowed(h).Restart {
			return agent.Pane{}, errors.New(
				"this hand-over does not let you restart the pane." +
					` Ask the user to tick "Restart a closed connection"`)
		}
		if !h.pane.Exited() {
			return agent.Pane{}, errors.New(
				"the program in that pane is still running, so there is nothing to start again")
		}
		e := w.a.panes[h.pane]
		if e == nil {
			return agent.Pane{}, errors.New("that pane is no longer open")
		}
		// Starting the program again on a machine the window has let go
		// of means dialling it, and dialling is the user's. What the box
		// allows is a program started again on a connection this window
		// already holds.
		if err := w.a.connectedAlready(e.Host); err != nil {
			return agent.Pane{}, err
		}
		if err := w.a.startAgain(h.pane); err != nil {
			return agent.Pane{}, err
		}
		if h.pane.Exited() {
			// startAgain reports some failures to the user rather than to
			// its caller. The question on the pane is what offers the
			// user the same choice, so it is left where it is.
			return agent.Pane{}, errors.New(
				"the window could not start it again; the pane says why, and the user" +
					" can answer the question on it")
		}
		// The question on the pane was offering exactly this, and it has
		// been answered.
		h.pane.Ask("")
		return w.a.toldAbout(h)
	})
}

// Open opens another pane where a pane is, handed over as it opens.
//
// It opens no connection. A pane on this machine means another pane on
// this machine; a pane on a machine means another channel on the
// connection the window already holds, and a machine the window is not
// connected to is refused rather than dialled.
func (w agentWindow) Open(id string) (agent.Pane, error) {
	return onDrawing(w.a, func() (agent.Pane, error) {
		h, err := w.a.handedPane(id)
		if err != nil {
			return agent.Pane{}, err
		}
		if !w.a.agents.allowed(h).OpenMore {
			return agent.Pane{}, errors.New(
				"this hand-over does not let you open another pane." +
					` Ask the user to tick "Open another pane there"`)
		}
		e := w.a.panes[h.pane]
		if e == nil {
			return agent.Pane{}, errors.New("that pane is no longer open")
		}
		// Not from a pane that was opened to run one command. "Another
		// pane there" reads as that command run a second time, and this
		// opens a shell; neither is what the user meant to allow, so
		// they are asked for the pane they want instead.
		if e.Kind == conns.Command {
			return agent.Pane{}, errors.New(
				"that pane was opened to run one command, and this does not run commands." +
					" Ask the user to open the pane you need")
		}
		if err := w.a.connectedAlready(e.Host); err != nil {
			return agent.Pane{}, err
		}
		if opened := w.a.agents.openedFrom(h); opened >= mostOpened {
			return agent.Pane{}, fmt.Errorf(
				"you have opened %d panes from this one, which is as many as a hand-over"+
					" gives: work in the ones you have, or ask the user for another pane",
				opened)
		}
		was := w.a.panesNow()
		// Beside the agent's own pane, and the keys stay where the user
		// left them: a pane that opened itself under somebody's hands
		// would take the next thing they typed.
		focused := ui.FocusedLeaf(w.a.root.Widget())
		opening := w.a.openTerminalOn(e.Host, &spot{beside: h.pane})
		if focused != nil {
			w.a.focus(focused)
		}
		if opening != nil {
			return agent.Pane{}, opening
		}
		opened := w.a.paneOpenedSince(was)
		if opened == nil {
			return agent.Pane{}, errors.New("the window opened no pane")
		}
		// In the share as it opens, under the pane it was opened from:
		// the user said what an agent may do where that pane is, and
		// what they allow there is what this one gets, for as long as
		// they go on allowing it.
		next := w.a.agents.hand(opened)
		if next == nil {
			return agent.Pane{}, errors.New("that share is over")
		}
		next.from = h
		w.a.markDirty()
		return w.a.toldAbout(next)
	})
}

// Secret asks the user to type something into a pane, and waits.
//
// The characters never come here. The user types them into the pane, the
// pane passes them to the program that asked for them, and all that
// comes back is whether a line was typed at all. That is the whole point
// of it: an agent that needs a password can get past the prompt without
// ever being given one.
//
// The waiting happens off the goroutine that draws. Everything else an
// agent asks is answered on it, and a question that waits for a person
// would stop the window drawing for as long as they took.
func (w agentWindow) Secret(id, what string, wait time.Duration) (bool, error) {
	ask, err := onDrawing(w.a, func() (secretAsk, error) {
		h, err := w.a.handedPane(id)
		if err != nil {
			return secretAsk{}, err
		}
		// Read only means the agent puts nothing in the user's pane, and
		// this line is the one thing it could otherwise put there.
		if w.a.agents.allowed(h).ReadOnly {
			return secretAsk{}, errors.New(
				"the user handed this pane over to be read, so nothing of yours goes on it." +
					` Ask them to turn "Read only" off, or ask them for what you need` +
					" in your own words")
		}
		if h.pane.Exited() {
			return secretAsk{}, errors.New(
				"the program in that pane has finished, so nothing is waiting to be told anything")
		}
		answered, stop, err := h.pane.WaitForSecret(secretLine(what))
		if err != nil {
			return secretAsk{}, err
		}
		w.a.markDirty()
		return secretAsk{answered: answered, stop: stop}, nil
	})
	if err != nil {
		return false, err
	}
	// On the goroutine that draws, like everything else that touches the
	// pane, and however this ends: the line has to come off the pane and
	// the pane has to stop waiting.
	defer w.a.pump.post(ask.stop)

	// A person needs a moment, so the wait is not allowed to be an
	// instant: an ask that came back at once could be asked again and
	// again, and each one writes a line on the user's screen.
	wait = min(max(wait, shortestSecretWait), agent.LongestSecretWait)
	giveUp := time.NewTimer(wait)
	defer giveUp.Stop()

	select {
	case answered := <-ask.answered:
		if !answered {
			return false, errors.New(
				"that ask ended without the user answering it: the pane closed, the program" +
					" in it finished, or the user took the pane back")
		}
		return true, nil
	case <-giveUp.C:
		return false, nil
	case <-w.a.ctx.Done():
		return false, errors.New("this window is closing")
	}
}

// shortestSecretWait is the least an ask waits, whatever the agent asked
// for. Long enough that asking again and again is not a way to write on
// somebody's screen.
const shortestSecretWait = 5 * time.Second

// secretAsk is a pane waiting for the user to type something: where the
// answer comes back, and what stops waiting.
type secretAsk struct {
	answered <-chan bool
	stop     func()
}

// secretLine is what the pane says when an agent asks for a secret.
//
// It names the agent as the one asking and says plainly that what is
// typed does not go back to it, because a line asking for a password is
// exactly the line somebody should be suspicious of.
func secretLine(what string) string {
	if what = strings.TrimSpace(cleanSecretAsk(what)); what == "" {
		what = "something it says it cannot see"
	}
	// gridterm's own words first and the agent's last, in quotes: the
	// agent cannot push the warning off the first rows, and cannot make
	// its own text read as another line of this one.
	return `-- gridterm: an agent wants something typed here. gridterm never tells it` +
		` what you type. The program in this pane gets it, so if you can see the` +
		` characters as you type them, the agent can read them off the screen too.` +
		` It asked for: "` + what + `" --`
}

// cleanSecretAsk cuts an agent's own words down to one plain line.
//
// It is the agent's text on the user's screen, so it carries nothing
// that could draw somewhere else or pretend to be the window talking.
func cleanSecretAsk(what string) string {
	var out strings.Builder
	shown := 0
	for _, r := range what {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			out.WriteByte(' ')
		case r < ' ' || r == 0x7f:
			// Dropped: an escape sequence in it would draw.
		case r == '"':
			// The quotes around it are gridterm's, and a quote inside
			// would look like the end of them.
			out.WriteByte('\'')
		case unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cs, r) || unicode.Is(unicode.Co, r):
			// Zero-width marks, direction overrides and the like: they
			// take no room and change how the rest reads.
		case r == '-' && strings.HasSuffix(out.String(), "-"):
			// Two dashes are how one of this window's own remarks opens
			// and closes, and the agent's words are not one.
		default:
			out.WriteRune(r)
		}
		if shown++; shown >= mostSecretWords {
			break
		}
	}
	return out.String()
}

// mostSecretWords is how many characters of an agent's asking go on the
// screen. Enough to name what is wanted, short enough that the line it
// sits in is still read.
const mostSecretWords = 60

// connectedAlready says whether the window already holds a connection to
// a machine, so that opening another pane there opens nothing.
//
// This is the whole of the rule that an agent never dials. A machine the
// user has closed the connection to is theirs to open again.
func (a *app) connectedAlready(host string) error {
	if host == conns.Local {
		return nil
	}
	f := a.about(host)
	// A window that has not been taken over is reached by taking it
	// over, which is a connection of its own however the name is
	// recorded.
	if !f.toTakeOver() && (f.machine != nil || f.window != nil) {
		return nil
	}
	return fmt.Errorf("this window is not connected to %s any more,"+
		" and opening connections is the user's to do: ask them to connect to it", host)
}

// mostOpened is how many panes one handed-over pane may have opened from
// it.
//
// A pane opened this way is handed over as it opens, so without a cap an
// agent could open panes from panes without end. Enough for a shell to
// watch a log in beside the one being worked in, and no more.
const mostOpened = 4

// panesNow is the panes the window holds, for telling a new one from the
// ones that were already there.
func (a *app) panesNow() map[*term.Terminal]bool {
	was := make(map[*term.Terminal]bool, len(a.panes))
	for pane := range a.panes {
		was[pane] = true
	}
	return was
}

// paneOpenedSince is the one pane the window has that it did not have
// before, and nil when there is none or more than one.
func (a *app) paneOpenedSince(was map[*term.Terminal]bool) *term.Terminal {
	var found *term.Terminal
	for pane := range a.panes {
		if was[pane] {
			continue
		}
		if found != nil {
			return nil
		}
		found = pane
	}
	return found
}

// toldAbout is a handover as an agent is told about it.
func (a *app) toldAbout(h *handover) (agent.Pane, error) {
	e := a.panes[h.pane]
	if e == nil {
		return agent.Pane{}, errors.New("that pane is no longer open")
	}
	size := h.pane.Size()
	return agent.Pane{
		ID:    h.name(),
		Label: agentLabel(e),
		Cols:  size.Cols,
		Rows:  size.Rows,
		Ended: h.pane.Exited(),
		May:   asMay(a.agents.allowed(h)),
	}, nil
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

// shareItem is what the menu line that shares a pane says: a share is
// started once, and after that panes join the one that is open.
func (a *app) shareItem() string {
	if a.agents.sharing() {
		return "Add this pane to the share…"
	}
	return "Share this pane with an agent…"
}

// handHere hands the pane the user is looking at to an agent.
func (a *app) handHere() error { return a.handPane(a.focusedTerminal()) }

// takeBackHere stops an agent working in the pane the user is looking
// at.
func (a *app) takeBackHere() error { return a.takeBackPane(a.focusedTerminal()) }

// showPaneBoxes asks what the agent may do in one pane of the share.
//
// The code is not here: it belongs to the share and one is enough for
// all of it. What is here is the four boxes, which are this pane's own.
func (a *app) showPaneBoxes(h *handover) {
	f := a.newForm(paneBoxesTitle)
	f.Lines = []string{
		"This pane is in the share. An agent reads it and types into it,",
		"and each box below adds one thing, the moment you tick it.",
		"",
		"The code is the share's, and \"Show the share\" on the Servers",
		"menu has it, along with every pane in it.",
	}
	a.addAgentBoxes(f, h)
	f.Lines = append(f.Lines, "", "Space ticks a box.")
	f.AddButton(ui.Button{Title: "Done"})
	f.AddButton(ui.Button{Title: "Take it out", Do: func() error {
		return a.takeBackPane(h.pane)
	}})
	a.showForm(f, func() { a.rememberAgentMay(h.may) })
}

// paneBoxesTitle names the dialog that says what an agent may do in one
// pane.
const paneBoxesTitle = "What an agent may do in this pane"

// showShare is the share: the code, the panes in it, and what to paste
// to an agent.
//
// Which agent is asked first because the setup lines the prompt and the
// skill carry are that agent's, and because the skill goes wherever that
// agent reads skills from.
func (a *app) showShare() error {
	if !a.agents.sharing() {
		return errors.New("nothing is shared with an agent")
	}
	code := a.agents.code()
	f := a.newForm(shareTitle)
	f.Lines = []string{
		"An agent with this code reads the panes below and types into",
		"them, and nothing else of yours. The code is:",
		"  " + code,
	}
	// The code on its own, for an agent that has had the prompt already.
	f.Copyable = code

	pick := f.AddField("Agent", a.newField("", 0))
	pick.Options = agentHostNames()
	pick.SetText(a.agents.startHost().name)
	a.addShareRows(f)

	f.Lines = append(f.Lines, "",
		"Ctrl+down picks the agent, space takes a pane out or puts it",
		"back. "+a.copiesTheCode())

	// The three leave the form open, so the user can copy the prompt,
	// read the setup and write the skill in one visit.
	f.AddButton(ui.Button{Title: "Copy the prompt", Keep: true, Do: func() error {
		host := hostNamed(pick.Text())
		exe, err := exePath()
		if err != nil {
			// Said and carried on: the prompt still says "gridterm", which works
			// where gridterm is on the PATH, and the instructions say so.
			a.logError(err)
		}
		a.clip.set(handoverPrompt(host, code, exe))
		a.pump.post(func() {
			if err != nil {
				// The prompt on the clipboard says just "gridterm", which
				// works only where gridterm is on the PATH. Said here
				// because the user may never open the instructions.
				a.showNotice("The prompt says gridterm, not a path",
					"gridterm could not read its own path, so the line the prompt asks"+
						" the user to run says just gridterm. That works where gridterm"+
						" is on the PATH, and nowhere else.", true)
			}
			a.rememberAgentHost(host)
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Instructions", Keep: true, Do: func() error {
		host := hostNamed(pick.Text())
		exe, err := exePath()
		if err != nil {
			a.logError(err)
		}
		// Not from here: a dialog opened while this button is running would be
		// stacked before the form has finished with the press.
		a.pump.post(func() {
			a.showSetup(host, exe, err)
			// After that dialog, so a failure to write the settings down
			// lands on top of it rather than underneath.
			a.rememberAgentHost(host)
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Write the skill", Keep: true, Do: func() error {
		host := hostNamed(pick.Text())
		a.pump.post(func() {
			a.writeSkillFor(host, false)
			a.rememberAgentHost(host)
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Done"})
	f.AddButton(ui.Button{Title: "Stop sharing", Do: a.stopSharing})
	a.showForm(f, nil)
	return nil
}

// shareTitle names the dialog that shows the share.
const shareTitle = "Sharing with an agent"

// addShareRows puts a row on the share dialog for every pane in it: a
// tick box that takes the pane out and puts it back.
//
// A box rather than a list, because taking a pane out is the thing the
// dialog is for and a box is one key. The row stays after the pane goes,
// so putting it back is the same key again.
func (a *app) addShareRows(f *ui.Form) {
	for _, h := range a.agents.shared() {
		pane, in := h.pane, h
		tick := f.AddTick(a.shareRowLabel(h), true)
		tick.OnChange = func(string) {
			if tick.On() {
				a.agents.hand(pane)
			} else {
				a.agents.forget(pane)
			}
			a.markDirty()
			_ = a.agents.stopIfDone()
			_ = in
		}
	}
}

// shareRowLabel names a pane on the share dialog, and says what it
// allows beyond reading and typing.
func (a *app) shareRowLabel(h *handover) string {
	name := "a pane"
	if e := a.panes[h.pane]; e != nil {
		name = agentLabel(e)
	}
	may := a.agents.allowed(h)
	var adds []string
	if may.ReadOnly {
		adds = append(adds, "read only")
	}
	if may.Restart {
		adds = append(adds, "restart")
	}
	if may.OpenMore {
		adds = append(adds, "open panes")
	}
	if may.ReadBack {
		adds = append(adds, "read above a clear")
	}
	if len(adds) == 0 {
		return name
	}
	return name + " (" + strings.Join(adds, ", ") + ")"
}

// stopSharing ends the share, so the code stops working and every pane
// in it comes back.
func (a *app) stopSharing() error {
	if !a.agents.sharing() {
		return errors.New("nothing is shared with an agent")
	}
	for _, h := range a.agents.shared() {
		a.agents.forget(h.pane)
	}
	a.markDirty()
	a.refreshServers()
	return a.agents.stopIfDone()
}

// addAgentBoxes puts the tick boxes on the hand-over dialog: what this
// agent may do with this pane beyond reading it and typing into it.
//
// Every box is off unless the user has ticked it before, and turning one
// over changes what the agent may do at once -- the next call it makes
// is answered by the new answer, the way taking the pane back is.
func (a *app) addAgentBoxes(f *ui.Form, h *handover) {
	for _, box := range []struct {
		label string
		on    func(*settings.AgentMay) *bool
	}{
		{"Restart a closed connection", func(m *settings.AgentMay) *bool { return &m.Restart }},
		{"Open another pane there", func(m *settings.AgentMay) *bool { return &m.OpenMore }},
		{"Read only", func(m *settings.AgentMay) *bool { return &m.ReadOnly }},
		{"Read above a clear", func(m *settings.AgentMay) *bool { return &m.ReadBack }},
	} {
		at := box.on
		tick := f.AddTick(box.label, *at(&h.may))
		tick.OnChange = func(string) {
			*at(&h.may) = tick.On()
			a.markDirty()
		}
	}
}

// rememberAgentMay writes down what the boxes were set to, and says so
// when it could not be written.
func (a *app) rememberAgentMay(may settings.AgentMay) {
	if err := a.agents.rememberMay(may); err != nil {
		a.reportError("Could not remember what this hand-over allows", err)
	}
}

// copiesTheCode names the key that copies the code on its own, for a
// dialog that has room for the words and not for a button.
func (a *app) copiesTheCode() string {
	if chord := a.chordFor(copyCommand); chord != "" {
		return chord + " copies the code."
	}
	return "The copy chord copies the code."
}

// rememberAgentHost writes down which agent was picked, and says so when
// it could not be written.
func (a *app) rememberAgentHost(host agentHost) {
	if err := a.agents.rememberHost(host.name); err != nil {
		a.reportError("Could not remember which agent this was for", err)
	}
}

// writeSkillFor writes a host's skill and says where it went.
//
// over writes over a skill already there that says something else;
// without it the user is asked first, so their own edits are not lost.
func (a *app) writeSkillFor(host agentHost, over bool) {
	// The path is not guessed here: a skill saying only "gridterm" would sit on disk saying the
	// wrong thing long after the failure was forgotten.
	exe, err := exePath()
	if err != nil {
		a.reportError("Could not write the skill for "+host.name, err)
		return
	}
	path, ask, err := writeSkill(host, exe, over)
	if err != nil {
		a.reportError("Could not write the skill for "+host.name, err)
		return
	}
	if ask {
		a.askToReplaceSkill(host, path)
		return
	}
	a.showNotice("The skill for "+host.name+" is written", skillWritten(host, path), false)
}

// skillWritten says where a skill went, and says to move it when it went
// somewhere the host will not look.
func skillWritten(host agentHost, path string) string {
	if len(host.skillIn) > 0 {
		return "The skill is at\n\n  " + path + "\n\n" +
			"Start " + host.name + " again and it will read it."
	}
	return "gridterm does not know where " + host.name + " reads skills from," +
		" so the skill went under gridterm's own settings:\n\n  " + path + "\n\n" +
		"Copy it to wherever that host reads skills from."
}

// askToReplaceSkill asks before writing over a skill that says something
// else, which is a skill the user may have edited.
func (a *app) askToReplaceSkill(host agentHost, path string) {
	f := a.newConfirm("Replace the skill that is there?", []string{
		"There is already a skill at",
		"",
		"  " + path,
		"",
		"and it says something else. Replacing it loses whatever was",
		"changed in it.",
	})
	f.AddButton(ui.Button{Title: "Replace", Do: func() error {
		// Not from here: this form closes as soon as this returns, and
		// closing a dialog takes anything stacked on top of it.
		a.pump.post(func() { a.writeSkillFor(host, true) })
		return nil
	}})
	f.AddButton(ui.Button{Title: "Keep"})
	a.showForm(f, nil)
}

// showSetup says how to add gridterm's MCP server to a host: what to run
// or write, and that the host has to be started again afterwards.
//
// Short on purpose. A dialog draws the lines that fit and drops the
// rest, and this one has to read whole in eighty columns by twenty four.
func (a *app) showSetup(host agentHost, exe string, exeErr error) {
	lines := host.setupLines(exe)
	if exeErr != nil {
		lines = append(lines, "",
			"gridterm could not read its own path, so that says just",
			"gridterm, which works where gridterm is on the PATH.")
	}
	// Two lines, because one runs wider than a dialog draws and a line
	// too wide is trimmed at the edge without a word.
	lines = append(lines, "",
		`"`+host.copyTitle()+`" puts `+host.copyWhat()+" on the clipboard.")
	if chord := a.chordFor(copyCommand); chord != "" {
		lines = append(lines, chord+" does the same.")
	}
	f := a.newConfirm("Adding gridterm to "+host.called, lines)
	// The line itself, not the dialog: what the user does with this is
	// paste it into a shell or a config file.
	f.Copyable = host.setupToCopy(exe)
	f.AddButton(ui.Button{Title: host.copyTitle(), Keep: true, Do: func() error {
		a.clip.set(f.Copyable)
		return nil
	}})
	f.AddButton(ui.Button{Title: "Done"})
	a.showForm(f, nil)
}

// handoverPrompt is what the user pastes to an agent that has never
// heard of gridterm: what it has been handed, how to reach the MCP
// server on the host it is running in, and the code.
//
// It carries the rules in short as well. How the tools work comes from
// the MCP server's own instructions, which a host passes to the agent
// when it connects; the rules are here too, because a host is free to
// pass none of that on and the rules are the part with a cost.
func handoverPrompt(host agentHost, code, exe string) string {
	return fmt.Sprintf(`The user has shared terminal panes with you in gridterm, a terminal
running on this machine. You work in those panes through gridterm's MCP
server, and the user watches everything you do.

That server runs on this machine, on standard input and output (stdio), because
the port inside the code is on the loopback address. If you do not have
gridterm's tools, it has not been added here yet.

%s

This code is the only credential and it came from the user. Call
use_session_code with it before anything else. The answer lists the panes, and
every other tool takes a pane's name.

  %s

%s

The server's own instructions say how the tools work, and say this again.
`, host.setupForAgent(exe), code, mcp.Short)
}

// exePath is this program's own path, for the lines that say how to
// start the MCP server, and the error when the system would not say.
//
// The plain name comes back alongside the error, and works only where
// gridterm is on the PATH. Every caller either says so or refuses. Under
// go run the path is a temporary binary, which is a real path and a
// useless one.
// A variable so a test can fail it: the path is read from the system,
// and every caller has something to say when it cannot be.
var exePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "gridterm", fmt.Errorf("gridterm could not read its own path: %w", err)
	}
	if exe == "" {
		return "gridterm", errors.New("gridterm could not read its own path")
	}
	return exe, nil
}
