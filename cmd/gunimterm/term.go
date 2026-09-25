package main

import (
	"image/color"
	"math"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// term is the terminal on screen: a CellGrid showing the shell's
// screen, taking keys for it.
type term struct {
	id      string
	keys    *ui.Keymap
	sh      *shell
	cells   *widget.CellGrid
	row     []widget.Cell
	focused bool
	// wheel gathers the wheel's movement until it makes a whole line.
	wheel float32
}

func newTerm(id string, sh *shell, keys *ui.Keymap) *term {
	g := widget.NewCellGrid()
	g.Size = 15
	bg := sh.pal.BG
	g.Background = theme.Color("gunimterm.background", color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: 0xff})
	t := &term{id: id, keys: keys, sh: sh, cells: g}
	t.sync()
	return t
}

// Children implements [gunim.Composite].
func (t *term) Children() []gunim.Node { return []gunim.Node{t.cells} }

// Focusable implements [gunim.Focusable].
func (t *term) Focusable() bool { return true }

// Layout implements [gunim.Node]. The shell takes as many cells as fit.
func (t *term) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	size := k.Layout(c)
	k.Place(geom.Point{})
	if cols, rows := t.cells.Fit(); t.sh.resize(cols, rows) {
		t.sync()
	}
	return size
}

// Paint implements [gunim.Node].
func (t *term) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
}

// sync copies the rows of the shell's screen that changed into the
// grid, with the cursor.
func (t *term) sync() {
	sh := t.sh
	sh.mu.Lock()
	defer sh.mu.Unlock()
	g := sh.grid
	sh.vt.Render(g)
	cols, rows := g.Size()
	t.cells.Resize(cols, rows)
	for y := range rows {
		if !g.RowDirty(y) {
			continue
		}
		t.row = t.row[:0]
		for x := range cols {
			t.row = append(t.row, cellOf(g, x, y))
		}
		t.cells.SetRow(y, t.row)
	}
	g.ClearDirty()
	cur := g.Cursor()
	shape := widget.CursorBlock
	switch cur.Style {
	case grid.CursorBar:
		shape = widget.CursorBar
	case grid.CursorUnderline:
		shape = widget.CursorUnderline
	case grid.CursorBlock:
	}
	if !t.focused {
		shape = widget.CursorOutline
	}
	t.cells.SetCursor(widget.Cursor{Col: cur.X, Row: cur.Y, Shape: shape, Visible: cur.Visible})
}

// cellOf turns one of gridterm's cells into gunim's, with its colours
// resolved.
func cellOf(g *grid.Grid, x, y int) widget.Cell {
	c := g.At(x, y)
	if c.Width == 0 {
		return widget.Cell{}
	}
	fg, bg := g.FGOf(x, y), g.BGOf(x, y)
	switch {
	case c.Attr&grid.AttrHidden != 0:
		fg = bg
	case c.Attr&grid.AttrDim != 0:
		fg = grid.Blend(fg, bg, 1, 2)
	}
	out := widget.Cell{
		Rune: c.Rune,
		FG:   color.NRGBA{R: fg.R, G: fg.G, B: fg.B, A: 0xff},
		BG:   color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: 0xff},
		Wide: c.Width == 2,
	}
	if len(c.Comb) > 0 {
		out.Marks = string(c.Comb)
	}
	for _, s := range [...]struct {
		a grid.Attr
		s widget.CellStyle
	}{{grid.AttrBold, widget.CellBold}, {grid.AttrItalic, widget.CellItalic}, {grid.AttrUnderline, widget.CellUnderline}, {grid.AttrStrike, widget.CellStrike}} {
		if c.Attr&s.a != 0 {
			out.Style |= s.s
		}
	}
	return out
}

// Handle implements [gunim.Handler]: keys and text go to the shell,
// and the wheel scrolls back through what has scrolled off.
func (t *term) Handle(e gi.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case gi.FocusGained, gi.FocusLost:
		_, t.focused = e.(gi.FocusGained)
		if t.focused {
			u.Send(t, FocusPane{Pane: t.id})
		}
		t.sync()
		u.Invalidate()
		return true
	case gi.KeyPress:
		// A press that typed leaves it to the text, which follows.
		if e.Typed {
			return true
		}
		ev, ok := keyEvent(e)
		if !ok {
			return true
		}
		if id, bound := t.keys.Lookup(ui.ChordOf(ev)); bound {
			// The window's shortcut, unless the terminal carries it out.
			return t.command(id, u)
		}
		t.key(ev)
		return true
	case gi.TextInput:
		for _, r := range e.Text {
			t.key(input.Event{Kind: input.Text, Rune: r, NormalText: true})
		}
		return true
	case gi.Scroll:
		t.scroll(e.Delta.Y, u)
		return true
	}
	return false
}

