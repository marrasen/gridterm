package files

import (
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// Browser is a container of panes, and the tree takes it at its word:
// anything that walks the widgets reaches the panes through it, and a
// method here with the wrong shape would silently make it a leaf again.
var _ ui.Container = (*Browser)(nil)

// Work is something the user asked to do to the names they picked out.
//
// The browser does none of it. It says what was asked for and leaves the
// doing to whatever built it, which is what keeps a job queue, a dialog
// and a connection out of a widget.
type Work struct {
	// From is the pane the names are in, and To the pane they are going
	// to. To is nil for work that has only one side, which is deleting
	// and making a directory.
	From, To *Pane

	// At is the directory the names are in. It is where the From pane
	// was when they were picked out, which is not always where it is
	// now: names copied from one directory can be pasted after the pane
	// has moved on to another.
	At string

	// Names are what was picked out, each a name in At.
	Names []string
}

// Clipboard is what is waiting to be pasted: which names, where they
// are, and whether pasting moves them or copies them.
type Clipboard struct {
	From  *Pane
	At    string
	Names []string

	// Cut says the names are to be moved rather than copied. A cut can
	// be pasted once, because after that the names are somewhere else; a
	// copy can be pasted as often as the user likes.
	Cut bool
}

// Empty reports whether there is nothing to paste.
func (c Clipboard) Empty() bool { return c.From == nil || len(c.Names) == 0 }

// divider is the rule between two panes.
const divider = '│'

// Browser is any number of panes side by side.
//
// Which pane has the keys decides everything: a copy goes from there to
// the next one along. That is what makes one key enough for a copy
// between two machines, and it is why the window can hold as many panes
// as the user wants to put in it.
type Browser struct {
	// OnCopy, OnMove and OnDelete are what the F keys ask for. A nil one
	// means that key does nothing.
	OnCopy, OnMove, OnDelete func(Work)

	// OnMkdir and OnRename are asked for one name at a time: both need
	// something typed, which a widget does not do.
	OnMkdir, OnRename func(Work)

	// OnClose is asked to take a pane away. A browser cannot remove its
	// own pane from the tree it sits in, so it says which one and leaves
	// the rest to whatever built it.
	OnClose func(*Pane)

	// Style colours the dividers between the panes and the bar of keys
	// along the bottom. It is the panes' own style, so both belong to
	// what is around them.
	Style Style

	panes []*Pane

	// active is the pane with the keys, which is where work comes from.
	// A pointer rather than a position: removing a pane to the left of
	// it shifts every later pane along, and an index would then name the
	// pane next door.
	active *Pane

	hasFocus bool
	keys     []fkey
	size     ui.Size
	clip     Clipboard
}

// NewBrowser puts panes side by side, with the keys on the first.
//
// Adding one later gives it the keys instead: a pane opened is a pane
// the user wants to look at. Building a browser is not the same thing,
// so it opens where a browser always has.
func NewBrowser(panes ...*Pane) *Browser {
	b := &Browser{keys: browserKeys()}
	for _, p := range panes {
		b.Add(p)
	}
	if len(b.panes) > 0 {
		// Straight to the field: nothing holds the keys yet, so there is
		// no pane to tell about it.
		b.active = b.panes[0]
	}
	return b
}

// Panes returns them, left to right. The slice is a copy, so a caller
// cannot swap one out from under the browser.
func (b *Browser) Panes() []*Pane {
	out := make([]*Pane, len(b.panes))
	copy(out, b.panes)
	return out
}

// Add puts a pane on the right-hand end and gives it the keys, which is
// what opening one is for. It reports whether the pane went in.
//
// Nothing and a pane already here are both refused. The same pane twice
// would give Remove two answers and leave the browser holding a ghost,
// and a caller that has put a row on a sidebar for it needs to hear that
// it is not there.
func (b *Browser) Add(p *Pane) bool {
	if p == nil || b.indexOf(p) >= 0 {
		return false
	}
	b.panes = append(b.panes, p)
	b.Focus(p)
	b.Layout(b.size)
	return true
}

// Remove takes a pane out and says what stands in the browser's place:
// itself while it has a pane left, and nothing when it has none.
//
// One pane is a browser worth keeping. It cannot copy anywhere, but it
// is still a directory the user is looking at, and taking it away the
// moment the pane beside it closed would be closing something nobody
// asked to close.
//
// The keys go to the pane that takes its place, or to the one before it
// when the last pane went: they have to end up somewhere, or the browser
// is a thing no key reaches.
func (b *Browser) Remove(w ui.Widget) (ui.Widget, bool) {
	p, ok := w.(*Pane)
	if !ok {
		return nil, false
	}
	i := b.indexOf(p)
	if i < 0 {
		return nil, false
	}
	if b.clip.From == p {
		// Its directory is going with it, so there is nothing left to
		// paste from.
		b.setClip(Clipboard{})
	}
	had := b.active == p
	if had && b.hasFocus {
		p.SetFocus(false)
	}
	copy(b.panes[i:], b.panes[i+1:])
	// Clear the slot the shift vacated, or the array behind the slice
	// keeps the pane it just gave up alive, along with its filesystem.
	b.panes[len(b.panes)-1] = nil
	b.panes = b.panes[:len(b.panes)-1]

	if len(b.panes) == 0 {
		b.active = nil
		return nil, true
	}
	if had {
		b.active = b.panes[min(i, len(b.panes)-1)]
		if b.hasFocus {
			b.active.SetFocus(true)
		}
	}
	b.Layout(b.size)
	return b, true
}

// indexOf returns where a pane sits, or -1.
func (b *Browser) indexOf(p *Pane) int {
	for i, have := range b.panes {
		if have == p {
			return i
		}
	}
	return -1
}

// Here is the pane with the keys, or nil when the browser has none.
//
// Asked of the browser rather than of the panes: a pane only believes it
// has the keys while the browser does, and which pane work comes from
// has to be the same answer whether or not the browser is being looked
// at.
func (b *Browser) Here() *Pane { return b.active }

// There is the next pane along, wrapping at the end, which is where a
// copy goes. It is nil when there is nowhere else to send one.
func (b *Browser) There() *Pane {
	if len(b.panes) < 2 {
		return nil
	}
	return b.panes[(b.indexOf(b.active)+1)%len(b.panes)]
}

// Next moves the keys to the pane after this one, wrapping at the end.
func (b *Browser) Next() {
	if next := b.There(); next != nil {
		b.Focus(next)
	}
}

// Prev moves the keys to the pane before this one, wrapping at the
// start.
func (b *Browser) Prev() {
	if len(b.panes) < 2 {
		return
	}
	at := b.indexOf(b.active)
	b.Focus(b.panes[(at+len(b.panes)-1)%len(b.panes)])
}

// Clip is what is waiting to be pasted.
func (b *Browser) Clip() Clipboard { return b.clip }

// setClip records what is waiting to be pasted and shows it in the pane
// the names are in.
func (b *Browser) setClip(c Clipboard) {
	b.clip = c
	for _, p := range b.panes {
		if p == c.From {
			p.SetClipped(c.At, c.Names)
			continue
		}
		p.SetClipped("", nil)
	}
}

// pick puts what the user has marked on the clipboard, to be pasted
// wherever they go next.
func (b *Browser) pick(cut bool) (bool, error) {
	here := b.Here()
	if here == nil {
		return false, nil
	}
	names := here.Marked()
	if len(names) == 0 {
		return false, nil
	}
	b.setClip(Clipboard{From: here, At: here.At(), Names: names, Cut: cut})
	// The marks go: they said what the next key would act on, and that
	// key has been pressed. What is waiting to be pasted is marked its
	// own way.
	here.ClearMarks()
	return true, nil
}

// pasteWith is whoever does the work for what is on the clipboard: a cut
// is a move and a copy is a copy.
func (b *Browser) pasteWith() func(Work) {
	if b.clip.Cut {
		return b.OnMove
	}
	return b.OnCopy
}

// paste asks for the names on the clipboard to be put in the pane with
// the keys.
//
// The directory they came from is the one they were picked out in, not
// wherever that pane is now: a copy can be pasted into several places,
// and the pane it came from may have been used to look elsewhere in
// between.
func (b *Browser) paste() (bool, error) {
	to := b.Here()
	if to == nil || b.clip.Empty() {
		return false, nil
	}
	do := b.pasteWith()
	if do == nil {
		return false, nil
	}
	do(Work{From: b.clip.From, At: b.clip.At, To: to, Names: b.clip.Names})
	if b.clip.Cut {
		// They are somewhere else now, so there is nothing left to paste
		// again.
		b.setClip(Clipboard{})
	}
	return true, nil
}

// work is what the pane with the keys has picked out. one asks for the
// name under the bar alone, for something that can only be done to one
// thing at a time.
func (b *Browser) work(one bool) (Work, bool) {
	here := b.Here()
	if here == nil {
		return Work{}, false
	}
	names := here.Marked()
	if one {
		e, ok := here.Selected()
		if !ok {
			return Work{}, false
		}
		names = []string{e.Name}
	}
	if len(names) == 0 {
		return Work{}, false
	}
	return Work{From: here, At: here.At(), Names: names}, true
}

// paneCell returns the columns one pane is drawn in.
//
// The width is divided by counting from the left edge each time rather
// than by stepping, so the remainder is spread across the panes and the
// last one ends at the right edge. A divider sits in the column before
// every pane but the first.
func (b *Browser) paneCell(i, cols int) (start, end int) {
	n := len(b.panes)
	if n <= 0 || cols <= 0 || i < 0 || i >= n {
		return 0, 0
	}
	// The dividers come off the top, and what is left is shared out.
	room := max(cols-(n-1), 0)
	start = min(room*i/n+i, cols)
	end = min(room*(i+1)/n+i, cols)
	return start, max(end, start)
}

// Layout gives every pane its share of the width, less the bar of keys.
func (b *Browser) Layout(size ui.Size) {
	b.size = size
	rows := max(size.Rows-b.barRows(), 0)
	for i := range b.panes {
		start, end := b.paneCell(i, size.Cols)
		b.panes[i].Layout(ui.Size{Cols: end - start, Rows: rows})
	}
}

// barRows is how many rows the bar of keys takes.
func (b *Browser) barRows() int {
	// Never at the cost of the panes: a browser showing a bar and one
	// row of names is a browser showing nothing.
	if len(b.keys) == 0 || b.size.Rows < 4 {
		return 0
	}
	return 1
}

// Draw paints the panes, the dividers between them, and the bar of keys
// under them.
func (b *Browser) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	body := rows - b.barRows()
	for i, p := range b.panes {
		start, end := b.paneCell(i, cols)
		if i > 0 && start > 0 {
			// The divider goes in the column the sharing left for it.
			for y := 0; y < body; y++ {
				v.Set(start-1, y, grid.Cell{
					Rune: divider, FG: b.Style.NoteFG, BG: b.Style.BG, Width: 1,
				})
			}
		}
		if end > start && body > 0 {
			p.Draw(v.Sub(start, 0, end-start, body))
		}
	}
	if b.barRows() == 0 {
		return
	}
	drawKeys(v, rows-1, cols, b.keys, b.Style, b.wired)
}

