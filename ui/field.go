package ui

import (
	"image/color"
	"strings"
	"unicode"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// FieldStyle colours a text field.
type FieldStyle struct {
	// FG and BG are the text and the box it sits in.
	FG, BG color.RGBA

	// PlaceholderFG dims the hint shown while the field is empty.
	PlaceholderFG color.RGBA
}

// Field is one line of text being typed into.
//
// It is the only text editor in the toolkit, so the palette, the forms
// and anything else that takes typing all move the caret the same way.
// The caret steps by grapheme cluster rather than by rune, because the
// grid draws a base character and its combining marks in one cell.
//
// Draw writes every cell it is given twice: once to fill and once for
// the text. Whatever shows a field is expected to draw through a buffer,
// as Form and Palette do, or an idle frame dirties the row.
type Field struct {
	Style FieldStyle

	// Placeholder is shown while the field is empty and says what the
	// field is for.
	Placeholder string

	// Mask replaces every cluster on screen, for a password. What was
	// typed is unchanged; only the drawing differs.
	Mask rune

	// OnPick is called when one of the Options is put in the field by
	// Ctrl+Down or Ctrl+Up, and not when the same text is typed. It is
	// how a caller tells the two apart.
	OnPick func(string)

	// OnChange is called after every change to the text.
	OnChange func(string)

	// ReadClipboard backs the paste shortcut. A nil one disables it.
	ReadClipboard func() string

	// WriteClipboard backs copy and cut. A nil one disables them.
	WriteClipboard func(string)

	// Options are the answers worth offering, for a field whose value is
	// usually one of a known few. Ctrl+Down and Ctrl+Up step through
	// them, and an empty one is included so an optional field can be
	// cycled back to nothing.
	//
	// They are a suggestion, not a rule: the field still takes anything
	// that is typed into it.
	Options []string

	// Ghost is the rest of an answer the caller thinks is being typed,
	// drawn after the text in the placeholder's colour. It is not part
	// of the value: Right or End at the end of the text takes it, and
	// anything else leaves it behind.
	//
	// The field never clears it. Whoever sets it clears it, which is the
	// same thing that knows when the answer has gone stale.
	Ghost string

	// Tick makes this a tick box rather than something to type in. It
	// holds Ticked or nothing, space turns it over, and nothing is typed
	// into it.
	Tick bool

	text string

	// ghostDrawn is the Ghost the field last drew, so only one the user
	// has seen can be taken.
	ghostDrawn string

	at   int // the caret, a byte offset into text at a cluster boundary
	left int // the first byte drawn, for text wider than the field

	// picked says a stretch of the text is selected, and mark is the end
	// of it the caret is not on.
	picked bool
	mark   int

	cols    int
	focused bool
}

// NewField returns an empty field.
func NewField() *Field { return &Field{} }

// Ticked is what a tick box holds when it is on. A tick box is a field,
// so what it holds is text like any other field's.
const Ticked = "yes"

// NewTick returns a tick box.
func NewTick(on bool) *Field {
	f := &Field{Tick: true}
	f.SetOn(on)
	return f
}

// On reports whether a tick box is ticked.
func (f *Field) On() bool { return f.text == Ticked }

// SetOn ticks a box or clears it.
func (f *Field) SetOn(on bool) {
	if on {
		f.SetText(Ticked)
		return
	}
	f.SetText("")
}

// Toggle turns a tick box over and reports what it holds now.
func (f *Field) Toggle() bool {
	f.SetOn(!f.On())
	return f.On()
}

// Text returns what has been typed.
func (f *Field) Text() string { return f.text }

// SetText replaces the text and puts the caret at the end.
func (f *Field) SetText(s string) {
	changed := f.text != s
	f.text = s
	f.at = len(s)
	f.picked = false
	// The old offset was a position in the old text, and can land inside
	// a character of the new one.
	f.left = 0
	f.scroll()
	if changed {
		f.changed()
	}
}

// Caret returns the caret's byte offset into the text.
func (f *Field) Caret() int { return f.at }

// SetCaret moves the caret to the cluster boundary at or before a byte
// offset, so a caller cannot put it inside a character.
func (f *Field) SetCaret(at int) {
	f.at = min(max(at, 0), len(f.text))
	f.picked = false
	f.snapCaret()
	f.scroll()
}

// snapCaret pulls the caret back to the cluster boundary at or before
// where it is.
func (f *Field) snapCaret() {
	marks := f.bounds()
	at := marks[0]
	for _, m := range marks {
		if m > f.at {
			break
		}
		at = m
	}
	f.at = at
}

// Layout notes how wide the field is.
func (f *Field) Layout(size Size) {
	f.cols = size.Cols
	f.scroll()
}

// SetFocus takes and gives up the caret.
func (f *Field) SetFocus(on bool) {
	f.focused = on
	if !on {
		f.picked = false
	}
}

// Focused reports whether the field is the one being typed into.
func (f *Field) Focused() bool { return f.focused }

// HandleKey edits the text. Keys it has no use for travel on, so Enter,
// Escape and Tab still reach whatever is showing the field.
func (f *Field) HandleKey(ev input.Event) (bool, error) {
	// A tick box takes space and the keys that step through options, and
	// nothing else: there is nothing to type into it, and every other
	// key belongs to whatever is showing it.
	if f.Tick {
		// A press, not a repeat: a box held down would flicker, and a
		// box is answered once.
		if ev.Kind == input.Text && ev.Rune == ' ' && ev.NormalText {
			f.Toggle()
			return true, nil
		}
		if ev.Kind != input.KeyPress {
			return false, nil
		}
		switch {
		case ev.Key == input.KeySpace && ev.Mods == 0,
			ev.Mods == input.ModCtrl && (ev.Key == input.KeyDown || ev.Key == input.KeyUp):
			f.Toggle()
			return true, nil
		}
		return false, nil
	}
	if ev.Kind == input.Text {
		// Delete is not a character to type, whatever the platform says.
		if !ev.NormalText || ev.Rune < ' ' || ev.Rune == 0x7f {
			return false, nil
		}
		f.insert(string(ev.Rune))
		return true, nil
	}
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return false, nil
	}

	// Alt means something else everywhere it is used here, and Super is
	// the window manager's. Checked first, because AltGr on a Windows
	// layout arrives as Ctrl+Alt: AltGr+V would otherwise paste as well
	// as typing the character it composes.
	if ev.Mods.Has(input.ModAlt) || ev.Mods.Has(input.ModSuper) {
		return false, nil
	}
	// Ctrl and Ctrl+Shift both paste, because the window binds paste to
	// Ctrl+Shift+V and every other program binds it to Ctrl+V.
	// Shift+Insert as well: it is what X11 and a lot of terminals use,
	// and it is in many people's fingers.
	if ev.Ctrl() && ev.Key == input.KeyV ||
		ev.Mods == input.ModShift && ev.Key == input.KeyInsert {
		return f.paste(), nil
	}

	// Ctrl and Ctrl+Shift both copy, for the same reason both paste, and
	// Ctrl+Insert is the other half of the pair with Shift+Insert.
	if ev.Ctrl() && ev.Key == input.KeyC ||
		ev.Mods == input.ModCtrl && ev.Key == input.KeyInsert {
		return f.Copy(), nil
	}
	if ev.Ctrl() && ev.Key == input.KeyX {
		return f.Cut(), nil
	}
	// Ctrl alone: the window binds Ctrl+Shift+A to the pane switcher, and
	// a field that swallowed it would take that away.
	if ev.Mods == input.ModCtrl && ev.Key == input.KeyA {
		return f.SelectAll(), nil
	}

	if ev.Ctrl() && (ev.Key == input.KeyDown || ev.Key == input.KeyUp) {
		step := 1
		if ev.Key == input.KeyUp {
			step = -1
		}
		return f.cycle(step), nil
	}

	switch ev.Key {
	case input.KeyLeft, input.KeyRight, input.KeyHome, input.KeyEnd:
		from, to := f.span()
		switch {
		case ev.Shift():
			f.pick()
		case !f.picked, ev.Ctrl():
			f.picked = false
		case ev.Key == input.KeyLeft:
			// A plain move off a selection lands on the edge it moves
			// toward.
			f.at, f.picked = from, false
			f.scroll()
			return true, nil
		case ev.Key == input.KeyRight:
			f.at, f.picked = to, false
			f.scroll()
			return true, nil
		default:
			f.picked = false
		}
	}

	switch ev.Key {
	case input.KeyLeft:
		if ev.Ctrl() {
			f.at = f.wordLeft(f.at)
		} else {
			f.at = f.prev(f.at)
		}
	case input.KeyRight:
		// Right at the end of the text takes the rest of the answer, and
		// shift picks text out instead.
		if !ev.Ctrl() && !ev.Shift() && f.TakeGhost() {
			return true, nil
		}
		if ev.Ctrl() {
			f.at = f.wordRight(f.at)
		} else {
			f.at = f.next(f.at)
		}
	case input.KeyHome:
		f.at = 0
	case input.KeyEnd:
		if !ev.Ctrl() && !ev.Shift() && f.TakeGhost() {
			return true, nil
		}
		f.at = len(f.text)
	case input.KeyBackspace:
		if f.cutPicked() {
			return true, nil
		}
		to := f.prev(f.at)
		if ev.Ctrl() {
			to = f.wordLeft(f.at)
		}
		f.cut(to, f.at)
		return true, nil
	case input.KeyDelete:
		if f.cutPicked() {
			return true, nil
		}
		to := f.next(f.at)
		if ev.Ctrl() {
			to = f.wordRight(f.at)
		}
		f.cut(f.at, to)
		return true, nil
	case input.KeyU:
		// Ctrl+U clears back to the start, the way a shell line does.
		if !ev.Ctrl() {
			return false, nil
		}
		f.cut(0, f.at)
		return true, nil
	default:
		return false, nil
	}
	f.scroll()
	return true, nil
}

