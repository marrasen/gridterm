package ui

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// How a menu is measured: a blank column each side of the text, a gap
// between a title and its key binding, and a floor so a menu of short
// titles is still a box rather than a sliver.
const (
	menuPad     = 1
	menuGap     = 3
	menuMinCols = 12
)

// separatorRune draws the rule between groups of items.
const separatorRune = '─'

// MenuItem is one line of a menu.
//
// An item names a command rather than carrying an action of its own, so
// a menu, a key binding and the palette are three ways into one list and
// cannot drift apart. An item with no command is a separator.
type MenuItem struct {
	// Command is the id to run.
	Command string

	// Title overrides the command's own title. Normally empty, so the
	// menu and the palette name the same thing the same way.
	Title string
}

// MenuSeparator returns an item that draws a rule instead of a line that
// can be chosen.
func MenuSeparator() MenuItem { return MenuItem{} }

// MenuStyle colours a menu.
type MenuStyle struct {
	FG, BG color.RGBA

	// SelectedFG and SelectedBG mark the line Enter would run.
	SelectedFG, SelectedBG color.RGBA

	// ChordFG is the key binding shown at the end of a line.
	ChordFG color.RGBA

	// DisabledFG is an item naming a command that is not registered.
	// Panes and tabs register commands as they open, so a menu written
	// once can hold lines that are not always available.
	DisabledFG color.RGBA
}

// Menu is a list of commands shown over everything else, anchored to
// whatever opened it.
//
// It is a modal: it takes the keys the tree would have seen, so the
// arrows move through the list rather than reaching a program running in
// a pane underneath.
type Menu struct {
	Style MenuStyle

	// Anchor is where the menu points, in the coordinates Layout is
	// given. The menu hangs under it, or above it when there is no room
	// below. It is a function rather than a rectangle because the thing
	// it points at moves when the window is resized, and the menu is laid
	// out again after the tree is.
	//
	// A nil Anchor puts the menu in the top left corner.
	Anchor func() Rect

	// OnEdge is called when the user presses Left or Right, with -1 or
	// +1. It is how a menu bar moves between its menus. A nil OnEdge
	// makes those keys do nothing.
	OnEdge func(step int)

	// OnOutside reports a press that landed outside the menu, in the
	// coordinates Layout is given, and returns whether it was dealt with.
	// A press it does not claim closes the menu.
	OnOutside func(col, row int) bool

	items []MenuItem
	cmds  *Commands
	keys  *Keymap
	close func()

	// at is the line Enter would run, and top the first line drawn.
	at   int
	top  int
	size Size

	// buf keeps the menu off the layer until it is finished, so an
	// unchanged menu leaves the layer clean.
	buf buffer
}

// NewMenu returns a menu over a registry. close is called when the menu
// is finished with, which is what takes it off the modal stack: the menu
// does not know what is showing it.
func NewMenu(cmds *Commands, keys *Keymap, items []MenuItem, close func()) *Menu {
	m := &Menu{cmds: cmds, keys: keys, close: close}
	m.items = make([]MenuItem, len(items))
	copy(m.items, items)
	m.at = m.nextFrom(-1, 1)
	return m
}

// Items returns the lines of the menu. The slice is a copy.
func (m *Menu) Items() []MenuItem {
	out := make([]MenuItem, len(m.items))
	copy(out, m.items)
	return out
}

// Selected returns the command Enter would run, and whether there is
// one.
func (m *Menu) Selected() (Command, bool) {
	if m.cmds == nil || m.at < 0 || m.at >= len(m.items) {
		return Command{}, false
	}
	return m.cmds.Lookup(m.items[m.at].Command)
}

// SelectedIndex returns which line is picked out, or -1 when no line can
// be chosen.
func (m *Menu) SelectedIndex() int { return m.at }

// Layout notes how much room the menu has to place itself in, and asks
// again where it is pointing.
func (m *Menu) Layout(size Size) {
	m.size = size
	m.scroll()
}

// Draw paints the menu over whatever is behind it.
func (m *Menu) Draw(v grid.View) { m.buf.draw(v, m.paint) }

// paint draws the box into a view of its own.
func (m *Menu) paint(v grid.View) {
	box := m.box()
	if box.Empty() {
		return
	}
	in := box.In(v)
	cols, rows := in.Size()
	in.Fill(grid.Cell{Rune: ' ', FG: m.Style.FG, BG: m.Style.BG, Width: 1})
	for row := 0; row < rows; row++ {
		i := m.top + row
		if i >= len(m.items) {
			break
		}
		m.paintItem(in.Sub(0, row, cols, 1), i, cols)
	}
}

