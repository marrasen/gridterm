package appicon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
	"testing"
)

// The icon comes at every size a window is offered, square and the size
// it was asked for.
func TestTheIconComesAtEverySize(t *testing.T) {
	got := Images()

	if len(got) != len(windowSizes) {
		t.Fatalf("%d images, want %d", len(got), len(windowSizes))
	}
	for i, img := range got {
		b := img.Bounds()
		if b.Dx() != windowSizes[i] || b.Dy() != windowSizes[i] {
			t.Errorf("image %d is %dx%d, want %d square", i, b.Dx(), b.Dy(), windowSizes[i])
		}
	}
	for _, list := range [][]int{Sizes, windowSizes} {
		if !slices.IsSorted(list) {
			t.Errorf("the sizes are %v, want them smallest first", list)
		}
	}
	// 16 and 32 are what Windows asks a running window for; the file
	// also holds the big one Explorer shows.
	for _, want := range []int{16, 32} {
		if !slices.Contains(windowSizes, want) {
			t.Errorf("a window is not offered %d pixels, and a window system asks for it", want)
		}
	}
	if !slices.Contains(Sizes, 256) {
		t.Error("the icon file has no 256 pixel icon")
	}
	// Every size a window is offered is in the file too, so the two
	// cannot show different pictures at the same size.
	for _, want := range windowSizes {
		if !slices.Contains(Sizes, want) {
			t.Errorf("the icon file has no %d pixel icon", want)
		}
	}
}

// The corners are clear at every size, so the icon reads as a rounded
// square rather than a block of colour.
func TestTheCornersAreClear(t *testing.T) {
	for _, size := range Sizes {
		img := Draw(size)
		last := size - 1

		for _, at := range []image.Point{{0, 0}, {last, 0}, {0, last}, {last, last}} {
			// Not wholly clear at sixteen pixels, where the corner is
			// three pixels across and the very corner keeps a sixteenth of
			// itself. A square one would be solid.
			if got := img.NRGBAAt(at.X, at.Y).A; got > 0x40 {
				t.Errorf("at %d pixels the corner at %v is %d of 255 solid", size, at, got)
			}
		}
		// And the middle is not.
		if _, _, _, a := img.At(size/2, size/2).RGBA(); a == 0 {
			t.Errorf("at %d pixels the middle of the icon is clear", size)
		}
	}
}

// The mark is on it: the chevron in one colour and the cursor in
// another, so an icon that drew only its ground would be caught.
func TestTheMarkIsOnTheIcon(t *testing.T) {
	for _, size := range Sizes {
		img := Draw(size)
		seen := map[color.NRGBA]int{}
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				seen[img.NRGBAAt(x, y)]++
			}
		}
		for _, want := range []struct {
			what string
			c    color.NRGBA
		}{
			{"its ground", ground},
			{"the chevron", prompt},
			{"the cursor", cursor},
		} {
			if seen[want.c] == 0 {
				t.Errorf("at %d pixels there is none of %s", size, want.what)
			}
		}
	}
}

// Drawing it twice gives the same picture, so a build is repeatable.
func TestDrawingItTwiceGivesTheSamePicture(t *testing.T) {
	first, again := Draw(48), Draw(48)

	if !bytes.Equal(first.Pix, again.Pix) {
		t.Error("the icon came out different the second time")
	}
}

// A size of nothing draws nothing rather than failing.
func TestASizeOfNothingDrawsNothing(t *testing.T) {
	for _, size := range []int{0, -1} {
		if got := Draw(size).Bounds(); !got.Empty() {
			t.Errorf("a size of %d drew %v", size, got)
		}
	}
}

