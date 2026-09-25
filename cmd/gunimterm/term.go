package main

import (
	"image/color"
	"math"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
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
	// selecting is set while the pointer drags a selection. held is the
	// button a program reporting the mouse saw go down, and at the cell
	// it last heard of.
	selecting bool
	held      input.MouseButton
	at        grid.Point
	// wantBlink says the program asked for a blinking cursor, blinking
	// that a blink is running, and blinkOff that the cursor is in the
	// off half of one.
	wantBlink, blinking, blinkOff bool
}

// blinkHalf is each half of a cursor's blink, as xterm times it.
const blinkHalf = 530 * time.Millisecond

// blink starts the cursor blinking, while the pane has the keyboard and
// its program asked for a blinking cursor.
func (t *term) blink(u *gunim.UI) {
	if t.blinking || !t.focused || !t.wantBlink {
		return
	}
	t.blinking = true
	u.After(blinkHalf, t.blinkStep)
}

func (t *term) blinkStep(u *gunim.UI) {
	if !t.focused || !t.wantBlink {
		t.blinking, t.blinkOff = false, false
	} else {
		t.blinkOff = !t.blinkOff
		u.After(blinkHalf, t.blinkStep)
	}
	t.sync()
	u.Invalidate()
}

func newTerm(id string, sh *shell, keys *ui.Keymap) *term {
	g := widget.NewCellGrid()
	g.Size = 15
	g.Background = termBackground
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
	t.wantBlink = cur.Blink
	t.cells.SetCursor(widget.Cursor{Col: cur.X, Row: cur.Y, Shape: shape, Visible: cur.Visible,
		Blinked: t.blinkOff && t.wantBlink && t.focused})
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
		t.blinkOff = false
		t.sync()
		t.blink(u)
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
		if t.reporting(e.Mods) {
			t.wheelReport(e, u)
			return true
		}
		t.scroll(e.Delta.Y, u)
		return true
	case gi.PointerDown:
		return t.press(e, u)
	case gi.PointerMove:
		return t.drag(e, u)
	case gi.PointerUp:
		return t.release(e, u)
	}
	return false
}

// reporting reports whether the program has the mouse: it asked for
// it, and Shift, which keeps the mouse for selecting, is up.
func (t *term) reporting(mods gi.Mods) bool {
	t.sh.mu.Lock()
	on := t.sh.vt.Screen().MouseEnabled()
	t.sh.mu.Unlock()
	return on && !mods.Has(gi.ModShift)
}

// report sends a mouse event to the program, in the encoding it asked
// for.
func (t *term) report(e input.MouseEvent) {
	sh := t.sh
	sh.mu.Lock()
	click, drag, motion, sgr := sh.vt.Screen().MouseModes()
	b := input.EncodeMouse(e, input.MouseMode{Click: click, Drag: drag, Motion: motion, SGR: sgr}, nil)
	sh.mu.Unlock()
	sh.send(b)
}

func mouseMods(m gi.Mods) input.Mods {
	var out input.Mods
	if m.Has(gi.ModShift) {
		out |= input.ModShift
	}
	if m.Has(gi.ModAlt) {
		out |= input.ModAlt
	}
	if m.Has(gi.ModControl) {
		out |= input.ModCtrl
	}
	return out
}

func mouseButton(b gi.Button) input.MouseButton {
	switch b {
	case gi.ButtonPrimary:
		return input.MouseLeft
	case gi.ButtonMiddle:
		return input.MouseMiddle
	case gi.ButtonSecondary:
		return input.MouseRight
	}
	return input.MouseNone
}

func (t *term) cellAt(p geom.Point) grid.Point {
	col, row := t.cells.CellAt(p)
	return grid.Point{X: col, Y: row}
}

// press starts a selection, or a program's click; the middle button
// pastes.
func (t *term) press(e gi.PointerDown, u *gunim.UI) bool {
	at := t.cellAt(e.Pos)
	if t.reporting(e.Mods) {
		t.held, t.at = mouseButton(e.Button), at
		t.report(input.MouseEvent{Kind: input.MousePress, Button: t.held, Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)})
		return true
	}
	switch e.Button {
	case gi.ButtonPrimary:
		t.sh.mu.Lock()
		t.sh.grid.SetSelection(grid.Selection{Anchor: at, Cursor: at, Block: e.Mods.Has(gi.ModAlt)})
		t.sh.mu.Unlock()
		t.selecting = true
	case gi.ButtonMiddle:
		t.paste(u.Clipboard())
	case gi.ButtonSecondary:
		return false
	}
	t.sync()
	u.Invalidate()
	return true
}

// drag extends the selection, or tells a program the pointer moved.
func (t *term) drag(e gi.PointerMove, u *gunim.UI) bool {
	at := t.cellAt(e.Pos)
	if t.held != input.MouseNone || (!t.selecting && t.reporting(e.Mods)) {
		if at != t.at {
			t.at = at
			t.report(input.MouseEvent{Kind: input.MouseMove, Button: t.held, Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)})
		}
		return true
	}
	if !t.selecting {
		return false
	}
	t.sh.mu.Lock()
	sel := t.sh.grid.Selection()
	sel.Cursor = at
	sel.Active = sel.Active || at != sel.Anchor
	t.sh.grid.SetSelection(sel)
	t.sh.mu.Unlock()
	t.sync()
	u.Invalidate()
	return true
}

func (t *term) release(e gi.PointerUp, _ *gunim.UI) bool {
	if t.held != input.MouseNone {
		at := t.cellAt(e.Pos)
		t.report(input.MouseEvent{Kind: input.MouseRelease, Button: t.held, Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)})
		t.held = input.MouseNone
		return true
	}
	if t.selecting {
		t.selecting = false
		return true
	}
	return false
}

// wheelReport sends the wheel to a program that reports the mouse, a
// press for each line's worth.
func (t *term) wheelReport(e gi.Scroll, u *gunim.UI) {
	h := t.cells.CellSize().H
	if h <= 0 {
		return
	}
	t.wheel += e.Delta.Y / h
	lines := int(math.Trunc(float64(t.wheel)))
	t.wheel -= float32(lines)
	b := input.MouseWheelUp
	if lines < 0 {
		b, lines = input.MouseWheelDown, -lines
	}
	at := t.cellAt(e.Pos)
	for range lines {
		t.report(input.MouseEvent{Kind: input.MousePress, Button: b, Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)})
	}
	u.Invalidate()
}

// copySelection puts the selected text on the clipboard.
func (t *term) copySelection(u *gunim.UI) {
	t.sh.mu.Lock()
	text := t.sh.grid.SelectedText()
	t.sh.mu.Unlock()
	if text != "" {
		u.SetClipboard(text)
	}
}

// command carries out one of the window's commands that belongs to the
// terminal, and reports false for the rest, which go on to the window.
func (t *term) command(id string, u *gunim.UI) bool {
	switch id {
	case "edit.copy":
		t.copySelection(u)
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
	// Typing clears the selection, and shows a blinking cursor.
	if len(b) > 0 && sh.grid.Selection().Active {
		sh.grid.ClearSelection()
	}
	t.blinkOff = false
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