// paintItem draws one line: a rule for a separator, otherwise a title
// with its key binding after it.
func (m *Menu) paintItem(line grid.View, i, cols int) {
	item := m.items[i]
	if m.isSeparator(i) {
		line.Fill(grid.Cell{Rune: separatorRune, FG: m.Style.ChordFG, BG: m.Style.BG, Width: 1})
		return
	}

	fg, bg := m.Style.FG, m.Style.BG
	if !m.enabled(i) {
		fg = m.Style.DisabledFG
	}
	if i == m.at {
		fg, bg = m.Style.SelectedFG, m.Style.SelectedBG
	}
	line.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})

	// The binding first, right-aligned, so the title can use whatever is
	// left without measuring around it.
	room := cols - menuPad
	if chord, ok := m.chordFor(item.Command); ok {
		w := grid.StringWidth(chord)
		if at := cols - menuPad - w; at > menuPad {
			chordFG := m.Style.ChordFG
			if i == m.at {
				chordFG = fg
			}
			line.SetString(at, 0, chord, chordFG, bg, 0)
			room = at - 1
		}
	}
	m.paintTitle(line, i, fg, bg, room)
}

// paintTitle writes a title, stopping where the key binding starts.
//
// It walks grapheme clusters, not runes. A grid draws a base character
// and its combining marks in one cell, so writing runes one at a time
// would give the mark a cell of its own.
func (m *Menu) paintTitle(line grid.View, i int, fg, bg color.RGBA, room int) {
	at := menuPad
	for _, cluster := range grid.Clusters(m.titleOf(i)) {
		if at+grid.StringWidth(cluster) > room {
			break
		}
		at = line.SetString(at, 0, cluster, fg, bg, 0)
	}
}

// HandleKey drives the menu. Keys it has no use for travel on, so the
// shortcuts that close or quit still work while it is open.
func (m *Menu) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		// Text events included: a menu is chosen from, not typed into.
		return false, nil
	}
	switch ev.Key {
	case input.KeyEscape:
		m.dismiss()
		return true, nil
	case input.KeyEnter, input.KeySpace:
		return true, m.run()
	case input.KeyUp:
		m.move(-1)
		return true, nil
	case input.KeyDown:
		m.move(1)
		return true, nil
	case input.KeyHome:
		m.jump(-1, 1)
		return true, nil
	case input.KeyEnd:
		m.jump(len(m.items), -1)
		return true, nil
	case input.KeyLeft:
		m.edge(-1)
		return true, nil
	case input.KeyRight:
		m.edge(1)
		return true, nil
	}
	return false, nil
}

// HandleMouse runs the line that was clicked and highlights the line the
// pointer is over. A press outside the menu closes it, unless whoever
// opened the menu claims it.
func (m *Menu) HandleMouse(ev input.MouseEvent) (bool, error) {
	box := m.box()
	inside := !box.Empty() && box.Contains(ev.Col, ev.Row)
	row := m.top + ev.Row - box.Y

	switch {
	case ev.Button.IsWheel():
		// A notch has no release to confuse, and a menu is short enough
		// that scrolling it is not worth the state.
		return true, nil
	case ev.Kind == input.MouseMove:
		if inside && m.selectable(row) {
			m.at = row
		}
		return true, nil
	case ev.Kind != input.MousePress:
		// A release is swallowed rather than acted on. The press that
		// opened the menu is released over it, and closing on that would
		// make the menu impossible to open with a click.
		return true, nil
	case !inside:
		if m.OnOutside != nil && m.OnOutside(ev.Col, ev.Row) {
			return true, nil
		}
		m.dismiss()
		return true, nil
	case m.selectable(row):
		m.at = row
		return true, m.run()
	}
	// A press on a separator, or on a line with no command behind it.
	return true, nil
}

// CancelGesture is here because the menu swallows drags: it keeps
// nothing between a press and its release, and saying so keeps the rule
// visible.
func (m *Menu) CancelGesture() {}