// command carries out one of the window's commands that belongs to the
// terminal, and reports false for the rest, which go on to the window.
func (t *term) command(id string, u *gunim.UI) bool {
	switch id {
	case "edit.paste":
		t.paste(u.Clipboard())
	case "view.scrollUp", "view.scrollDown":
		_, rows := t.cells.GridSize()
		page := max(1, rows-1)
		if id == "view.scrollDown" {
			page = -page
		}
		t.sh.mu.Lock()
		if !t.sh.vt.Screen().OnAltBuffer() {
			t.sh.vt.Screen().ScrollView(page)
		}
		t.sh.mu.Unlock()
		t.sync()
		u.Invalidate()
	default:
		return false
	}
	return true
}

// key encodes one event for the shell, and brings the view back to the
// live screen, as typing does in gridterm.
func (t *term) key(ev input.Event) {
	sh := t.sh
	sh.mu.Lock()
	scr := sh.vt.Screen()
	b := input.EncodeMode(ev, input.Mode{AppCursor: scr.AppCursor()}, nil)
	if len(b) > 0 && scr.ViewOffset() != 0 {
		scr.ResetView()
	}
	sh.mu.Unlock()
	sh.send(b)
}

func (t *term) paste(s string) {
	sh := t.sh
	sh.mu.Lock()
	b := input.EncodePaste(s, sh.vt.Screen().Bracketed(), nil)
	sh.mu.Unlock()
	sh.send(b)
}

// scroll moves the view through history by the wheel's movement, a
// line at a time. On the alternate screen, which keeps no history, it
// sends the arrow keys instead, as gridterm does.
func (t *term) scroll(dy float32, u *gunim.UI) {
	h := t.cells.CellSize().H
	if h <= 0 {
		return
	}
	t.wheel += dy / h
	lines := int(math.Trunc(float64(t.wheel)))
	if lines == 0 {
		return
	}
	t.wheel -= float32(lines)
	sh := t.sh
	sh.mu.Lock()
	alt := sh.vt.Screen().OnAltBuffer()
	if !alt {
		sh.vt.Screen().ScrollView(lines)
	}
	sh.mu.Unlock()
	if alt {
		k := input.KeyUp
		if lines < 0 {
			k, lines = input.KeyDown, -lines
		}
		for range lines {
			t.key(input.Event{Kind: input.KeyPress, Key: k})
		}
		return
	}
	t.sync()
	u.Invalidate()
}

// keyEvent turns a gunim key press into gridterm's, for a key gridterm
// encodes.
func keyEvent(e gi.KeyPress) (input.Event, bool) {
	k, ok := keyMap[e.Key]
	if !ok {
		return input.Event{}, false
	}
	ev := input.Event{Kind: input.KeyPress, Key: k}
	if e.Repeat {
		ev.Kind = input.KeyRepeat
	}
	for _, m := range [...]struct {
		from gi.Mods
		to   input.Mods
	}{{gi.ModShift, input.ModShift}, {gi.ModControl, input.ModCtrl}, {gi.ModAlt, input.ModAlt}, {gi.ModSuper, input.ModSuper}} {
		if e.Mods.Has(m.from) {
			ev.Mods |= m.to
		}
	}
	return ev, true
}

// keyMap holds the keys gridterm encodes. The keypad's keys, pressed
// without Num Lock, are the keys printed on them.
var keyMap = func() map[gi.Key]input.Key {
	m := map[gi.Key]input.Key{
		gi.KeyEnter: input.KeyEnter, gi.KeyKPEnter: input.KeyEnter,
		gi.KeyTab: input.KeyTab, gi.KeyBackspace: input.KeyBackspace,
		gi.KeyEscape: input.KeyEscape, gi.KeySpace: input.KeySpace,
		gi.KeyLeftBracket: input.KeyBracketLeft, gi.KeyRightBracket: input.KeyBracketRight,
		gi.KeyBackslash: input.KeyBackslash, gi.KeyEqual: input.KeyEquals,
		gi.KeyMinus: input.KeyMinus, gi.Key0: input.Key0,
		gi.KeyUp: input.KeyUp, gi.KeyDown: input.KeyDown,
		gi.KeyLeft: input.KeyLeft, gi.KeyRight: input.KeyRight,
		gi.KeyHome: input.KeyHome, gi.KeyEnd: input.KeyEnd,
		gi.KeyInsert: input.KeyInsert, gi.KeyDelete: input.KeyDelete,
		gi.KeyPageUp: input.KeyPageUp, gi.KeyPageDown: input.KeyPageDown,
		gi.KeyKP8: input.KeyUp, gi.KeyKP2: input.KeyDown,
		gi.KeyKP4: input.KeyLeft, gi.KeyKP6: input.KeyRight,
		gi.KeyKP7: input.KeyHome, gi.KeyKP1: input.KeyEnd,
		gi.KeyKP0: input.KeyInsert, gi.KeyKPDecimal: input.KeyDelete,
		gi.KeyKP9: input.KeyPageUp, gi.KeyKP3: input.KeyPageDown,
	}
	for i := range 26 {
		m[gi.KeyA+gi.Key(i)] = input.KeyA + input.Key(i)
	}
	for i := range 12 {
		m[gi.KeyF1+gi.Key(i)] = input.KeyF1 + input.Key(i)
	}
	return m
}()
