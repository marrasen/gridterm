package files

import (
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// fkey is one key on the bar along the bottom of the browser: the key
// itself, the modifiers it wants, what the bar calls it, and what it
// does.
type fkey struct {
	Key   input.Key
	Mods  input.Mods
	Chord string
	Title string
}

// matches reports whether an event is this key.
func (k fkey) matches(ev input.Event) bool {
	return ev.Key == k.Key && ev.Mods == k.Mods
}

// browserKeys is what the bar offers.
//
// The order and the names are Midnight Commander's, because that is
// what a two-pane browser means to anyone who has used one: a user who
// knows those keys should not have to learn these.
func browserKeys() []fkey {
	return []fkey{
		{Key: input.KeyTab, Chord: "Tab", Title: "Next"},
		{Key: input.KeyG, Mods: input.ModCtrl, Chord: "^G", Title: "Go to"},
		{Key: input.KeyF2, Chord: "F2", Title: "Rename"},
		// Copy, cut and paste are the chords they are everywhere else. A
		// file pane is not a terminal, so nothing else wants them here.
		{Key: input.KeyC, Mods: input.ModCtrl, Chord: "^C", Title: "Copy"},
		{Key: input.KeyX, Mods: input.ModCtrl, Chord: "^X", Title: "Cut"},
		{Key: input.KeyV, Mods: input.ModCtrl, Chord: "^V", Title: "Paste"},
		{Key: input.KeyF8, Chord: "F8", Title: "Delete"},
		{Key: input.KeyF9, Chord: "F9", Title: "Mkdir"},
		// Not F10: the window opens its menu bar on that, and an
		// accelerator wins before any widget sees the key. ^D is what
		// closes a shell, which is near enough the same thing.
		{Key: input.KeyD, Mods: input.ModCtrl, Chord: "^D", Title: "Close"},
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
	for i := 0; i < n; i++ {
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
func drawKeys(v grid.View, y, cols int, keys []fkey, st Style, wired func(fkey) bool) {
	if cols <= 0 || len(keys) == 0 {
		return
	}
	for i, k := range keys {
		start, end := keyCell(i, cols, len(keys))
		// The key itself, on the bar's own ground, the way a number is
		// on the bar this was copied from.
		at := start
		for _, r := range k.Chord {
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
		title := trimTitle(" "+k.Title, end-at)
		at = v.SetString(at, y, title, fg, bg, 0)
		for ; at < end; at++ {
			v.Set(at, y, grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
		}
	}
}

// trimTitle cuts a key's name to the room the bar has for it.
//
// Cut rather than marked with an ellipsis: on a narrow window the bar is
// a reminder of which key does what, and "Del" reminds where "…" does
// not.
func trimTitle(s string, cols int) string {
	if cols <= 0 {
		return ""
	}
	runes := []rune(s)
	for len(runes) > 0 && grid.StringWidth(string(runes)) > cols {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