// box returns where the menu goes, hanging under its anchor.
//
// A menu that would fall off the bottom is flipped above the anchor
// instead, and one that fits neither way is pinned to the top and
// scrolls. Falling off the right is answered by sliding left, because a
// menu narrower than the window can always be moved onto it.
func (m *Menu) box() Rect {
	if m.size.Empty() || len(m.items) == 0 {
		return Rect{}
	}
	cols := min(m.width(), m.size.Cols)
	rows := min(len(m.items), m.size.Rows)
	if cols <= 0 || rows <= 0 {
		return Rect{}
	}

	anchor := m.anchor()
	y := anchor.Y + anchor.Rows
	switch {
	case y+rows <= m.size.Rows:
		// It fits below.
	case anchor.Y-rows >= 0:
		y = anchor.Y - rows
	default:
		y = min(max(m.size.Rows-rows, 0), max(anchor.Y, 0))
	}
	x := min(max(anchor.X, 0), m.size.Cols-cols)
	return Rect{X: x, Y: y, Cols: cols, Rows: rows}
}

// anchor returns what the menu points at, which is the top left corner
// when nothing said otherwise.
func (m *Menu) anchor() Rect {
	if m.Anchor == nil {
		return Rect{}
	}
	return m.Anchor()
}

// width returns how wide the menu wants to be: the longest title and the
// longest binding, with room to breathe around them.
func (m *Menu) width() int {
	titles, chords := 0, 0
	for i, item := range m.items {
		if m.isSeparator(i) {
			continue
		}
		titles = max(titles, grid.StringWidth(m.titleOf(i)))
		if chord, ok := m.chordFor(item.Command); ok {
			chords = max(chords, grid.StringWidth(chord))
		}
	}
	want := titles + menuPad*2
	if chords > 0 {
		want += menuGap + chords
	}
	return max(want, menuMinCols)
}

// titleOf names one line, preferring what the item says over what the
// command it names is called.
func (m *Menu) titleOf(i int) string {
	item := m.items[i]
	if item.Title != "" {
		return item.Title
	}
	if cmd, ok := m.lookup(item.Command); ok {
		return cmd.Title
	}
	return item.Command
}

// chordFor returns the key binding to show beside a command.
func (m *Menu) chordFor(id string) (string, bool) {
	if m.keys == nil || id == "" {
		return "", false
	}
	chord, ok := m.keys.ChordFor(id)
	if !ok {
		return "", false
	}
	return chord.String(), true
}

// lookup finds the command an item names.
func (m *Menu) lookup(id string) (Command, bool) {
	if m.cmds == nil || id == "" {
		return Command{}, false
	}
	return m.cmds.Lookup(id)
}

// isSeparator reports whether a line is a rule rather than a command.
func (m *Menu) isSeparator(i int) bool {
	return i >= 0 && i < len(m.items) && m.items[i].Command == ""
}

// enabled reports whether a line names a command that is registered.
func (m *Menu) enabled(i int) bool {
	if i < 0 || i >= len(m.items) {
		return false
	}
	_, ok := m.lookup(m.items[i].Command)
	return ok
}

// selectable reports whether a line can be picked out: a separator
// cannot, and neither can a command that is not registered.
func (m *Menu) selectable(i int) bool { return !m.isSeparator(i) && m.enabled(i) }

// move steps through the list, skipping what cannot be chosen and
// stopping at the ends rather than wrapping.
func (m *Menu) move(by int) {
	if next := m.nextFrom(m.at, by); next >= 0 {
		m.at = next
	}
	m.scroll()
}

// jump goes to the first selectable line from one end.
func (m *Menu) jump(from, by int) {
	if next := m.nextFrom(from, by); next >= 0 {
		m.at = next
	}
	m.scroll()
}

// nextFrom returns the first selectable line past from in the given
// direction, or -1 when there is none.
func (m *Menu) nextFrom(from, by int) int {
	if by == 0 {
		return -1
	}
	for i := from + by; i >= 0 && i < len(m.items); i += by {
		if m.selectable(i) {
			return i
		}
	}
	return -1
}

// edge moves to the menu beside this one, when there is a bar to move
// along.
func (m *Menu) edge(step int) {
	if m.OnEdge != nil {
		m.OnEdge(step)
	}
}

// scroll brings the selected line into the box, so Enter always runs
// something the user can see.
func (m *Menu) scroll() {
	rows := m.box().Rows
	if rows <= 0 {
		m.top = 0
		return
	}
	m.top = min(max(m.top, m.at-rows+1), max(m.at, 0))
	m.top = min(max(m.top, 0), max(len(m.items)-rows, 0))
}

// run invokes the selected command, closing the menu first so that a
// command which opens another one is not fighting this for the stack.
func (m *Menu) run() error {
	cmd, ok := m.Selected()
	m.dismiss()
	if !ok {
		return nil
	}
	return m.cmds.Run(cmd.ID)
}

// dismiss closes the menu, if it is not closed already.
func (m *Menu) dismiss() {
	if m.close != nil {
		m.close()
	}
}
