package main

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	uiterm "github.com/marrasen/gridterm/ui/term"
)

// term is the terminal on screen: a CellGrid showing the shell's
// screen, taking keys for it.
type term struct {
	anim.Group
	id      string
	keys    *ui.Keymap
	sh      *shell
	cells   *widget.CellGrid
	row     []widget.Cell
	focused bool
	// wheel gathers the wheel's movement until it makes a whole notch.
	wheel float32
	// held is the button down in the pane, and at the cell the pointer
	// was last heard of in.
	held input.MouseButton
	// hoverMods are the modifiers the pointer last moved with, or held
	// since, and over says the pointer is over the pane.
	hoverMods input.Mods
	over      bool
	at        grid.Point
	// wantBlink says the program asked for a blinking cursor, blinking
	// that a blink is running, and blinkOff that the cursor is in the
	// off half of one. blinkRun numbers the blink running, so one
	// started again leaves the old one's steps to fall away.
	wantBlink, blinking, blinkOff bool
	blinkRun                      int
	// cursorAt is where the cursor was drawn, and cursorShown whether
	// it was; checking says a look at a cursor hidden is on its way.
	cursorAt              grid.Point
	cursorShown, checking bool
	// small is a size too small to give the shell until the pane has
	// held it a moment, since when, and settle keeps frames coming
	// while it waits.
	small      [2]int
	smallSince time.Time
	settle     *anim.Float
	// pics are the inline pictures on screen as the painter holds
	// them, by the picture each was made from.
	pics map[image.Image]*paint.Image
	// agent says the pane is shared with an agent, and marks are the
	// colours of the rings that say so.
	agent bool
	marks Marks
	// scale is how much a held screen bigger than the pane is shrunk to
	// fit it, and offset where it is drawn: 1 and nothing otherwise.
	scale  float32
	offset geom.Point
}

// leastCols and leastRows are the smallest screen a shell is given at
// once. A smaller one is given once the pane has held it for
// smallSettle: a pane sliding in or folding away passes through every
// width on the way, and a shell told each of them would print its
// prompt a few columns wide.
const (
	leastCols, leastRows = 20, 3
	smallSettle          = 150 * time.Millisecond
)

// blinkHalf is each half of a cursor's blink: once a second, as
// gridterm blinks.
const blinkHalf = 500 * time.Millisecond

// blink starts the cursor blinking, while the pane has the keyboard and
// its program asked for a blinking cursor.
func (t *term) blink(u *gunim.UI) {
	if t.blinking || !t.focused || !t.wantBlink {
		return
	}
	t.blinking = true
	run := t.blinkRun
	u.After(blinkHalf, func(u *gunim.UI) { t.blinkStep(run, u) })
}

func (t *term) blinkStep(run int, u *gunim.UI) {
	if run != t.blinkRun {
		return
	}
	if !t.focused || !t.wantBlink {
		t.blinking, t.blinkOff = false, false
	} else {
		t.blinkOff = !t.blinkOff
		u.After(blinkHalf, func(u *gunim.UI) { t.blinkStep(run, u) })
	}
	t.sync()
	u.Invalidate()
}

// blinkAgain starts the blink over, lit, as a key or a move of the
// cursor does in gridterm: the cursor is on screen where the user is
// looking. The next blink starts it again.
func (t *term) blinkAgain() {
	t.blinkRun++
	t.blinking, t.blinkOff = false, false
}

// lookAgain looks at the cursor again once a hide would have landed:
// the terminal keeps a hidden cursor on screen a moment, across a
// program's repaint, and nothing else may draw the pane after it.
func (t *term) lookAgain(u *gunim.UI) {
	if t.checking || !t.cursorShown {
		return
	}
	t.checking = true
	u.After(uiterm.CursorHideGrace+10*time.Millisecond, func(u *gunim.UI) {
		t.checking = false
		t.sync()
		u.Invalidate()
	})
}

func newTerm(id string, sh *shell, keys *ui.Keymap) *term {
	g := widget.NewCellGrid()
	g.Size = 15
	g.Background = termBackground
	t := &term{id: id, keys: keys, sh: sh, cells: g, settle: anim.NewFloat(0)}
	t.Add(t.settle)
	t.sync()
	return t
}

