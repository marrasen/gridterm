package term

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vt"
)

// askPad is the blank column kept at each end of the question row.
const askPad = 1

// Choice is one answer to a question on a pane's last row.
type Choice struct {
	// Label is what the user reads on the choice.
	Label string

	// Default says Enter picks this one. The first choice is the default
	// when none says so, and the first marked one wins.
	Default bool

	// Do runs when the user picks it. An error leaves the question up,
	// so an answer that did not work can be given again.
	Do func() error
}

// asked is what the window is asking in a pane and the answers it
// offers.
type asked struct {
	text    string
	choices []Choice

	// at is the choice Enter would pick.
	at int
}

// labels is the choices' labels, in order.
func (q *asked) labels() []string {
	out := make([]string, len(q.choices))
	for i, c := range q.choices {
		out[i] = c.Label
	}
	return out
}

// Ask puts a question on the pane's last row, with the choices the user
// can pick. Asking again replaces it, and Ask with no question takes it
// away.
//
// It is for the drawing goroutine, like the rest of the widget. The
// question is the window talking to this user: it never goes into the
// emulator, so it is in neither the transcript nor what a watcher on
// another machine is shown.
func (t *Terminal) Ask(question string, choices ...Choice) {
	if question == "" {
		t.ask = nil
	} else {
		t.ask = &asked{text: question, choices: choices, at: defaultChoice(choices)}
	}
	t.pending.Store(true)
}

// defaultChoice is the choice Enter picks: the one marked, and the first
// when none is.
func defaultChoice(choices []Choice) int {
	for i, c := range choices {
		if c.Default {
			return i
		}
	}
	return 0
}

// Asking is the question on the pane's last row, and "" when there is
// none.
func (t *Terminal) Asking() string {
	if t.ask == nil {
		return ""
	}
	return t.ask.text
}

// Choices are the labels of the answers the question offers, in the
// order they are drawn, and none when there is no question.
func (t *Terminal) Choices() []string {
	if t.ask == nil {
		return nil
	}
	return t.ask.labels()
}

// choiceCols is the column each choice is drawn at, and -1 for one the
// pane is too narrow to show.
func (t *Terminal) choiceCols() []int {
	if t.ask == nil {
		return nil
	}
	return ui.ButtonColsIn(t.ask.labels(), t.Box().Cols, askPad)
}

// drawn reports whether a choice has the room to be drawn, so nothing
// runs an answer the user was never shown.
func drawn(at []int, i int) bool {
	return i >= 0 && i < len(at) && at[i] >= 0
}

// paintAsk draws the question and its choices over the last row of a
// view, after the screen has been copied into it.
func (t *Terminal) paintAsk(v grid.View) {
	q := t.ask
	if q == nil {
		return
	}
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	at := ui.ButtonColsIn(q.labels(), cols, askPad)
	if !anyDrawn(at) {
		// Not even one answer fits. A row with no answer on it would only
		// cover the last line the program printed.
		return
	}
	bg := askBG(t.pal)
	row := v.Sub(0, rows-1, cols, 1)
	row.Fill(grid.Cell{Rune: ' ', FG: t.pal.FG, BG: bg, Width: 1})

	// The question fills the room to the left of the first choice that
	// fitted, cut with an ellipsis so a row stopping mid-word says so.
	room := cols - askPad*2
	for _, x := range at {
		if x >= 0 {
			room = x - 1 - askPad
			break
		}
	}
	if room > 0 {
		row.SetString(askPad, 0, grid.TrimTail(q.text, room), t.pal.FG, bg, 0)
	}

	for i, x := range at {
		if x < 0 {
			// No room for this one. Drawing it would land it on top of
			// the choices that did fit.
			continue
		}
		fg, cbg := t.pal.FG, t.pal.Surface()
		if i == q.at {
			// The colours swapped, the way a selected row or tab is
			// marked everywhere else in the window.
			fg, cbg = t.pal.BG, t.pal.FG
		}
		ui.DrawButton(row, x, 0, q.choices[i].Label, fg, cbg)
	}
}

// anyDrawn reports whether there is room for a single choice.
func anyDrawn(at []int) bool {
	for _, x := range at {
		if x >= 0 {
			return true
		}
	}
	return false
}

// askBG is the question row's own ground, lifted off the screen behind
// it so the row reads as the window talking rather than as output.
func askBG(p vt.Palette) color.RGBA { return grid.Blend(p.BG, p.FG, 1, 6) }

// askKey gives a key to the question and reports whether it took it.
//
// A key the question does not use goes no further: the program this
// question is about has gone, and nothing typed at the question is meant
// for it.
func (t *Terminal) askKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return false, nil
	}
	t.settleChoice()
	switch ev.Key {
	case input.KeyLeft:
		t.askMove(-1)
	case input.KeyRight, input.KeyTab:
		t.askMove(1)
	case input.KeyEnter:
		return true, t.askPick(t.ask.at)
	case input.KeyEscape:
		// Nothing at all: a question the user may ignore has no way out.
	default:
		return false, nil
	}
	return true, nil
}

// settleChoice moves the selection onto a choice the pane has the room
// to draw, for one narrowed since the question went up.
func (t *Terminal) settleChoice() {
	q := t.ask
	at := t.choiceCols()
	if drawn(at, q.at) {
		return
	}
	for i, x := range at {
		if x >= 0 {
			q.at = i
			t.pending.Store(true)
			return
		}
	}
}

// askMove steps the selection to the next choice the pane has the room
// to draw, stopping at either end rather than wrapping.
func (t *Terminal) askMove(by int) {
	q := t.ask
	if len(q.choices) == 0 || by == 0 {
		return
	}
	at := t.choiceCols()
	for i := q.at + by; i >= 0 && i < len(q.choices); i += by {
		if !drawn(at, i) {
			continue
		}
		q.at = i
		t.pending.Store(true)
		return
	}
}

// askMouse gives a press on the question row to the question and reports
// whether it took the event.
//
// Only a press: a drag that started on the screen above still ends where
// the user let go, and the wheel still scrolls the transcript.
func (t *Terminal) askMouse(ev input.MouseEvent) (bool, error) {
	if t.ask == nil || ev.Kind != input.MousePress || ev.Button != input.MouseLeft {
		return false, nil
	}
	box := t.Box()
	if box.Rows <= 0 || ev.Row != box.Rows-1 {
		return false, nil
	}
	at, ok := ui.ButtonAtCol(t.ask.labels(), box.Cols, askPad, ev.Col)
	if !ok {
		// The row but not a choice. Swallowed, so a selection does not
		// start under the question.
		return true, nil
	}
	t.ask.at = at
	t.pending.Store(true)
	return true, t.askPick(at)
}

// askPick runs a choice and takes the question away, unless the choice
// failed, in which case it stays up to be answered again.
func (t *Terminal) askPick(at int) error {
	q := t.ask
	if at < 0 || at >= len(q.choices) {
		return nil
	}
	// Nothing the user was not shown: on a narrow pane Enter would
	// otherwise run a choice that was never drawn.
	if !drawn(t.choiceCols(), at) {
		return nil
	}
	if do := q.choices[at].Do; do != nil {
		if err := do(); err != nil {
			return err
		}
	}
	// Only if it is still this question, since Do may have asked
	// another or restarted the pane.
	if t.ask == q {
		t.ask = nil
	}
	t.pending.Store(true)
	return nil
}
