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

	// menuFrame is the rule around the outside, which costs a row and a
	// column at each edge.
	menuFrame = 1
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

	// BorderFG is the rule around the outside, and ShadowBG darkens the
	// cells it falls on below and to the right. A zero alpha leaves
	// either one out.
	BorderFG color.RGBA
	ShadowBG color.RGBA

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
	// coordinates Layout is given, and returns whether it was dealt with
	// and whatever dealing with it failed with. A press it does not claim
	// closes the menu, and whatever that press failed with is still
	// reported.
	OnOutside func(col, row int) (bool, error)

	items []MenuItem
	cmds  *Commands
	keys  *Keymap
	close func()

	// place is the line Enter would run and the first line drawn.
	place listState
	size  Size

	// buf keeps the menu off the layer until it is finished, so an
	// unchanged menu leaves the layer clean.
	buf buffer
}

// NewMenu returns a menu over a registry. close is called when the menu
// is finished with, which is what takes it off the modal stack: the menu
// does not know what is showing it.
func NewMenu(cmds *Commands, keys *Keymap, items []MenuItem, close func()) *Menu {
	m := &Menu{cmds: cmds, keys: keys, close: close}
	m.items = usableItems(cmds, items)
	m.place = listState{count: m.itemCount, rows: m.lines, selectable: m.selectable}
	m.place.at = m.place.first()
	return m
}

// itemCount is how many lines the menu has. lines, which is how many of
// them fit, is beside the box it measures.
func (m *Menu) itemCount() int { return len(m.items) }

// usableItems drops the lines a menu cannot name and tidies the
// separators that leaves stranded.
//
// A line with no title of its own naming a command that is not
// registered has nothing to show but the id, which means nothing to the
// person reading it. A line the program gave a title keeps it and is
// drawn greyed out instead.
//
// The list is settled when the menu opens. A menu is on screen for a
// moment, so a command registered while one is up belongs in the next
// menu rather than appearing halfway down this one.
func usableItems(cmds *Commands, in []MenuItem) []MenuItem {
	out := make([]MenuItem, 0, len(in))
	for _, item := range in {
		switch {
		case item.Command == "":
			// A rule that separates nothing is not a rule.
			if len(out) > 0 && out[len(out)-1].Command != "" {
				out = append(out, item)
			}
		case item.Title != "":
			out = append(out, item)
		default:
			if cmds == nil {
				continue
			}
			if _, ok := cmds.Lookup(item.Command); ok {
				out = append(out, item)
			}
		}
	}
	if n := len(out); n > 0 && out[n-1].Command == "" {
		out = out[:n-1]
	}
	return out
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
	if m.cmds == nil || m.place.at < 0 || m.place.at >= len(m.items) {
		return Command{}, false
	}
	return m.cmds.Lookup(m.items[m.place.at].Command)
}

// SelectedIndex returns which line is picked out, or -1 when no line can
// be chosen.
func (m *Menu) SelectedIndex() int { return m.place.at }

// Box returns where the menu sits in the view it draws through, so
// whatever is showing it can treat that part differently.
func (m *Menu) Box() Rect { return m.box() }

// Layout notes how much room the menu has to place itself in, and asks
// again where it is pointing.
func (m *Menu) Layout(size Size) {
	m.size = size
	m.place.ensureVisible()
}

// Draw paints the menu over whatever is behind it.
func (m *Menu) Draw(v grid.View) { m.buf.draw(v, m.paint) }

// paint draws the box into a view of its own.
func (m *Menu) paint(v grid.View) {
	box := m.box()
	if box.Empty() {
		return
	}
	drawShadow(v, box, m.Style.ShadowBG)
	in := box.In(v)
	cols, rows := in.Size()
	in.Fill(grid.Cell{Rune: ' ', FG: m.Style.FG, BG: m.Style.BG, Width: 1})
	drawFrame(v, box, m.Style.BorderFG, m.Style.BG)

	lines := in.Sub(menuFrame, menuFrame,
		max(cols-menuFrame*2, 0), max(rows-menuFrame*2, 0))
	inner, shown := lines.Size()
	for row := 0; row < shown; row++ {
		i := m.place.top + row
		if i >= len(m.items) {
			break
		}
		m.paintItem(lines.Sub(0, row, inner, 1), i, inner)
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
	if i == m.place.at {
		fg, bg = m.Style.SelectedFG, m.Style.SelectedBG
	}
	line.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})

	// The binding first, right-aligned, so the title can use whatever is
	// left without measuring around it.
	room := cols - menuPad
	if chord, ok := m.chordFor(item.Command); ok {
		w := grid.StringWidth(chord)
		// Shown only if a column of title survives it. A line holding
		// nothing but a key binding does not say what the key does, so a
		// menu too narrow for both drops the binding.
		if at := cols - menuPad - w; at >= menuPad+2 {
			chordFG := m.Style.ChordFG
			if i == m.place.at {
				chordFG = fg
			}
			line.SetString(at, 0, chord, chordFG, bg, 0)
			room = at - 1
		}
	}
	m.paintTitle(line, i, fg, bg, room)
}

