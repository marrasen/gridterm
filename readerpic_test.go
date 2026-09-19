package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// aPictureOnDisk writes a PNG and returns its name and its path.
func aPictureOnDisk(t *testing.T, w, h int) (name, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encode it: %v", err)
	}
	name = "shot.png"
	path = filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}
	return name, path
}

// withCompositor gives a test window a compositor and its own layer, for
// a test about what goes on the stack.
func withCompositor(t *testing.T, a *testApp) {
	t.Helper()
	a.comp = render.NewCompositor(a.renderer)
	a.comp.OnError = func(err error) { t.Errorf("compositor: %v", err) }
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.relayout()
}

// openedPicture opens a reader on a picture and waits for it to arrive.
func openedPicture(t *testing.T, a *testApp, w, h int) *readerPic {
	t.Helper()
	name, path := aPictureOnDisk(t, w, h)
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the picture to be read", func() bool {
		a.pump.run()
		return r.Picture() != nil
	})
	a.relayout()
	a.placeReaderPics()
	p := a.readerPics[r]
	if p == nil {
		t.Fatal("the reader has a picture and the window drew no layer for it")
	}
	return p
}

// A picture opened from the browser goes on a layer of its own over the
// pane, under any dialog.
func TestAPictureGoesOnALayerOverThePane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withCompositor(t, a)

	p := openedPicture(t, a, 64, 32)

	if p.pic.Img == nil {
		t.Fatal("the layer carries no picture")
	}
	if p.layer.Hidden {
		t.Error("the layer is hidden")
	}
	if p.pic.Rect.Empty() {
		t.Error("the picture has nowhere to go")
	}
	if p.layer.Grid != nil {
		t.Error("the picture's layer carries a grid: the grid is for text")
	}
	on := false
	for _, l := range a.comp.Layers() {
		if l == p.layer {
			on = true
		}
	}
	if !on {
		t.Error("the layer is not on the stack")
	}
}

// The picture's box leaves the name at the top and the bar at the bottom
// showing.
func TestThePictureDoesNotCoverTheNameOrTheBar(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withCompositor(t, a)

	r := openedPicture(t, a, 64, 32)

	reader := onlyReader(t, a)
	area, shown := a.paneArea(reader)
	if !shown {
		t.Fatal("the reader has no room in the window")
	}
	top, _ := a.geo.RowBox(area.Y, area.Y+1)
	if r.pic.Rect.Min.Y <= top {
		t.Errorf("the picture starts at %d, over the name at %d", r.pic.Rect.Min.Y, top)
	}
	last, height := a.geo.RowBox(area.Y+area.Rows-1, area.Y+area.Rows)
	if r.pic.Rect.Max.Y > last {
		t.Errorf("the picture ends at %d, over the bar at %d", r.pic.Rect.Max.Y, last+height)
	}
}

// Closing the pane takes the picture off the stack and gives its texture
// back.
func TestClosingAPictureTakesItsLayerAway(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withCompositor(t, a)
	p := openedPicture(t, a, 64, 32)
	r := onlyReader(t, a)

	if err := a.closePane(r); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	a.placeReaderPics()

	if len(a.readerPics) != 0 {
		t.Errorf("the window still holds %d pictures", len(a.readerPics))
	}
	if p.pic.Img != nil {
		t.Error("the texture was not given back")
	}
	for _, l := range a.comp.Layers() {
		if l == p.layer {
			t.Error("the layer is still on the stack")
		}
	}
}

// A reread that gives the same picture back keeps the texture it has.
func TestARereadOfTheSamePictureKeepsItsTexture(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withCompositor(t, a)
	p := openedPicture(t, a, 64, 32)
	was := p.pic.Img

	a.placeReaderPics()

	if p.pic.Img != was {
		t.Error("a second frame built the texture again")
	}
}

// A picture behind a tab is hidden rather than given up: building the
// texture again on every switch would cost a whole picture each time.
func TestAPictureBehindATabKeepsItsTexture(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withCompositor(t, a)
	p := openedPicture(t, a, 64, 32)
	r := onlyReader(t, a)
	was := p.pic.Img

	// A pane the tree has nowhere for, which is what a tab behind is.
	r.Layout(ui.Size{})
	a.placeReaderPics()

	if !p.layer.Hidden {
		t.Error("the picture is still drawn although the pane has no room")
	}
	if p.pic.Img != was {
		t.Error("the texture was given up")
	}

	// And it comes back without building another.
	a.relayout()
	a.placeReaderPics()
	if p.layer.Hidden {
		t.Error("the picture did not come back")
	}
	if p.pic.Img != was {
		t.Error("coming back built the texture again")
	}
}

// A picture's row on the sidebar says how big the picture is, because a
// picture has no lines to count.
func TestAPictureRowSaysHowBigItIs(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withCompositor(t, a)
	openedPicture(t, a, 64, 32)

	got := a.readerNote(onlyReader(t, a))

	if !strings.Contains(got, "64×32") {
		t.Errorf("the row says %q, want it to say how big the picture is", got)
	}
}
