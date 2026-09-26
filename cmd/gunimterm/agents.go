package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/mcp"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	uiterm "github.com/marrasen/gridterm/ui/term"
)

// Panes shared with an agent, as gridterm shares them. The user puts
// panes into a share and gives a program they are talking to the
// share's one code. With it, through gridterm's MCP server, the agent
// can read those panes, type into them and wait for them to settle,
// and reaches nothing else. Nothing listens until a pane is shared,
// the port is on the loopback address, and taking the last pane back
// makes the code useless.

// Share is the share, as the window shows it: its code, and the panes
// in it with what each allows.
type Share struct {
	Code  string
	Panes []SharedPane
	// Host is the agent program the prompt is written for.
	Host string
}

// SharedPane is one pane in the share.
type SharedPane struct {
	Pane string
	May  settings.AgentMay
	// Note says whether an agent is working in it or it is only
	// offered.
	Note string
}

// Intents for sharing panes with an agent.
type (
	// SharePane puts a pane into the share, starting one if there is
	// none.
	SharePane struct{ Pane string }
	// UnsharePane takes a pane out of the share.
	UnsharePane struct{ Pane string }
	// StopSharing ends the share, taking every pane back.
	StopSharing struct{}
	// SetAgentMay says what the agent may do in a pane beyond reading
	// and typing.
	SetAgentMay struct {
		Pane string
		May  settings.AgentMay
	}
	// CopyAgentPrompt puts the prompt that hands the share to an agent
	// program on the clipboard, and CopyAgentSetup the line that adds
	// the MCP server to it.
	CopyAgentPrompt struct{ Host string }
	CopyAgentSetup  struct{ Host string }
)

// agents is the program's side of the share.
type agents struct {
	server *agent.Server
	open   *share
	by     map[string]*handover
	next   uint64
}

// share is a set of panes behind one code.
type share struct {
	id    uint64
	code  string
	panes map[string]*handover
	// working counts the agents connected to it.
	working int
}

// handover is one pane in a share, and what the agent has done there.
type handover struct {
	pane string
	n    uint64
	in   *share
	// read is the last reading given, lines how many lines it holds,
	// and size the pane's size then; a reading still current is given
	// again rather than taken again.
	read  *uiterm.Reading
	lines int
	size  ui.Size
	// typed is the prompt the agent last typed at, on typedLine, and
	// typedDone the commands finished by then; sent says it has typed.
	typed     string
	typedLine uint64
	typedDone uint64
	sent      bool
	// given says the agent has seen the pane.
	given bool
	may   settings.AgentMay
	// from is the pane this one was opened from, by the agent.
	from *handover
}

func (h *handover) name() string {
	return strconv.FormatUint(h.in.id, 10) + "." + strconv.FormatUint(h.n, 10)
}

// allowed is what a pane allows: its own boxes, or for a pane the
// agent opened, those of the pane it opened it from.
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

// mostOpened is how many panes an agent may open from one it was given.
const mostOpened = 4

// terminal returns the terminal in pane id, or nil.
func (a *app) terminal(id string) *uiterm.Terminal {
	if sh := a.shells.get(id); sh != nil && a.kindOfPane(id) == kindTerminal {
		return sh.t
	}
	return nil
}

// sharePane puts a pane into the share, listening and making a code
// first when there is no share yet.
func (a *app) sharePane(id string) error {
	if a.terminal(id) == nil {
		return errors.New("only a terminal pane can be shared with an agent")
	}
	if a.agents.by[id] != nil {
		return nil
	}
	if a.agents.server == nil {
		s, err := agent.Listen(agent.Config{
			Window:  agentWindow{a},
			OnError: func(err error) { a.events <- func() { a.notify("The agent share had trouble", err.Error(), "") } },
			OnUse:   func(id uint64) { a.events <- func() { a.markAgent(id, 1) } },
			OnGone:  func(id uint64) { a.events <- func() { a.markAgent(id, -1) } },
		})
		if err != nil {
			return err
		}
		a.agents.server = s
	}
	if a.agents.open == nil {
		code, err := agent.NewCode(a.agents.server.Port())
		if err != nil {
			return errors.Join(err, a.stopSharingIfDone())
		}
		a.agents.next++
		a.agents.open = &share{id: a.agents.next, code: code, panes: map[string]*handover{}}
	}
	a.handInto(a.agents.open, id)
	a.showShare()
	return nil
}