// Children implements [gunim.Composite].
func (t *term) Children() []gunim.Node { return []gunim.Node{t.cells} }

// Focusable implements [gunim.Focusable].
func (t *term) Focusable() bool { return true }

// Layout implements [gunim.Node]. The shell takes as many cells as fit.
func (t *term) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	size := k.Layout(c)
	k.Place(geom.Point{})
	cols, rows := t.cells.Fit()
	give := false
	switch {
	case cols < 1 || rows < 1:
	case cols >= leastCols && rows >= leastRows:
		t.small, give = [2]int{}, true
	case t.small != [2]int{cols, rows}:
		// Too small to give at once: given if the pane stays so.
		t.small, t.smallSince = [2]int{cols, rows}, f.Now
		t.settle.Jump(0)
		t.settle.Animate(1, anim.Tween{Duration: smallSettle + 50*time.Millisecond})
	default:
		give = f.Now.Sub(t.smallSince) >= smallSettle
	}
	if give && t.sh.resize(cols, rows) {
		t.sync()
	}
	// A screen somebody watching has sized bigger than this pane is laid
	// out whole, and drawn shrunk to fit, keeping its shape, in the
	// middle of the pane, as gridterm draws it.
	t.scale, t.offset = 1, geom.Point{}
	cell := t.cells.CellSize()
	cols, rows = t.cells.GridSize()
	need := geom.Sz(float32(cols)*cell.W, float32(rows)*cell.H)
	if t.sh.t.Held() && (need.W > size.W || need.H > size.H) && need.W > 0 && need.H > 0 {
		t.scale = min(size.W/need.W, size.H/need.H)
		k.Layout(gunim.Tight(need))
		t.offset = geom.Pt((size.W-need.W*t.scale)/2, (size.H-need.H*t.scale)/2)
	}
	return size
}

// Paint implements [gunim.Node]: the cells, and over them the
// pictures programs put in the output.
func (t *term) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	func() {
		if t.scale < 1 {
			defer p.Push(paint.Translate(t.offset))()
			defer p.Push(paint.Scale(t.scale, geom.Point{}))()
		}
		kids.At(0).Paint(p)
		t.paintPictures(p)
	}()
	t.paintRings(p, f, box)
}

// paintPictures draws the inline pictures on screen, each over the
// cells it was given, as gridterm draws them. A picture half scrolled
// off is drawn in part.
func (t *term) paintPictures(p *paint.Painter) {
	placed := t.sh.t.Pictures()
	if len(placed) == 0 && len(t.pics) == 0 {
		return
	}
	cols, rows := t.cells.GridSize()
	cell := t.cells.CellSize()
	kept := make(map[image.Image]*paint.Image, len(placed))
	for _, at := range placed {
		if at.Img == nil || at.Cols <= 0 || at.Rows <= 0 {
			continue
		}
		img, ok := t.pics[at.Img]
		if !ok {
			img = paint.NewImage(at.Img)
		}
		kept[at.Img] = img
		top, bottom := max(at.Top, 0), min(at.Top+at.Rows, rows)
		left, right := max(at.Col, 0), min(at.Col+at.Cols, cols)
		if top >= bottom || left >= right {
			continue
		}
		// The part of the picture that shows, in its own pixels.
		w, h := img.Size()
		sx, sy := float32(w)/float32(at.Cols), float32(h)/float32(at.Rows)
		src := geom.Rc(float32(left-at.Col)*sx, float32(top-at.Top)*sy, float32(right-left)*sx, float32(bottom-top)*sy)
		dst := geom.Rc(float32(left)*cell.W, float32(top)*cell.H, float32(right-left)*cell.W, float32(bottom-top)*cell.H)
		p.Image(img, dst, paint.ImageOpts{Src: src, Opacity: 1})
	}
	t.pics = kept
}