// Draw paints the text into the first row of the view.
func (f *Field) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}

	blank := grid.Cell{Rune: ' ', FG: f.Style.FG, BG: f.Style.BG, Width: 1}
	v.Sub(0, 0, cols, 1).Fill(blank)

	if f.Tick {
		box := "[ ]"
		if f.On() {
			box = "[x]"
		}
		v.SetString(0, 0, grid.Trim(box, cols), f.Style.FG, f.Style.BG, 0)
		// The caret sits on the tick itself, so the row with the keys on
		// it is the row that looks answerable.
		if f.focused && cols > 1 {
			v.SetCursor(grid.Cursor{X: 1, Y: 0, Visible: true, Style: grid.CursorBlock})
		}
		return
	}

	if f.text == "" && f.Placeholder != "" {
		// Shown even with the caret in it: an empty field is exactly
		// when the user is wondering what to type.
		v.SetString(0, 0, f.Placeholder, f.Style.PlaceholderFG, f.Style.BG, 0)
		if f.focused {
			v.SetCursor(grid.Cursor{X: 0, Y: 0, Visible: true, Style: grid.CursorBar})
		}
		return
	}

	at, off := 0, f.left
	for _, c := range f.clustersFrom(f.left) {
		shown, width := c, grid.StringWidth(c)
		if f.Mask != 0 {
			shown, width = string(f.Mask), grid.RuneWidth(f.Mask)
		}
		if at+width > cols {
			break
		}
		fg, bg := f.Style.FG, f.Style.BG
		if f.inPick(off) {
			// The field's own colours swapped, which contrasts whatever
			// the two of them are.
			fg, bg = f.Style.BG, f.Style.FG
		}
		at = v.SetString(at, 0, shown, fg, bg, 0)
		off += len(c)
	}
	// After the text, so the caret sits on the first character of it.
	if f.ghostShows() && at < cols {
		f.ghostDrawn = f.Ghost
		v.SetString(at, 0, grid.Trim(f.Ghost, cols-at), f.Style.PlaceholderFG, f.Style.BG, 0)
	}

	// Only the focused field may place the cursor: the grid has one and
	// no idea who owns it.
	if f.focused {
		if col := f.colOf(f.at) - f.colOf(f.left); col < cols {
			v.SetCursor(grid.Cursor{X: col, Y: 0, Visible: true, Style: grid.CursorBar})
		}
	}
}

