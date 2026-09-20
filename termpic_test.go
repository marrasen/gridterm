package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/marrasen/gridterm/ui/term"
)

// anInlinePNG is the sequence a program sends to put a picture in its
// output, with the picture the size asked for in cells.
func anInlinePNG(t *testing.T, cols, rows int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return "\x1b]1337;File=inline=1;width=" + itoa(cols) + ";height=" + itoa(rows) +
		":" + base64.StdEncoding.EncodeToString(b.Bytes()) + "\x07"
}

// itoa spells a small number.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// picturesOn is how many layers a pane's pictures have.
func picturesOn(a *testApp, t *term.Terminal) int { return len(a.termPics[t]) }

// A picture a program put in the output gets a layer of its own over
// the pane. The grid is for text.
func TestAnInlinePictureGetsALayer(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.shells[0].out <- []byte(anInlinePNG(t, 10, 4))
	waitFor(t, a, "the pane to take the picture", func() bool {
		return len(pane.Pictures()) == 1
	})

	a.relayout()
	a.placeTermPics()

	if got := picturesOn(a, pane); got != 1 {
		t.Errorf("%d layers for one picture", got)
	}
}

// Closing the pane takes its pictures off the screen, so the textures
// are not held for a pane that has gone.
func TestClosingAPaneTakesItsPicturesAway(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.shells[0].out <- []byte(anInlinePNG(t, 10, 4))
	waitFor(t, a, "the pane to take the picture", func() bool {
		return len(pane.Pictures()) == 1
	})
	a.relayout()
	a.placeTermPics()
	if picturesOn(a, pane) != 1 {
		t.Fatal("no layer to take away")
	}

	if err := a.closePane(pane); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	a.placeTermPics()

	if got := picturesOn(a, pane); got != 0 {
		t.Errorf("%d layers left for a pane that has closed", got)
	}
}

// A pane that never showed a picture holds no layers, so the ordinary
// case costs nothing.
func TestAPaneWithNoPicturesHoldsNoLayers(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.shells[0].out <- []byte("just text\r\n")
	waitFor(t, a, "the pane to say it", func() bool {
		return len(paneText(pane)) > 0
	})

	a.relayout()
	a.placeTermPics()

	if got := picturesOn(a, pane); got != 0 {
		t.Errorf("%d layers for a pane with no pictures", got)
	}
	if _, held := a.termPics[pane]; held {
		t.Error("a pane with no pictures is being kept in the map")
	}
}

// A picture scrolled out of sight loses its layer, and the pane keeps
// working.
func TestAPictureScrolledOffLosesItsLayer(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.shells[0].out <- []byte(anInlinePNG(t, 10, 4))
	waitFor(t, a, "the pane to take the picture", func() bool {
		return len(pane.Pictures()) == 1
	})
	a.relayout()
	a.placeTermPics()

	for range 60 {
		a.shells[0].out <- []byte("a line of text\r\n")
	}
	waitFor(t, a, "the picture to scroll out of sight", func() bool {
		return len(pane.Pictures()) == 0
	})
	a.placeTermPics()

	if got := picturesOn(a, pane); got != 0 {
		t.Errorf("%d layers for a picture out of sight", got)
	}
}
