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

	left, right *Pane
	split       *ui.Split
}

// NewBrowser puts two panes side by side, with the keys on the left.
func NewBrowser(left, right *Pane, divider func(*ui.Split)) *Browser {
	b := &Browser{left: left, right: right}
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

// Layout tells the split how much room it has.
func (b *Browser) Layout(size ui.Size) { b.split.Layout(size) }

// Draw paints both panes.
func (b *Browser) Draw(v grid.View) { b.split.Draw(v) }

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

	switch ev.Key {
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
	return b.split.HandleKey(ev)
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

// HandleMouse passes the mouse to the split, which knows where its panes
// are.
func (b *Browser) HandleMouse(ev input.MouseEvent) (bool, error) {
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
