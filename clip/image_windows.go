//go:build windows

package clip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"time"
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

// How long the clipboard is waited for, and how often it is asked.
//
// The clipboard is one object shared by every program on the machine.
// Another holding it for a few milliseconds is ordinary rather than a
// failure, so opening it is tried for a moment rather than once.
const (
	clipboardTry = 10 * time.Millisecond
	wait         = 300 * time.Millisecond
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

// clipOpen takes the clipboard and clipClose gives it back. Variables
// so a test can stand in for the operating system: everything else here
// needs the real clipboard, and a test that took it would take it from
// whoever is using the machine.
var (
	clipOpen = func() error {
		// A window handle of zero asks for the clipboard without owning
		// a window, which is what a program that only pastes needs.
		if ok, _, err := openClipboard.Call(0); ok == 0 {
			return err
		}
		return nil
	}
	clipClose = func() error {
		if ok, _, err := closeClipboard.Call(); ok == 0 {
			return err
		}
		return nil
	}
)

// withClipboard runs f with the clipboard open and gives it back
// afterwards.
//
// The thread is locked for the whole of it. Windows gives the clipboard
// to the thread that opened it rather than to the process, and Go moves
// a goroutine from one thread to another whenever it likes. A close that
// landed on another thread fails, and a picture put on the clipboard is
// then never committed.
//
// That is what happened to a picture sent from another window: it
// arrives on a goroutine of the server's, where nothing holds the
// thread still. A picture pasted at this machine went through the
// goroutine that draws, which ebiten keeps on one thread, so it worked.
func withClipboard(f func() error) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := waitForClipboard(); err != nil {
		return err
	}
	defer func() {
		// Said rather than dropped: a close that failed means whatever
		// was put on the clipboard is not there for anyone else.
		if cerr := clipClose(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("give the clipboard back: %w", cerr))
		}
	}()
	return f()
}

// waitForClipboard opens the clipboard, waiting a moment for another
// program that is holding it.
func waitForClipboard() error {
	var last error
	for waited := time.Duration(0); waited < wait; waited += clipboardTry {
		last = clipOpen()
		if last == nil {
			return nil
		}
		time.Sleep(clipboardTry)
	}
	return fmt.Errorf("open the clipboard: %w", last)
}

// HasText reports whether there is any text on the clipboard.
//
// It is asked before reading, because the library that reads text says
// "the operation completed successfully" when there is none: it hands
// back whatever the last error was, and no error is what happened.
func HasText() bool {
	for _, want := range []uintptr{cfUnicodeText, cfText} {
		if ok, _, _ := isFormatAvail.Call(want); ok != 0 {
			return true
		}
	}
	return false
}

// Image returns the picture on the clipboard.
//
// It reports whether there is one separately from whether reading it
// failed: an empty clipboard is an ordinary thing to meet and a locked
// one is not.
func Image() (image.Image, bool, error) {
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
	var dib []byte
	err := withClipboard(func() error {
		handle, _, err := getClipboardData.Call(format)
		if handle == 0 {
			return fmt.Errorf("read the clipboard: %w", err)
		}
		ptr, _, err := globalLock.Call(handle)
		if ptr == 0 {
			return fmt.Errorf("lock the clipboard: %w", err)
		}
		defer func() { _, _, _ = globalUnlock.Call(handle) }()

		size, _, _ := globalSize.Call(handle)
		if size == 0 {
			return errors.New("the picture on the clipboard is empty")
		}
		raw := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size))
		// Copied out from under the lock, because everything past this
		// point works on it after the clipboard has been given back.
		dib = make([]byte, len(raw))
		copy(dib, raw)
		return nil
	})
	if err != nil {
		return nil, true, err
	}

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
	const headerLeast = headerSize
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

// Putting a picture on the clipboard.
const (
	gmemMoveable = 0x0002
	headerSize   = 40
)

var (
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")

	globalAlloc = kernel32.NewProc("GlobalAlloc")
	globalFree  = kernel32.NewProc("GlobalFree")
)

// SetImage puts a picture on the clipboard, as the bitmap every
// Windows program knows how to read.
func SetImage(img image.Image) error {
	dib := dibFrom(img)

	mem, _, err := globalAlloc.Call(gmemMoveable, uintptr(len(dib)))
	if mem == 0 {
		return fmt.Errorf("make room for the picture: %w", err)
	}
	// Given to the clipboard on success, and freed here on every way
	// out before that.
	handed := false
	defer func() {
		if !handed {
			_, _, _ = globalFree.Call(mem)
		}
	}()
	ptr, _, err := globalLock.Call(mem)
	if ptr == 0 {
		return fmt.Errorf("lock the room for the picture: %w", err)
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(dib)), dib)
	_, _, _ = globalUnlock.Call(mem)

	return withClipboard(func() error {
		if ok, _, err := emptyClipboard.Call(); ok == 0 {
			return fmt.Errorf("empty the clipboard: %w", err)
		}
		if got, _, err := setClipboardData.Call(cfDIB, mem); got == 0 {
			return fmt.Errorf("put the picture on the clipboard: %w", err)
		}
		// The clipboard owns it now, and freeing it would take the
		// picture out from under whoever pastes it.
		handed = true
		return nil
	})
}

// dibFrom lays a picture out as a device independent bitmap: a header
// and then the pixels, bottom row first, thirty-two bits a pixel.
//
// Thirty-two rather than twenty-four so the alpha survives, and rows of
// four bytes a pixel need no padding.
func dibFrom(img image.Image) []byte {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	out := make([]byte, headerSize+width*height*4)
	binary.LittleEndian.PutUint32(out[0:4], headerSize)
	binary.LittleEndian.PutUint32(out[4:8], uint32(int32(width)))
	binary.LittleEndian.PutUint32(out[8:12], uint32(int32(height)))
	binary.LittleEndian.PutUint16(out[12:14], 1)
	binary.LittleEndian.PutUint16(out[14:16], 32)
	binary.LittleEndian.PutUint32(out[16:20], biRGB)
	binary.LittleEndian.PutUint32(out[20:24], uint32(width*height*4))

	at := headerSize
	for y := height - 1; y >= 0; y-- {
		for x := range width {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// Windows writes blue, green, red, alpha, and wants the
			// colours as they are rather than multiplied by the alpha.
			out[at+0] = unmultiply(bl, a)
			out[at+1] = unmultiply(g, a)
			out[at+2] = unmultiply(r, a)
			out[at+3] = uint8(a >> 8)
			at += 4
		}
	}
	return out
}

// unmultiply takes a colour back out of its alpha, for a picture Go
// holds multiplied by it and Windows does not.
func unmultiply(c, a uint32) uint8 {
	if a == 0 {
		return 0
	}
	return uint8(min(c*0xff/a, 0xff))
}
