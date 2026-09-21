package main

import "github.com/marrasen/gridterm/grid"

// drawHint writes the whole of what a menu line does along the bottom
// row of the window, while a menu is open.
//
// A row borrowed rather than a bar kept. The menus say the short half
// of a title -- "Right" under a "Split" header -- and the sentence
// has to be somewhere; a permanent bar would cost a row of every pane
// for the moments a menu is up.
//
// Written into the window's grid after the tree has drawn, so it goes
// over whatever was on that row. The menus themselves are drawn on
// layers above it: one long enough to reach the bottom covers the
// hint, which is right, because then the line is on screen anyway.
func (a *app) drawHint() {
	if a.g == nil {
		return
	}
	// The menu's own line first: a menu is open in front of the user and
	// is what they are reading, and a line saying something worked a
	// moment ago is not worth covering it with.
	hint := ""
	if a.bar != nil {
		hint = a.bar.Hint()
	}
	if hint == "" {
		hint = a.saying()
	}
	if hint == "" {
		return
	}
	cols, rows := a.g.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	fg, bg := a.frameFG(), a.barFoot()
	row := a.g.View().Sub(0, rows-1, cols, 1)
	row.Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
	row.SetString(1, 0, grid.TrimTail(hint, max(cols-2, 0)), fg, bg, 0)
}
