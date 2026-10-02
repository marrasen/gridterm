package view

import (
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// A question that wants something typed opens in a small window of its
// own (see app/prompts.go), which shows the dialog a kakel window would
// show, filling it: the same title, facts, fields and buttons, with
// Enter to answer and Escape to cancel. The window is sized to the
// question before it opens, and the dialog sits in its middle, so a
// size a little out shows as a margin.

// Prompt is the view of a question's own window.
type Prompt struct {
	live   *theme.Live
	id     uint64
	dialog *widget.Dialog
}

// promptTheme leaves out of the window's theme what sets a dialog off
// from the window behind it: the dimmed and blurred backdrop, the
// shadow, the border and the round corners. What is left is the
// dialog's face, as wide as the window, and the window's own edge is
// its edge.
var promptTheme = theme.Make("kakel.prompt",
	theme.Set(widget.Scrim, color.NRGBA{}),
	theme.Set(widget.DialogBackdrop, float32(0)),
	theme.Set(widget.DialogShadow, color.NRGBA{}),
	theme.Set(widget.DialogBorder, color.NRGBA{}),
	theme.Set(widget.DialogRadius, float32(0)),
	theme.Set(widget.DialogMargin, float32(0)),
)

// NewPrompt returns the view of a question's own window.
func NewPrompt() *Prompt { return &Prompt{live: theme.NewLive(promptTheme)} }

// Update shows q, once.
func (p *Prompt) Update(q app.Ask, u *gunim.UI) {
	if q.ID == p.id {
		return
	}
	p.id = q.ID
	p.dialog = askDialog(q, p)
	scrollBody(p.dialog)
	// The dialog takes the keyboard as it arrives, to its first field.
	u.Insert(p, p.dialog)
}

// ThemeScope implements [gunim.ThemeScope].
func (p *Prompt) ThemeScope() *theme.Live { return p.live }

// Step implements [gunim.Animator], for the theme's changes.
func (p *Prompt) Step(dt time.Duration) bool { return p.live.Step(dt) }

// Layout implements [gunim.Node]: the dialog fills the window.
func (p *Prompt) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	for kid := range kids.All {
		kid.Layout(gunim.Tight(c.Max))
		kid.Place(geom.Point{})
	}
	return c.Max
}

// Paint implements [gunim.Node].
func (p *Prompt) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.DialogFill.Get(f.Theme)))
	for kid := range kids.All {
		kid.Paint(pt)
	}
}

// promptMost is the tallest a question's window opens; a question
// taller than that, as a server's long banner, scrolls in it.
const promptMost = 640