// wired reports whether a key on the bar has anything behind it here.
func (b *Browser) wired(k fkey) bool {
	switch {
	case k.matches(key(input.KeyTab, 0)):
		return len(b.panes) > 1
	case k.matches(key(input.KeyF2, 0)):
		return b.OnRename != nil
	case k.matches(key(input.KeyC, input.ModCtrl)):
		return b.OnCopy != nil && b.Here() != nil
	case k.matches(key(input.KeyX, input.ModCtrl)):
		return b.OnMove != nil && b.Here() != nil
	case k.matches(key(input.KeyV, input.ModCtrl)):
		// Nothing picked out is nothing to paste, so the bar says so
		// rather than offering a key that does nothing. Which of the two
		// does the work depends on what is on the clipboard.
		return !b.clip.Empty() && b.pasteWith() != nil
	case k.matches(key(input.KeyF8, 0)):
		return b.OnDelete != nil
	case k.matches(key(input.KeyF9, 0)):
		return b.OnMkdir != nil
	case k.matches(key(input.KeyD, input.ModCtrl)):
		return b.OnClose != nil
	}
	return false
}

// key is one key press, for asking whether a key on the bar is this one.
func key(k input.Key, mods input.Mods) input.Event {
	return input.Event{Kind: input.KeyPress, Key: k, Mods: mods}
}