// The .ico file holds one PNG per size, and says the right size against
// each. Windows reads the directory rather than the images, so an entry
// that disagreed would hand back the wrong one.
func TestTheIcoFileHoldsOnePNGPerSize(t *testing.T) {
	raw, err := ICO()
	if err != nil {
		t.Fatalf("write it: %v", err)
	}
	if len(raw) < 6 {
		t.Fatalf("the file is %d bytes", len(raw))
	}
	if reserved := binary.LittleEndian.Uint16(raw[0:]); reserved != 0 {
		t.Errorf("it starts with %d, want 0", reserved)
	}
	if kind := binary.LittleEndian.Uint16(raw[2:]); kind != 1 {
		t.Errorf("it says type %d, want 1 for an icon", kind)
	}
	count := int(binary.LittleEndian.Uint16(raw[4:]))
	if count != len(Sizes) {
		t.Fatalf("it holds %d images, want %d", count, len(Sizes))
	}

	for i := range count {
		e := raw[6+16*i:]
		side := int(e[0])
		if side == 0 {
			// The one byte the format gives it cannot hold 256.
			side = 256
		}
		if side != Sizes[i] {
			t.Errorf("entry %d says %d pixels, want %d", i, side, Sizes[i])
		}
		size := int(binary.LittleEndian.Uint32(e[8:]))
		at := int(binary.LittleEndian.Uint32(e[12:]))
		if at < 0 || size < 0 || at+size > len(raw) {
			t.Fatalf("entry %d points at %d..%d of %d bytes", i, at, at+size, len(raw))
		}
		img, err := png.Decode(bytes.NewReader(raw[at : at+size]))
		if err != nil {
			t.Fatalf("entry %d is not a PNG: %v", i, err)
		}
		if b := img.Bounds(); b.Dx() != Sizes[i] || b.Dy() != Sizes[i] {
			t.Errorf("entry %d holds a %dx%d image, want %d square", i, b.Dx(), b.Dy(), Sizes[i])
		}
	}
}

// The Windows resource holds the icon as it is drawn now.
//
// rsrc_windows_amd64.syso is built from ICO by the icon target in the
// Makefile and checked in, so a build needs neither the network nor the
// tool. Nothing else notices when the drawing changes and the resource
// is not built again, and the window would then show one icon while
// Explorer showed another.
func TestTheWindowsResourceHoldsTheIconAsItIsDrawnNow(t *testing.T) {
	raw, err := ICO()
	if err != nil {
		t.Fatalf("write the icon: %v", err)
	}
	const at = "../rsrc_windows_amd64.syso"
	syso, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("read %s: %v", at, err)
	}

	// Every image the icon holds has to be in there, compared as
	// pixels rather than as bytes. Two PNG encoders can spell the same
	// picture differently, and Go 1.27 did: the images were identical
	// and every compressed byte had changed, which failed this test for
	// a toolchain bump rather than for a drawing that had drifted.
	count := int(binary.LittleEndian.Uint16(raw[4:]))
	for i := range count {
		e := raw[6+16*i:]
		size := int(binary.LittleEndian.Uint32(e[8:]))
		from := int(binary.LittleEndian.Uint32(e[12:]))
		drawn, err := png.Decode(bytes.NewReader(raw[from : from+size]))
		if err != nil {
			t.Fatalf("read the %d pixel icon as drawn now: %v", Sizes[i], err)
		}
		stored := storedIcon(syso, drawn.Bounds())
		if stored == nil {
			t.Fatalf("%s holds no %d pixel image at all. Run \"make icon\"", at, Sizes[i])
		}
		if x, y, same := firstDifference(drawn, stored); !same {
			t.Fatalf("%s draws the %d pixel icon differently at %d,%d. Run \"make icon\"",
				at, Sizes[i], x, y)
		}
	}
}

// storedIcon is the image of these bounds inside a resource file, or nil
// when it holds none.
func storedIcon(syso []byte, want image.Rectangle) image.Image {
	signature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	for at := 0; ; {
		i := bytes.Index(syso[at:], signature)
		if i < 0 {
			return nil
		}
		at += i
		if img, err := png.Decode(bytes.NewReader(syso[at:])); err == nil && img.Bounds() == want {
			return img
		}
		at += len(signature)
	}
}

// firstDifference is where two images first disagree.
func firstDifference(a, b image.Image) (x, y int, same bool) {
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return x, y, false
			}
		}
	}
	return 0, 0, true
}
