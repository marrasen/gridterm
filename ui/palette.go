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

	query   string
	matches []Match

	// at is the line Enter would run, and top the first line drawn. A
	// list longer than the box needs both: clamping only the selection
	// lets it walk off the bottom of what is on screen.
	at   int
	top  int
	size Size

	// buf keeps the dialog off the layer until it is finished, so an
	// unchanged dialog leaves the layer clean.
	buf buffer
}

// NewPalette returns a palette over a registry. close is called when the
// dialog is finished with, which is what takes it off the modal stack:
// the palette does not know what is showing it.
func NewPalette(cmds *Commands, keys *Keymap, close func()) *Palette {
	p := &Palette{cmds: cmds, keys: keys, close: close}
	p.refresh()
	return p
}

// Reset empties the query, for showing the palette afresh rather than
// where it was left.
func (p *Palette) Reset() {
	p.query = ""
	p.refresh()
}

// Query returns what has been typed.
func (p *Palette) Query() string { return p.query }

// Matches returns the commands the query found, best first.
func (p *Palette) Matches() []Match { return p.matches }

// Selected returns the command Enter would run, and whether there is
// one.
func (p *Palette) Selected() (Command, bool) {
	if p.at < 0 || p.at >= len(p.matches) {
		return Command{}, false
	}
	return p.matches[p.at].Command, true
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
	p.scroll()
}

// HandleKey drives the dialog. Keys it has no use for travel on, so the
// shortcuts that close or quit still work while it is open.
func (p *Palette) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind == input.Text {
		// Delete is not a character to type, whatever the platform says.
		if !ev.NormalText || ev.Rune < ' ' || ev.Rune == 0x7f {
			return false, nil
		}
		p.query += string(ev.Rune)
		p.refresh()
		return true, nil
	}
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return false, nil
	}

	switch ev.Key {
	case input.KeyEscape:
		p.dismiss()
		return true, nil
	case input.KeyEnter:
		return true, p.run()
	case input.KeyUp:
		p.move(-1)
		return true, nil
	case input.KeyDown:
		p.move(1)
		return true, nil
	case input.KeyBackspace:
		if p.query != "" {
			runes := []rune(p.query)
			p.query = string(runes[:len(runes)-1])
			p.refresh()
		}
		return true, nil
	}
	return false, nil
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
	// The first row inside the box is the query; the rest are matches.
	// The box is only ever as tall as the list it shows, so the upper
	// bound is guarding against a future box that is not.
	row := ev.Row - box.Y - 1
	if row >= 0 && row < p.rows() {
		p.at = p.top + row
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
	in := box.In(v)
	in.Fill(grid.Cell{Rune: ' ', FG: p.Style.FG, BG: p.Style.BG, Width: 1})

	cols, _ := in.Size()
	p.drawQuery(in, cols)
	for row := 0; row < p.rows(); row++ {
		p.drawMatch(in, row, cols)
	}
}

// drawQuery paints the line being typed into.
//
// A query longer than the line shows its end rather than its start, so
// the caret stays in view and the user is not typing blind.
func (p *Palette) drawQuery(in grid.View, cols int) {
	in.SetString(0, 0, string(promptRune)+" ", p.Style.ChordFG, p.Style.BG, 0)

	room := max(cols-3, 1)
	shown := p.query
	for grid.StringWidth(shown) > room {
		shown = string([]rune(shown)[1:])
	}
	in.SetString(2, 0, shown, p.Style.FG, p.Style.BG, grid.AttrBold)
	// The caret sits after what has been typed, so the dialog looks like
	// somewhere to type rather than somewhere to read.
	if at := 2 + grid.StringWidth(shown); at < cols {
		in.SetCursor(grid.Cursor{X: at, Y: 0, Visible: true, Style: grid.CursorBar})
	}
}

// drawMatch paints one line of the list, counting from the first one
// shown rather than the first there is.
func (p *Palette) drawMatch(in grid.View, row, cols int) {
	i := p.top + row
	if i >= len(p.matches) {
		return
	}
	m := p.matches[i]
	fg, bg := p.Style.FG, p.Style.BG
	if i == p.at {
		fg, bg = p.Style.SelectedFG, p.Style.SelectedBG
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
			line.SetString(at, 0, chord, p.Style.ChordFG, bg, 0)
			room = at - 3
		}
	}
	p.drawTitle(line, m, fg, bg, room)
}

// drawTitle writes a title with the letters the query found picked out.
//
// It walks grapheme clusters, not runes. A grid draws a base character
// and its combining marks in one cell, so writing runes one at a time
// would give the mark a cell of its own.
func (p *Palette) drawTitle(line grid.View, m Match, fg, bg color.RGBA, room int) {
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
			colour, attr = p.Style.MatchFG, grid.AttrBold
		}
		at = line.SetString(at, 0, cluster, colour, bg, attr)
		index += len([]rune(cluster))
	}
}

// rows returns how many lines of the list are drawn.
func (p *Palette) rows() int {
	return min(len(p.matches)-p.top, max(p.box().Rows-1, 0))
}

// box returns where the dialog goes, centred in the area it was given.
func (p *Palette) box() Rect {
	cols := min(p.size.Cols-paletteMargin*2, paletteMaxCols)
	// One line for the query, and one per match up to the cap. A longer
	// list scrolls rather than making a taller box.
	rows := min(min(p.size.Rows-paletteMargin*2, paletteMaxRows), len(p.matches)+1)
	if cols < 8 || rows < 1 {
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

// move steps through the list, stopping at the ends rather than
// wrapping: a list you can run off the end of is hard to aim at.
func (p *Palette) move(by int) {
	if len(p.matches) == 0 {
		return
	}
	p.at = min(max(p.at+by, 0), len(p.matches)-1)
	p.scroll()
}

// scroll brings the selected line into the box, so Enter always runs
// something the user can see.
func (p *Palette) scroll() {
	rows := max(p.box().Rows-1, 0)
	if rows <= 0 {
		p.top = 0
		return
	}
	p.top = min(max(p.top, p.at-rows+1), p.at)
	p.top = min(max(p.top, 0), max(len(p.matches)-rows, 0))
}

// refresh rebuilds the list after the query changed.
func (p *Palette) refresh() {
	if p.cmds == nil {
		p.matches, p.at = nil, 0
		return
	}
	p.matches = MatchCommands(p.cmds.All(), strings.TrimSpace(p.query))
	// A new list is a new answer: the best one is at the top, and the
	// line the old selection sat on means nothing now.
	p.at, p.top = 0, 0
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
