package main

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
)

// mostPictureSide is the biggest a picture may be, each way, before the
// reader shrinks it.
//
// A few pixels under what a texture can be, because ebiten pads an image
// before it goes on one. Asking for a texture past the limit is not an
// error the window can catch: it brings the window down.
const mostPictureSide = mostPanePixels - 8

// readerPic is the picture a reader shows, on a layer of its own over
// the pane.
//
// The grid is for text, so a picture does not go in it. The layer
// carries no grid at all: it is the picture and nothing else.
type readerPic struct {
	layer *render.Layer
	pic   render.Picture

	// from is the picture the texture was made from, so a reread that
	// gives the same picture back does not build another texture.
	from image.Image
}

// newReaderPic puts a picture on a layer of its own, hidden until it has
// been placed.
func newReaderPic() *readerPic {
	p := &readerPic{}
	p.layer = &render.Layer{Hidden: true, Picture: &p.pic}
	return p
}

// place puts the picture over the reader's pane, in the window's pixels.
func (p *readerPic) place(area, room ui.Rect, geo *render.Geometry) {
	left, width := geo.ColBox(area.X+room.X, area.X+room.X+room.Cols)
	top, height := geo.RowBox(area.Y+room.Y, area.Y+room.Y+room.Rows)
	p.layer.X, p.layer.Y = 0, 0
	p.pic.Rect = image.Rect(left, top, left+width, top+height)
	p.layer.Hidden = false
}

// take builds the texture the layer draws, and keeps the one it has when
// the picture has not changed.
func (p *readerPic) take(img image.Image) {
	if img == p.from {
		return
	}
	p.free()
	p.from = img
	if img != nil {
		p.pic.Img = ebiten.NewImageFromImage(img)
	}
}

// free gives the texture back.
func (p *readerPic) free() {
	if p.pic.Img != nil {
		p.pic.Img.Deallocate()
		p.pic.Img = nil
	}
	p.from = nil
}

// placeReaderPics gives every reader showing a picture a layer for it,
// and takes the layer away again when the picture goes.
func (a *app) placeReaderPics() {
	if a.comp == nil || a.g == nil {
		return
	}
	for r, p := range a.readerPics {
		if _, live := a.readers[r]; !live {
			a.dropReaderPic(r, p)
		}
	}
	for r := range a.readers {
		img := r.Picture()
		p := a.readerPics[r]
		if img == nil {
			if p != nil {
				a.dropReaderPic(r, p)
			}
			continue
		}
		area, shown := a.paneArea(r)
		room := r.PictureRoom()
		if !shown || room.Empty() {
			// A pane behind a tab, or one with no room for a picture,
			// keeps its texture and hides it: building one again on
			// every switch between tabs would cost a whole picture.
			if p != nil {
				p.layer.Hidden = true
			}
			continue
		}
		if p == nil {
			p = newReaderPic()
			if a.readerPics == nil {
				a.readerPics = map[*files.Reader]*readerPic{}
			}
			a.readerPics[r] = p
			a.addUnderModals(p.layer)
		}
		p.take(img)
		p.place(area, room, &a.geo)
	}
}

// dropReaderPic takes a reader's picture off the screen.
func (a *app) dropReaderPic(r *files.Reader, p *readerPic) {
	a.comp.Remove(p.layer)
	p.free()
	delete(a.readerPics, r)
}
