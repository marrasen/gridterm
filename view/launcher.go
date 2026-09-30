package view

import (
	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/match"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/widget"
)

// The launcher's window: a field that finds a machine by its name as it
// is typed, over the machines found. Enter opens what was opened there
// last, a terminal at first; Tab shows what else can be opened there,
// and Escape goes back. Escape again, or a click on another program,
// closes it. The field keeps Left and Right for its own caret.

// launcherRow is the height of a line of the launcher.
const launcherRow = 34

// launcherRows is how many lines the launcher shows.
const launcherRows = 8

// LauncherSize is the launcher window's size.
var LauncherSize = geom.Sz(560, 12*2+36+8+launcherRows*launcherRow)

// Launcher is the launcher's view.
type Launcher struct {
	field *widget.TextField
	st    app.LaunchState
	// opened is the opening shown, machine the machine whose things are
	// listed, or -1 while the machines are, and found what the field
	// finds, by index into the machines or the machine's things. hot is
	// the line lit, and first the first line shown.
	opened      uint64
	machine     int
	found       []int
	hot, first  int
	runs        []text.Run
	notes       []text.Run
	hint        text.Run
	listTop     float32
	pinned      bool
	shapedWidth float32
}

// NewLauncher returns the launcher's view.
func NewLauncher() *Launcher {
	l := &Launcher{field: widget.NewTextField(), machine: -1}
	l.field.Placeholder = "Type a machine's name"
	l.field.OnEdit = func(string, *gunim.UI) { l.find() }
	return l
}

// Update shows st, afresh for each opening.
func (l *Launcher) Update(st app.LaunchState, u *gunim.UI) {
	l.st = st
	if !l.pinned {
		l.pinned = true
		_ = u.SetPinned(true)
	}
	if st.Opened != l.opened {
		l.opened = st.Opened
		l.machine, l.hot, l.first = -1, 0, 0
		l.field.SetText("")
		u.Focus(l.field)
	}
	l.find()
	u.Invalidate()
}

// titles are the titles of what the launcher lists now.
func (l *Launcher) titles() []match.Item {
	var out []match.Item
	if l.machine < 0 {
		for _, m := range l.st.Machines {
			out = append(out, match.Item{Title: m.Name, Also: []string{m.Note}})
		}
		return out
	}
	for _, a := range l.st.Machines[l.machine].Actions {
		out = append(out, match.Item{Title: a.Title})
	}
	return out
}

// find lists what the field finds.
func (l *Launcher) find() {
	if l.machine >= len(l.st.Machines) {
		l.machine = -1
	}
	l.found = l.found[:0]
	for _, f := range match.Rank(l.titles(), l.field.Text()) {
		l.found = append(l.found, f.Index)
	}
	l.hot = min(max(l.hot, 0), max(len(l.found)-1, 0))
	l.runs = nil
}

// Children implements [gunim.Composite].
func (l *Launcher) Children() []gunim.Node { return []gunim.Node{l.field} }

// Layout implements [gunim.Node].
func (l *Launcher) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	const pad = 12
	fs := kids.At(0).Layout(gunim.Tight(geom.Sz(c.Max.W-2*pad, widget.FieldHeight.Get(f.Theme))))
	kids.At(0).Place(geom.Pt(pad, pad))
	l.listTop = pad + fs.H + 8
	return c.Max
}

// Paint implements [gunim.Node].
func (l *Launcher) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(look.SidebarFill.Get(th)))
	kids.At(0).Paint(p)
	size := widget.TextSize.Get(th)
	if l.runs == nil || l.shapedWidth != box.W {
		l.shapedWidth = box.W
		l.runs, l.notes = l.runs[:0], l.notes[:0]
		for _, i := range l.found {
			title, note := l.line(i)
			l.runs = append(l.runs, text.Default().Shape(title, size))
			l.notes = append(l.notes, text.Default().Shape(note, smallText.Get(th)))
		}
		l.hint = text.Default().Shape(l.hintText(), smallText.Get(th))
	}
	ink, faint := widget.Ink.Get(th), look.Faint.Get(th)
	if len(l.found) == 0 {
		t := text.Default().Shape("Nothing is called that", size)
		t.Paint(p, geom.Pt(20, l.listTop+(launcherRow-t.Height())/2), faint)
		return
	}
	l.first = min(max(l.first, l.hot-launcherRows+1), l.hot)
	for k := l.first; k < len(l.found) && k < l.first+launcherRows; k++ {
		r := geom.Rc(8, l.listTop+float32(k-l.first)*launcherRow, box.W-16, launcherRow-2)
		if k == l.hot {
			p.RRect(r, look.RowRadius.Get(th), paint.Solid(look.RowActive.Get(th)))
		}
		ic := icon.Server
		if l.machine >= 0 {
			ic = icon.SquareTerminal
		} else if l.st.Machines[l.found[k]].ID == "" {
			ic = icon.Laptop
		}
		mid := r.Min.Y + r.Size().H/2
		drawIcon(p, ic, geom.Rc(r.Min.X+10, mid-8, 16, 16), faint, 1.3)
		run := l.runs[k]
		run.Paint(p, geom.Pt(r.Min.X+36, mid-run.Height()/2), ink)
		if n := l.notes[k]; n.Advance > 0 {
			x := r.Min.X + 36 + run.Advance + 10
			n.Paint(p, geom.Pt(x, mid-n.Height()/2), faint)
		}
		if k == l.hot {
			h := l.hint
			h.Paint(p, geom.Pt(r.Max.X-12-h.Advance, mid-h.Height()/2), faint)
		}
	}
}