// handInto puts pane id into a share, with the boxes ticked last time.
func (a *app) handInto(sh *share, id string) *handover {
	if have := a.agents.by[id]; have != nil {
		return have
	}
	a.agents.next++
	h := &handover{pane: id, n: a.agents.next, in: sh}
	if a.settings != nil {
		h.may, _ = a.settings.AgentMay()
	}
	sh.panes[id] = h
	a.agents.by[id] = h
	return h
}

// unsharePane takes a pane out of the share, with any the agent opened
// from it, and stops listening once nothing is shared.
func (a *app) unsharePane(id string) error {
	h := a.agents.by[id]
	if h == nil {
		return nil
	}
	a.dropHandover(id)
	for other, from := range a.agents.by {
		for at := from.from; at != nil; at = at.from {
			if at == h {
				a.dropHandover(other)
				break
			}
		}
	}
	if a.agents.open != nil && len(a.agents.open.panes) == 0 {
		a.agents.open = nil
	}
	a.showShare()
	return a.stopSharingIfDone()
}

func (a *app) dropHandover(id string) {
	if h := a.agents.by[id]; h != nil {
		delete(h.in.panes, id)
	}
	delete(a.agents.by, id)
	if t := a.terminal(id); t != nil && t.AskedForASecret() {
		t.EndSecret(nil)
	}
}

// stopSharing takes every pane back and stops listening.
func (a *app) stopSharing() error {
	for id := range a.agents.by {
		a.dropHandover(id)
	}
	a.agents.open = nil
	a.showShare()
	return a.stopSharingIfDone()
}

// stopSharingIfDone stops listening once no pane is shared.
func (a *app) stopSharingIfDone() error {
	if a.agents.server == nil || len(a.agents.by) > 0 {
		return nil
	}
	s := a.agents.server
	a.agents.server = nil
	return s.Close()
}

func (a *app) markAgent(id uint64, by int) {
	if sh := a.agents.open; sh != nil && sh.id == id {
		sh.working = max(sh.working+by, 0)
		a.showShare()
	}
}

// setAgentMay changes what a pane allows, and keeps it as what the
// next pane shared starts with.
func (a *app) setAgentMay(in SetAgentMay) {
	h := a.agents.by[in.Pane]
	if h == nil {
		return
	}
	h.may = in.May
	if a.settings != nil {
		_ = a.settings.PutAgentMay(in.May)
	}
	a.showShare()
}

// showShare publishes the share.
func (a *app) showShare() {
	sh := a.agents.open
	host := hostClaudeCode
	if a.settings != nil {
		if name, ok := a.settings.AgentHost(); ok {
			host = hostNamed(name).name
		}
	}
	if sh == nil {
		a.st.Share = Share{Host: host}
		return
	}
	out := Share{Code: sh.code, Host: host}
	for _, h := range sh.panes {
		note := "offered to an agent"
		switch {
		case !h.given || sh.working == 0:
		case sh.working == 1:
			note = "an agent is working here"
		default:
			note = strconv.Itoa(sh.working) + " agents are working here"
		}
		out.Panes = append(out.Panes, SharedPane{Pane: h.pane, May: h.may, Note: note})
	}
	slices.SortFunc(out.Panes, func(x, y SharedPane) int {
		return int(a.agents.by[x.Pane].n) - int(a.agents.by[y.Pane].n)
	})
	a.st.Share = out
}

// copyAgentPrompt puts on the clipboard the prompt that gives an agent
// program the share.
func (a *app) copyAgentPrompt(name string) {
	sh := a.agents.open
	if sh == nil {
		return
	}
	host := hostNamed(name)
	if a.settings != nil {
		_ = a.settings.PutAgentHost(host.name)
	}
	a.notify("Prompt copied", "Paste it into "+host.called+". It carries the share's code.", handoverPrompt(host, sh.code, exePath()))
	a.showShare()
}

