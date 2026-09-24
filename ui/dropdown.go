package ui

import (
	"strings"
	"time"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// Choice is one answer a drop-down offers. Key is what the field holds
// and what the caller reads back; Label is what the user sees.
//
// Two, because what is saved and what is shown are not always the same
// thing: a jump host is saved as a server's id and shown as its name.
type Choice struct {
	Key   string
	Label string
}

// ChoicesOf is a list of choices each shown as what it is.
func ChoicesOf(labels ...string) []Choice {
	out := make([]Choice, len(labels))
	for i, label := range labels {
		out[i] = Choice{Key: label, Label: label}
	}
	return out
}

// dropMost is the most rows an open list shows at once. A longer one
// scrolls.
const dropMost = 8

// dropMarker is drawn at the end of a drop-down, so a field that opens
// a list says so rather than needing a sentence about it.
const dropMarker = "▾"

// DropDown reports whether this field is a drop-down: it has Choices.
func (f *Field) DropDown() bool { return len(f.Choices) > 0 }

// Label is what the chosen answer shows as, and empty when what the
// field holds is none of its choices.
func (f *Field) Label() string {
	if i := f.chosen(); i >= 0 {
		return f.Choices[i].Label
	}
	return ""
}

// IsOpen reports whether the list of a drop-down is showing.
func (f *Field) IsOpen() bool { return f.open }

// chosen is where what the field holds is in its choices, and -1 when it
// is none of them.
func (f *Field) chosen() int {
	for i, c := range f.Choices {
		if c.Key == f.text {
			return i
		}
	}
	return -1
}

// openList shows the list with the answer already chosen lit, so Enter
// straight away changes nothing.
func (f *Field) openList() {
	f.open, f.typed = true, ""
	f.lit = max(f.chosen(), 0)
}

// closeList puts the list away without choosing anything.
func (f *Field) closeList() { f.open, f.typed = false, "" }

// toggleList opens the list, or puts it away when it is showing.
func (f *Field) toggleList() {
	if f.open {
		f.closeList()
		return
	}
	f.openList()
}

// choose puts one of the choices in the field, as the user picking it.
func (f *Field) choose(i int) {
	if i < 0 || i >= len(f.Choices) {
		return
	}
	f.SetText(f.Choices[i].Key)
	if f.OnPick != nil {
		f.OnPick(f.text)
	}
}

// pickLit chooses the lit row and puts the list away.
func (f *Field) pickLit() {
	f.choose(f.lit)
	f.closeList()
}

// step chooses the next answer along without opening the list, wrapping
// at the ends. It reports whether anything changed, so a list of one
// already chosen leaves the key to whatever is further out.
func (f *Field) step(by int) bool {
	n := len(f.Choices)
	at := f.chosen()
	if at < 0 {
		at = -1
		if by < 0 {
			at = 0
		}
	}
	next := ((at+by)%n + n) % n
	f.typed = ""
	if next == at {
		return false
	}
	f.choose(next)
	return true
}

// light moves the lit row, stopping at the ends: a list that wrapped
// would carry a held key round and round.
func (f *Field) light(by int) {
	f.lit = min(max(f.lit+by, 0), len(f.Choices)-1)
	f.typed = ""
}

// typeAhead goes to the first answer that starts with what has been
// typed since the last key that was not typing, or with this letter
// alone when nothing starts with the whole of it.
//
// Lit when the list is open, chosen when it is not: a letter is how a
// long list of servers is got through without the arrows.
func (f *Field) typeAhead(r rune) {
	find := func(prefix string) int {
		for i, c := range f.Choices {
			if strings.HasPrefix(strings.ToLower(c.Label), prefix) {
				return i
			}
		}
		return -1
	}
	if !f.naming() {
		f.typed = ""
	}
	f.typedAt = time.Now()
	prefix := strings.ToLower(f.typed + string(r))
	at := find(prefix)
	if at < 0 {
		prefix = strings.ToLower(string(r))
		at = find(prefix)
	}
	f.typed = prefix
	if at < 0 {
		return
	}
	if f.open {
		f.lit = at
		return
	}
	f.choose(at)
}

// typeAheadGap is how long a pause starts a new name rather than
// carrying on the one being typed, the way a list box takes typing.
const typeAheadGap = time.Second

// naming reports whether a name is part way through being typed, so a
// space is part of it rather than a key that opens or picks.
func (f *Field) naming() bool {
	return f.typed != "" && time.Since(f.typedAt) < typeAheadGap
}

// dropKey is HandleKey for a drop-down.
//
// The tick box takes space the same way, for the same reason.
//
// Closed, it takes the keys that open it, the ones that step through it
// and letters, and hands everything else on: Up and Down move between
// fields and Enter presses the dialog's button, the way they do from any
// field. Open, it takes every key, so nothing reaches the dialog while
// the list is in front of it -- Escape puts the list away rather than
// the dialog.
//
// One keystroke arrives as two events: the key, and then the character
// it types. Each is acted on once. A letter is taken as the character,
// because that is what knows the layout. Space is taken as the key and
// its character is swallowed, or one press would open the list and pick
// from it straight away -- except part way through a name, where the
// space is a character of the name.
func (f *Field) dropKey(ev input.Event) bool {
	if ev.Kind == input.Text {
		if !ev.NormalText || ev.Rune < ' ' || ev.Rune == 0x7f {
			return f.open
		}
		if ev.Rune == ' ' && !f.naming() {
			return true
		}
		f.typeAhead(ev.Rune)
		return true
	}
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return f.open
	}
	fresh := ev.Kind == input.KeyPress
	if ev.Key == input.KeySpace && ev.Mods == 0 {
		switch {
		case f.naming():
			// Its character carries on the name.
		case !fresh:
		case f.open:
			f.pickLit()
		default:
			f.openList()
		}
		return true
	}
	if f.open {
		switch ev.Key {
		case input.KeyUp:
			f.light(-1)
		case input.KeyDown:
			f.light(1)
		case input.KeyPageUp:
			f.light(-dropMost)
		case input.KeyPageDown:
			f.light(dropMost)
		case input.KeyHome:
			f.light(-len(f.Choices))
		case input.KeyEnd:
			f.light(len(f.Choices))
		case input.KeyEnter:
			// A fresh press, for the reason a dialog's own Enter is: the
			// repeats of a key held down would pick whatever they had
			// just lit.
			if fresh {
				f.pickLit()
			}
		case input.KeyEscape:
			f.closeList()
		case input.KeyTab:
			// Put away, and the key goes on to move the focus: Tab
			// leaving a field is what it always does.
			f.closeList()
			return false
		}
		return true
	}
	switch {
	case !fresh:
		return false
	case ev.Mods == 0 && ev.Key == input.KeyF4,
		ev.Mods == input.ModAlt && ev.Key == input.KeyDown:
		f.openList()
		return true
	case ev.Mods == input.ModCtrl && ev.Key == input.KeyDown:
		return f.step(1)
	case ev.Mods == input.ModCtrl && ev.Key == input.KeyUp:
		return f.step(-1)
	}
	return false
}

