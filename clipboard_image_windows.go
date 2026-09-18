//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"unsafe"

	win "golang.org/x/sys/windows"
)

// The clipboard formats a picture arrives in. CF_DIB is a device
// independent bitmap; CF_DIBV5 is the same with a header that can carry
// an alpha channel, which is what a screenshot tool puts there.
const (
	cfText        = 1
	cfDIB         = 8
	cfUnicodeText = 13
	cfDIBV5       = 17
)

// Compressions a BITMAPINFOHEADER may name. Only these two hold pixels
// this reads: the rest are JPEG or PNG payloads, or run-length runs.
const (
	biRGB       = 0
	biBitfields = 3
)

var (
	user32           = win.NewLazySystemDLL("user32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	getClipboardData = user32.NewProc("GetClipboardData")
	isFormatAvail    = user32.NewProc("IsClipboardFormatAvailable")

	kernel32     = win.NewLazySystemDLL("kernel32.dll")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
	globalSize   = kernel32.NewProc("GlobalSize")
)

// clipboardHasText reports whether there is any text on the clipboard.
//
// It is asked before reading, because the library that reads text says
// "the operation completed successfully" when there is none: it hands
// back whatever the last error was, and no error is what happened.
func clipboardHasText() bool {
	for _, want := range []uintptr{cfUnicodeText, cfText} {
		if ok, _, _ := isFormatAvail.Call(want); ok != 0 {
			return true
		}
	}
	return false
}

// clipboardImage returns the picture on the clipboard.
//
// It reports whether there is one separately from whether reading it
// failed: an empty clipboard is an ordinary thing to meet and a locked
// one is not.
func clipboardImage() (image.Image, bool, error) {
	format := uintptr(0)
	for _, want := range []uintptr{cfDIBV5, cfDIB} {
		if ok, _, _ := isFormatAvail.Call(want); ok != 0 {
			format = want
			break
		}
	}
	if format == 0 {
		return nil, false, nil
	}
	// A window handle of zero asks for the clipboard without owning a
	// window, which is what a program that only reads needs.
	if ok, _, err := openClipboard.Call(0); ok == 0 {
		return nil, true, fmt.Errorf("open the clipboard: %w", err)
	}
	defer func() { _, _, _ = closeClipboard.Call() }()

	handle, _, err := getClipboardData.Call(format)
	if handle == 0 {
		return nil, true, fmt.Errorf("read the clipboard: %w", err)
	}
	ptr, _, err := globalLock.Call(handle)
	if ptr == 0 {
		return nil, true, fmt.Errorf("lock the clipboard: %w", err)
	}
	defer func() { _, _, _ = globalUnlock.Call(handle) }()

	size, _, _ := globalSize.Call(handle)
	if size == 0 {
		return nil, true, fmt.Errorf("the picture on the clipboard is empty")
	}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size))
	// Copied out from under the lock, because everything past this point
	// works on it after the clipboard has been given back.
	dib := make([]byte, len(raw))
	copy(dib, raw)

	img, err := imageFromDIB(dib)
	if err != nil {
		return nil, true, err
	}
	return img, true, nil
}

// imageFromDIB reads a device independent bitmap: a header, then the
// pixels, bottom row first unless the height is negative.
//
// It reads the 24 and 32 bit uncompressed forms, which is what a
// screenshot and a copied picture arrive as. Anything else is refused by
// name rather than drawn wrongly.
func imageFromDIB(dib []byte) (image.Image, error) {
	const headerLeast = 40
	if len(dib) < headerLeast {
		return nil, fmt.Errorf("the picture on the clipboard is %d bytes, too short for a header", len(dib))
	}
	headerSize := int(binary.LittleEndian.Uint32(dib[0:4]))
	if headerSize < headerLeast || headerSize > len(dib) {
		return nil, fmt.Errorf("the picture on the clipboard says its header is %d bytes", headerSize)
	}
	width := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	height := int(int32(binary.LittleEndian.Uint32(dib[8:12])))
	bits := int(binary.LittleEndian.Uint16(dib[14:16]))
	compression := int(binary.LittleEndian.Uint32(dib[16:20]))

	if bits != 24 && bits != 32 {
		return nil, fmt.Errorf("the picture on the clipboard is %d bits a pixel, and this reads 24 and 32", bits)
	}
	if compression != biRGB && compression != biBitfields {
		return nil, fmt.Errorf("the picture on the clipboard is compressed, and this reads the plain forms")
	}
	topDown := height < 0
	if topDown {
		height = -height
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("the picture on the clipboard is %dx%d", width, height)
	}

	// The pixels follow the header, and the colour masks when there are
	// any. A header of 40 bytes puts three masks after it; a longer one
	// holds them itself.
	at := headerSize
	if compression == biBitfields && headerSize == headerLeast {
		at += 12
	}
	// Every row is padded out to a multiple of four bytes.
	stride := (width*bits/8 + 3) &^ 3
	if need := at + stride*height; need > len(dib) {
		return nil, fmt.Errorf(
			"the picture on the clipboard is %d bytes and its %dx%d pixels need %d", len(dib), width, height, need)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	step := bits / 8
	for y := range height {
		row := y
		if !topDown {
			row = height - 1 - y
		}
		line := dib[at+row*stride:]
		for x := range width {
			px := line[x*step:]
			// Windows writes blue, green, red, and alpha when it has it.
			a := uint8(0xff)
			if step == 4 {
				a = px[3]
			}
			img.SetRGBA(x, y, color.RGBA{R: px[2], G: px[1], B: px[0], A: a})
		}
	}
	// A 32-bit bitmap whose alpha is zero everywhere has no alpha at
	// all: plenty of programs leave that byte unwritten, and taking it
	// at its word would make the whole picture see-through.
	if step == 4 && !anyOpaque(img) {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
	}
	return img, nil
}

// anyOpaque reports whether any pixel has an alpha at all.
func anyOpaque(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			return true
		}
	}
	return false
}
