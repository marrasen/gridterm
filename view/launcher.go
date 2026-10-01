package view

import (
	"slices"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"

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
// is typed, over the machines found, and once something is typed, the
// shells here, files on each machine, saved commands and kakel's own
// windows too. Enter opens what was opened there
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
	machineID   machines.ID
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
	l.field.Placeholder = "Type a machine, a shell, files, a command…"
	// What is typed lights the best of what it finds.
	l.field.OnEdit = func(string, *gunim.UI) { l.hot, l.first = 0, 0; l.find() }
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
		// Once something is typed, the rest too: shells, files, saved
		// commands, kakel's windows, after the machines.
		if l.field.Text() != "" {
			for _, t := range l.st.Things {
				out = append(out, match.Item{Title: t.Title, Also: t.Also})
			}
		}
		return out
	}
	for _, a := range l.st.Machines[l.machine].Actions {
		out = append(out, match.Item{Title: a.Title})
	}
	return out
}

// find lists what the field finds, in the things of the machine
// listed, found again by its ID, as the machines may come in another
// order.
func (l *Launcher) find() {
	if l.machine >= 0 {
		l.machine = slices.IndexFunc(l.st.Machines, func(m app.LaunchMachine) bool { return m.ID == l.machineID })
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
		switch {
		case l.machine >= 0:
			ic = icon.SquareTerminal
		case l.thing(l.found[k]) != nil:
			ic = thingIcons[l.thing(l.found[k]).Kind]
		case l.st.Machines[l.found[k]].ID == "":
			ic = icon.Laptop
		}
		if ic == nil {
			ic = icon.SquareTerminal
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

// thingIcons are the icons of the kinds of things the launcher finds.
var thingIcons = map[string]*icon.Icon{
	"terminal": icon.SquareTerminal, "files": icon.Folder, "log": icon.ScrollText, "command": icon.SquareChevronRight,
	"servers": icon.Server, "secrets": icon.Lock, "window": icon.AppWindow,
}

// thing is found item i when it is one of the things rather than a
// machine, and nil otherwise.
func (l *Launcher) thing(i int) *app.LaunchThing {
	n := len(l.st.Machines)
	if l.machine >= 0 || i < n || i-n >= len(l.st.Things) {
		return nil
	}
	return &l.st.Things[i-n]
}

// line is the title and the note of found item i.
func (l *Launcher) line(i int) (string, string) {
	if t := l.thing(i); t != nil {
		return t.Title, t.Note
	}
	if l.machine < 0 {
		m := l.st.Machines[i]
		return m.Name, m.Note
	}
	return l.st.Machines[l.machine].Actions[i].Title, ""
}

// hintText says what Enter and Tab do on the line lit.
func (l *Launcher) hintText() string {
	if l.machine >= 0 || l.hot >= len(l.found) || l.thing(l.found[l.hot]) != nil {
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
		if e.Button != input.ButtonPrimary || e.Pos.Y < l.listTop || k >= launcherRows || l.first+k >= len(l.found) {
			return false
		}
		l.hot = l.first + k
		l.runs = nil
		u.Invalidate()
		l.pick(u)
		return true
	case input.Scroll:
		// A line at a notch, the list staying within what it found.
		step := 1
		if e.Delta.Y > 0 {
			step = -1
		}
		l.first = min(max(l.first+step, 0), max(len(l.found)-launcherRows, 0))
		l.hot = min(max(l.hot, l.first), l.first+launcherRows-1)
		l.runs = nil
		u.Invalidate()
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
		if l.machine >= 0 || l.hot >= len(l.found) || l.thing(l.found[l.hot]) != nil {
			return e.Key == input.KeyTab
		}
		l.machine, l.hot = l.found[l.hot], 0
		l.machineID = l.st.Machines[l.machine].ID
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
	if t := l.thing(l.found[l.hot]); t != nil {
		u.Send(l, app.Launch{Machine: t.Machine, Action: t.Action})
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
