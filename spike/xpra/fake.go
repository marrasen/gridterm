package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"sync"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
)

// This file is a fake xpra server, richer than the one that ships inside
// go-xpra: that one paints raw RGB into a single window and stops there,
// which leaves everything gridterm actually has to answer for untested.
//
// What it adds, and why each one is here:
//
//   - An override-redirect popup that hangs past the right edge of the
//     window that opened it. That is the case a pane cannot hold, and
//     the reason REMOTE-APPS.md says a remote application is a pane and
//     some floating windows rather than a pane alone.
//   - Frames in PNG, WebP and JPEG as well as raw BGRX, because the
//     server picks the encoding and a client does not get to refuse.
//   - A session cursor and a window icon, which are the two things
//     arriving as PNG in packets of their own.
//   - A server-driven move, a title change and a raise.
//
// Every stage is triggered by something the client sent, so the order is
// the protocol's rather than a sleep: a click opens the menu, and a
// geometry change makes the application reflow and retitle.

// The session's geometry. Small on purpose -- a PNG is written for every
// damage rectangle, and none of this is looked at by eye.
const (
	mainWID  = 7
	popupWID = 8

	mainX, mainY = 200, 150
	mainW, mainH = 64, 48

	// The popup starts eight pixels short of the main window's right
	// edge and runs past it. Clipping it to the pane would cut a menu in
	// half.
	popupX, popupY = mainX + mainW - 8, mainY + 12
	popupW, popupH = 24, 16

	// Where the server moves the main window in the last stage. The size
	// is unchanged, so the pixels painted into it survive and a test can
	// still read them.
	movedX, movedY = 260, 190
)

// The colours each encoding paints, as they look on screen. Chosen so
// that swapping red and blue cannot go unnoticed.
var (
	baseColour = color.RGBA{R: 0x33, G: 0x22, B: 0x11, A: 0xff}
	pngColour  = color.RGBA{R: 0xff, A: 0xff}
	webpColour = color.RGBA{G: 0xff, A: 0xff}
	jpegColour = color.RGBA{B: 0xff, A: 0xff}
)

// Where each compressed damage rectangle lands in the main window.
const (
	patchSide           = 8
	pngAtX, pngAtY      = 4, 4
	webpAtX, webpAtY    = 16, 4
	jpegAtX, jpegAtY    = 28, 4
	compressedPatches   = 3
	mainPaintsExpected  = 1 + compressedPatches
	popupPaintsExpected = 1
)

// Go has no WebP encoder, in the standard library or in x/image, so the
// one frame in that encoding is a file. It is eight by eight, lossless
// and solid green, so the pixels that come out of it are exact.
//
//go:embed testdata/green8.webp
var greenWebP []byte

// fakeServer plays one session. One per connection, so the state in it
// is one session's.
type fakeServer struct {
	mu        sync.Mutex
	popupOpen bool

	// reflowed stops the application reacting to every geometry change.
	// It retitles itself and rings its bell the first time it is
	// resized, the way a program does something once at startup, and
	// after that a resize is just a resize.
	reflowed bool

	// desktops is every desktop size the client has reported, in order.
	// A real server would resize its virtual display to each one; this
	// records them so a test can say the client reported at all.
	desktops [][2]int
}

// desktopSizes returns what the client has said about its desktop.
func (f *fakeServer) desktopSizes() [][2]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]int(nil), f.desktops...)
}

