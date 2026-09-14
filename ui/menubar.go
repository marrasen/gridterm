package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// barRows is how tall the row of menu titles is.
const barRows = 1

// MenuDef is one menu on a bar: the word shown and the lines under it.
type MenuDef struct {
	Title string
	Items []MenuItem
}

// MenubarStyle colours a menu bar.
type MenubarStyle struct {
	FG, BG color.RGBA

	// BGEnd is the background of the last column, when it has an alpha.
	// The columns in between blend from BG to it, which gives the bar a
	// ground of its own rather than the window's.
	BGEnd color.RGBA

	// OpenFG and OpenBG mark the title whose menu is showing.
	OpenFG, OpenBG color.RGBA
}

// colAt is the background of one column of the bar, blended across it
// when the style asks for that.
func (s MenubarStyle) colAt(x, cols int) color.RGBA {
	if s.BGEnd.A == 0 || cols <= 1 {
		return s.BG
	}
	return blend(s.BG, s.BGEnd, min(max(x, 0), cols-1), cols-1)
}

// Menubar is a row of menu titles above one other widget.
//
// The bar never takes focus. It is chrome: keys go straight through to
// the widget under it, and a menu is opened by clicking a title or by
// running the command that calls Open. A bar that grabbed focus would
// make every program in a pane fight it for the arrow keys.
//
// It owns the menu that is showing. Everything about moving between
// menus lives here, so whoever builds the bar supplies only Present,
// which puts a menu on screen and returns the way to take it off again.
type Menubar struct {
	Style     MenubarStyle
	MenuStyle MenuStyle

	// Menus are the titles, left to right. Leave them alone while a menu
	// is open: the open menu holds the index of the title it hangs under,
	// and changing the list moves that title out from under it.
	Menus []MenuDef

	// Present shows a menu and returns the function that takes it away.
	// The bar knows nothing about the modal stack or the layers a menu is
	// drawn on, so this is how it reaches them. A nil Present means no
	// menu can be opened.
	Present func(*Menu) (close func())

	// Origin says where the bar itself sits, in the coordinates a menu is
	// laid out in. A menu is placed over the whole window while the bar
	// is told only about its own corner of it, so without this a menu
	// would hang under the wrong column. A nil Origin puts the bar at the
	// top left.
	Origin func() Rect

	cmds *Commands
	keys *Keymap

	child    Widget
	size     Size
	hasFocus bool

	// open is the menu showing and openAt which title it came from, or
	// -1 when none is open. closeOpen takes it away again.
	open      *Menu
	openAt    int
	closeOpen func()

	// buf keeps the title row off the layer until it is finished. Filling
	// the row and then writing the titles over it changes the same cell
	// twice, which would dirty the row on every frame.
	buf buffer
}

// NewMenubar puts a row of menu titles above a widget.
func NewMenubar(cmds *Commands, keys *Keymap, child Widget) *Menubar {
	return &Menubar{cmds: cmds, keys: keys, child: child, openAt: -1}
}

// Open shows the menu under title i, replacing whichever was showing. It
// reports whether there is such a menu and it could be shown.
func (b *Menubar) Open(i int) bool {
	if b.Present == nil || i < 0 || i >= len(b.Menus) {
		return false
	}
	b.Close()

	menu := NewMenu(b.cmds, b.keys, b.Menus[i].Items, b.Close)
	menu.Style = b.MenuStyle
	// The closure holds the title's own index rather than reading the
	// bar, so laying the menu out inside Present asks the right question
	// before the bar has recorded anything.
	menu.Anchor = func() Rect { return b.anchorFor(i) }
	menu.OnEdge = b.step
	menu.OnOutside = b.pressedBar

	close := b.Present(menu)
	if close == nil {
		return false
	}
	// Recorded only once the menu is up, and all three together. Set
	// beforehand, a Present that closed the menu again would clear them
	// and then have the closer written back over the top, leaving the bar
	// holding a way to tear down a menu that has already gone.
	b.open, b.openAt, b.closeOpen = menu, i, close
	return true
}

// Close takes the showing menu away, if there is one.
func (b *Menubar) Close() {
	// Forget it before closing it. Closing runs whatever the program
	// hung off that, which may ask the bar what is open, and a bar
	// halfway through letting go would answer with the menu it is losing.
	closeOpen := b.closeOpen
	b.open, b.openAt, b.closeOpen = nil, -1, nil
	if closeOpen != nil {
		closeOpen()
	}
}