// SetFocus passes the keys on to the pane that has them.
func (b *Browser) SetFocus(on bool) {
	if b.hasFocus == on {
		return
	}
	b.hasFocus = on
	if b.active != nil {
		b.active.SetFocus(on)
	}
}

// Focused returns the pane the keys are going to, which is what a
// container is asked for.
func (b *Browser) Focused() ui.Widget {
	if b.active == nil {
		return nil
	}
	return b.active
}

// HasFocus reports whether the browser has the keys at all.
func (b *Browser) HasFocus() bool { return b.hasFocus }

// Children are the panes, for the tree.
func (b *Browser) Children() []ui.Widget {
	out := make([]ui.Widget, len(b.panes))
	for i, p := range b.panes {
		out[i] = p
	}
	return out
}

// Focus points the browser at one of its panes.
func (b *Browser) Focus(w ui.Widget) bool {
	p, ok := w.(*Pane)
	if !ok {
		return false
	}
	if b.indexOf(p) < 0 {
		return false
	}
	if p == b.active {
		return true
	}
	// Only pass the change on while the browser holds the keys itself,
	// or a pane is told it lost keys it never had.
	if b.hasFocus && b.active != nil {
		b.active.SetFocus(false)
	}
	b.active = p
	if b.hasFocus {
		p.SetFocus(true)
	}
	return true
}