// line is the title and the note of found item i.
func (l *Launcher) line(i int) (string, string) {
	if l.machine < 0 {
		m := l.st.Machines[i]
		return m.Name, m.Note
	}
	return l.st.Machines[l.machine].Actions[i].Title, ""
}

// hintText says what Enter and Tab do on the line lit.
func (l *Launcher) hintText() string {
	if l.machine >= 0 || l.hot >= len(l.found) {
		return "Enter opens it"
	}
	m := l.st.Machines[l.found[l.hot]]
	if m.Default < len(m.Actions) {
		return "Enter: " + m.Actions[m.Default].Title + " · Tab: more"
	}
	return ""
}

// CaptionRects implements [gunim.Caption]: the launcher is a window
// with no title bar, moved by its edge round the field.
func (l *Launcher) CaptionRects(size geom.Size) []geom.Rect {
	const pad = 12
	return []geom.Rect{geom.Rc(0, 0, size.W, pad), geom.Rc(0, 0, pad, l.listTop), geom.Rc(size.W-pad, 0, pad, l.listTop)}
}

// Handle implements [gunim.Handler]: the keys the field passes on, a
// click on a line, and the window losing the keyboard.
func (l *Launcher) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.WindowFocusLost:
		u.Send(l, app.CloseLauncher{})
		return false
	case input.PointerDown:
		k := int((e.Pos.Y - l.listTop) / launcherRow)
		if e.Pos.Y < l.listTop || k < 0 || l.first+k >= len(l.found) {
			return false
		}
		l.hot = l.first + k
		l.runs = nil
		u.Invalidate()
		if e.Clicks >= 1 {
			l.pick(u)
		}
		return true
	case input.KeyPress:
		return l.key(e, u)
	}
	return false
}

// key moves the light, picks, goes into a machine's things and back,
// and closes.
func (l *Launcher) key(e input.KeyPress, u *gunim.UI) bool {
	switch e.Key {
	case input.KeyUp:
		l.hot = max(l.hot-1, 0)
	case input.KeyDown:
		l.hot = min(l.hot+1, max(len(l.found)-1, 0))
	case input.KeyEnter, input.KeyKPEnter:
		l.pick(u)
	case input.KeyTab, input.KeyRight:
		if l.machine >= 0 || l.hot >= len(l.found) {
			return e.Key == input.KeyTab
		}
		l.machine, l.hot = l.found[l.hot], 0
		l.field.SetText("")
		l.find()
	case input.KeyLeft, input.KeyEscape:
		if l.machine < 0 {
			if e.Key == input.KeyEscape {
				u.Send(l, app.CloseLauncher{})
				return true
			}
			return false
		}
		was := l.machine
		l.machine = -1
		l.field.SetText("")
		l.find()
		for k, i := range l.found {
			if i == was {
				l.hot = k
			}
		}
	default:
		return false
	}
	l.runs = nil
	u.Invalidate()
	return true
}

// pick opens the line lit: a machine's usual thing, or the thing lit.
func (l *Launcher) pick(u *gunim.UI) {
	if l.hot >= len(l.found) {
		return
	}
	if l.machine < 0 {
		m := l.st.Machines[l.found[l.hot]]
		if m.Default < len(m.Actions) {
			u.Send(l, app.Launch{Machine: m.ID, Action: m.Actions[m.Default].ID})
		}
		return
	}
	m := l.st.Machines[l.machine]
	u.Send(l, app.Launch{Machine: m.ID, Action: m.Actions[l.found[l.hot]].ID})
}

// noTitleBar is a title bar of no height, for a window with none: the
// launcher, which Escape closes and its edge moves.
type noTitleBar struct{}

// NoTitleBar returns a title bar that takes no room and shows nothing.
func NoTitleBar() gunim.TitleBar { return noTitleBar{} }

// Layout implements [gunim.Node].
func (noTitleBar) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Constrain(geom.Sz(c.Max.W, 0))
}

// Paint implements [gunim.Node].
func (noTitleBar) Paint(*paint.Painter, gunim.Frame, geom.Size, gunim.Children) {}

// SetTitle implements [gunim.TitleBar].
func (noTitleBar) SetTitle(string) {}
