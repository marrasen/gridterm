package files

import (
	"fmt"
	"image"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// ShowsAPicture reports whether the file is one the reader shows as a
// picture rather than as lines.
//
// It goes by the name, so it is settled before the file has been read
// and the pane is laid out for a picture from the first frame.
func (r *Reader) ShowsAPicture() bool { return r.isPic }

// Picture is the picture the file holds, and nil until it has been read.
func (r *Reader) Picture() image.Image { return r.pic.Img }

// PictureRoom is the part of the pane a picture is drawn in, in the
// reader's own cells: everything but the name at the top and the bar of
// keys at the bottom.
func (r *Reader) PictureRoom() ui.Rect {
	rows := r.rows()
	if rows <= 0 || r.size.Cols <= 0 {
		return ui.Rect{}
	}
	return ui.Rect{X: 0, Y: 1, Cols: r.size.Cols, Rows: rows}
}

// openPicture reads the picture again.
func (r *Reader) openPicture() {
	r.busy = true
	r.ReadPic(func(pic Pic, err error) {
		// The count goes with the read it belonged to, so nothing is
		// left holding how far a read that has finished got.
		r.busy, r.sofar = false, 0
		r.err = err
		if err != nil {
			// The picture that was there is dropped: a pane showing an
			// old picture under a fresh error says the file is fine.
			r.pic = Pic{}
			return
		}
		r.pic = pic
	})
}

// AsBytes shows a picture file as a file of lines instead, and does
// nothing to a file that was never a picture.
//
// It is the way out of a file named .png that is not one: the read
// fails, and a pane offering only Reread and Close would be a dead end.
func (r *Reader) AsBytes() {
	if !r.isPic {
		return
	}
	r.isPic, r.pic, r.err = false, Pic{}, nil
	r.Open()
}

// PictureKeys is what the bar offers for a picture. Fewer than a file of
// lines has: there is nothing to search and nowhere to scroll.
func PictureKeys() []Key {
	return []Key{
		{Chord: chord(input.KeyR, input.ModCtrl), Shown: "^R", Title: "Reread"},
		{Chord: chord(input.KeyH, input.ModCtrl), Shown: "^H", Title: "Bytes"},
		{Chord: chord(input.KeyD, input.ModCtrl), Shown: "^D", Title: "Close"},
	}
}

// pictureKey takes a key while a picture is shown, swallowing the rest:
// a reader is not a terminal, and a letter typed into one must not reach
// the shell behind it.
func (r *Reader) pictureKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return ev.Kind == input.Text, nil
	}
	// Only a plain Ctrl, for the reason the file of lines gives: the
	// window has its own meaning for Ctrl+Shift and a letter.
	if ev.Mods != 0 && ev.Mods != input.ModCtrl {
		return true, nil
	}
	switch {
	case ev.Key == input.KeyR && ev.Ctrl():
		r.Open()
	case ev.Key == input.KeyH && ev.Ctrl():
		r.AsBytes()
	case ev.Key == input.KeyD && ev.Ctrl(), ev.Key == input.KeyQ:
		if r.OnClose != nil {
			r.OnClose()
		}
	}
	return true, nil
}

// pictureNote is what the top line says about a picture: how big it is
// in the file, and what kind of file it came from.
func (r *Reader) pictureNote() string {
	if r.pic.Img == nil {
		return ""
	}
	was := r.pic.Was
	if r.pic.Kind == "" {
		return fmt.Sprintf("%d×%d", was.X, was.Y)
	}
	return fmt.Sprintf("%d×%d %s", was.X, was.Y, r.pic.Kind)
}
