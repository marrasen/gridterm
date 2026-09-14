package main

import (
	"image/color"

	"github.com/marrasen/gridterm/ui"
)

// formStyle colours a dialog in the window's own colours.
//
// The background has no alpha: the frosted panel behind the dialog is
// the background, and an opaque fill would hide it.
func (a *app) formStyle() ui.FormStyle {
	return ui.FormStyle{
		FG:      a.colours.FG,
		BG:      color.RGBA{},
		TitleFG: a.colours.FG,
		LabelFG: a.colours.ANSI[8],
		HintFG:  a.colours.ANSI[8],
		// A field is marked by its background rather than a border, so a
		// one-line box does not cost three rows of frame.
		FieldFG:  a.colours.FG,
		FieldBG:  a.colours.ANSI[0],
		FocusFG:  a.colours.FG,
		FocusBG:  a.colours.Selection,
		ButtonFG: a.colours.FG,
		ButtonBG: a.colours.ANSI[0],
		ActiveFG: a.colours.BG,
		ActiveBG: a.colours.FG,
		// Red, because a line saying why something failed has to read as
		// a failure before it is read as words.
		ErrorFG: a.colours.ANSI[1],
	}
}

// newForm builds a dialog carrying the window's colours.
func (a *app) newForm(title string) *ui.Form {
	f := ui.NewForm(title, nil)
	f.Style = a.formStyle()
	return f
}

// newConfirm builds a question with no fields, in the window's colours.
func (a *app) newConfirm(title string, lines []string) *ui.Form {
	f := ui.NewConfirm(title, lines, nil)
	f.Style = a.formStyle()
	return f
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

// newField builds a field that can be pasted into, which is how a long
// fingerprint or a generated password gets typed at all.
func (a *app) newField(placeholder string, mask rune) *ui.Field {
	fld := ui.NewField()
	fld.Placeholder = placeholder
	fld.Mask = mask
	fld.ReadClipboard = clipboardRead
	return fld
}
