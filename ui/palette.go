package ui

import (
	"image/color"
	"strings"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// How large the box is allowed to get, and how much of the area it
// leaves around itself.
const (
	paletteMaxCols = 60
	paletteMaxRows = 14
	paletteMargin  = 2

	// paletteFrame is the rule around the outside, which costs a row and
	// a column at each edge.
	paletteFrame = 1
)

// promptRune marks the line being typed into.
const promptRune = '>'

// PaletteStyle colours a palette.
type PaletteStyle struct {
	FG, BG color.RGBA

	// MatchFG picks out the letters the query found.
	MatchFG color.RGBA

	// SelectedFG and SelectedBG mark the line Enter would run.
	SelectedFG, SelectedBG color.RGBA

	// ChordFG is the key binding shown at the end of a line.
	ChordFG color.RGBA

	// BorderFG is the rule around the outside, and ShadowBG darkens the
	// cells it falls on below and to the right. A zero alpha leaves
	// either one out.
	BorderFG color.RGBA
	ShadowBG color.RGBA

	// Rule picks the characters the rule is drawn with.
	Rule Border
}

// Palette is a dialog that finds a command by typing part of its name.
//
// It is the third way into the command registry, after the key bindings
// and the menus, and it needs no setup: every command registered appears
// in it.
type Palette struct {
	Style PaletteStyle

	cmds  *Commands
	keys  *Keymap
	close func()

	// q is the line being typed into. It is a Field like any other, so
	// the caret moves the same way here as in a form.
	q       *Field
	matches []Match

	// place is the line Enter would run and the first line drawn.
	place listState
	size  Size

	// buf keeps the dialog off the layer until it is finished, so an
	// unchanged dialog leaves the layer clean.
	buf buffer
}

// NewPalette returns a palette over a registry. close is called when the
// dialog is finished with, which is what takes it off the modal stack:
// the palette does not know what is showing it.
func NewPalette(cmds *Commands, keys *Keymap, close func()) *Palette {
	p := &Palette{cmds: cmds, keys: keys, close: close, q: NewField()}
	p.place = listState{count: p.matchCount, rows: p.rowsShown}
	p.q.OnChange = func(string) { p.refresh() }
	p.refresh()
	return p
}

// matchCount is how many lines the list has and rowsShown how many of
// them fit, which is the box less the rule and the query line. Every
// match can be chosen, so the bar skips nothing.
func (p *Palette) matchCount() int { return len(p.matches) }
func (p *Palette) rowsShown() int  { return max(p.lines().Rows-1, 0) }

// SetClipboard backs paste, copy and cut in the query line.
func (p *Palette) SetClipboard(read func() string, write func(string)) {
	p.q.ReadClipboard, p.q.WriteClipboard = read, write
}

// SetFocus passes focus on to the query line, so the caret appears and
// disappears with the dialog.
//
// Without it a menu opened over the palette leaves a caret blinking in a
// query line that no longer has the keys.
func (p *Palette) SetFocus(on bool) { p.q.SetFocus(on) }

// Reset empties the query, for showing the palette afresh rather than
// where it was left.
func (p *Palette) Reset() {
	p.q.SetText("")
	p.refresh()
}

// Query returns what has been typed.
func (p *Palette) Query() string { return p.q.Text() }

// Matches returns the commands the query found, best first.
func (p *Palette) Matches() []Match { return p.matches }

// Selected returns the command Enter would run, and whether there is
// one.
func (p *Palette) Selected() (Command, bool) {
	if p.place.at < 0 || p.place.at >= len(p.matches) {
		return Command{}, false
	}
	return p.matches[p.place.at].Command, true
}

// Box returns where the dialog sits in the view it draws through, so
// whatever is showing it can treat that part differently.
func (p *Palette) Box() Rect { return p.box() }

// Layout notes how much room the dialog has to place itself in.
func (p *Palette) Layout(size Size) {
	p.size = size
	// A shorter box shows fewer lines, so the selection may now be below
	// it. A taller one can show more, so the list should not be left
	// scrolled past its end.
	p.place.ensureVisible()
	// And the query line is told how much room it has, which is what
	// lets it scroll to keep the caret in view.
	if in := p.lines(); !in.Empty() {
		p.q.Layout(Size{Cols: p.queryCols(in.Cols), Rows: 1})
	}
}

// HandleKey drives the dialog. Keys it has no use for travel on, so the
// shortcuts that close or quit still work while it is open.
func (p *Palette) HandleKey(ev input.Event) (bool, error) {
	if p.box().Empty() {
		// Nowhere to draw it, so there is nothing on screen to read and
		// nothing to type into. It is still the top modal, so Enter here
		// would run whatever the list had settled on. Escape is the way
		// out.
		if ev.Kind == input.KeyPress && ev.Key == input.KeyEscape {
			p.dismiss()
		}
		return true, nil
	}
	// The list keys first: Up and Down move the selection here, where in
	// an ordinary field they would do nothing.
	if ev.Kind == input.KeyPress || ev.Kind == input.KeyRepeat {
		switch ev.Key {
		case input.KeyEscape:
			p.dismiss()
			return true, nil
		case input.KeyEnter:
			return true, p.run()
		case input.KeyUp:
			p.place.move(-1)
			return true, nil
		case input.KeyDown:
			p.place.move(1)
			return true, nil
		}
	}
	// Everything else is typing. refresh runs from the field's OnChange,
	// so the list follows whatever the edit did.
	return p.q.HandleKey(ev)
}

// HandleMouse runs the line that was clicked, and dismisses the dialog
// when the click landed outside it.
func (p *Palette) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress || ev.Button.IsWheel() {
		// Everything else is swallowed: the dialog is over the whole
		// area, and letting a drag through to what is behind it would
		// select text nobody can see.
		return true, nil
	}
	box := p.box()
	if box.Empty() || !box.Contains(ev.Col, ev.Row) {
		p.dismiss()
		return true, nil
	}
	// The first row inside the rule is the query; the rest are matches.
	// The box is only ever as tall as the list it shows, so the upper
	// bound is guarding against a future box that is not.
	row := ev.Row - p.lines().Y - 1
	if row >= 0 && row < p.rows() {
		p.place.moveTo(p.place.top + row)
		return true, p.run()
	}
	return true, nil
}

