package main

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// sidebar is the list of everything the window has open, with one thing
// pinned under it.
//
// The pinned row does not scroll. It is how a window with nothing in it
// yet gets its first connection, so it has to be there whatever the list
// above it is showing.
type sidebar struct {
	list *ui.List

	// Label is the pinned row, and Press is what it does.
	Label string
	Press func() error

	// FG and BG colour the pinned row.
	FG, BG color.RGBA

	size ui.Size

	// at is the row the pinned line was last drawn on, or -1 when there
	// was none. A click is measured against this rather than against the
	// size Layout promised, because Draw may be given a shorter view and
	// a row has to be clicked where it is drawn.
	at int
}

// newSidebar puts a pinned row under a list.
func newSidebar(list *ui.List, label string, press func() error) *sidebar {
	return &sidebar{list: list, Label: label, Press: press, at: -1}
}

// pinnedRows is how many rows the pinned part takes in a box this tall.
func (s *sidebar) pinnedRows(rows int) int {
	if s.Label == "" || rows < 3 {
		// Never at the cost of the list itself: a sidebar showing one
		// row and a button is a sidebar showing nothing.
		return 0
	}
	return 1
}

// Layout gives the list everything but the pinned row.
func (s *sidebar) Layout(size ui.Size) {
	s.size = size
	s.list.Layout(ui.Size{Cols: size.Cols, Rows: max(size.Rows-s.pinnedRows(size.Rows), 0)})
}

// Draw paints the list and the row under it.
//
// Every cell of the pinned row is written every frame. A row that
// remembered what it drew and skipped an unchanged frame would be left
// holding whatever else had written there since: the panes paint over
// this whole strip while the sidebar is hidden, and a taller window puts
// the row somewhere nothing has written at all. Writing the same value
// costs nothing, because grid.Set leaves a cell that did not change
// alone.
func (s *sidebar) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		s.at = -1
		return
	}
	pinned := s.pinnedRows(rows)
	if rows > pinned {
		s.list.Draw(v.Sub(0, 0, cols, rows-pinned))
	}
	if pinned == 0 {
		s.at = -1
		return
	}
	s.at = rows - 1
	drawRow(v.Sub(0, s.at, cols, 1), s.Label, s.FG, s.BG)
}

// RowPads hands the question on to the list, which is what has the
// headings. The pinned row under it wants nothing, so the answer stops
// one short of the box.
func (s *sidebar) RowPads(rows int) []grid.Pad {
	return s.list.RowPads(max(rows-s.pinnedRows(rows), 0))
}

// SetFocus passes the keys on to the list, which is what they are for.
func (s *sidebar) SetFocus(on bool) { s.list.SetFocus(on) }

// HandleKey gives the keys to the list.
func (s *sidebar) HandleKey(ev input.Event) (bool, error) { return s.list.HandleKey(ev) }

// HandleMouse presses the pinned row, or passes the event to the list.
func (s *sidebar) HandleMouse(ev input.MouseEvent) (bool, error) {
	if s.at < 0 || ev.Row != s.at || ev.Button.IsWheel() {
		// The wheel goes to the list wherever the pointer is: the bottom
		// row is where somebody scrolling to the end of a long list will
		// have put it.
		return s.list.HandleMouse(ev)
	}
	if ev.Kind != input.MousePress || ev.Button != input.MouseLeft {
		// A release or a drag over the row is swallowed rather than
		// acted on, the way a press on a button is.
		return true, nil
	}
	if s.Press == nil {
		return true, nil
	}
	return true, s.Press()
}

// drawRow writes one line of text, padded to the width.
//
// Every cell is written, and each one exactly once: a cell written twice
// in a frame is a cell that changed, and a window that redraws a sidebar
// nobody is touching is what the whole display is built to avoid.
func drawRow(v grid.View, text string, fg, bg color.RGBA) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	blank := grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1}
	v.Set(0, 0, blank)
	at := v.SetString(1, 0, grid.TrimTail(text, max(cols-2, 0)), fg, bg, 0)
	for x := max(at, 1); x < cols; x++ {
		v.Set(x, 0, blank)
	}
}