// OpenIndex returns which title's menu is showing, or -1 when none is.
func (b *Menubar) OpenIndex() int { return b.openAt }

// Children returns the one widget under the bar.
func (b *Menubar) Children() []Widget {
	if b.child == nil {
		return nil
	}
	return []Widget{b.child}
}

// Focused returns the widget under the bar, which is the only thing that
// can hold focus here.
func (b *Menubar) Focused() Widget { return b.child }

// Focus reports whether w is the widget under the bar. There is nothing
// to move focus between.
func (b *Menubar) Focus(w Widget) bool { return w != nil && w == b.child }

// Replace swaps the widget under the bar, reporting whether old was
// there.
func (b *Menubar) Replace(old, new Widget) bool {
	if new == nil || old == nil || old != b.child {
		return false
	}
	if new != old {
		if b.hasFocus {
			SetFocus(old, false)
		}
		b.child = new
		if b.hasFocus {
			SetFocus(new, true)
		}
	}
	if !b.size.Empty() {
		b.Layout(b.size)
	}
	return true
}

// Remove takes the widget out from under the bar. A bar with nothing
// under it has no reason to exist, so it reports that nothing should
// stand in its place.
func (b *Menubar) Remove(w Widget) (Widget, bool) {
	if w == nil || w != b.child {
		return nil, false
	}
	if b.hasFocus {
		SetFocus(w, false)
	}
	b.child = nil
	return nil, true
}

// ChildArea returns where the widget under the bar is drawn.
func (b *Menubar) ChildArea(w Widget) (Rect, bool) {
	if w == nil || w != b.child {
		return Rect{}, false
	}
	body := b.body()
	if body.Empty() {
		return Rect{}, false
	}
	return body, true
}

// Layout gives the widget under the bar everything except the title row.
func (b *Menubar) Layout(size Size) {
	b.size = size
	layoutIfVisible(b.child, b.body())
}

// Draw paints the titles and the widget under them.
//
// The menu itself is not drawn here. It is a modal on a layer of its
// own, so that closing it costs a blit rather than a repaint of the
// pane underneath.
func (b *Menubar) Draw(v grid.View) {
	b.drawBar(v)
	if body := b.body(); !body.Empty() && b.child != nil {
		b.child.Draw(body.In(v))
	}
}

// drawBar paints the row of titles.
func (b *Menubar) drawBar(v grid.View) {
	if bar := b.bar(); !bar.Empty() {
		b.buf.draw(bar.In(v), b.paintBar)
	}
}

// paintBar draws the titles into a row of their own.
func (b *Menubar) paintBar(row grid.View) {
	// Column by column, because the ground can be a blend across the bar
	// rather than one colour.
	cols, _ := row.Size()
	for x := 0; x < cols; x++ {
		row.Set(x, 0, grid.Cell{
			Rune: ' ', FG: b.Style.FG, BG: b.Style.colAt(x, cols), Width: 1,
		})
	}

	for i, label := range b.labels() {
		if label.Empty() {
			continue
		}
		cell := label.In(row)
		if i == b.openAt {
			// The open title is marked out, so it carries its own
			// colour rather than the bar's ground.
			fg, bg := b.Style.OpenFG, b.Style.OpenBG
			cell.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
			cell.SetString(1, 0, b.Menus[i].Title, fg, bg, 0)
			continue
		}
		// Written a cluster at a time, each on the ground its own column
		// carries, so the blend runs under the titles as well as between
		// them.
		at := 1
		for _, cluster := range grid.Clusters(b.Menus[i].Title) {
			bg := b.Style.colAt(label.X+at, cols)
			next := cell.SetString(at, 0, cluster, b.Style.FG, bg, 0)
			if next <= at {
				break
			}
			at = next
		}
	}
}

// SetFocus passes focus on to the widget under the bar. The bar itself
// never holds it.
func (b *Menubar) SetFocus(on bool) {
	if b.hasFocus == on {
		return
	}
	b.hasFocus = on
	SetFocus(b.child, on)
}

// HandleKey offers the key to the widget under the bar. The bar has no
// keys of its own: opening a menu is a command, so it can be rebound and
// can itself appear in a menu.
func (b *Menubar) HandleKey(ev input.Event) (bool, error) {
	return HandleKey(b.child, ev)
}

