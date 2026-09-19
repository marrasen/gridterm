package ui

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
