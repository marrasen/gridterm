package view

import (
	"strconv"
	"strings"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/widget"
)

// Dragging files out of a file pane and dropping them into one. Rows
// dragged lift off as a stack of cards naming them, which says what a
// drop under it would do: move or copy into the folder under it, or
// nothing. A folder a drag rests on springs open. Files from another
// program drop in as copies, and files on this computer drag out to one.

// dragRows starts a drag of the rows keys: the file pane's files, as
// app.FileDrag, under a card naming them.
func (b *browser) dragRows(keys []widget.Key, at geom.Point) (any, gunim.Node, geom.Point) {
	d := app.FileDrag{Pane: b.id, Window: b.w.winID, Machine: b.w.filesKeyOf(b.id), At: b.st.Path, Sep: b.sep()}
	for _, k := range keys {
		e, ok := b.byName[k]
		if k == up || !ok {
			continue
		}
		d.Names = append(d.Names, e.Name)
		d.Dirs = append(d.Dirs, e.IsDir() && !e.Archive)
	}
	if len(d.Names) == 0 || b.st.Seq == 0 {
		return nil, nil, geom.Point{}
	}
	d.Local = d.Machine == "" && !b.st.Archive
	card := newFileCard(b, d)
	g := widget.NewDragGhost(card, geom.Pt(16, 12))
	if n := len(d.Names); n > 1 {
		g.Badge, g.Stack = strconv.Itoa(n), n-1
	}
	return d, g, geom.Pt(16, 12)
}

// sep is the separator of the pane's paths.
func (b *browser) sep() string {
	if b.st.Sep != "" {
		return b.st.Sep
	}
	return "/"
}

// joined is name in folder at, on the pane's machine.
func (b *browser) joined(at, name string) string {
	return strings.TrimSuffix(at, b.sep()) + b.sep() + name
}

// parent is the folder over at, on the pane's machine.
func (b *browser) parent(at string) string {
	sep := b.sep()
	trimmed := strings.TrimSuffix(at, sep)
	i := strings.LastIndex(trimmed, sep)
	switch {
	case i < 0:
		return at
	case i == 0:
		return sep
	}
	p := trimmed[:i]
	if strings.HasSuffix(p, ":") {
		// A drive, as C:, is its root with the separator.
		p += sep
	}
	return p
}

// dropKey is the spot of a drop on the folder shown rather than a row.
const dropKey = widget.Key("\x00here")

// dropSpot is where a drop d on the pane lands, and what it does there:
// into the folder row under it, which springs open, or into the folder
// shown. It keeps the drop that would be made in b.plan.
func (b *browser) dropSpot(d gi.Drop, u *gunim.UI) (widget.DropSpot, bool) {
	drag, rows := d.Data.(app.FileDrag)
	if !rows && (d.Data != nil || len(d.Paths) == 0) {
		return widget.DropSpot{}, false
	}
	box, ok := u.Bounds(b.table)
	if !ok {
		return widget.DropSpot{}, false
	}
	head := b.table.Header()
	spot := widget.DropSpot{Key: dropKey, Rect: geom.Rc(0, head, box.Size().W, max(0, box.Size().H-head))}
	into, name := b.st.Path, ""
	if k, ok := b.table.RowAt(d.Pos); ok {
		r, _ := b.table.RowRect(k)
		r = geom.Rc(4, r.Min.Y+1, r.Size().W-8, r.Size().H-2)
		switch e, isEntry := b.byName[k]; {
		case k == up && !b.st.Top:
			into = b.parent(b.st.Path)
			name = ".."
			spot = widget.DropSpot{Key: k, Rect: r, Radius: 5, Opens: true}
		case isEntry && e.IsDir() && !e.Archive:
			into = b.joined(b.st.Path, e.Name)
			name = e.Name
			spot = widget.DropSpot{Key: k, Rect: r, Radius: 5, Opens: true}
		}
	}
	here := b.w.filesKeyOf(b.id)
	plan := app.DropOnFiles{Pane: b.id, Into: into, Paths: d.Paths, Copy: true}
	if rows {
		plan.Paths, plan.Drag = nil, drag
		plan.Copy = drag.Machine != here
		switch {
		case d.Mods.Has(gi.ModControl):
			plan.Copy = true
		case d.Mods.Has(gi.ModShift):
			plan.Copy = false
		}
	}
	where := name
	if where == "" {
		where = baseOf(b.st.Path, b.sep())
	}
	hint := widget.DropHint{Text: "Move to " + where, Effect: widget.DropMove}
	if plan.Copy {
		hint = widget.DropHint{Text: "Copy to " + where, Effect: widget.DropCopy}
	}
	switch refused := b.refuse(plan, drag, rows, here); {
	case refused != "":
		spot.Refused = true
		hint = widget.DropHint{Text: refused, Effect: widget.DropRefused}
	}
	spot.Hint = hint
	b.plan = plan
	return spot, true
}

