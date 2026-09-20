package ui

import "github.com/marrasen/gridterm/grid"

// Selected is the text picked out in the field, and empty when nothing
// is.
func (f *Field) Selected() string {
	if !f.picked {
		return ""
	}
	from, to := f.span()
	return f.text[from:to]
}

// SelectAll picks out the whole text. It reports whether there was any
// to pick out, so the key travels on from an empty field.
func (f *Field) SelectAll() bool {
	if f.text == "" {
		return false
	}
	f.mark, f.at, f.picked = 0, len(f.text), true
	f.scroll()
	return true
}

// Copy puts the selection on the clipboard, and reports whether there
// was anything to copy. A masked field copies nothing, because the
// clipboard would show what the mask hides.
func (f *Field) Copy() bool {
	s := f.Selected()
	if s == "" || f.Mask != 0 || f.WriteClipboard == nil {
		return false
	}
	f.WriteClipboard(s)
	return true
}

// Cut copies the selection and takes it out of the field, and reports
// whether there was one to cut. It refuses wherever Copy does.
func (f *Field) Cut() bool {
	if !f.Copy() {
		return false
	}
	f.cutPicked()
	return true
}

// span is the selection's ends in reading order, both the caret when
// nothing is picked out.
func (f *Field) span() (from, to int) {
	if f.mark < f.at {
		return f.mark, f.at
	}
	return f.at, f.mark
}

// pick starts a selection at the caret, and leaves one already there
// alone so its other end stays put.
func (f *Field) pick() {
	if !f.picked {
		f.mark, f.picked = f.at, true
	}
}

// settle turns the selection off once its two ends have met, which is
// what shift and a key that could not move leaves.
func (f *Field) settle() {
	if f.mark == f.at {
		f.picked = false
	}
}

// cutPicked takes the selection out and reports whether there was one.
func (f *Field) cutPicked() bool {
	if !f.picked {
		return false
	}
	from, to := f.span()
	if from == to {
		f.picked = false
		return false
	}
	f.cut(from, to)
	return true
}

// inPick reports whether the cluster at a byte offset is picked out.
func (f *Field) inPick(off int) bool {
	if !f.picked || !f.focused {
		return false
	}
	from, to := f.span()
	return off >= from && off < to
}

// PressAt puts the caret where the field was clicked and starts picking
// text out from there. Holding shift carries the loose end of whatever
// is already picked out to the click instead, which is what a press
// with shift does everywhere else.
//
// The column is measured from the field's own first cell, so whatever
// shows the field takes its position off the label beside it.
func (f *Field) PressAt(col int, shift bool) {
	if f.Tick {
		return
	}
	at := f.caretAt(col)
	if shift {
		f.pick()
		f.at = at
		f.settle()
	} else {
		f.at, f.mark, f.picked = at, at, false
	}
	f.dragging = true
	f.scroll()
}

// DragTo carries the loose end of the selection to a column, for a
// pointer moved with the button held down. It does nothing until a
// press has started a drag.
//
// The column may be outside the field. A drag off the right-hand end
// runs to the end of the text, which is how a value wider than its box
// is picked out whole.
func (f *Field) DragTo(col int) {
	if !f.dragging {
		return
	}
	f.at = f.caretAt(col)
	f.picked = f.at != f.mark
	f.scroll()
}

// EndDrag ends a drag started by PressAt. A drag that never left the
// cluster it began on is a click, so it leaves no selection behind.
func (f *Field) EndDrag() {
	f.dragging = false
	f.settle()
}

// Dragging reports whether the pointer is still down in the field.
func (f *Field) Dragging() bool { return f.dragging }

// caretAt turns a column inside the field into a byte offset in its
// text. A column left of the first lands on the first cluster shown,
// which is not the start of the text in a field scrolled along.
func (f *Field) caretAt(col int) int {
	if col <= 0 {
		return f.left
	}
	at, width := f.left, 0
	for _, c := range f.clustersFrom(f.left) {
		w := grid.StringWidth(c)
		if f.Mask != 0 {
			w = max(grid.RuneWidth(f.Mask), 1)
		}
		if width+w > col {
			break
		}
		width += w
		at += len(c)
	}
	return at
}
