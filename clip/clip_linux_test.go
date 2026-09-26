//go:build linux

package clip

import (
	"image"
	"image/color"
	"os"
	"testing"
)

// The clipboard belongs to whoever is using the machine, and these take
// it: they write to it and read back what they wrote. So they are asked
// for by name rather than run by an ordinary go test ./..., which is
// also what keeps them out of a run with no display.
//
// Run them with:
//
//	GRIDTERM_CLIPBOARD_TEST=1 go test -run Clipboard ./clip/
func needsClipboard(t *testing.T) {
	t.Helper()
	if os.Getenv("GRIDTERM_CLIPBOARD_TEST") == "" {
		t.Skip("set GRIDTERM_CLIPBOARD_TEST=1 to let this take the clipboard")
	}
	if err := ready(); err != nil {
		t.Skipf("no clipboard here: %v", err)
	}
}

// Text written to the clipboard reads back as itself, and the clipboard
// is then holding text rather than a picture.
func TestClipboardCarriesTextBothWays(t *testing.T) {
	needsClipboard(t)

	want := "gridterm on Linux: ÅÄÖ and a 🐧"
	if err := SetText(want); err != nil {
		t.Fatalf("write the text: %v", err)
	}
	got, err := Text()
	if err != nil {
		t.Fatalf("read the text: %v", err)
	}
	if got != want {
		t.Errorf("read back %q, want %q", got, want)
	}
	if !HasText() {
		t.Error("the clipboard holds text and did not say so")
	}
	if _, have, err := Image(); err != nil || have {
		t.Errorf("the clipboard holds text, and it reported a picture: have=%v err=%v", have, err)
	}
}

// A picture written to the clipboard reads back with its size and its
// colours, alpha included.
func TestClipboardCarriesAPictureBothWays(t *testing.T) {
	needsClipboard(t)

	want := image.NewRGBA(image.Rect(0, 0, 4, 3))
	want.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	want.Set(3, 2, color.RGBA{G: 0x80, B: 0x40, A: 0xff})
	// A see-through pixel, because a picture that loses its alpha on
	// the way through looks right until it is pasted onto something.
	//
	// It is left black. Go holds a colour multiplied by its alpha, so a
	// pixel that is fully see-through has no colour left to carry, and
	// asking for one back would be asking PNG for what nothing stores.
	want.Set(1, 1, color.RGBA{})

	if err := SetImage(want); err != nil {
		t.Fatalf("put the picture on the clipboard: %v", err)
	}
	got, have, err := Image()
	if err != nil {
		t.Fatalf("read the picture: %v", err)
	}
	if !have {
		t.Fatal("the picture was put on the clipboard and did not come back")
	}
	if got.Bounds() != want.Bounds() {
		t.Fatalf("it came back %v, want %v", got.Bounds(), want.Bounds())
	}
	for _, at := range []image.Point{{X: 0, Y: 0}, {X: 3, Y: 2}, {X: 1, Y: 1}} {
		wr, wg, wb, wa := want.At(at.X, at.Y).RGBA()
		gr, gg, gb, ga := got.At(at.X, at.Y).RGBA()
		if wr != gr || wg != gg || wb != gb || wa != ga {
			t.Errorf("pixel %v came back %v, want %v",
				at, []uint32{gr, gg, gb, ga}, []uint32{wr, wg, wb, wa})
		}
	}
}

// A clipboard holding a picture holds no text, so a paste into a field
// is told there is nothing for it rather than pasting the bytes.
func TestAClipboardHoldingAPictureHoldsNoText(t *testing.T) {
	needsClipboard(t)

	if err := SetImage(image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("put the picture on the clipboard: %v", err)
	}
	if HasText() {
		t.Error("the clipboard holds a picture and said it holds text")
	}
}
