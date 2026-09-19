package files

import (
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// Key is one key on the bar along the bottom of the browser: the chord
// it wants, what the bar calls it, and what it does.
type Key struct {
	Chord ui.Chord

	// Typed is the character this key is, for one that is a character
	// rather than a place on the keyboard: "/" is "/" wherever a layout
	// puts it, and binding the key beside the right shift would find it
	// on one layout and not on another.
	Typed rune

	// Shown is how the bar spells the chord, which is its own spelling
	// rather than Chord.String(): "^G" fits a bar of ten keys where
	// "ctrl+G" does not.
	Shown string
	Title string
}

// press is the key press this chord is, for asking whether a key on the
// bar is this one and for running it from a click.
func (k Key) press() input.Event {
	if k.Typed != 0 {
		return input.Event{Kind: input.Text, Rune: k.Typed, NormalText: true}
	}
	return input.Event{Kind: input.KeyPress, Key: k.Chord.Key, Mods: k.Chord.Mods}
}

// chord names a key and the modifiers held with it.
func chord(k input.Key, mods input.Mods) ui.Chord { return ui.Chord{Key: k, Mods: mods} }

// BrowserKeys is what the bar offers.
//
// The order and the names are Midnight Commander's, because that is
// what a two-pane browser means to anyone who has used one: a user who
// knows those keys should not have to learn these.
func BrowserKeys() []Key {
	return []Key{
		{Chord: chord(input.KeyTab, 0), Shown: "Tab", Title: "Next"},
		{Chord: chord(input.KeyG, input.ModCtrl), Shown: "^G", Title: "Go to"},
		{Chord: chord(input.KeyF2, 0), Shown: "F2", Title: "Rename"},
		// F3 and F4 are what a two-pane browser has meant since Norton
		// Commander: read this file, and follow it as it grows.
		{Chord: chord(input.KeyF3, 0), Shown: "F3", Title: "View"},
		{Chord: chord(input.KeyF4, 0), Shown: "F4", Title: "Tail"},
		// Copy, cut and paste are the chords they are everywhere else. A
		// file pane is not a terminal, so nothing else wants them here.
		{Chord: chord(input.KeyC, input.ModCtrl), Shown: "^C", Title: "Copy"},
		{Chord: chord(input.KeyX, input.ModCtrl), Shown: "^X", Title: "Cut"},
		{Chord: chord(input.KeyV, input.ModCtrl), Shown: "^V", Title: "Paste"},
		{Chord: chord(input.KeyF8, 0), Shown: "F8", Title: "Delete"},
		{Chord: chord(input.KeyF9, 0), Shown: "F9", Title: "Mkdir"},
		// Not F10: the window opens its menu bar on that, and an
		// accelerator wins before any widget sees the key. ^D is what
		// closes a shell, which is near enough the same thing.
		{Chord: chord(input.KeyD, input.ModCtrl), Shown: "^D", Title: "Close"},
	}
}

// keyCell returns the columns one key on the bar is drawn in.
//
// The width is divided by counting from the left edge each time rather
// than by stepping, so the remainder is spread down the bar and the
// last cell ends exactly at the right edge.
func keyCell(i, cols, n int) (start, end int) {
	if n <= 0 {
		return 0, 0
	}
	return cols * i / n, cols * (i + 1) / n
}

// keyAt returns which key on the bar a column belongs to.
//
// It asks keyCell rather than dividing the other way, because the two
// have to agree exactly: a click has to run the key it looks like it is
// on, and a bar is short enough that walking it costs nothing.
func keyAt(col, cols, n int) (int, bool) {
	if n <= 0 || cols <= 0 || col < 0 || col >= cols {
		return 0, false
	}
	for i := range n {
		if start, end := keyCell(i, cols, n); col >= start && col < end {
			return i, true
		}
	}
	return 0, false
}

// drawKeys paints the bar.
//
// Every cell is written once, with the same value each frame, so a
// browser nobody is touching leaves the row clean.
func drawKeys(v grid.View, y, cols int, keys []Key, st Style, wired func(Key) bool) {
	if cols <= 0 || len(keys) == 0 {
		return
	}
	for i, k := range keys {
		start, end := keyCell(i, cols, len(keys))
		// The key itself, on the bar's own ground, the way a number is
		// on the bar this was copied from.
		at := start
		for _, r := range k.Shown {
			if at >= end {
				break
			}
			v.Set(at, y, grid.Cell{Rune: r, FG: st.KeyFG, BG: st.BG, Width: 1})
			at++
		}
		// Then what it does, marked out, so the bar reads as a row of
		// keys rather than a sentence. A blank column in front of the
		// word, inside the marked-out part, so the key and its name do
		// not run into one another.
		fg, bg := st.SelectedFG, st.SelectedBG
		if wired != nil && !wired(k) {
			// Nothing is wired to it here, so it is shown without being
			// offered: dimmer than a key that works, and still lit
			// enough to read.
			fg, bg = st.KeyFG, st.OffBG
		}
		title := grid.Trim(" "+k.Title, end-at)
		at = v.SetString(at, y, title, fg, bg, 0)
		for ; at < end; at++ {
			v.Set(at, y, grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
		}
	}
}
