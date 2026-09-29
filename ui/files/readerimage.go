package files

import (
	"fmt"
	"image"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
)

// ShowsAnImage reports whether the file is one the reader shows as a
// image rather than as lines.
//
// It goes by the name, so it is settled before the file has been read
// and the pane is laid out for an image from the first frame.
func (r *Reader) ShowsAnImage() bool { return r.isPic }

// Image is the image the file holds, and nil until it has been read.
func (r *Reader) Image() image.Image { return r.pic.Img }

// ImageRoom is the part of the pane an image is drawn in, in the
// reader's own cells: everything but the name at the top and the bar of
// keys at the bottom.
func (r *Reader) ImageRoom() ui.Rect {
	rows := r.rows()
	if rows <= 0 || r.size.Cols <= 0 {
		return ui.Rect{}
	}
	return ui.Rect{X: 0, Y: 1, Cols: r.size.Cols, Rows: rows}
}

// openImage reads the image again.
func (r *Reader) openImage() {
	r.busy = true
	r.ReadPic(func(pic Pic, err error) {
		// The count goes with the read it belonged to, so nothing is
		// left holding how far a read that has finished got.
		r.busy, r.sofar = false, 0
		r.err = err
		if err != nil {
			// The image that was there is dropped: a pane showing an
			// old image under a fresh error says the file is fine.
			r.pic = Pic{}
			return
		}
		r.pic = pic
	})
}

// AsBytes shows an image file as a file of lines instead, and does
// nothing to a file that was never an image.
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

// NotAnImage has a reader of a file named as an image show it as
// lines from the start, as one that already found it was not one does.
// It reads nothing: the first read reads the lines.
func (r *Reader) NotAnImage() {
	r.isPic, r.pic = false, Pic{}
}

// ImageKeys is what the bar offers for an image. Fewer than a file of
// lines has: there is nothing to search and nowhere to scroll.
func ImageKeys() []Key {
	return []Key{
		{Chord: chord(input.KeyR, input.ModCtrl), Shown: "^R", Title: "Reload"},
		{Chord: chord(input.KeyH, input.ModCtrl), Shown: "^H", Title: "Bytes"},
		{Chord: chord(input.KeyD, input.ModCtrl), Shown: "^D", Title: "Close"},
	}
}

// imageKey takes a key while an image is shown, swallowing the rest:
// a reader is not a terminal, and a letter typed into one must not reach
// the shell behind it.
func (r *Reader) imageKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return ev.Kind == input.Text, nil
	}
	// The bar's letters want a plain Ctrl, for the reason the file of
	// lines gives: the window has its own meaning for Ctrl+Shift and a
	// letter. Q takes shift the same way it does there, so the two
	// readers close on the same keys.
	switch {
	case ev.Key == input.KeyR && plainCtrl(ev):
		r.Open()
	case ev.Key == input.KeyH && plainCtrl(ev):
		r.AsBytes()
	case ev.Key == input.KeyD && plainCtrl(ev), ev.Key == input.KeyQ:
		if r.OnClose != nil {
			r.OnClose()
		}
	}
	return true, nil
}

// imageNote is what the top line says about an image: how big it is
// in the file, and what kind of file it came from.
func (r *Reader) imageNote() string {
	if r.pic.Img == nil {
		return ""
	}
	was := r.pic.Was
	if r.pic.Kind == "" {
		return fmt.Sprintf("%d×%d", was.X, was.Y)
	}
	return fmt.Sprintf("%d×%d %s", was.X, was.Y, r.pic.Kind)
}