// serve plays one session and returns when the client goes away.
func (f *fakeServer) serve(conn *protocol.Conn) {
	defer conn.Close()

	for packet := range conn.Packets() {
		switch packet.Type() {
		case "hello":
			f.noteDesktopSize(helloDesktopSize(packet))
			log.Printf("fake: <- hello")
			send(conn, "hello", rencodeplus.Dict{
				{Key: "version", Value: "6.6-fake"},
				{Key: "encoding", Value: rencodeplus.Dict{
					{Key: "core", Value: []string{"rgb24", "rgb32", "png", "webp", "jpeg"}},
				}},
			})
			send(conn, "startup-complete")
			sendCursor(conn)
			send(conn, "window-create", mainWID, mainX, mainY, mainW, mainH,
				rencodeplus.Dict{{Key: "title", Value: "fake application"}},
				rencodeplus.Dict{})

		case "window-map":
			// The client maps the window and the server answers with
			// pixels. Override-redirect windows never get here: the
			// client does not map them, and the server damages them
			// itself.
			log.Printf("fake: <- window-map %d", packet.Int(1))
			sendIcon(conn, mainWID)
			paintMain(conn)

		case "pointer-button":
			// A click opens a menu. This is the whole reason the fake
			// server exists.
			if f.popupOpen || !buttonPressed(packet) {
				break
			}
			f.popupOpen = true
			log.Printf("fake: <- a click, opening a menu at %d,%d (%dx%d)",
				popupX, popupY, popupW, popupH)
			send(conn, "new-override-redirect", popupWID, popupX, popupY, popupW, popupH,
				rencodeplus.Dict{{Key: "title", Value: "menu"}},
				rencodeplus.Dict{})
			drawRaw(conn, popupWID, 0, 0, popupW, popupH, color.RGBA{R: 0x80, G: 0x80, B: 0x80}, 10)

		case "window-focus":
			log.Printf("fake: <- window-focus %d, raising it", packet.Int(1))
			send(conn, "window-raise", mainWID)

		case "window-configure":
			// The application reflows: it retitles itself, rings the
			// bell and is moved by the server.
			log.Printf("fake: <- window-configure %v", []any(packet)[1:])
			if f.reflowed {
				break
			}
			f.reflowed = true
			send(conn, "window-metadata", mainWID,
				rencodeplus.Dict{{Key: "title", Value: "fake application -- reflowed"}})
			send(conn, "window-bell", mainWID, 0, 100, 440, 120, "", 0, "bell")
			send(conn, "window-move-resize", mainWID, movedX, movedY, mainW, mainH)

		case "display-configure":
			// The packet the fork adds. A real server resizes its
			// virtual display here, which is what keeps a remote
			// toolkit placing menus somewhere the user can see.
			size, ok := configuredDesktopSize(packet)
			if !ok {
				log.Printf("fake: <- display-configure with no desktop-size: %v", []any(packet))
				break
			}
			f.noteDesktopSize(size, true)
			log.Printf("fake: <- display-configure, the desktop is now %dx%d", size[0], size[1])

		case "window-close":
			log.Printf("fake: <- window-close %d, tearing the session down", packet.Int(1))
			if f.popupOpen {
				send(conn, "window-destroy", popupWID)
			}
			send(conn, "window-destroy", mainWID)
			send(conn, "connection-close", "session over")

		case "window-draw-ack", "ping", "ping_echo", "pointer-motion":
			// Expected traffic, and too noisy to log.

		default:
			log.Printf("fake: <- %v", []any(packet))
		}
	}
	if err := conn.Err(); err != nil {
		log.Printf("fake: connection: %v", err)
	}
}

// buttonPressed reports whether a pointer-button packet is a press.
// The packet is [type, device, seq, wid, button, pressed, position,
// properties].
func buttonPressed(packet protocol.Packet) bool {
	if len(packet) < 6 {
		return false
	}
	pressed, _ := packet[5].(bool)
	return pressed
}

// paintMain sends the main window's pixels: the whole thing as raw BGRX,
// then one patch in each compressed encoding the client advertised.
func paintMain(conn *protocol.Conn) {
	drawRaw(conn, mainWID, 0, 0, mainW, mainH, baseColour, 1)
	drawEncoded(conn, "png", encodePNG(pngColour), pngAtX, pngAtY, 2)
	drawEncoded(conn, "webp", greenWebP, webpAtX, webpAtY, 3)
	drawEncoded(conn, "jpeg", encodeJPEG(jpegColour), jpegAtX, jpegAtY, 4)
}