// refuse says why a drop would do nothing, or "" when it would do what
// it says.
func (b *browser) refuse(plan app.DropOnFiles, drag app.FileDrag, rows bool, here machines.ID) string {
	switch {
	case b.st.Seq == 0 || b.st.Err != "":
		return "Not read yet"
	case b.st.Archive:
		return "Inside an archive, which is read only"
	case !rows:
		return ""
	case drag.Machine == here && drag.At == plan.Into:
		return "Already here"
	}
	if drag.Machine == here {
		for _, n := range drag.Names {
			dir := b.joined(drag.At, n)
			if plan.Into == dir || strings.HasPrefix(plan.Into, strings.TrimSuffix(dir, b.sep())+b.sep()) {
				return "Cannot go inside itself"
			}
		}
	}
	return ""
}

// baseOf is the last name of path, or path itself at a root.
func baseOf(path, sep string) string {
	t := strings.TrimSuffix(path, sep)
	if i := strings.LastIndex(t, sep); i >= 0 && i+1 < len(t) {
		return t[i+1:]
	}
	return path
}

// fileCard is the card a drag of files carries: the first few names,
// each with its kind's icon, and how many more there are.
type fileCard struct {
	b     *browser
	d     app.FileDrag
	runs  []text.Run
	kinds []fileKind
	more  int
	size  float32
}

// cardRows is how many names a card shows.
const cardRows = 3

func newFileCard(b *browser, d app.FileDrag) *fileCard {
	c := &fileCard{b: b, d: d}
	for i, n := range d.Names {
		if i == cardRows {
			c.more = len(d.Names) - cardRows
			break
		}
		c.kinds = append(c.kinds, kindOf(b.byName[widget.Key(n)]))
	}
	return c
}

// cardLine is the height of one line of a card.
const cardLine = 24

// Layout implements [gunim.Node].
func (c *fileCard) Layout(cs gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	size := widget.TextSize.Get(f.Theme) * 0.95
	if c.size != size || len(c.runs) == 0 {
		c.size = size
		c.runs = c.runs[:0]
		for i := range c.kinds {
			c.runs = append(c.runs, text.Default().Shape(c.d.Names[i], size))
		}
		if c.more > 0 {
			c.runs = append(c.runs, text.Default().Shape("and "+strconv.Itoa(c.more)+" more", size*0.9))
		}
	}
	w := float32(0)
	for _, r := range c.runs {
		w = max(w, r.Advance)
	}
	return cs.Constrain(geom.Sz(min(w+44, 260), float32(len(c.runs))*cardLine+12))
}

// Paint implements [gunim.Node].
func (c *fileCard) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true,
		Radius: widget.CardRadius.Get(th)})()
	for i, r := range c.runs {
		y := 6 + float32(i)*cardLine
		ink := widget.Ink.Get(th)
		if i < len(c.kinds) {
			drawIcon(p, c.kinds[i].icon, geom.Rc(10, y+(cardLine-16)/2, 16, 16), c.kinds[i].ink.Get(th), 1.3)
		} else {
			ink = look.Faint.Get(th)
		}
		r.Paint(p, geom.Pt(34, y+(cardLine-r.Height())/2), ink)
	}
}