// sync draws the shell's screen and copies the rows that changed into
// the grid, with the cursor.
func (t *term) sync() {
	sh := t.sh
	sh.draw()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	g := sh.view
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
	t.wantBlink = cur.Blink
	// A pane whose program has ended takes no typing, and one without
	// the keyboard takes none now, so neither shows a cursor, as in
	// gridterm.
	visible := cur.Visible && !sh.t.Exited() && t.focused
	if at := (grid.Point{X: cur.X, Y: cur.Y}); at != t.cursorAt {
		t.cursorAt = at
		t.blinkAgain()
	}
	t.cursorShown = visible
	t.cells.SetCursor(widget.Cursor{Col: cur.X, Row: cur.Y, Shape: shape, Visible: visible,
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
	case gi.Drop:
		if len(e.Paths) == 0 {
			return false
		}
		u.Send(t, DropFiles{Pane: t.id, Paths: e.Paths})
		return true
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
		t.ctrlHeld(e.Key, e.Mods, true, u)
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
		t.typed(u)
		return true
	case gi.KeyRelease:
		t.ctrlHeld(e.Key, e.Mods, false, u)
		return false
	case gi.TextInput:
		for _, r := range e.Text {
			t.key(input.Event{Kind: input.Text, Rune: r, NormalText: true})
		}
		t.typed(u)
		return true
	case gi.PointerLeave:
		t.over = false
		return false
	case gi.Scroll:
		if e.Mods&gi.ModControl != 0 {
			// Ctrl and the wheel size the font, which the window does.
			return false
		}
		t.scroll(e, u)
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

// typed shows what a key did, with the cursor lit.
func (t *term) typed(u *gunim.UI) {
	t.sync()
	t.blink(u)
	u.Invalidate()
}

// ctrlHeld lights or unlights the link under a still pointer as Ctrl
// goes down or comes up, as gridterm does, rather than at the pointer's
// next move.
func (t *term) ctrlHeld(k gi.Key, mods gi.Mods, down bool, u *gunim.UI) {
	if k != gi.KeyLeftControl && k != gi.KeyRightControl {
		return
	}
	m := mouseMods(mods) &^ input.ModCtrl
	if down {
		m |= input.ModCtrl
	}
	if !t.over || t.held != input.MouseNone || m == t.hoverMods {
		return
	}
	t.hoverMods = m
	t.sh.t.SetHover(t.at.X, t.at.Y, m)
	t.sync()
	u.Invalidate()
}

// mouse hands a pointer event to the terminal, which reports it to a
// program that asked for the mouse, and otherwise selects.
func (t *term) mouse(e input.MouseEvent, u *gunim.UI) bool {
	took, _ := t.sh.t.HandleMouse(e)
	t.sync()
	u.Invalidate()
	return took
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
	if t.scale > 0 && t.scale < 1 {
		// Drawn shrunk: the point back in the cells' own space.
		p = geom.Pt((p.X-t.offset.X)/t.scale, (p.Y-t.offset.Y)/t.scale)
	}
	col, row := t.cells.CellAt(p)
	return grid.Point{X: col, Y: row}
}

// press starts a selection, or a program's click; the middle button
// pastes, and the right is left to the window.
func (t *term) press(e gi.PointerDown, u *gunim.UI) bool {
	at := t.cellAt(e.Pos)
	mods := mouseMods(e.Mods)
	switch {
	case e.Focusing && e.Button == gi.ButtonPrimary:
		// The click that gives the pane the keyboard only does that, as
		// in gridterm: it starts no selection, and a program with the
		// mouse is not clicked at a place nobody aimed for.
		return true
	case e.Button == gi.ButtonMiddle && !t.sh.t.MouseTaken(mods):
		// Text alone, the X11 way: a picture is pasted with the key.
		if s := u.Clipboard(); s != "" {
			t.paste(s)
		} else {
			u.Send(t, NoTextToPaste{})
		}
		return true
	case e.Button == gi.ButtonSecondary && !t.sh.t.MouseTaken(mods):
		return false
	}
	t.held, t.at = mouseButton(e.Button), at
	return t.mouse(input.MouseEvent{Kind: input.MousePress, Button: t.held, Col: at.X, Row: at.Y, Mods: mods}, u)
}

// drag extends the selection, or tells a program the pointer moved.
func (t *term) drag(e gi.PointerMove, u *gunim.UI) bool {
	at := t.cellAt(e.Pos)
	t.over = true
	// With Ctrl down, a link under the pointer is underlined, and a
	// click follows it.
	if t.held == input.MouseNone {
		mods := mouseMods(e.Mods)
		if mods != t.hoverMods || at != t.at {
			t.hoverMods = mods
			t.sh.t.SetHover(at.X, at.Y, mods)
			t.sync()
			u.Invalidate()
		}
	}
	if at == t.at {
		return t.held != input.MouseNone
	}
	t.at = at
	return t.mouse(input.MouseEvent{Kind: input.MouseMove, Button: t.held, Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)}, u)
}

func (t *term) release(e gi.PointerUp, u *gunim.UI) bool {
	if t.held == input.MouseNone {
		return false
	}
	at := t.cellAt(e.Pos)
	held := t.held
	t.held = input.MouseNone
	return t.mouse(input.MouseEvent{Kind: input.MouseRelease, Button: held, Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)}, u)
}

// copySelection puts the selected text on the clipboard.
func (t *term) copySelection(u *gunim.UI) {
	if text := t.sh.t.SelectionText(); text != "" {
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
		t.pasteClipboard(u)
	case "view.scrollUp", "view.scrollDown":
		// Half a screen, as gridterm moves.
		page := 1
		if id == "view.scrollDown" {
			page = -1
		}
		t.sh.t.ScrollPages(page)
		t.sync()
		u.Invalidate()
	default:
		return false
	}
	return true
}

// key hands one event to the terminal, which encodes it for the
// program, brings the view back to the live screen and clears the
// selection, as typing does in gridterm.
func (t *term) key(ev input.Event) {
	t.blinkAgain()
	_, _ = t.sh.t.HandleKey(ev)
}

func (t *term) paste(s string) { t.sh.t.Paste(s) }

// pasteClipboard pastes the text on the clipboard, and with no text
// there, asks the program to hand over the picture that may be there
// instead. A clipboard holding both is text: copying from a browser
// leaves both, and the words are what was meant.
func (t *term) pasteClipboard(u *gunim.UI) {
	if s := u.Clipboard(); s != "" {
		t.paste(s)
		return
	}
	u.Send(t, PastePicture{Pane: t.id})
}

// scroll hands the terminal a wheel notch at a time, which it takes as
// gridterm does: a report to a program that has the mouse, arrows on
// the alternate screen, and otherwise three lines of history.
func (t *term) scroll(e gi.Scroll, u *gunim.UI) {
	notches := e.Notches.Y
	if notches == 0 {
		// A wheel that does not count notches: the distance, at the
		// 40 pixels a notch gunim's driver gives.
		notches = e.Delta.Y / 40
	}
	t.wheel += notches
	n := int(math.Trunc(float64(t.wheel)))
	if n == 0 {
		return
	}
	t.wheel -= float32(n)
	b := input.MouseWheelUp
	if n < 0 {
		b, n = input.MouseWheelDown, -n
	}
	at := t.cellAt(e.Pos)
	for range n {
		_, _ = t.sh.t.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: b, Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)})
	}
	t.sync()
	u.Invalidate()
}

// keyEvent turns a gunim key press into gridterm's, for a key gridterm
// encodes.
func keyEvent(e gi.KeyPress) (input.Event, bool) {
	k, ok := keyMap[e.Key]
	// Punctuation is the key it types, as gridterm reads it: a Swedish
	// keyboard puts + where a US one has -, and Ctrl and the key marked
	// plus should make the font bigger.
	if p, typed := punctuation[e.Char]; typed {
		k, ok = p, true
	}
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

// punctuation is the punctuation gridterm binds, by the character the
// key types rather than where it sits.
var punctuation = map[rune]input.Key{
	'=': input.KeyEquals, '+': input.KeyPlus, '-': input.KeyMinus,
	'[': input.KeyBracketLeft, ']': input.KeyBracketRight, '\\': input.KeyBackslash,
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

// Cursor implements [gunim.CursorShaper]: a hand over a link that a
// click would follow, and otherwise the arrow, as in gridterm.
func (t *term) Cursor(p geom.Point) gi.Cursor {
	at := t.cellAt(p)
	if _, on := t.sh.t.CursorAt(at.X, at.Y, t.hoverMods); on {
		return gi.CursorHand
	}
	return gi.CursorArrow
}
