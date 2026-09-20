package main

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// termPic is one inline picture a program put in a pane's output,
// drawn on a layer of its own over the pane.
//
// The grid is for text, so a picture does not go in it. Its own layer
// for the same reason a reader's picture has one, and one per picture
// because they scroll independently.
type termPic struct {
	layer *render.Layer
	pic   render.Picture

	// from is the picture the texture was made from, so the frames
	// between one write and the next build no second texture.
	from image.Image
}

// placeTermPics gives every inline picture on screen a layer, and
// takes away the layers of the ones that have gone.
//
// Called once a frame, because a picture moves whenever the pane it
// is in scrolls, which is whenever the program says anything.
func (a *app) placeTermPics() {
	if a.comp == nil || a.g == nil {
		return
	}
	if a.termPics == nil {
		a.termPics = map[*term.Terminal][]*termPic{}
	}
	for t := range a.termPics {
		if _, live := a.panes[t]; !live {
			a.dropTermPics(t)
		}
	}
	for t := range a.panes {
		a.placePicsOf(t)
	}
}

// placePicsOf puts the pictures of one pane on screen.
func (a *app) placePicsOf(t *term.Terminal) {
	area, shown := a.paneArea(t)
	var placed []vt.Placement
	if shown && !t.Elsewhere() {
		placed = t.Pictures()
	}
	have := a.termPics[t]
	// One layer per picture, made and given back as the count moves.
	for len(have) < len(placed) {
		p := &termPic{}
		p.layer = &render.Layer{Hidden: true, Picture: &p.pic}
		a.addUnderModals(p.layer)
		have = append(have, p)
	}
	for len(have) > len(placed) {
		last := have[len(have)-1]
		a.comp.Remove(last.layer)
		last.free()
		have = have[:len(have)-1]
	}
	if len(have) == 0 {
		delete(a.termPics, t)
		return
	}
	a.termPics[t] = have
	for i, at := range placed {
		have[i].take(at.Img)
		have[i].place(area, at, &a.geo)
	}
}

// place puts one picture over the cells it was given, clipped to the
// pane: a picture half scrolled off is drawn in part.
func (p *termPic) place(area ui.Rect, at vt.Placement, geo *render.Geometry) {
	top := max(at.Top, 0)
	bottom := min(at.Top+at.Rows, area.Rows)
	left := max(at.Col, 0)
	right := min(at.Col+at.Cols, area.Cols)
	if top >= bottom || left >= right {
		p.layer.Hidden = true
		return
	}
	x, width := geo.ColBox(area.X+left, area.X+right)
	y, height := geo.RowBox(area.Y+top, area.Y+bottom)
	p.layer.X, p.layer.Y = 0, 0
	p.pic.Rect = image.Rect(x, y, x+width, y+height)
	p.layer.Hidden = false
}

// take builds the texture the layer draws, keeping the one it has
// when the picture has not changed.
func (p *termPic) take(img image.Image) {
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
func (p *termPic) free() {
	if p.pic.Img != nil {
		p.pic.Img.Deallocate()
		p.pic.Img = nil
	}
	p.from = nil
}

// dropTermPics takes a pane's pictures off the screen, for a pane
// that has closed.
func (a *app) dropTermPics(t *term.Terminal) {
	for _, p := range a.termPics[t] {
		a.comp.Remove(p.layer)
		p.free()
	}
	delete(a.termPics, t)
}
