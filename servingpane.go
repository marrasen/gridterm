package main

import (
	"image/color"
	"strconv"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
)

// servingPane is what this window is serving, on screen: where it is
// served, what to check it by, and who is working in it.
//
// A pane rather than a dialog. What it says changes while it is up --
// windows connect and go -- and a box that has to be dismissed cannot
// show that. It is also what the row of a window working here opens,
// and a row opens a pane.
type servingPane struct {
	app *app

	// entry is its row on the sidebar, which is how it is found and
	// closed like anything else the window has open.
	entry *conns.Entry

	size ui.Size

	// at is the choice the keyboard is on, cols where each was drawn,
	// drawn what they were, and row which row they are on. See jobPane:
	// a click measured against a row that has changed presses nothing.
	at    int
	cols  []int
	drawn []choice
	row   int

	// were is how many windows were connected when this last drew, so
	// the focus can move when that changes.
	were int
}

// newServingPane opens the pane on what this window is serving.
func newServingPane(a *app) *servingPane {
	return &servingPane{app: a, row: -1, were: -1}
}

// Layout takes the room it is given.
func (p *servingPane) Layout(size ui.Size) { p.size = size }

// Draw paints the pane.
func (p *servingPane) Draw(v grid.View) {
	p.draw(v, p.app.serving.clients(), time.Now())
}

// draw paints one reading of what is being served, taken apart from
// Draw so a test can hand it a list of windows rather than having to
// arrange for them.
func (p *servingPane) draw(v grid.View, clients []*serve.Client, now time.Time) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	st := p.app.formStyle()
	at := jobPaneMargin
	width := max(cols-2*jobPaneMargin, 1)
	// Nothing above a pane clears its rect.
	p.blank(v, 0)
	line := 1

	head := p.pen(v, line)
	head.skip(at)
	head.write(dlgServingWindow, st.TitleFG)
	head.right(connectedNote(clients), st.HintFG, jobPaneMargin)
	head.rest()
	p.blank(v, line+1)
	line += 2

	// Where it is served and what to check it by: the two things
	// somebody at the other end needs, and the two this window cannot
	// tell them itself.
	p.say(v, at, line, width, "Address     "+p.app.serving.addr(), st.FG)
	line++
	p.say(v, at, line, width, "Fingerprint "+p.app.serving.fingerprint(), st.FG)
	line += 2

	// Who is here now, which is the part that changes while this is up.
	line = p.drawClients(v, at, line, width, rows, clients, st)

	for y := line; y < rows-2; y++ {
		p.blank(v, y)
	}
	if rows-1 >= 0 {
		p.blank(v, rows-1)
	}
	p.settle(clients)
	p.drawChoices(v, clients, rows)
}

// connectedNote says how many windows are working in this one.
func connectedNote(clients []*serve.Client) string {
	switch len(clients) {
	case 0:
		return "nobody is connected"
	case 1:
		return "1 connected"
	}
	return strconv.Itoa(len(clients)) + " connected"
}

// drawClients writes who is connected, and hands back the row after
// them.
func (p *servingPane) drawClients(v grid.View, at, line, width, rows int,
	clients []*serve.Client, st ui.FormStyle) int {

	if len(clients) == 0 {
		p.say(v, at, line, width,
			"Nothing is connected. This window is waiting to be taken over.", st.HintFG)
		return line + 1
	}
	room := rows - line - 3
	for i, c := range clients {
		if i >= room-1 && len(clients)-i > 1 {
			p.say(v, at, line, width,
				"and "+strconv.Itoa(len(clients)-i)+" more", st.HintFG)
			line++
			break
		}
		if i >= room {
			break
		}
		p.say(v, at, line, width, c.Name+"  from "+c.Addr, st.FG)
		line++
	}
	p.blank(v, line)
	return line + 1
}

// choices are what can be done about it, left to right.
func (p *servingPane) choices() []choice { return p.choicesFor(p.app.serving.clients()) }

// choicesFor is what can be done about one reading of it.
func (p *servingPane) choicesFor(clients []*serve.Client) []choice {
	var out []choice
	if len(clients) > 0 {
		out = append(out, choice{title: kickTitle(clients)})
	}
	return append(out, choice{title: btnStopServing}, choice{title: btnClose})
}

// settle moves the focus when the windows connected change under it.
//
// The button that disconnects them comes and goes with them, so the
// rest of the row moves along: a finger on Stop serving would find
// Disconnect under it the moment somebody connected. The focus goes to
// Close, which does nothing to anybody.
func (p *servingPane) settle(clients []*serve.Client) {
	if len(clients) == p.were {
		return
	}
	p.were = len(clients)
	p.at = len(p.choicesFor(clients)) - 1
}

