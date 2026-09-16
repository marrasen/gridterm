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
		t.ask = &asked{text: question, choices: choices}
	}
	t.pending.Store(true)
}

// Asking is the question on the pane's last row, and "" when there is
// none.
func (t *Terminal) Asking() string {
	if t.ask == nil {
		return ""
	}
	return t.ask.text
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
	bg := askBG(t.pal)
	row := v.Sub(0, rows-1, cols, 1)
	row.Fill(grid.Cell{Rune: ' ', FG: t.pal.FG, BG: bg, Width: 1})

	// The question fills the room to the left of the first choice that
	// fitted.
	at := ui.ButtonColsIn(q.labels(), cols, askPad)
	room := cols - askPad*2
	for _, x := range at {
		if x >= 0 {
			room = x - 1 - askPad
			break
		}
	}
	if room > 0 {
		row.SetString(askPad, 0, grid.Trim(q.text, room), t.pal.FG, bg, 0)
	}

	for i, x := range at {
		if x < 0 {
			// No room for this one. Drawing it would land it on top of
			// the choices that did fit.
			continue
		}
		fg, cbg := t.pal.FG, t.pal.ANSI[0]
		if i == q.at {
			// The colours swapped, the way a selected row or tab is
			// marked everywhere else in the window.
			fg, cbg = t.pal.BG, t.pal.FG
		}
		ui.DrawButton(row, x, 0, q.choices[i].Label, fg, cbg)
	}
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

// askMove steps the selection, stopping at either end rather than
// wrapping.
func (t *Terminal) askMove(by int) {
	q := t.ask
	if len(q.choices) == 0 {
		return
	}
	q.at = min(max(q.at+by, 0), len(q.choices)-1)
	t.pending.Store(true)
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