// HandleMouse opens the menu under a title that was clicked, or passes
// the event to the widget below.
func (b *Menubar) HandleMouse(ev input.MouseEvent) (bool, error) {
	if bar := b.bar(); bar.Contains(ev.Col, ev.Row) {
		if ev.Kind != input.MousePress || ev.Button.IsWheel() {
			return false, nil
		}
		return b.clickLabel(ev.Col, ev.Row), nil
	}

	body := b.body()
	if b.child == nil || !body.Contains(ev.Col, ev.Row) {
		return false, nil
	}
	ev.Col, ev.Row = body.Local(ev.Col, ev.Row)
	return HandleMouse(b.child, ev)
}

// clickLabel acts on a press in the title row, reporting whether it
// meant anything. Pressing the title of the menu already showing closes
// it, which is what a control that opens something is expected to do.
func (b *Menubar) clickLabel(col, row int) bool {
	for i, label := range b.labels() {
		if label.Empty() || !label.Contains(col, row) {
			continue
		}
		if i == b.openAt {
			b.Close()
			return true
		}
		return b.Open(i)
	}
	// The empty part of the bar belongs to nothing.
	return false
}

// pressedBar handles a press that fell outside the open menu, reporting
// whether the bar dealt with it. The point is in the menu's coordinates,
// which cover the whole window.
func (b *Menubar) pressedBar(col, row int) bool {
	origin := b.origin()
	col, row = col-origin.X, row-origin.Y
	if !b.bar().Contains(col, row) {
		return false
	}
	// A press anywhere on the bar is the bar's, even between titles: it
	// closes the menu rather than reaching the pane underneath.
	if !b.clickLabel(col, row) {
		b.Close()
	}
	return true
}

// step moves to the menu beside the one showing, wrapping at the ends.
func (b *Menubar) step(by int) {
	if len(b.Menus) == 0 || b.openAt < 0 {
		return
	}
	// Go's % keeps the sign of the dividend, so a step back from the
	// first menu needs the extra turn to land on the last.
	next := ((b.openAt+by)%len(b.Menus) + len(b.Menus)) % len(b.Menus)
	if next != b.openAt {
		b.Open(next)
	}
}

// anchorFor returns where a menu hangs, in the coordinates a menu is
// laid out in.
//
// A title too far along to fit on the bar has no rectangle of its own,
// and the menu hangs from the bar's own row instead. Falling back to
// whatever Origin reports would use the bar's whole height, which is the
// window's, and put the menu off the bottom.
func (b *Menubar) anchorFor(i int) Rect {
	origin := b.origin()
	at := Rect{X: origin.X, Y: origin.Y, Rows: barRows}
	if labels := b.labels(); i >= 0 && i < len(labels) && !labels[i].Empty() {
		at.X, at.Y = labels[i].X+origin.X, labels[i].Y+origin.Y
		at.Cols, at.Rows = labels[i].Cols, labels[i].Rows
	}
	return at
}

// origin returns where the bar sits in the window.
func (b *Menubar) origin() Rect {
	if b.Origin == nil {
		return Rect{}
	}
	return b.Origin()
}

// bar returns the title row, which is empty when there is no room for
// both it and something under it.
func (b *Menubar) bar() Rect {
	if b.size.Cols <= 0 || b.size.Rows <= barRows {
		return Rect{}
	}
	return Rect{Cols: b.size.Cols, Rows: barRows}
}

// body returns where the widget under the bar goes.
func (b *Menubar) body() Rect {
	if b.size.Empty() {
		return Rect{}
	}
	top := b.bar().Rows
	return Rect{Y: top, Cols: b.size.Cols, Rows: b.size.Rows - top}
}

// labels returns where each title goes, in the bar's own coordinates. A
// title that does not fit is empty, and an empty title is drawn nowhere
// and cannot be clicked.
func (b *Menubar) labels() []Rect {
	out := make([]Rect, len(b.Menus))
	bar := b.bar()
	if bar.Empty() {
		return out
	}
	at := 0
	for i, menu := range b.Menus {
		// Measured in columns, not runes: a CJK title takes two columns a
		// character, and a label sized by rune count would be drawn with
		// its end cut off.
		width := grid.StringWidth(menu.Title) + 2 // a space each side
		if at+width > bar.Cols {
			// No room for this one or any after it.
			break
		}
		out[i] = Rect{X: at, Cols: width, Rows: barRows}
		at += width
	}
	return out
}