// ghostShows reports whether the rest of the answer is drawn: only in
// the focused field, unmasked, with the caret at the end of the text,
// where taking it would land.
func (f *Field) ghostShows() bool {
	return f.Ghost != "" && f.Mask == 0 && f.focused && f.text != "" && f.at == len(f.text)
}

// TakeGhost puts the rest of the answer in the field and reports whether
// there was one.
//
// Only one the field has drawn, so a key cannot take an answer that
// arrived in the same frame and was never on screen.
func (f *Field) TakeGhost() bool {
	if !f.ghostShows() || f.ghostDrawn != f.Ghost {
		return false
	}
	ghost := f.Ghost
	f.Ghost, f.ghostDrawn = "", ""
	f.insert(ghost)
	return true
}

// insert puts text in at the caret, in place of what is picked out, and
// moves the caret past it.
func (f *Field) insert(s string) {
	from, to := f.at, f.at
	if f.picked {
		from, to = f.span()
	}
	f.picked = false
	if from == to && s == "" {
		return
	}
	f.text = f.text[:from] + s + f.text[to:]
	f.at = from + len(s)
	f.snapCaret()
	f.scroll()
	f.changed()
}

// cut removes the text between two byte offsets and leaves the caret at
// the start of the gap.
func (f *Field) cut(from, to int) {
	f.picked = false
	if from > to {
		from, to = to, from
	}
	if from == to {
		return
	}
	f.text = f.text[:from] + f.text[to:]
	f.at = from
	f.scroll()
	f.changed()
}