// paintTitle writes a title, stopping where the key binding starts.
func (m *Menu) paintTitle(line grid.View, i int, fg, bg color.RGBA, room int) {
	line.SetString(menuPad, 0, grid.Trim(m.titleOf(i), max(room-menuPad, 0)), fg, bg, 0)
}

// HandleKey drives the menu. Keys it has no use for travel on, so the
// shortcuts that close or quit still work while it is open.
func (m *Menu) HandleKey(ev input.Event) (bool, error) {
	if m.box().Empty() {
		// Nowhere to draw it, so there is nothing on screen to choose
		// from. It is still the top modal, so Enter here would run a
		// line nobody has read. Escape is the way out.
		if ev.Kind == input.KeyPress && ev.Key == input.KeyEscape {
			m.dismiss()
		}
		return true, nil
	}
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		// Text events included: a menu is chosen from, not typed into.
		return false, nil
	}
	if ev.Mods != 0 {
		// Ctrl+Enter is not Enter. Swallowing a chord the menu has no
		// meaning for would kill it for whatever it is bound to.
		return false, nil
	}
	switch ev.Key {
	case input.KeyEscape:
		m.dismiss()
		return true, nil
	case input.KeyEnter, input.KeySpace:
		return true, m.run()
	case input.KeyUp:
		m.place.move(-1)
		return true, nil
	case input.KeyDown:
		m.place.move(1)
		return true, nil
	case input.KeyHome:
		m.place.moveTo(m.place.first())
		return true, nil
	case input.KeyEnd:
		m.place.moveTo(m.place.last())
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
	// The rule is inside the box and is not a line. Counting from it
	// would name the line above the first or below the last -- one the
	// user cannot see and did not click.
	row := -1
	if at := ev.Row - box.Y - menuFrame; inside && at >= 0 && at < m.lines() {
		row = m.place.top + at
	}

	switch {
	case ev.Button.IsWheel():
		// A notch has no release to confuse, and a menu is short enough
		// that scrolling it is not worth the state.
		return true, nil
	case ev.Kind == input.MouseMove:
		if inside && m.selectable(row) {
			m.place.at = row
		}
		return true, nil
	case ev.Kind != input.MousePress:
		// A release is swallowed rather than acted on. The press that
		// opened the menu is released over it, and closing on that would
		// make the menu impossible to open with a click.
		return true, nil
	case !inside:
		var err error
		if m.OnOutside != nil {
			var took bool
			if took, err = m.OnOutside(ev.Col, ev.Row); took {
				return true, err
			}
		}
		// Dismissed, and the failure still handed back: a press the
		// program looked at and gave up on failed at something, and one
		// that closed the menu silently threw that away.
		m.dismiss()
		return true, err
	case m.selectable(row):
		m.place.at = row
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
// A menu never covers what opened it. It takes the room under the
// anchor, or the room over it when that is larger, and scrolls when
// neither side fits the whole list. A menu drawn over its own title
// would turn a click meant to switch menus into a line of this one.
//
// Falling off the right is answered by sliding left, because a menu
// narrower than the window can always be moved onto it.
func (m *Menu) box() Rect {
	if m.size.Empty() || len(m.items) == 0 {
		return Rect{}
	}
	cols := min(m.width(), m.size.Cols)

	// Clamped to the window, because Anchor belongs to whoever opened the
	// menu and may name a rectangle that is partly off it.
	anchor := m.anchor()
	under := min(max(anchor.Y+anchor.Rows, 0), m.size.Rows)
	over := min(max(anchor.Y, 0), m.size.Rows)

	// The rule takes a row at each end, so the box is taller than the
	// lines in it.
	want := len(m.items) + menuFrame*2
	rows, y := min(want, m.size.Rows-under), under
	if above := min(want, over); above > rows {
		rows, y = above, over-above
	}
	// Room for the rule at each end and a line between them, or there is
	// no menu to show. A box holding nothing but its own rule is a modal
	// nobody can read and Enter would still run something out of it.
	if cols <= menuFrame*2 || rows <= menuFrame*2 {
		return Rect{}
	}
	x := min(max(anchor.X, 0), max(m.size.Cols-cols, 0))
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
	return max(want, menuMinCols) + menuFrame*2
}

// titleOf names one line, preferring what the item says over what the
// command it names is called.
//
// A line with neither is empty rather than showing a command id. Only a
// command unregistered since the menu opened can reach that, because
// NewMenu drops a line it cannot name.
func (m *Menu) titleOf(i int) string {
	item := m.items[i]
	if item.Title != "" {
		return item.Title
	}
	cmd, _ := m.lookup(item.Command)
	return cmd.Title
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

// edge moves to the menu beside this one, when there is a bar to move
// along.
func (m *Menu) edge(step int) {
	if m.OnEdge != nil {
		m.OnEdge(step)
	}
}

// lines is how many items the box has room for, which is its height
// less the rule at each end.
func (m *Menu) lines() int { return max(m.box().Rows-menuFrame*2, 0) }

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