// copyAgentSetup puts on the clipboard what adds the MCP server to an
// agent program.
func (a *app) copyAgentSetup(name string) {
	host := hostNamed(name)
	if a.settings != nil {
		_ = a.settings.PutAgentHost(host.name)
	}
	what := "the command"
	if host.cmd == "" {
		what = "the config"
	}
	a.notify("Setup copied", "Add "+what+" to "+host.called+", then start it again.", host.setupToCopy(exePath()))
	a.showShare()
}

// onApp runs f on the program's goroutine and waits for what it says.
// The agent's goroutines ask through it, as the panes are the
// program's.
func onApp[T any](a *app, f func() (T, error)) (T, error) {
	type answer struct {
		v   T
		err error
	}
	back := make(chan answer, 1)
	select {
	case a.events <- func() {
		v, err := f()
		back <- answer{v, err}
	}:
	case <-a.ctx.Done():
		var zero T
		return zero, errors.New("this window is closing")
	}
	select {
	case got := <-back:
		return got.v, got.err
	case <-a.ctx.Done():
		var zero T
		return zero, errors.New("this window is closing")
	}
}

// agentWindow is what an agent reaches: the panes of the share its
// code names, and nothing else.
type agentWindow struct{ a *app }

func (a *app) handedPane(id string) (*handover, *uiterm.Terminal, error) {
	for _, h := range a.agents.by {
		if h.name() == id {
			if t := a.terminal(h.pane); t != nil {
				return h, t, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("%q is not a pane you have been handed any more", id)
}

// gave notes that the agent has seen a pane, which the sidebar shows.
func (a *app) gave(h *handover) {
	if !h.given {
		h.given = true
		a.showShare()
	}
}

// toldAbout is a pane as the agent is told of it.
func (a *app) toldAbout(h *handover) (agent.Pane, error) {
	t := a.terminal(h.pane)
	if t == nil {
		return agent.Pane{}, errors.New("that pane is no longer open")
	}
	a.gave(h)
	size := t.Size()
	may := a.agents.allowed(h)
	return agent.Pane{
		ID: h.name(), Label: a.agentLabel(h.pane), Cols: size.Cols, Rows: size.Rows, Ended: t.Exited(),
		May: agent.May{Restart: may.Restart, OpenMore: may.OpenMore, ReadOnly: may.ReadOnly, ReadBack: may.ReadBack},
	}, nil
}

// agentLabel is what the agent calls a pane: its title and where it is.
func (a *app) agentLabel(id string) string {
	where := a.machineOf(id)
	if where == "" {
		where = "this machine"
	}
	return a.titleOf(id) + " on " + where
}

func (a *app) told(sh *share) []agent.Pane {
	var hs []*handover
	for _, h := range sh.panes {
		hs = append(hs, h)
	}
	slices.SortFunc(hs, func(x, y *handover) int { return int(x.n) - int(y.n) })
	var out []agent.Pane
	for _, h := range hs {
		if p, err := a.toldAbout(h); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Use implements [agent.Window].
func (w agentWindow) Use(code string) (agent.Share, error) {
	return onApp(w.a, func() (agent.Share, error) {
		sh := w.a.agents.open
		if sh == nil || sh.code != code {
			return agent.Share{}, errors.New("that code does not name a share this window is offering")
		}
		return agent.Share{ID: sh.id, Panes: w.a.told(sh)}, nil
	})
}

// Shared implements [agent.Window].
func (w agentWindow) Shared(id uint64) ([]agent.Pane, error) {
	return onApp(w.a, func() ([]agent.Pane, error) {
		sh := w.a.agents.open
		if sh == nil || sh.id != id {
			return nil, agent.ErrShareOver
		}
		return w.a.told(sh), nil
	})
}

// Look implements [agent.Window].
func (w agentWindow) Look(id string, lines int) (agent.Look, error) {
	return onApp(w.a, func() (agent.Look, error) {
		h, t, err := w.a.handedPane(id)
		if err != nil {
			return agent.Look{}, err
		}
		w.a.gave(h)
		want := min(max(lines, 0), agent.MostLines)
		size := t.Size()
		if want == 0 {
			want = size.Rows
		}
		need := max(want, size.Rows)
		if h.read == nil || h.read.Said != t.Said() || need > h.lines || size != h.size {
			read := t.ReadLines(need)
			h.read, h.lines, h.size = &read, need, size
		}
		text, atFloor, note := h.read.Text, false, ""
		if !w.a.agents.allowed(h).ReadBack {
			text, atFloor, note = stopAtTheFloor(text, *h.read)
		}
		enough := countLines(text) < want
		cut := trimBlankTail(text)
		trimmed := len(cut) < len(text)
		status, hasStatus := h.read.Cmd.Exit()
		return agent.Look{
			Screen: lastLines(cut, want), Note: note, Gone: t.Exited(), Changed: h.read.Said,
			Row: h.read.Row, Col: h.read.Col, Alt: h.read.Alt, All: atFloor || enough, Trimmed: trimmed,
			Pictures: picturesSeen(h.read.Pictures), Cols: size.Cols, Rows: size.Rows,
			Marks: h.read.Cmd.Integrated, Running: h.read.Cmd.Running, Done: h.read.Cmd.Done,
			Status: status, HasStatus: hasStatus, Back: h.promptIsBack(*h.read),
			Watching: h.typed != "", Yours: h.sent && h.read.Cmd.Done > h.typedDone,
		}, nil
	})
}

// Output implements [agent.Window].
func (w agentWindow) Output(id string, most int) (agent.Look, error) {
	return onApp(w.a, func() (agent.Look, error) {
		h, t, err := w.a.handedPane(id)
		if err != nil {
			return agent.Look{}, err
		}
		w.a.gave(h)
		size := t.Size()
		at := t.ReadLines(1)
		if at.Alt {
			return agent.Look{}, errors.New("a full-screen program is drawing in that pane, so there is no command output to read: read the pane instead")
		}
		from, note, err := h.outputFrom(at)
		if err != nil {
			return agent.Look{}, err
		}
		if from < at.Floor && !w.a.agents.allowed(h).ReadBack {
			from = at.Floor
			note = "The pane was cleared after this output began, so this is only what has been printed since the clear. The lines above it are still in the pane, and the user can scroll up to them."
		}
		if at.Line < from {
			return agent.Look{}, errors.New("the screen has been cleared since that command started, so what it printed is not in this pane any more: read the pane with read_pane instead")
		}
		want := agent.MostLines
		if most > 0 {
			want = min(most, agent.MostLines)
		}
		read, there := t.ReadFrom(from, want)
		if read.Alt {
			return agent.Look{}, errors.New("a full-screen program started drawing in that pane while this was reading it, so there is no command output to give: read the pane instead")
		}
		status, hasStatus := read.Cmd.Exit()
		text := strings.TrimRight(read.Text, "\n")
		if countLines(read.Text) < there {
			note += fmt.Sprintf(" The start of it is missing: it printed %d lines and this is the last %d.", there, countLines(read.Text))
		}
		if strings.TrimSpace(text) == "" {
			note += " Nothing is there: either it has printed nothing yet, or the screen has been cleared since it started."
		}
		return agent.Look{
			Screen: text, Gone: t.Exited(), Changed: read.Said, Row: read.Row, Col: read.Col, Alt: read.Alt,
			Pictures: picturesIn(read.Pictures, read.Row, countLines(text)), Cols: size.Cols, Rows: size.Rows,
			Note: note, Marks: read.Cmd.Integrated, Running: read.Cmd.Running, Done: read.Cmd.Done,
			Status: status, HasStatus: hasStatus, Back: h.promptIsBack(read),
			Watching: h.typed != "", Yours: h.sent && read.Cmd.Done > h.typedDone,
		}, nil
	})
}

// outputFrom is the line the last command's output begins on.
func (h *handover) outputFrom(at uiterm.Reading) (uint64, string, error) {
	from, marked := at.Cmd.Output()
	mine := at.Cmd.Running || (h.sent && at.Cmd.Done > h.typedDone)
	switch {
	case marked && (!h.sent || mine):
		return from, "This is what the last command printed, from where the shell said its output began.", nil
	case h.sent:
		return h.typedLine, "This shell does not say where a command's output begins, so this is everything the pane has said since you last typed. It starts on the line you typed at, so the first line is the prompt with your command echoed after it.", nil
	}
	return 0, "", errors.New("nothing here knows where the last command's output began: this shell does not mark its commands, and you have typed nothing in this pane. Read the pane with read_pane instead")
}

// Send implements [agent.Window].
func (w agentWindow) Send(id, text string, keys []string) error {
	_, err := onApp(w.a, func() (struct{}, error) {
		h, t, err := w.a.handedPane(id)
		if err != nil {
			return struct{}{}, err
		}
		if w.a.agents.allowed(h).ReadOnly {
			return struct{}{}, errors.New(`the user handed this pane over to be read and not typed into. Ask them to turn "` + agent.BoxReadOnly + `" off if you need to type`)
		}
		w.a.gave(h)
		if t.Exited() {
			return struct{}{}, errors.New("the program in that pane has finished, so nothing is left to type into")
		}
		h.markPrompt(t)
		w.a.agentTyped(h.pane, text, keys)
		return struct{}{}, typeInto(t, text, keys)
	})
	return err
}

// markPrompt writes down the prompt the agent is typing at, to tell
// when it comes back.
func (h *handover) markPrompt(t *uiterm.Terminal) {
	read := t.ReadLines(1)
	h.sent, h.typedDone = true, read.Cmd.Done
	if read.Alt {
		return
	}
	if h.typed != "" && read.Line == h.typedLine && strings.HasPrefix(read.Before, h.typed) {
		return
	}
	h.typed, h.typedLine = read.Before, read.Line
}

func (h *handover) promptIsBack(read uiterm.Reading) bool {
	if h.typed == "" || read.Alt {
		return false
	}
	return read.Line > h.typedLine && read.Before == h.typed
}

// Restart implements [agent.Window]: it starts a pane's program again,
// once it has ended, when the hand-over allows it.
func (w agentWindow) Restart(id string) (agent.Pane, error) {
	_, err := onApp(w.a, func() (struct{}, error) {
		h, t, err := w.a.handedPane(id)
		if err != nil {
			return struct{}{}, err
		}
		if !w.a.agents.allowed(h).Restart {
			return struct{}{}, errors.New(`this hand-over does not let you restart the pane. Ask the user to tick "` + agent.BoxRestart + `"`)
		}
		w.a.gave(h)
		if !t.Exited() {
			return struct{}{}, errors.New("the program in that pane is still running, so there is nothing to start again")
		}
		return struct{}{}, w.a.startAgain(h.pane)
	})
	if err != nil {
		return agent.Pane{}, err
	}
	// A server's shell opens over the connection on a goroutine of its
	// own; the pane is told about once it is running again.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		p, err := onApp(w.a, func() (agent.Pane, error) {
			h, _, err := w.a.handedPane(id)
			if err != nil {
				return agent.Pane{}, err
			}
			return w.a.toldAbout(h)
		})
		if err != nil || !p.Ended {
			return p, err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return agent.Pane{}, errors.New("the pane took more than half a minute to start again")
}

// Open implements [agent.Window]: another pane where a pane is, handed
// over as it opens.
func (w agentWindow) Open(id string) (agent.Pane, error) {
	opened := make(chan string, 1)
	failed := make(chan error, 1)
	var from *handover
	_, err := onApp(w.a, func() (struct{}, error) {
		h, _, err := w.a.handedPane(id)
		if err != nil {
			return struct{}{}, err
		}
		if !w.a.agents.allowed(h).OpenMore {
			return struct{}{}, errors.New(`this hand-over does not let you open another pane. Ask the user to tick "` + agent.BoxOpenMore + `"`)
		}
		n := 0
		for _, other := range w.a.agents.by {
			for at := other.from; at != nil; at = at.from {
				if at == h {
					n++
					break
				}
			}
		}
		if n >= mostOpened {
			return struct{}{}, fmt.Errorf("you have opened %d panes from this one, which is as many as a hand-over gives: work in the ones you have, or ask the user for another pane", n)
		}
		machine := w.a.machineOf(h.pane)
		if machine != "" && w.a.conns[machine] == nil {
			return struct{}{}, fmt.Errorf("this window is not connected to %s any more, and opening connections is the user's to do: ask them to connect to it", machine)
		}
		from = h
		focus := w.a.st.Focus
		return struct{}{}, w.a.openThen(machine, placement{beside: h.pane}, func(pane string, err error) {
			// The user keeps the keyboard where it was.
			if w.a.has(focus) {
				w.a.st.Focus = focus
			}
			if err != nil {
				failed <- err
				return
			}
			opened <- pane
		})
	})
	if err != nil {
		return agent.Pane{}, err
	}
	select {
	case pane := <-opened:
		return onApp(w.a, func() (agent.Pane, error) {
			if w.a.agents.open != from.in {
				return agent.Pane{}, agent.ErrShareOver
			}
			next := w.a.handInto(from.in, pane)
			next.from = from
			w.a.showShare()
			return w.a.toldAbout(next)
		})
	case err := <-failed:
		return agent.Pane{}, err
	case <-time.After(time.Minute):
		return agent.Pane{}, errors.New("the pane took more than a minute to open")
	case <-w.a.ctx.Done():
		return agent.Pane{}, errors.New("this window is closing")
	}
}

// Secret implements [agent.Window]: it asks the user, on the pane, to
// type something there, and says whether they did, never what.
func (w agentWindow) Secret(id, what string, wait time.Duration) (bool, error) {
	type ask struct {
		answered <-chan bool
		stop     func()
	}
	got, err := onApp(w.a, func() (ask, error) {
		h, t, err := w.a.handedPane(id)
		if err != nil {
			return ask{}, err
		}
		if w.a.agents.allowed(h).ReadOnly {
			return ask{}, errors.New(`the user handed this pane over to be read, so nothing of yours goes on it. Ask them to turn "` + agent.BoxReadOnly + `" off, or ask them for what you need in your own words`)
		}
		w.a.gave(h)
		if t.Exited() {
			return ask{}, errors.New("the program in that pane has finished, so nothing is waiting to be told anything")
		}
		answered, stop, err := t.WaitForSecret(secretLine(what))
		if err != nil {
			return ask{}, err
		}
		return ask{answered, stop}, nil
	})
	if err != nil {
		return false, err
	}
	defer func() {
		select {
		case w.a.events <- got.stop:
		case <-w.a.ctx.Done():
		}
	}()
	giveUp := time.NewTimer(min(max(wait, 5*time.Second), agent.LongestSecretWait))
	defer giveUp.Stop()
	select {
	case answered := <-got.answered:
		if !answered {
			return false, errors.New("that ask ended without the user answering it: the pane closed, the program in it finished, or the user took the pane back")
		}
		return true, nil
	case <-giveUp.C:
		return false, nil
	case <-w.a.ctx.Done():
		return false, errors.New("this window is closing")
	}
}

// secretLine is what the pane says when an agent asks for a secret.
func secretLine(what string) string {
	// Cleaned as gridterm cleans it: one plain line, cut short, with
	// nothing in it that could draw or pass itself off as the window.
	asked := strings.TrimSpace(agent.CleanSecretAsk(what))
	if asked == "" {
		asked = "something it says it cannot see"
	}
	return `-- gunimterm: an agent wants something typed here. The window never tells it what you type. The program in this pane gets it, so if you can see the characters as you type them, the agent can read them off the screen too. It asked for: "` + asked + `" --`
}

// typeInto types text into a terminal and then presses keys.
func typeInto(t *uiterm.Terminal, text string, keys []string) error {
	if err := agent.CheckKeys(keys); err != nil {
		return err
	}
	going := []byte(text)
	for _, name := range keys {
		chord, err := ui.ParseChord(name)
		if err != nil {
			return fmt.Errorf("gunimterm cannot press %q: %w", name, err)
		}
		press := input.Event{Kind: input.KeyPress, Key: chord.Key, Mods: chord.Mods}
		if chord.Key == input.KeySpace && chord.Mods == 0 {
			press = input.Event{Kind: input.Text, Rune: ' ', NormalText: true}
		}
		going = append(going, t.EncodeKey(press)...)
	}
	t.Send(going)
	return nil
}

// stopAtTheFloor cuts what was read at the last clear, and says so.
func stopAtTheFloor(screen string, read uiterm.Reading) (string, bool, string) {
	if read.Alt || read.Floor == 0 || read.Bottom < read.Floor {
		return screen, false, ""
	}
	below := int(read.Bottom-read.Floor) + 1
	if below > countLines(screen) {
		return screen, false, ""
	}
	return lastLines(screen, below), true, "The pane was cleared, so the lines above the clear are not offered here. They are still in the pane, and the user can scroll up to them."
}

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

func picturesIn(on []uiterm.Picture, lastRow, lines int) []agent.Picture {
	first := lastRow - lines + 1
	var keep []uiterm.Picture
	for _, p := range on {
		if p.Top <= lastRow && p.Top+p.Rows-1 >= first {
			keep = append(keep, p)
		}
	}
	return picturesSeen(keep)
}

func picturesSeen(on []uiterm.Picture) []agent.Picture {
	var out []agent.Picture
	for _, p := range on {
		out = append(out, agent.Picture{Top: p.Top, Rows: p.Rows, Cols: p.Cols, Width: p.Width, Height: p.Height, Wire: p.Wire})
	}
	return out
}

func trimBlankTail(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	end := len(text)
	for end > 0 {
		cut := strings.LastIndexByte(text[:end], '\n')
		if strings.TrimSpace(text[cut+1:end]) != "" {
			break
		}
		end = cut
	}
	return text[:end]
}

func countLines(text string) int { return strings.Count(text, "\n") + 1 }

// The agent programs the prompt and setup are written for, as gridterm
// knows them.
const (
	hostClaudeCode = "Claude Code"
	hostCodex      = "Codex"
	hostCursor     = "Cursor"
	hostOther      = "Another host"
)

// agentHost is an agent program: cmd adds an MCP server from the
// command line, and configAt is where one without a command keeps its
// servers.
type agentHost struct {
	name, called, cmd, configAt string
	// skillIn is where under home it reads skills from, and skillEnv
	// the setting that moves that.
	skillIn  []string
	skillEnv string
}

var agentHosts = []agentHost{
	{name: hostClaudeCode, called: hostClaudeCode, cmd: "claude", skillIn: []string{".claude", "skills", "gridterm"}, skillEnv: "CLAUDE_CONFIG_DIR"},
	{name: hostCodex, called: hostCodex, cmd: "codex"},
	{name: hostCursor, called: hostCursor, configAt: "~/.cursor/mcp.json"},
	{name: hostOther, called: "the host"},
}

func hostNamed(name string) agentHost {
	for _, h := range agentHosts {
		if h.name == name {
			return h
		}
	}
	return agentHosts[0]
}

func agentHostNames() []string {
	var out []string
	for _, h := range agentHosts {
		out = append(out, h.name)
	}
	return out
}

// exePath is this program's path, for the MCP server's command line.
func exePath() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return "gunimterm"
}

func quotedPath(path string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return `'` + strings.ReplaceAll(path, `'`, `'\''`) + `'`
}

func mcpConfig(exe string) string {
	inJSON := strings.ReplaceAll(strings.ReplaceAll(exe, `\`, `\\`), `"`, `\"`)
	return `{"mcpServers": {"gridterm": {` + "\n" + `  "command": "` + inJSON + `",` + "\n" + `  "args": ["-mcp"]}}}`
}

// setupToCopy is what adds the MCP server to h.
func (h agentHost) setupToCopy(exe string) string {
	if h.cmd != "" {
		return h.cmd + " mcp add gridterm -- " + quotedPath(exe) + " -mcp"
	}
	return mcpConfig(exe)
}

// setupForAgent is the part of the prompt that says how to add the
// server, for an agent that does not have its tools.
func (h agentHost) setupForAgent(exe string) string {
	if h.cmd != "" {
		return "Ask the user to run this line and then start you again:\n\n  " + h.setupToCopy(exe)
	}
	where := h.configAt
	if where == "" {
		where = "its MCP config"
	}
	lines := strings.Split(mcpConfig(exe), "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return "Ask the user to put this in " + where + " and then start " + h.called + " again:\n\n" + strings.Join(lines, "\n")
}

// handoverPrompt is what the user pastes into the agent program.
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

// WriteSkill writes the skill for an agent program, which tells it how
// to work in the panes; Over writes over one edited since.
type WriteSkill struct {
	Host string
	Over bool
}

// skillFile is what a skill's file is called.
const skillFile = "SKILL.md"

// skillFor is the skill for an agent program.
func skillFor(host agentHost, exe string) string {
	return fmt.Sprintf(`---
name: gridterm
description: Work in the terminal panes the user shared with you in gridterm, through its MCP server
---
# Working in gridterm panes

gridterm is a terminal on the user's machine. The user puts panes into a share -- on whatever
machines, as whatever user -- and gives you one code for the whole share. You work in those panes
through gridterm's MCP server, and the user watches everything you do.

## Reaching the server

The server runs on the user's machine, on standard input and output (stdio), because the port
inside a session code is on the loopback address. If you do not have gridterm's tools, it has not
been added here yet.

%s

## Getting the panes

The user starts a share in gridterm, adds panes to it, and gets one session code for the whole
share. Ask the user for the code if you have not been given one. Call use_session_code with it
before anything else. The answer lists the panes, and every other tool takes a pane's name.

The share is not a fixed set. The user adds panes and takes them out while you work, so call
list_panes when you want to know what you have now.

## Working in a pane

%s

## Rules

%s
`, host.setupForAgent(exe), mcp.Workflow, mcp.Rules)
}

// skillPathFor is where an agent program's skill goes: where it reads
// skills from, or beside the settings for one that has no such place.
func skillPathFor(host agentHost) (string, error) {
	if len(host.skillIn) > 0 {
		if dir := os.Getenv(host.skillEnv); host.skillEnv != "" && dir != "" {
			dir, err := expandHome(dir)
			if err != nil {
				return "", err
			}
			return filepath.Join(append(append([]string{dir}, host.skillIn[1:]...), skillFile)...), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(append(append([]string{home}, host.skillIn...), skillFile)...), nil
	}
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills", "gridterm", skillFile), nil
}

// writeSkill writes an agent program's skill, and asks before writing
// over one edited since.
func (a *app) writeSkill(in WriteSkill) error {
	host := hostNamed(in.Host)
	path, err := skillPathFor(host)
	if err != nil {
		return err
	}
	body := skillFor(host, exePath())
	if !in.Over {
		was, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return err
		case string(was) != body:
			go func() {
				ans, err := a.ask(a.ctx, Ask{Title: "Replace the skill?", Text: path + " has been edited, and replacing it discards the edits.", Yes: "Replace", Danger: true})
				if err == nil && ans.Yes {
					in.Over = true
					a.events <- func() {
						if err := a.writeSkill(in); err != nil {
							a.notify("Couldn't write the skill", err.Error(), "")
						}
					}
				}
			}()
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	how := "Restart " + host.name + " to load it."
	if len(host.skillIn) == 0 {
		how = "Copy it to where " + host.called + " reads skills from."
	}
	a.notify("Skill written", path+". "+how, "")
	return nil
}
