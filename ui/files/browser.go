package files

import (
	"errors"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// Work is something the user asked to do to the names they picked out.
//
// The browser does none of it. It says what was asked for and leaves the
// doing to whatever built it, which is what keeps a job queue, a dialog
// and a connection out of a widget.
type Work struct {
	// From is the pane the names are in, and To the other one. To is nil
	// for work that has only one side, which is deleting and making a
	// directory.
	From, To *Pane

	// Names are what was picked out, each a name in the From pane's
	// directory.
	Names []string
}

// Browser is two panes side by side.
//
// Which pane has the keys decides everything: a copy goes from there to
// the other one. That is what makes one key enough for a copy between
// two machines.
type Browser struct {
	// OnCopy, OnMove and OnDelete are what the F keys ask for. A nil one
	// means that key does nothing.
	OnCopy, OnMove, OnDelete func(Work)

	// OnMkdir and OnRename are asked for one name at a time: both need
	// something typed, which a widget does not do.
	OnMkdir, OnRename func(Work)

	// Style colours the bar of keys along the bottom. It is the panes'
	// own style, so the bar belongs to what is above it.
	Style Style

	left, right *Pane
	split       *ui.Split
	keys        []fkey
	size        ui.Size
}

// NewBrowser puts two panes side by side, with the keys on the left.
func NewBrowser(left, right *Pane, divider func(*ui.Split)) *Browser {
	b := &Browser{left: left, right: right, keys: browserKeys()}
	b.split = ui.NewSplit(ui.Columns, left, right)
	if divider != nil {
		divider(b.split)
	}
	b.split.Focus(left)
	return b
}

// Panes returns the two, left first.
func (b *Browser) Panes() (*Pane, *Pane) { return b.left, b.right }

// Here is the pane with the keys, and There the other one.
//
// Asked of the split rather than of the panes: a pane only believes it
// has the keys while the browser does, and which side a copy comes from
// has to be the same answer whether or not the browser is being looked
// at.
func (b *Browser) Here() *Pane {
	if b.split.Focused() == ui.Widget(b.right) {
		return b.right
	}
	return b.left
}

// There is the pane the keys are not in, which is where a copy goes.
func (b *Browser) There() *Pane {
	if b.Here() == b.right {
		return b.left
	}
	return b.right
}

// Swap moves the keys to the other pane.
func (b *Browser) Swap() { b.split.Focus(b.There()) }

// work is what the pane with the keys has picked out. one asks for the
// name under the bar alone, for something that can only be done to one
// thing at a time.
func (b *Browser) work(both, one bool) (Work, bool) {
	here := b.Here()
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
	w := Work{From: here, Names: names}
	if both {
		w.To = b.There()
	}
	return w, true
}

// Layout gives the panes everything but the bar of keys.
func (b *Browser) Layout(size ui.Size) {
	b.size = size
	b.split.Layout(ui.Size{Cols: size.Cols, Rows: max(size.Rows-b.barRows(), 0)})
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

// Draw paints the panes and the bar of keys under them.
func (b *Browser) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	bar := b.barRows()
	if rows > bar {
		b.split.Draw(v.Sub(0, 0, cols, rows-bar))
	}
	if bar == 0 {
		return
	}
	drawKeys(v, rows-1, cols, b.keys, b.Style, b.wired)
}

// wired reports whether a key on the bar has anything behind it here.
func (b *Browser) wired(k input.Key) bool {
	switch k {
	case input.KeyTab:
		return true
	case input.KeyF2:
		return b.OnRename != nil
	case input.KeyF5:
		return b.OnCopy != nil
	case input.KeyF6:
		return b.OnMove != nil
	case input.KeyF7:
		return b.OnMkdir != nil
	case input.KeyF8:
		return b.OnDelete != nil
	}
	return false
}

// SetFocus passes the focus to whichever pane has it.
func (b *Browser) SetFocus(on bool) { b.split.SetFocus(on) }

// Focused returns the pane the keys are going to, which is what a
// container is asked for.
func (b *Browser) Focused() ui.Widget { return b.split.Focused() }

// HasFocus reports whether the browser has the keys at all.
func (b *Browser) HasFocus() bool { return b.left.Focused() || b.right.Focused() }

// Children are the two panes, for the tree.
func (b *Browser) Children() []ui.Widget { return b.split.Children() }

// Focus points the browser at one of its panes.
func (b *Browser) Focus(w ui.Widget) bool { return b.split.Focus(w) }

// HandleKey takes the browser's own keys and passes the rest to the pane
// with the focus.
//
// The keys are the ones a two-pane browser has had for thirty years: a
// user who knows one of these knows this one.
func (b *Browser) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return b.split.HandleKey(ev)
	}
	if ev.Mods != 0 {
		return b.split.HandleKey(ev)
	}

	if took, err := b.press(ev.Key); took || err != nil {
		return took, err
	}
	return b.split.HandleKey(ev)
}

// press runs what a key means, whether it was typed or clicked on the
// bar. It reports whether the key was used.
func (b *Browser) press(key input.Key) (bool, error) {
	switch key {
	case input.KeyTab:
		b.Swap()
		return true, nil
	case input.KeyF5:
		return b.ask(b.OnCopy, true, false)
	case input.KeyF6:
		return b.ask(b.OnMove, true, false)
	case input.KeyF7:
		// Making a directory acts on the pane rather than on what is
		// picked out in it.
		if b.OnMkdir == nil {
			return false, nil
		}
		b.OnMkdir(Work{From: b.Here()})
		return true, nil
	case input.KeyF8, input.KeyDelete:
		return b.ask(b.OnDelete, false, false)
	case input.KeyF2:
		// One name: renaming asks what to call it, and there is one
		// answer to that question.
		return b.ask(b.OnRename, false, true)
	}
	return false, nil
}

// ask hands on what the user picked out, if anything and if there is
// anybody to hand it to.
//
// A key nothing is wired to, or one with nothing to act on, is not
// taken: a widget that swallows a key it does nothing with swallows
// whatever that key is bound to everywhere else.
func (b *Browser) ask(to func(Work), both, one bool) (bool, error) {
	if to == nil {
		return false, nil
	}
	w, ok := b.work(both, one)
	if !ok {
		return false, nil
	}
	to(w)
	return true, nil
}

// HandleMouse runs a key clicked on the bar, and otherwise passes the
// mouse to the split, which knows where its panes are.
func (b *Browser) HandleMouse(ev input.MouseEvent) (bool, error) {
	if bar := b.barRows(); bar > 0 && ev.Row == b.size.Rows-1 {
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
		_, err := b.press(b.keys[i].Key)
		return true, err
	}
	return b.split.HandleMouse(ev)
}

// Reload reads both panes again, for after a job has changed something.
func (b *Browser) Reload() {
	b.left.Reload()
	b.right.Reload()
}

// Close lets go of both filesystems.
func (b *Browser) Close() error {
	return errors.Join(b.left.FS().Close(), b.right.FS().Close())
}

// SameFS reports whether both panes are on the same filesystem, which is
// what makes a move a rename.
func (b *Browser) SameFS() bool { return vfs.Same(b.left.FS(), b.right.FS()) }