// CancelGesture is here because the dialog swallows drags: without it a
// press with no release would be remembered by nothing, which is fine,
// but saying so keeps the rule visible.
func (p *Palette) CancelGesture() {}

// Draw paints the box over whatever is behind it.
func (p *Palette) Draw(v grid.View) { p.buf.draw(v, p.paint) }

// paint draws the box into a view of its own.
func (p *Palette) paint(v grid.View) {
	box := p.box()
	if box.Empty() {
		return
	}
	drawShadow(v, box, p.Style.ShadowBG)
	full := box.In(v)
	full.Fill(grid.Cell{Rune: ' ', FG: p.Style.FG, BG: p.Style.BG, Width: 1})
	drawFrame(v, box, p.Style.BorderFG, p.Style.BG, p.Style.Rule)

	in := p.lines().In(v)
	cols, _ := in.Size()
	if cols <= 0 {
		return
	}
	p.drawQuery(in, cols)
	for row := 0; row < p.rows(); row++ {
		p.drawMatch(in, row, cols)
	}
}

// lines is the part of the box the query and the matches go in, which is
// the box less the rule around it.
func (p *Palette) lines() Rect {
	box := p.box()
	if box.Empty() {
		return box
	}
	return Rect{
		X: box.X + paletteFrame, Y: box.Y + paletteFrame,
		Cols: max(box.Cols-paletteFrame*2, 0), Rows: max(box.Rows-paletteFrame*2, 0),
	}
}

// drawQuery paints the line being typed into.
//
// A query longer than the line shows its end rather than its start, so
// the caret stays in view and the user is not typing blind.
func (p *Palette) drawQuery(in grid.View, cols int) {
	in.SetString(0, 0, string(promptRune)+" ", p.Style.ChordFG, p.Style.BG, 0)
	p.q.Style = FieldStyle{FG: p.Style.FG, BG: p.Style.BG, PlaceholderFG: p.Style.ChordFG}
	p.q.Draw(in.Sub(2, 0, p.queryCols(cols), 1))
}

// queryCols is how wide the query line is, and is what the field is laid
// out with. A field scrolls to keep the caret in view, and it can only
// do that once it has been told how much room it has.
func (p *Palette) queryCols(cols int) int { return max(cols-2, 1) }

