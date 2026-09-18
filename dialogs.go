package main

import (
	"image/color"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// formStyle colours a dialog in the window's own colours.
//
// The background has no alpha: the frosted panel behind the dialog is
// the background, and an opaque fill would hide it.
func (a *app) formStyle() ui.FormStyle {
	return ui.FormStyle{
		FG:      a.frameFG(),
		BG:      a.panelBG(),
		TitleFG: a.frameFG(),
		LabelFG: a.panelDimFG(),
		HintFG:  a.panelDimFG(),
		// A field is marked by its background rather than a border, so a
		// one-line box does not cost three rows of frame.
		FieldFG:  a.frameFG(),
		FieldBG:  a.panelFieldBG(),
		FocusFG:  a.frameFG(),
		FocusBG:  a.colours.Selection,
		ButtonFG: a.buttonFG(),
		ButtonBG: a.buttonBG(),
		ActiveFG: a.activeFG(),
		ActiveBG: a.activeBG(),
		// Red, because a line saying why something failed has to read as
		// a failure before it is read as words.
		ErrorFG: a.onFrame(a.colours.ANSI[1]),
		// A rule around it, and a shadow under it. A dialog over a
		// terminal is otherwise two lots of text with nothing between
		// them.
		BorderFG: a.panelBorderFG(),
		ShadowBG: a.panelShadow(),
		Rule:     a.panelRule(),
	}
}

// shadow is what a menu or a dialog lays over the cells below and to the
// right of it. Dark and mostly see-through: it darkens what is behind
// rather than covering it.
var shadow = color.RGBA{A: 0x70}

// newForm builds a dialog carrying the window's colours.
func (a *app) newForm(title string) *ui.Form {
	f := ui.NewForm(title, nil)
	a.dressForm(f)
	return f
}

// newConfirm builds a question with no fields, in the window's colours.
func (a *app) newConfirm(title string, lines []string) *ui.Form {
	f := ui.NewConfirm(title, lines, nil)
	a.dressForm(f)
	return f
}

// dressForm gives a dialog the window's colours and its copy chord.
//
// The chord, because a form swallows every key it does not use and the
// toolkit cannot see the window's keymap. The reason a connection failed
// is shown on the form and is worth pasting somewhere.
func (a *app) dressForm(f *ui.Form) {
	f.Style = a.formStyle()
	f.Copy = a.clip.set
	f.CopyChord = a.copyChord
}

// showForm puts a dialog on the modal stack and returns what takes it
// away. onHidden is told when it has gone, however it went.
func (a *app) showForm(f *ui.Form, onHidden func()) func() {
	dismiss := a.showModal(f, onHidden)
	// Only now is there something to hand the dialog: what shows it is
	// what takes it away.
	f.SetClose(dismiss)
	return dismiss
}

// noticeStyle colours a notice in the window's own colours.
//
// The background has no alpha for the same reason a form's has none: the
// frosted panel behind the dialog is the background.
func (a *app) noticeStyle() ui.NoticeStyle {
	return ui.NoticeStyle{
		FG:      a.frameFG(),
		BG:      a.panelBG(),
		TitleFG: a.frameFG(),
		// Red, because a title saying something failed has to read as a
		// failure before it is read as words.
		FailureFG:   a.onFrame(a.colours.ANSI[1]),
		SelectionFG: a.frameFG(),
		SelectionBG: a.colours.Selection,
		ButtonFG:    a.buttonFG(),
		ButtonBG:    a.buttonBG(),
		ActiveFG:    a.activeFG(),
		ActiveBG:    a.activeBG(),
		BorderFG:    a.panelBorderFG(),
		ShadowBG:    a.panelShadow(),
		Rule:        a.panelRule(),
	}
}

// newNotice builds a dialog showing a message whole, in the window's
// colours and able to reach the clipboard.
func (a *app) newNotice(title, message string) *ui.Notice {
	n := ui.NewNotice(title, message, nil)
	n.Style = a.noticeStyle()
	n.Copy = a.clip.set
	// The dialog swallows every other key, so it has to be told which
	// one copies: the toolkit cannot see the window's keymap.
	n.CopyChord = a.copyChord
	return n
}

// copyCommand is the command a copy chord runs.
const copyCommand = "edit.copy"

// copyChord reports whether a key press is bound to copy, so a dialog
// holding text can answer that chord itself.
func (a *app) copyChord(ev input.Event) bool {
	c := ui.ChordOf(ev)
	for _, keys := range a.keymaps() {
		if id, ok := keys.Lookup(c); ok {
			return id == copyCommand
		}
	}
	return false
}

// showNotice puts a message on the modal stack. failure draws the title
// in the error colour.
func (a *app) showNotice(title, message string, failure bool) *ui.Notice {
	n := a.newNotice(title, message)
	n.Failure = failure
	return a.presentNotice(n)
}

// presentNotice puts a notice on the modal stack, for a caller that had
// something of its own to set on it first.
//
// An open drop-down goes first, because rebuilding a menu closes the
// drop-down and takes everything stacked above it away with it.
func (a *app) presentNotice(n *ui.Notice) *ui.Notice {
	if a.bar != nil {
		a.bar.Close()
	}
	n.SetClose(a.showModal(n, nil))
	return n
}

// newField builds a field that can be pasted into, which is how a long
// fingerprint or a generated password gets typed at all.
func (a *app) newField(placeholder string, mask rune) *ui.Field {
	fld := ui.NewField()
	fld.Placeholder = placeholder
	fld.Mask = mask
	fld.ReadClipboard = a.pasteText
	return fld
}