// Replace swaps one pane for another, reporting whether old was there.
//
// A pane already in the browser is refused: the same pane twice would
// give Remove two answers and leave the browser holding a ghost.
func (b *Browser) Replace(old, next ui.Widget) bool {
	was, wasOK := old.(*Pane)
	now, nowOK := next.(*Pane)
	if !wasOK || !nowOK {
		return false
	}
	i := b.indexOf(was)
	if i < 0 || (now != was && b.indexOf(now) >= 0) {
		return false
	}
	b.panes[i] = now
	if b.active == was {
		b.active = now
		if b.hasFocus && now != was {
			was.SetFocus(false)
			now.SetFocus(true)
		}
	}
	b.Layout(b.size)
	return true
}

// ChildArea returns where a pane is drawn.
func (b *Browser) ChildArea(w ui.Widget) (ui.Rect, bool) {
	p, ok := w.(*Pane)
	if !ok {
		return ui.Rect{}, false
	}
	i := b.indexOf(p)
	if i < 0 {
		return ui.Rect{}, false
	}
	start, end := b.paneCell(i, b.size.Cols)
	area := ui.Rect{X: start, Cols: end - start, Rows: max(b.size.Rows-b.barRows(), 0)}
	if area.Empty() {
		return ui.Rect{}, false
	}
	return area, true
}

// HandleKey takes the browser's own keys and passes the rest to the pane
// with the focus.
//
// The keys are the ones a two-pane browser has had for thirty years: a
// user who knows one of these knows this one.
func (b *Browser) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return b.toPane(ev)
	}
	if ev.Key == input.KeyTab && ev.Mods == input.ModShift && len(b.panes) > 1 {
		b.Prev()
		return true, nil
	}
	if took, err := b.press(ev); took || err != nil {
		return took, err
	}
	return b.toPane(ev)
}

// toPane hands a key to whichever pane has them.
func (b *Browser) toPane(ev input.Event) (bool, error) {
	p := b.Here()
	if p == nil {
		return false, nil
	}
	return p.HandleKey(ev)
}