// drawMatch paints one line of the list, counting from the first one
// shown rather than the first there is.
func (p *Palette) drawMatch(in grid.View, row, cols int) {
	i := p.place.top + row
	if i >= len(p.matches) {
		return
	}
	m := p.matches[i]
	fg, bg := p.Style.FG, p.Style.BG
	matchFG, chordFG := p.Style.MatchFG, p.Style.ChordFG
	if i == p.place.at {
		fg, bg = p.Style.SelectedFG, p.Style.SelectedBG
		// The selected line has a background of its own, and a colour
		// picked to stand out against the other lines can disappear
		// against it. Whatever the line writes its own text in is the one
		// colour known to show there. The matched letters keep their
		// weight, so they are still picked out.
		matchFG, chordFG = fg, fg
	}
	y := row + 1
	line := in.Sub(0, y, cols, 1)
	line.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})

	// The binding first, right-aligned, so the title can use whatever is
	// left without measuring around it.
	room := cols - 2
	if chord, ok := p.chordFor(m.Command.ID); ok {
		w := grid.StringWidth(chord)
		// Shown only if a column of title survives it. A line holding
		// nothing but a key binding does not say what the key does, so a
		// dialog too narrow for both drops the binding.
		if at := cols - 1 - w; at >= 6 {
			line.SetString(at, 0, chord, chordFG, bg, 0)
			room = at - 3
		}
	}
	p.drawTitle(line, m, fg, bg, matchFG, room)
}

// drawTitle writes a title with the letters the query found picked out.
//
// It walks grapheme clusters, not runes. A grid draws a base character
// and its combining marks in one cell, so writing runes one at a time
// would give the mark a cell of its own.
func (p *Palette) drawTitle(line grid.View, m Match, fg, bg, matchFG color.RGBA, room int) {
	hit := make(map[int]bool, len(m.At))
	for _, i := range m.At {
		hit[i] = true
	}
	at, index := 2, 0
	for _, cluster := range grid.Clusters(m.Command.Title) {
		width := grid.StringWidth(cluster)
		if at+width > room {
			break
		}
		// A cluster counts as found when any of its runes was matched:
		// the whole cell is one character to look at.
		found := false
		for i := index; i < index+len([]rune(cluster)); i++ {
			found = found || hit[i]
		}
		colour, attr := fg, grid.Attr(0)
		if found {
			colour, attr = matchFG, grid.AttrBold
		}
		at = line.SetString(at, 0, cluster, colour, bg, attr)
		index += len([]rune(cluster))
	}
}

// rows returns how many lines of the list are drawn.
func (p *Palette) rows() int {
	return min(len(p.matches)-p.place.top, max(p.lines().Rows-1, 0))
}

// box returns where the dialog goes, centred in the area it was given.
func (p *Palette) box() Rect {
	cols := min(p.size.Cols-paletteMargin*2, paletteMaxCols+paletteFrame*2)
	// One line for the query, one per match up to the cap, and the rule
	// at each end. A longer list scrolls rather than making a taller box.
	rows := min(min(p.size.Rows-paletteMargin*2, paletteMaxRows+paletteFrame*2),
		len(p.matches)+1+paletteFrame*2)
	// Room for the rule at each end and the query line between them, or
	// there is nothing to show and nothing to type into.
	if cols < 8+paletteFrame*2 || rows < 1+paletteFrame*2 {
		return Rect{}
	}
	return Rect{
		X:    (p.size.Cols - cols) / 2,
		Y:    (p.size.Rows - rows) / 2,
		Cols: cols,
		Rows: rows,
	}
}

// chordFor returns the key binding to show beside a command.
func (p *Palette) chordFor(id string) (string, bool) {
	if p.keys == nil {
		return "", false
	}
	chord, ok := p.keys.ChordFor(id)
	if !ok {
		return "", false
	}
	return chord.String(), true
}

// refresh rebuilds the list after the query changed.
func (p *Palette) refresh() {
	if p.cmds == nil {
		p.matches, p.place.at, p.place.top = nil, 0, 0
		return
	}
	p.matches = MatchCommands(p.cmds.All(), strings.TrimSpace(p.q.Text()))
	// A new list is a new answer: the best one is at the top, and the
	// line the old selection sat on means nothing now.
	p.place.at, p.place.top = 0, 0
}

// run invokes the selected command, closing the dialog first so that a
// command which opens another one is not fighting this for the stack.
func (p *Palette) run() error {
	cmd, ok := p.Selected()
	p.dismiss()
	if !ok {
		return nil
	}
	return p.cmds.Run(cmd.ID)
}

// dismiss closes the dialog, if it is not closed already.
func (p *Palette) dismiss() {
	if p.close != nil {
		p.close()
	}
}
