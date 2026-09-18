//go:build windows

package main

import (
	"encoding/binary"
	"image/color"
	"strings"
	"testing"
)

// dib builds a device independent bitmap the way Windows lays one out,
// for a test that does not depend on what is on the real clipboard.
//
// A negative height means the rows are given top row first, which is
// what a top-down bitmap says.
func dib(width, height, bits, compression int, rows [][]byte) []byte {
	const headerSize = 40
	head := make([]byte, headerSize)
	binary.LittleEndian.PutUint32(head[0:4], headerSize)
	binary.LittleEndian.PutUint32(head[4:8], uint32(int32(width)))
	binary.LittleEndian.PutUint32(head[8:12], uint32(int32(height)))
	binary.LittleEndian.PutUint16(head[12:14], 1)
	binary.LittleEndian.PutUint16(head[14:16], uint16(bits))
	binary.LittleEndian.PutUint32(head[16:20], uint32(compression))
	out := head
	if compression == biBitfields {
		out = append(out, make([]byte, 12)...)
	}
	stride := (width*bits/8 + 3) &^ 3
	for _, row := range rows {
		padded := make([]byte, stride)
		copy(padded, row)
		out = append(out, padded...)
	}
	return out
}

// Windows writes a bitmap's rows bottom first, so the last row given is
// the top of the picture.
func TestABottomUpBitmapIsTurnedTheRightWayUp(t *testing.T) {
	// Two rows of one pixel: red written first, so red is the bottom.
	raw := dib(1, 2, 24, biRGB, [][]byte{
		{0x00, 0x00, 0xff}, // blue, green, red
		{0x00, 0xff, 0x00},
	})

	got, err := imageFromDIB(raw)

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if want := (color.RGBA{0, 0xff, 0, 0xff}); got.At(0, 0) != want {
		t.Errorf("the top pixel is %v, want the green one written last", got.At(0, 0))
	}
	if want := (color.RGBA{0xff, 0, 0, 0xff}); got.At(0, 1) != want {
		t.Errorf("the bottom pixel is %v, want the red one written first", got.At(0, 1))
	}
}

// A negative height says the rows are already the right way up.
func TestATopDownBitmapIsLeftAlone(t *testing.T) {
	raw := dib(1, -2, 24, biRGB, [][]byte{
		{0x00, 0x00, 0xff},
		{0x00, 0xff, 0x00},
	})

	got, err := imageFromDIB(raw)

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if want := (color.RGBA{0xff, 0, 0, 0xff}); got.At(0, 0) != want {
		t.Errorf("the top pixel is %v, want the red one written first", got.At(0, 0))
	}
}

// A 32-bit bitmap carries its alpha, and rows are padded out to four
// bytes, which a 24-bit one of an odd width needs.
func TestTheColoursAndThePaddingAreRead(t *testing.T) {
	// Three pixels of 24 bits is nine bytes, padded to twelve.
	raw := dib(3, 1, 24, biRGB, [][]byte{{
		0x01, 0x02, 0x03,
		0x04, 0x05, 0x06,
		0x07, 0x08, 0x09,
	}})

	got, err := imageFromDIB(raw)

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	for x, want := range []color.RGBA{
		{0x03, 0x02, 0x01, 0xff},
		{0x06, 0x05, 0x04, 0xff},
		{0x09, 0x08, 0x07, 0xff},
	} {
		if got := got.At(x, 0); got != want {
			t.Errorf("pixel %d is %v, want %v: Windows writes blue, green, red", x, got, want)
		}
	}
}

// A 32-bit bitmap whose alpha is set is taken at its word.
func TestAnAlphaChannelThatWasWrittenIsKept(t *testing.T) {
	raw := dib(2, 1, 32, biBitfields, [][]byte{{
		0x10, 0x20, 0x30, 0x80,
		0x40, 0x50, 0x60, 0xff,
	}})

	got, err := imageFromDIB(raw)

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if want := (color.RGBA{0x30, 0x20, 0x10, 0x80}); got.At(0, 0) != want {
		t.Errorf("the first pixel is %v, want %v", got.At(0, 0), want)
	}
}

// A 32-bit bitmap whose alpha is zero everywhere has none at all.
//
// Plenty of programs leave that byte unwritten, and taking it at its
// word would make the whole picture see-through.
func TestAnAlphaChannelNobodyWroteIsIgnored(t *testing.T) {
	raw := dib(2, 1, 32, biRGB, [][]byte{{
		0x10, 0x20, 0x30, 0x00,
		0x40, 0x50, 0x60, 0x00,
	}})

	got, err := imageFromDIB(raw)

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if want := (color.RGBA{0x30, 0x20, 0x10, 0xff}); got.At(0, 0) != want {
		t.Errorf("the first pixel is %v, want %v: an alpha nobody wrote is not an alpha", got.At(0, 0), want)
	}
}

// A bitmap this cannot read is refused by name rather than drawn wrongly
// or read past its end.
func TestABitmapThisCannotReadIsRefused(t *testing.T) {
	for what, raw := range map[string][]byte{
		"too short for a header": make([]byte, 8),
		"eight bits a pixel":     dib(1, 1, 8, biRGB, [][]byte{{0x01}}),
		"compressed":             dib(1, 1, 24, 4, [][]byte{{0x01, 0x02, 0x03}}),
		"no pixels at all":       dib(0, 0, 24, biRGB, nil),
		"fewer rows than it says": append(
			dib(1, 4, 24, biRGB, [][]byte{{0x01, 0x02, 0x03}})[:40],
			make([]byte, 4)...),
	} {
		if _, err := imageFromDIB(raw); err == nil {
			t.Errorf("a bitmap %s was read", what)
		} else if !strings.Contains(err.Error(), "clipboard") {
			t.Errorf("a bitmap %s says %q, which does not say where it came from", what, err)
		}
	}
}