// drawRaw paints a solid rectangle as raw BGRX.
//
// The rowstride is wider than the pixels it carries, which is what a
// real server sends whenever its rows are padded. A client that assumed
// packed pixels would fail here rather than on somebody's desktop.
func drawRaw(conn *protocol.Conn, wid, x, y, w, h int, c color.RGBA, sequence int) {
	const padding = 12
	stride := w*4 + padding
	pixels := make([]byte, stride*h)
	for row := range h {
		for col := range w {
			i := row*stride + col*4
			pixels[i], pixels[i+1], pixels[i+2] = c.B, c.G, c.R
		}
	}
	send(conn, "window-draw", wid, x, y, w, h, "rgb32", pixels, sequence, stride,
		rencodeplus.Dict{{Key: "rgb_format", Value: "BGRX"}})
	log.Printf("fake: -> window %d, %dx%d at %d,%d, raw BGRX, rowstride %d", wid, w, h, x, y, stride)
}

// drawEncoded paints a patch the client has to decode. The rowstride is
// the decoder's business for these, so it goes out as zero.
func drawEncoded(conn *protocol.Conn, coding string, data []byte, x, y, sequence int) {
	send(conn, "window-draw", mainWID, x, y, patchSide, patchSide, coding, data, sequence, 0,
		rencodeplus.Dict{})
	log.Printf("fake: -> window %d, %dx%d at %d,%d, %s, %d bytes",
		mainWID, patchSide, patchSide, x, y, coding, len(data))
}

// sendCursor sends the one pointer image the session shares.
func sendCursor(conn *protocol.Conn) {
	const side, hotspot = 12, 3
	data := encodePNGSized(color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, side)
	send(conn, "cursor-data", "png", side, side, hotspot, hotspot, 1, data, "left_ptr")
	log.Printf("fake: -> cursor %dx%d, hotspot %d,%d", side, side, hotspot, hotspot)
}

// sendIcon sends one window's application icon.
func sendIcon(conn *protocol.Conn, wid int) {
	const side = 16
	data := encodePNGSized(color.RGBA{R: 0x20, G: 0xc0, B: 0x40, A: 0xff}, side)
	send(conn, "window-icon", wid, side, side, "png", data)
	log.Printf("fake: -> window %d icon, %dx%d", wid, side, side)
}

func encodePNG(c color.RGBA) []byte { return encodePNGSized(c, patchSide) }

func encodePNGSized(c color.RGBA, side int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// encodeJPEG is the one lossy encoding here, so what comes back is near
// the colour rather than equal to it.
func encodeJPEG(c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, patchSide, patchSide))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 0xff
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func send(conn *protocol.Conn, packet ...any) {
	if err := conn.Send(packet...); err != nil {
		log.Printf("fake: sending %v: %v", packet[0], err)
	}
}

// noteDesktopSize records a desktop size the client reported.
func (f *fakeServer) noteDesktopSize(size [2]int, ok bool) {
	if !ok {
		return
	}
	f.mu.Lock()
	f.desktops = append(f.desktops, size)
	f.mu.Unlock()
}

// helloDesktopSize reads the size out of the hello's display namespace,
// which is where released go-xpra already puts it.
func helloDesktopSize(packet protocol.Packet) ([2]int, bool) {
	if len(packet) < 2 {
		return [2]int{}, false
	}
	caps, ok := packet[1].(map[string]any)
	if !ok {
		return [2]int{}, false
	}
	display, ok := caps["display"].(map[string]any)
	if !ok {
		return [2]int{}, false
	}
	return intPair(display["desktop_size"])
}

// configuredDesktopSize reads the size out of a display-configure.
func configuredDesktopSize(packet protocol.Packet) ([2]int, bool) {
	if len(packet) < 2 {
		return [2]int{}, false
	}
	return intPair(packet.Dict(1)["desktop-size"])
}

// intPair reads a two-element list of whole numbers off the wire. The
// encoder is free to pick the narrowest type that holds a value, so this
// takes whatever integer came back.
func intPair(value any) ([2]int, bool) {
	list, ok := value.([]any)
	if !ok || len(list) != 2 {
		return [2]int{}, false
	}
	var pair [2]int
	for i, v := range list {
		switch n := v.(type) {
		case int64:
			pair[i] = int(n)
		case int:
			pair[i] = n
		default:
			return [2]int{}, false
		}
	}
	return pair, true
}