// drawDrop paints a drop-down's chosen answer, with the marker at the
// end. The list is the form's to draw, because it covers what is under
// the field and the field has only its own row.
func (f *Field) drawDrop(v grid.View) {
	cols, _ := v.Size()
	room := cols
	if cols >= 3 {
		// The marker and a blank before it.
		room = cols - 2
		v.SetString(cols-1, 0, dropMarker, f.Style.FG, f.Style.BG, 0)
	}
	if label := f.Label(); label != "" {
		v.SetString(0, 0, grid.Trim(label, room), f.Style.FG, f.Style.BG, 0)
	} else if f.Placeholder != "" {
		v.SetString(0, 0, grid.Trim(f.Placeholder, room), f.Style.PlaceholderFG, f.Style.BG, 0)
	}
	// On the marker, so the row with the keys on it looks answerable and
	// the caret does not sit on a letter that cannot be typed over.
	if f.focused && cols > 0 {
		v.SetCursor(grid.Cursor{X: cols - 1, Y: 0, Visible: true, Style: grid.CursorBlock})
	}
}

// showLit scrolls a list of rows rows so the lit row is on it.
func (f *Field) showLit(rows int) {
	if f.lit < f.top {
		f.top = f.lit
	}
	if f.lit >= f.top+rows {
		f.top = f.lit - rows + 1
	}
	f.top = min(max(f.top, 0), max(len(f.Choices)-rows, 0))
}