// paste puts the clipboard in at the caret, as one line: a newline in a
// one-line field would be typed into a box that cannot show it.
//
// It reports whether it took the key. A field with no clipboard has not
// handled it, so the window's own paste binding still runs rather than
// the key doing nothing at all.
func (f *Field) paste() bool {
	if f.ReadClipboard == nil {
		return false
	}
	s := f.ReadClipboard()
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	f.insert(s)
	return true
}

func (f *Field) changed() {
	if f.OnChange != nil {
		f.OnChange(f.text)
	}
}

// bounds returns the byte offsets the caret may sit at: the start of
// every grapheme cluster, and the end of the text.
func (f *Field) bounds() []int {
	out := make([]int, 1, len(f.text)+1)
	at := 0
	for _, c := range grid.Clusters(f.text) {
		at += len(c)
		out = append(out, at)
	}
	return out
}

// prev returns the caret position one cluster before at.
func (f *Field) prev(at int) int {
	last := 0
	for _, m := range f.bounds() {
		if m >= at {
			return last
		}
		last = m
	}
	return last
}

// next returns the caret position one cluster after at.
func (f *Field) next(at int) int {
	for _, m := range f.bounds() {
		if m > at {
			return m
		}
	}
	return len(f.text)
}

// wordLeft returns the start of the word before at, stepping over
// whatever separated them.
//
// A word is letters and digits; everything else separates. That is what
// makes Ctrl+Backspace in "deploy@web1" take the host and leave the
// account, rather than taking the lot.
func (f *Field) wordLeft(at int) int {
	for at > 0 && !isWordRune(f.runeBefore(at)) {
		at = f.prev(at)
	}
	for at > 0 && isWordRune(f.runeBefore(at)) {
		at = f.prev(at)
	}
	return at
}

// wordRight returns the position just past the word after at.
func (f *Field) wordRight(at int) int {
	for at < len(f.text) && !isWordRune(f.runeAt(at)) {
		at = f.next(at)
	}
	for at < len(f.text) && isWordRune(f.runeAt(at)) {
		at = f.next(at)
	}
	return at
}

func (f *Field) runeAt(at int) rune {
	for _, r := range f.text[at:] {
		return r
	}
	return 0
}

func (f *Field) runeBefore(at int) rune {
	return f.runeAt(f.prev(at))
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

// colOf returns the column a byte offset is drawn at, counting from the
// start of the text rather than from the first cluster shown.
func (f *Field) colOf(at int) int {
	if f.Mask != 0 {
		return len(grid.Clusters(f.text[:at])) * max(grid.RuneWidth(f.Mask), 1)
	}
	return grid.StringWidth(f.text[:at])
}

// clustersFrom returns the clusters from a byte offset on.
func (f *Field) clustersFrom(at int) []string {
	if at >= len(f.text) {
		return nil
	}
	return grid.Clusters(f.text[at:])
}

// scroll slides the shown text along so the caret is always in the box.
//
// Without it a field narrower than what it holds is typed into blind:
// the caret walks off the right-hand edge and nothing moves.
func (f *Field) scroll() {
	f.at = min(max(f.at, 0), len(f.text))
	if f.cols <= 0 {
		f.left = 0
		return
	}
	if f.left > f.at {
		f.left = f.at
	}
	// One column is kept for the caret itself, which sits past the last
	// character when the text ends there.
	for f.colOf(f.at)-f.colOf(f.left) > f.cols-1 {
		f.left = f.next(f.left)
	}
}

// cycle puts the next option in the field, wrapping at the ends.
//
// It starts from whatever is there: text that is one of the options
// steps on from it, and anything else starts at the first. An empty
// field counts as the empty option when there is one, so an optional
// field cycles back round to nothing rather than stopping at a value.
func (f *Field) cycle(step int) bool {
	if len(f.Options) == 0 {
		return false
	}
	at := -1
	for i, option := range f.Options {
		if option == f.text {
			at = i
			break
		}
	}
	if at < 0 {
		// Not one of them, so the first step lands on the first option
		// going forwards and the last going back.
		at = -1
		if step < 0 {
			at = 0
		}
	}
	next := ((at+step)%len(f.Options) + len(f.Options)) % len(f.Options)
	if f.Options[next] == f.text {
		// One option, and it is already in the field. Taking the key
		// here would make it a dead key rather than whatever it is bound
		// to further out.
		return false
	}
	f.SetText(f.Options[next])
	if f.OnPick != nil {
		f.OnPick(f.text)
	}
	return true
}