// PromptSize is the size of the window that asks q in the theme th:
// as wide as a dialog and as tall as the dialog askDialog makes of q,
// measured as the dialog and its form lay it out.
func PromptSize(q app.Ask, th theme.Theme) geom.Size {
	l := theme.NewLive(th)
	pad, gap := widget.DialogPadding.Get(l), widget.FormGap.Get(l)
	size, field := widget.TextSize.Get(l), widget.FieldHeight.Get(l)
	face, bold := widget.Font.Get(l), widget.BoldFont.Get(l)
	high := func(f *text.Face, s string, size, width float32) float32 {
		return f.Layout(s, text.Style{Size: size}, width).Size.H
	}
	wide := func(s string) float32 { return face.Shape(s, size).Advance }

	// As wide as the theme's dialog, or as the buttons need in a row.
	no := q.No
	if no == "" {
		no = "Cancel"
	}
	buttons := append(append([]string{}, q.Actions...), no, q.Yes)
	if len(q.Choose) > 0 {
		buttons = append(append(append([]string{}, q.Actions...), q.Choose[1:]...), no, q.Choose[0])
	}
	row := -widget.DialogGap.Get(l)
	for _, b := range buttons {
		row += wide(b) + 2*widget.ButtonPadding.Get(l) + widget.DialogGap.Get(l)
	}
	width := max(widget.DialogWidth.Get(l), row+2*pad)
	inner := width - 2*pad

	// The form's labels make a column as wide as the widest, two fifths
	// of the form at most, and the rest is the fields'.
	labels := []string{}
	for _, f := range q.Facts {
		labels = append(labels, f.Label)
	}
	for _, p := range q.Prompts {
		labels = append(labels, strings.TrimSuffix(strings.TrimSpace(p), ":"))
	}
	if len(q.Saved) > 0 {
		labels = append(labels, "Or use")
	}
	labelW := float32(0)
	for _, s := range labels {
		labelW = max(labelW, min(wide(s), inner*0.4))
	}
	fieldW := max(inner-labelW-gap, 0)
	var rows []float32
	if q.Text != "" {
		if q.Preformatted {
			lines := float32(strings.Count(q.Text, "\n") + 1)
			rows = append(rows, lines*widget.MonoFont.Get(l).Layout("M", text.Style{Size: size}, inner).LineHeight)
		} else {
			rows = append(rows, high(face, q.Text, size, fieldW))
		}
	}
	for _, f := range q.Facts {
		h := high(bold, f.Name, size, fieldW)
		if f.Note != "" {
			h += factGap.Get(l) + high(face, f.Note, smallText.Get(l), fieldW)
		}
		rows = append(rows, h)
	}
	if q.Problem != "" {
		rows = append(rows, high(face, q.Problem, size, fieldW))
	}
	for range q.Prompts {
		rows = append(rows, field)
	}
	if len(q.Saved) > 0 {
		rows = append(rows, field)
	}
	if q.Also != "" {
		// A box to tick: its label on one line beside it.
		rows = append(rows, max(widget.ControlHeight.Get(l), widget.CheckSize.Get(l), face.Shape(q.Also, size).Height()))
	}
	body := float32(0)
	for i, h := range rows {
		if i > 0 {
			body += gap
		}
		body += h
	}
	// The title, after the icon.
	room := float32(0)
	if q.Icon != "" || q.Danger {
		room = widget.IconSize.Get(l) + widget.IconGap.Get(l)
	}
	title := face.Layout(q.Title, text.Style{Size: widget.DialogTitleSize.Get(l), MaxLines: 2}, inner-room).Size.H
	h := pad + title + pad + body + pad + widget.ButtonHeight.Get(l) + pad
	// A few pixels over, as a field's text is measured by the face the
	// theme gives it, which may stand a little taller.
	h = min(h+8, promptMost)
	return geom.Sz(float32(math.Ceil(float64(width))), float32(math.Ceil(float64(h))))
}

// PromptPlace is where a question's window size large opens: centred
// over near, the window the user works in, or on the main display
// without one, and kept inside the work area of the monitor it is on.
// It is nil when there is no monitor to say.
func PromptPlace(monitors []driver.Monitor, near *driver.Placement, size geom.Size) *driver.Placement {
	if len(monitors) == 0 {
		return nil
	}
	m := monitors[0]
	for _, o := range monitors {
		if o.Primary {
			m = o
		}
	}
	var over geom.Rect
	if near != nil {
		mid := near.Bounds.Center()
		for _, o := range monitors {
			if o.Bounds.Contains(mid) {
				m = o
			}
		}
		// A maximized window's bounds are where it goes back to, so its
		// monitor stands for it.
		if !near.Maximized {
			over = near.Bounds
		}
	}
	area := m.WorkArea
	if area.Empty() {
		area = m.Bounds
	}
	if over.Empty() {
		over = area
	}
	scale := m.CoordsPerLogical
	if scale <= 0 {
		scale = 1
	}
	w, h := size.W*scale, size.H*scale
	c := over.Center()
	x := min(max(c.X-w/2, area.Min.X), area.Max.X-w)
	y := min(max(c.Y-h/2, area.Min.Y), area.Max.Y-h)
	return &driver.Placement{Bounds: geom.Rc(x, y, w, h)}
}
