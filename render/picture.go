package render

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
)

// Picture is an image drawn on a layer, inside a box given in pixels.
//
// It goes straight onto the screen after the layer's own texture, the
// way a rule does, so a picture is not held to the grid the layer is
// measured in.
//
// The zero Picture draws nothing.
type Picture struct {
	// Img is what is drawn.
	Img *ebiten.Image

	// Rect is the box it goes in, in pixels from the layer's own
	// top-left corner. The picture is centred in the box.
	Rect image.Rectangle
}

// Empty reports whether a picture draws nothing.
func (p *Picture) Empty() bool {
	return p == nil || p.Img == nil || p.Img.Bounds().Empty() || p.Rect.Empty()
}

// shownPicture is a picture as it was last drawn, for spotting one that
// changed while no grid did.
type shownPicture struct {
	img  *ebiten.Image
	rect image.Rectangle
	at   image.Point
}

// pictureOf is what a layer's picture would be drawn as now.
func pictureOf(l *Layer) shownPicture {
	if l.Picture.Empty() {
		return shownPicture{}
	}
	return shownPicture{img: l.Picture.Img, rect: l.Picture.Rect, at: image.Pt(l.X, l.Y)}
}

// drawPicture blits a picture into its box, shrunk to fit and centred.
//
// Shrunk only: a picture smaller than the box is drawn at its own size
// rather than blown up, because a thumbnail stretched over a pane says
// less about the file than the thumbnail does.
func (c *Compositor) drawPicture(screen *ebiten.Image, l *Layer) {
	p := l.Picture
	box := p.Rect.Add(image.Pt(l.X, l.Y))
	src := p.Img.Bounds()

	scale := min(
		float64(box.Dx())/float64(src.Dx()),
		float64(box.Dy())/float64(src.Dy()),
		1,
	)
	w, h := float64(src.Dx())*scale, float64(src.Dy())*scale

	op := &ebiten.DrawImageOptions{}
	if scale != 1 {
		op.GeoM.Scale(scale, scale)
		op.Filter = ebiten.FilterLinear
	}
	op.GeoM.Translate(
		float64(box.Min.X)+(float64(box.Dx())-w)/2,
		float64(box.Min.Y)+(float64(box.Dy())-h)/2,
	)
	screen.DrawImage(p.Img, op)
}

// anyPicture reports whether a layer the viewer can see draws a picture.
func (c *Compositor) anyPicture() bool {
	for _, l := range c.layers {
		if !l.Hidden && !l.Picture.Empty() {
			return true
		}
	}
	return false
}

// anyPictureChanged reports whether a picture is not the one that was
// drawn last time, including one that has just gone.
//
// A picture is blitted straight to the screen, so nothing else marks it
// stale, and one that has gone leaves its pixels behind until the frame
// that notices wipes the screen.
func (c *Compositor) anyPictureChanged() bool {
	for _, l := range c.layers {
		if l.Hidden {
			if l.shownPic != (shownPicture{}) {
				// Hidden with a picture still on screen.
				return true
			}
			continue
		}
		if pictureOf(l) != l.shownPic {
			return true
		}
	}
	return false
}

// notePictureDrawn records what a layer's picture was drawn as. A hidden
// layer draws none, whatever it carries.
func (l *Layer) notePictureDrawn() {
	if l.Hidden {
		l.shownPic = shownPicture{}
		return
	}
	l.shownPic = pictureOf(l)
}
