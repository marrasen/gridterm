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

	// FG and BG colour the pinned row, and PressedFG marks it while the
	// pointer is on it.
	FG, BG, OverFG color.RGBA

	size ui.Size
	over bool
	buf  paddedRow
}

// newSidebar puts a pinned row under a list.
func newSidebar(list *ui.List, label string, press func() error) *sidebar {
	return &sidebar{list: list, Label: label, Press: press}
}

// rows is how many rows the pinned part takes.
func (s *sidebar) rows() int {
	if s.Label == "" || s.size.Rows < 3 {
		// Never at the cost of the list itself: a sidebar showing one
		// row and a button is a sidebar showing nothing.
		return 0
	}
	return 1
}

// Layout gives the list everything but the pinned row.
func (s *sidebar) Layout(size ui.Size) {
	s.size = size
	s.list.Layout(ui.Size{Cols: size.Cols, Rows: max(size.Rows-s.rows(), 0)})
}

// Draw paints the list and the row under it.
func (s *sidebar) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	pinned := s.rows()
	if rows > pinned {
		s.list.Draw(v.Sub(0, 0, cols, rows-pinned))
	}
	if pinned == 0 {
		return
	}
	fg := s.FG
	if s.over {
		fg = s.OverFG
	}
	s.buf.draw(v.Sub(0, rows-1, cols, 1), s.Label, fg, s.BG)
}

// SetFocus passes the keys on to the list, which is what they are for.
func (s *sidebar) SetFocus(on bool) { s.list.SetFocus(on) }

// Focused reports whether the sidebar has the keys.
func (s *sidebar) Focused() bool { return s.list.Focused() }

// HandleKey gives the keys to the list.
func (s *sidebar) HandleKey(ev input.Event) (bool, error) { return s.list.HandleKey(ev) }

// HandleMouse presses the pinned row, or passes the event to the list.
func (s *sidebar) HandleMouse(ev input.MouseEvent) (bool, error) {
	pinned := s.rows()
	if pinned > 0 && ev.Row == s.size.Rows-1 {
		s.over = ev.Kind != input.MouseRelease
		if ev.Kind != input.MousePress || ev.Button != input.MouseLeft {
			return true, nil
		}
		s.over = false
		if s.Press == nil {
			return true, nil
		}
		return true, s.Press()
	}
	s.over = false
	return s.list.HandleMouse(ev)
}

// CancelGesture lets go of a press whose release will never arrive.
func (s *sidebar) CancelGesture() { s.over = false }

// paddedRow draws one row and keeps what it drew, so a row that has not
// changed is not written again.
//
// The toolkit has one of these for its own widgets, unexported. This is
// the same idea for the one row this draws.
type paddedRow struct {
	was    string
	fg, bg color.RGBA
	cols   int
	drawn  bool
}

// draw writes the text, padded to the width, when anything about it has
// changed.
func (p *paddedRow) draw(v grid.View, text string, fg, bg color.RGBA) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	if p.drawn && p.was == text && p.fg == fg && p.bg == bg && p.cols == cols {
		return
	}
	p.was, p.fg, p.bg, p.cols, p.drawn = text, fg, bg, cols, true

	at := v.SetString(1, 0, trimTo(text, max(cols-2, 0)), fg, bg, 0)
	for x := 0; x < cols; x++ {
		if x >= 1 && x < at {
			continue
		}
		v.Set(x, 0, grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
	}
}

// trimTo cuts a string to a width in cells.
func trimTo(s string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if grid.StringWidth(s) <= cols {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && grid.StringWidth(string(runes)+"…") > cols {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