// drawChoices paints the row along the bottom, centred.
func (p *servingPane) drawChoices(v grid.View, clients []*serve.Client, rows int) {
	cols, _ := v.Size()
	choices := p.choicesFor(clients)
	p.at = min(max(p.at, 0), len(choices)-1)
	st := p.app.formStyle()
	row := rows - 2
	if row < 1 {
		p.row = -1
		return
	}
	p.row = row
	p.cols = placeChoices(p.cols[:0], choices, cols)
	p.drawn = append(p.drawn[:0], choices...)
	pen := p.pen(v, row)
	for i, at := range p.cols {
		if at < 0 {
			continue
		}
		pen.skip(at - pen.at)
		fg, bg := st.ButtonFG, st.ButtonBG
		if i == p.at {
			fg, bg = st.ActiveFG, st.ActiveBG
		}
		pen.writeOn(" "+choices[i].title+" ", fg, bg)
	}
	pen.rest()
}

// HandleKey moves along the choices and presses one.
func (p *servingPane) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress {
		return false, nil
	}
	choices := p.choices()
	switch ev.Key {
	case input.KeyLeft:
		p.at = (p.at - 1 + len(choices)) % len(choices)
	case input.KeyRight, input.KeyTab:
		p.at = (p.at + 1) % len(choices)
	case input.KeyEnter, input.KeySpace:
		return true, p.press(p.at)
	default:
		return false, nil
	}
	p.app.markDirty()
	return true, nil
}

// HandleMouse presses the choice under the pointer.
func (p *servingPane) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress || ev.Button != input.MouseLeft || p.row < 0 {
		return false, nil
	}
	if ev.Row != p.row {
		return false, nil
	}
	choices := p.choices()
	if !sameChoices(choices, p.drawn) {
		// Somebody connected or went since this row was drawn, so where
		// the pointer went is not what it went to.
		p.app.markDirty()
		return true, nil
	}
	for i, at := range p.cols {
		if at < 0 || i >= len(choices) {
			continue
		}
		if ev.Col >= at && ev.Col < at+choices[i].width() {
			p.at = i
			return true, p.press(i)
		}
	}
	return false, nil
}

// press does what the choice at i says.
func (p *servingPane) press(i int) error {
	choices := p.choices()
	if i < 0 || i >= len(choices) {
		return nil
	}
	switch choices[i].title {
	case btnClose:
		return p.app.closePane(p)
	case btnStopServing:
		// Forgotten here rather than in stopServing, which a window
		// closing also calls: quitting is not the user saying they are
		// done serving.
		if err := p.app.stopServing(); err != nil {
			return err
		}
		if err := p.app.serving.rememberOn(false); err != nil {
			return err
		}
		// Nothing left to show, so the pane goes with it.
		return p.app.closePane(p)
	default:
		// Disconnect: the windows connected now, not the ones drawn
		// when the row was painted, which sameChoices has already made
		// sure are the same.
		return p.app.kickOut(p.app.serving.clients())
	}
}

// blank, pen and say are the pane's rows, as jobPane's are.
func (p *servingPane) blank(v grid.View, y int) { p.pen(v, y).rest() }

func (p *servingPane) pen(v grid.View, y int) *rowPen {
	cols, _ := v.Size()
	return &rowPen{v: v, bg: p.app.colours.BG, y: y, cols: cols}
}

func (p *servingPane) say(v grid.View, x, y, width int, text string, fg color.RGBA) {
	pen := p.pen(v, y)
	pen.skip(x)
	pen.write(text, fg)
	pen.rest()
}

// showServingPane opens the pane on what this window is serving, or
// goes to the one already open.
func (a *app) showServingPane() error {
	if !a.serving.on() {
		return nil
	}
	if pane := a.servingPane(); pane != nil {
		a.focus(pane)
		return nil
	}
	pane := newServingPane(a)
	// A row like anything else open: the sidebar is the list of what
	// the window has, and a pane missing from it is one the user can
	// lose behind another.
	pane.entry = &conns.Entry{
		Host:   conns.Local,
		Kind:   conns.Served,
		Label:  servingPaneLabel,
		Meter:  &meter.Meter{},
		Reveal: func() { a.focus(pane) },
		Close:  func() error { return a.closePane(pane) },
	}
	a.servePanes = append(a.servePanes, pane)
	a.registry.Add(pane.entry)
	if err := a.placePane(pane); err != nil {
		a.servePanes = a.servePanes[:len(a.servePanes)-1]
		a.registry.Drop(pane.entry)
		return err
	}
	return nil
}

// servingPaneLabel is what the pane's row says. What it is, not what it
// is doing: the rows beside it are the windows connected, and this one
// is the page about the serving itself.
const servingPaneLabel = "Serving"

// servingPane is the pane already open on it, or nil.
func (a *app) servingPane() *servingPane {
	if len(a.servePanes) == 0 {
		return nil
	}
	return a.servePanes[0]
}

// forgetServingPane takes a closed pane off the window's record of it.
func (a *app) forgetServingPane(w ui.Widget) {
	pane, is := w.(*servingPane)
	if !is {
		return
	}
	for i, open := range a.servePanes {
		if open == pane {
			a.servePanes = append(a.servePanes[:i], a.servePanes[i+1:]...)
			a.registry.Drop(pane.entry)
			return
		}
	}
}
