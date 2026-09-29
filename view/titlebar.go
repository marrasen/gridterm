package view

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/appicon"
)

// The title bar reads, from the left: kakel's icon, the button that
// opens the menus, "kakel" and the pane in front, then what the window
// is doing for others and the window's buttons. All but the buttons
// moves the window, however little of it is on the screen.

// appMark is kakel's icon at the start of the title bar.
type appMark struct {
	img *paint.Image
}

// appMarkSide is how large the icon is drawn, and appMarkPixels how
// many pixels it is drawn from, for a sharp icon on a screen that
// scales by two.
const (
	appMarkSide   = 18
	appMarkPixels = 36
)

func newAppMark() *appMark { return &appMark{img: paint.NewImage(appicon.Draw(appMarkPixels))} }

// Layout implements [gunim.Node]: a square as high as the bar.
func (m *appMark) Layout(c gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	if !f.Chromeless() {
		// The system's title bar shows the icon.
		return c.Constrain(geom.Size{})
	}
	h := widget.MenubarHeight.Get(f.Theme)
	return c.Constrain(geom.Sz(h+4, h))
}

// Paint implements [gunim.Node].
func (m *appMark) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	if box.W <= 0 {
		return
	}
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.MenubarFill.Get(f.Theme)))
	at := geom.Pt((box.W-appMarkSide)/2+2, (box.H-appMarkSide)/2)
	p.Image(m.img, geom.Rc(at.X, at.Y, appMarkSide, appMarkSide), paint.ImageOpts{Opacity: 1})
}

// CaptionRects implements [gunim.Caption]: the icon moves the window.
func (m *appMark) CaptionRects(size geom.Size) []geom.Rect {
	return []geom.Rect{{Max: size.Point()}}
}
