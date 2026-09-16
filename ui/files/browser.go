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
//
// Every divider can be dragged. A press on one is taken, so whatever
// routes the mouse keeps the pointer here until the button comes up,
// however far it has wandered.
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
	keys     []Key
	size     ui.Size
	clip     Clipboard

	// weights say where each divider sits, one per boundary between two
	// panes, as a share of the room the panes divide. They rise from
	// left to right, and the last pane always ends at the right edge, so
	// there is no weight for it.
	//
	// Adding or taking away a pane shares the room out evenly again.
	weights []float64

	// cut is scratch for paneCell, reused to keep drawing off the heap.
	cut []int

	// dragging is the boundary being moved, or -1 when none is.
	dragging int
}

// NewBrowser puts panes side by side, with the keys on the first.
//
// Adding one later gives it the keys instead: a pane opened is a pane
// the user wants to look at. Building a browser is not the same thing,
// so it opens where a browser always has.
func NewBrowser(panes ...*Pane) *Browser {
	b := &Browser{keys: BrowserKeys(), dragging: -1}
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
	b.shareEvenly()
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

	b.shareEvenly()
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

// shareEvenly puts every divider back where an even division would put
// it, which is what adding or taking away a pane does.
//
// A drag is given up with them: the boundary the pointer was holding is
// not the same boundary once the panes have changed.
func (b *Browser) shareEvenly() {
	b.dragging = -1
	n := len(b.panes)
	if n < 2 {
		b.weights = nil
		return
	}
	b.weights = make([]float64, n-1)
	for i := range b.weights {
		b.weights[i] = float64(i+1) / float64(n)
	}
}

// weightAt is where a boundary sits, as a share of the room the panes
// divide. A browser whose weights have not been worked out yet divides
// its room evenly.
func (b *Browser) weightAt(i, n int) float64 {
	if i < len(b.weights) {
		return b.weights[i]
	}
	return float64(i+1) / float64(n)
}

// cuts returns the column each pane ends at within the room left after
// the dividers, keeping a cell for every pane.
//
// The last entry is the room itself, so the last pane always ends at the
// right edge whatever the weights say. The slice is reused between
// calls, so read it before asking again.
func (b *Browser) cuts(room int) []int {
	n := len(b.panes)
	if cap(b.cut) < n {
		b.cut = make([]int, n)
	}
	out := b.cut[:n]
	out[n-1] = room
	for i := 0; i < n-1; i++ {
		out[i] = int(float64(room)*b.weightAt(i, n) + 0.5)
	}
	// One pass out from the left edge and one back from the right, so a
	// pane is not squeezed to nothing from either side.
	for i := 0; i < n-1; i++ {
		lower := 1
		if i > 0 {
			lower = out[i-1] + 1
		}
		out[i] = min(max(out[i], lower), room)
	}
	for i := n - 2; i >= 0; i-- {
		out[i] = max(min(out[i], out[i+1]-1), 0)
	}
	return out
}

// paneCell returns the columns one pane is drawn in.
//
// A divider sits in the column before every pane but the first, and the
// last pane ends at the right edge.
func (b *Browser) paneCell(i, cols int) (start, end int) {
	n := len(b.panes)
	if n <= 0 || cols <= 0 || i < 0 || i >= n {
		return 0, 0
	}
	// The dividers come off the top, and what is left is shared out.
	room := max(cols-(n-1), 0)
	cut := b.cuts(room)
	// Each pane starts a divider along from where the one before it
	// ended, so i dividers stand before pane i.
	start = i
	if i > 0 {
		start = cut[i-1] + i
	}
	end = cut[i] + i
	return min(start, cols), min(max(end, start), cols)
}

// dividerAt returns the boundary drawn in a column, or false when the
// column belongs to a pane.
func (b *Browser) dividerAt(col int) (int, bool) {
	for i := 1; i < len(b.panes); i++ {
		// The same column Draw puts the rule in.
		start, _ := b.paneCell(i, b.size.Cols)
		if start > 0 && col == start-1 {
			return i - 1, true
		}
	}
	return 0, false
}

// dragBoundary reports which boundary the pointer is moving, if any.
func (b *Browser) dragBoundary() (int, bool) {
	return b.dragging, b.dragging >= 0 && b.dragging < len(b.panes)-1
}

// CancelGesture gives up a drag whose release is not coming.
func (b *Browser) CancelGesture() { b.dragging = -1 }

// dragTo moves one divider to a column, stopping it at its neighbours so
// the weights stay in order.
func (b *Browser) dragTo(at, col int) {
	n := len(b.panes)
	room := max(b.size.Cols-(n-1), 0)
	if at < 0 || at >= len(b.weights) || room <= 0 {
		return
	}
	lo, hi := 0.0, 1.0
	if at > 0 {
		lo = b.weights[at-1]
	}
	if at < len(b.weights)-1 {
		hi = b.weights[at+1]
	}
	// The dividers to the left of this one take a column each, so the
	// room before it is the column less the number of them.
	b.weights[at] = min(max(float64(col-at)/float64(room), lo), hi)
	b.Layout(b.size)
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
func (b *Browser) wired(k Key) bool {
	switch {
	case k.Chord == chord(input.KeyTab, 0):
		return len(b.panes) > 1
	case k.Chord == chord(input.KeyG, input.ModCtrl):
		return b.Here() != nil && b.Here().OnGoTo != nil
	case k.Chord == chord(input.KeyF2, 0):
		return b.OnRename != nil
	case k.Chord == chord(input.KeyC, input.ModCtrl):
		return b.OnCopy != nil && b.Here() != nil
	case k.Chord == chord(input.KeyX, input.ModCtrl):
		return b.OnMove != nil && b.Here() != nil
	case k.Chord == chord(input.KeyV, input.ModCtrl):
		// Nothing picked out is nothing to paste, so the bar says so
		// rather than offering a key that does nothing. Which of the two
		// does the work depends on what is on the clipboard.
		return !b.clip.Empty() && b.pasteWith() != nil
	case k.Chord == chord(input.KeyF8, 0):
		return b.OnDelete != nil
	case k.Chord == chord(input.KeyF9, 0):
		return b.OnMkdir != nil
	case k.Chord == chord(input.KeyD, input.ModCtrl):
		return b.OnClose != nil
	}
	return false
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
	if ev.Key == input.KeyEscape {
		// The innermost thing first. A name half typed to jump to it is
		// closer to the user than what is on the clipboard, and Escape
		// should take back the thing they are in the middle of. The pane
		// has had it, so what is left is the clipboard and nothing else.
		if took, err := b.toPane(ev); took || err != nil {
			return took, err
		}
		return b.press(ev)
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
	// The chords come off the bar's own table, so what the bar says is
	// live and what the key does cannot drift apart. Ctrl+Shift+X is not
	// Ctrl+X: taking a chord the bar never offered would cut files on a
	// key the user pressed for something else.
	if ev.Mods != 0 {
		switch {
		case ev.Mods != input.ModCtrl:
			return false, nil
		case ev.Key == input.KeyC:
			return b.pick(false)
		case ev.Key == input.KeyX:
			return b.pick(true)
		case ev.Key == input.KeyV:
			return b.paste()
		case ev.Key == input.KeyD:
			if b.OnClose == nil || b.Here() == nil {
				return false, nil
			}
			b.OnClose(b.Here())
			return true, nil
		case ev.Key == input.KeyG:
			here := b.Here()
			if here == nil || here.OnGoTo == nil {
				return false, nil
			}
			here.OnGoTo()
			return true, nil
		}
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

// HandleMouse moves a divider that was grabbed, runs a key clicked on
// the bar, and otherwise hands the event to the pane the pointer is
// over, putting the keys on it.
//
// The drag comes first, before the bar: the gesture belongs to the
// divider until the button comes up, wherever the pointer has gone.
func (b *Browser) HandleMouse(ev input.MouseEvent) (bool, error) {
	if at, ok := b.dragBoundary(); ok {
		switch {
		case ev.Kind == input.MouseRelease:
			b.dragging = -1
		case !ev.Button.IsWheel():
			b.dragTo(at, ev.Col)
		}
		return true, nil
	}
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
		_, err := b.press(k.press())
		return true, err
	}
	if ev.Kind == input.MousePress && !ev.Button.IsWheel() {
		if at, ok := b.dividerAt(ev.Col); ok {
			b.dragging = at
			return true, nil
		}
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