// press runs what a key means, whether it was typed or clicked on the
// bar. It reports whether the key was used.
//
// The F keys Midnight Commander uses for copy and move still work
// alongside the chords on the bar: a user who knows those should not
// have to unlearn them for the sake of the label.
func (b *Browser) press(ev input.Event) (bool, error) {
	if ev.Ctrl() {
		switch ev.Key {
		case input.KeyC:
			return b.pick(false)
		case input.KeyX:
			return b.pick(true)
		case input.KeyV:
			return b.paste()
		case input.KeyD:
			if b.OnClose == nil || b.Here() == nil {
				return false, nil
			}
			b.OnClose(b.Here())
			return true, nil
		}
		return false, nil
	}
	if ev.Mods != 0 {
		return false, nil
	}
	switch ev.Key {
	case input.KeyTab:
		if len(b.panes) < 2 {
			return false, nil
		}
		b.Next()
		return true, nil
	case input.KeyEscape:
		// Nothing left to paste. A clipboard the user has forgotten
		// about is one that pastes something they did not mean.
		if b.clip.Empty() {
			return false, nil
		}
		b.setClip(Clipboard{})
		return true, nil
	case input.KeyF5:
		return b.pick(false)
	case input.KeyF6:
		return b.pick(true)
	case input.KeyF7:
		return b.paste()
	case input.KeyF8, input.KeyDelete:
		return b.ask(b.OnDelete, false)
	case input.KeyF9:
		// Making a directory acts on the pane rather than on what is
		// picked out in it.
		if b.OnMkdir == nil || b.Here() == nil {
			return false, nil
		}
		b.OnMkdir(Work{From: b.Here(), At: b.Here().At()})
		return true, nil
	case input.KeyF2:
		// One name: renaming asks what to call it, and there is one
		// answer to that question.
		return b.ask(b.OnRename, true)
	}
	return false, nil
}

// ask hands on what the user picked out, if anything and if there is
// anybody to hand it to.
//
// A key nothing is wired to, or one with nothing to act on, is not
// taken: a widget that swallows a key it does nothing with swallows
// whatever that key is bound to everywhere else.
func (b *Browser) ask(to func(Work), one bool) (bool, error) {
	if to == nil {
		return false, nil
	}
	w, ok := b.work(one)
	if !ok {
		return false, nil
	}
	to(w)
	return true, nil
}

// HandleMouse runs a key clicked on the bar, and otherwise hands the
// event to the pane the pointer is over, putting the keys on it.
func (b *Browser) HandleMouse(ev input.MouseEvent) (bool, error) {
	if b.barRows() > 0 && ev.Row == b.size.Rows-1 {
		if ev.Kind != input.MousePress || ev.Button != input.MouseLeft {
			// A release or a drag over the bar is swallowed rather than
			// acted on, the way a press on a button is.
			return true, nil
		}
		i, ok := keyAt(ev.Col, b.size.Cols, len(b.keys))
		if !ok {
			return true, nil
		}
		// Taken whatever the key does: the press landed on the bar, not
		// on whatever is under the browser.
		k := b.keys[i]
		_, err := b.press(key(k.Key, k.Mods))
		return true, err
	}
	for i, p := range b.panes {
		start, end := b.paneCell(i, b.size.Cols)
		if ev.Col < start || ev.Col >= end {
			continue
		}
		if ev.Kind == input.MousePress && !ev.Button.IsWheel() {
			b.Focus(p)
		}
		ev.Col -= start
		return p.HandleMouse(ev)
	}
	// A divider, or the space past the last pane.
	return true, nil
}

// Reload reads every pane again, for after a job has changed something.
func (b *Browser) Reload() {
	for _, p := range b.panes {
		p.Reload()
	}
}

// SameFS reports whether the pane with the keys and the one a copy goes
// to are one filesystem, which is what makes a move a rename.
func (b *Browser) SameFS() bool {
	here, there := b.Here(), b.There()
	if here == nil || there == nil {
		return false
	}
	return vfs.Same(here.FS(), there.FS())
}
