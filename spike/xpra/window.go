package main

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Xpra-org/go-xpra/ui"

	"github.com/marrasen/gridterm/input"
)

// window is one forwarded window, held as a buffer of pixels.
//
// The layout is B,G,R,X because that is what ui.Convert writes and what
// an X framebuffer, a Windows DIB and a wl_shm buffer all are. ebiten
// wants R,G,B,A, so gridterm would pay one swap per damage rectangle --
// or ask for it the other way round, since the client advertises the
// format it prefers and the server honours it.
type window struct {
	display *display
	id      ui.WindowID

	mu     sync.Mutex
	x, y   int
	width  int
	height int
	popup  bool
	title  string
	pixels []byte

	// frame numbers the PNGs.
	frame int
}

func (w *window) ID() ui.WindowID { return w.id }

func (w *window) Geometry() (x, y, width, height int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.x, w.y, w.width, w.height
}

func (w *window) SetTitle(title string) {
	w.mu.Lock()
	w.title = title
	w.mu.Unlock()
	w.display.mu.Lock()
	w.display.seen.titles++
	w.display.mu.Unlock()
	log.Printf("window %d: title %q", w.id, title)
}

// SetIcon takes the application's own icon. gridterm has somewhere to
// put this already: a pane title carries one.
func (w *window) SetIcon(icon *ui.Icon) error {
	w.display.mu.Lock()
	w.display.seen.icons++
	w.display.mu.Unlock()
	if icon == nil {
		log.Printf("window %d: icon cleared", w.id)
		return nil
	}
	if err := icon.Validate(); err != nil {
		return err
	}
	log.Printf("window %d: icon %dx%d", w.id, icon.Width, icon.Height)
	return nil
}

// Map shows the window. The server paints only after the client says a
// window is on screen, so the first frame follows this.
func (w *window) Map() {
	log.Printf("window %d: mapped", w.id)
	// A popup is never scripted: it is not what the user is typing at,
	// and the client does not map it in the first place.
	if w.display.drive && !w.popup && w.display.takeTheWheel() {
		go w.script()
	}
}

func (w *window) Raise() {
	w.display.mu.Lock()
	w.display.seen.raises++
	w.display.stack.Raise(w.id)
	w.display.mu.Unlock()
	log.Printf("window %d: raised", w.id)
}

func (w *window) Minimize(minimized bool) {
	log.Printf("window %d: minimized=%v", w.id, minimized)
}

func (w *window) Destroy() {
	log.Printf("window %d: destroyed by the server", w.id)
	w.display.mu.Lock()
	delete(w.display.windows, w.id)
	w.display.stack.Remove(w.id)
	w.display.seen.destroys++
	w.display.mu.Unlock()
}

// MoveResize is the server moving the window. Reallocating on a resize
// loses the old pixels, which is right: the server repaints everything
// it wants kept, and holding stale pixels only shows them stretched.
func (w *window) MoveResize(x, y, width, height int) error {
	log.Printf("window %d: server moved it to %dx%d at %d,%d", w.id, width, height, x, y)
	return w.resize(x, y, width, height)
}

// Resized records a change the desktop made itself. gridterm calls this
// when a pane is split or the window is dragged wider, and then reports
// the same geometry back with a Configure event.
func (w *window) Resized(x, y, width, height int) error {
	log.Printf("window %d: desktop resized it to %dx%d at %d,%d", w.id, width, height, x, y)
	return w.resize(x, y, width, height)
}

func (w *window) resize(x, y, width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("window %d: refusing a %dx%d geometry", w.id, width, height)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.x, w.y = x, y
	if width != w.width || height != w.height {
		w.width, w.height = width, height
		w.pixels = make([]byte, width*height*ui.BytesPerPixel)
	}
	w.display.mu.Lock()
	w.display.seen.moves++
	w.display.stack.MoveResize(w.id, Box{X: x, Y: y, W: width, H: height})
	w.display.mu.Unlock()
	return nil
}

// Paint takes one damage rectangle.
//
// rowstride is the server's, not ours: it rounds up to four bytes and
// is free to pad further, so the payload is never assumed to be tightly
// packed. ui.Convert does the whole job -- bounds, stride and format --
// and is the reason a backend has no pixel arithmetic in it.
func (w *window) Paint(x, y, width, height int, pixels []byte, rowstride int, format string) error {
	w.mu.Lock()
	stride := w.width * ui.BytesPerPixel
	if x < 0 || y < 0 || x+width > w.width || y+height > w.height {
		w.mu.Unlock()
		return fmt.Errorf("window %d: %dx%d at %d,%d falls outside %dx%d",
			w.id, width, height, x, y, w.width, w.height)
	}
	at := y*stride + x*ui.BytesPerPixel
	err := ui.Convert(w.pixels[at:], stride, pixels, rowstride, width, height, format)
	w.mu.Unlock()
	if err != nil {
		return err
	}

	w.display.mu.Lock()
	w.display.seen.paints++
	w.display.seen.pixels += int64(width) * int64(height)
	w.display.seen.formats[format]++
	w.display.mu.Unlock()

	log.Printf("window %d: paint %dx%d at %d,%d, %s, rowstride %d (%d bytes for %d pixels)",
		w.id, width, height, x, y, format, rowstride, len(pixels), width*height)
	return w.writePNG()
}

// writePNG dumps the whole window as it now stands. One file per damage
// rectangle: the point of the spike is to see what arrived and in what
// order, and a real client would upload the rectangle to a texture and
// leave the rest alone.
func (w *window) writePNG() error {
	w.mu.Lock()
	n := w.frame
	w.frame++
	img := image.NewNRGBA(image.Rect(0, 0, w.width, w.height))
	for i := 0; i+3 < len(w.pixels); i += 4 {
		b, g, r := w.pixels[i], w.pixels[i+1], w.pixels[i+2]
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 0xff
	}
	w.mu.Unlock()

	path := filepath.Join(w.display.dir, fmt.Sprintf("window%d-%03d.png", w.id, n))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	log.Printf("window %d: wrote %s", w.id, path)
	return nil
}

// script pretends to be a user, so the other direction of the protocol
// is exercised too. The server's log is where the result shows up.
//
// The order is the one a real desktop produces: focus, then the pointer
// arriving, then a click, then a keystroke with its release, then the
// window being resized under it.
func (w *window) script() {
	time.Sleep(500 * time.Millisecond)

	x, y, width, height := w.Geometry()
	box := Box{X: x, Y: y, W: width, H: height}
	midX, midY := box.Centre()

	log.Printf("--- sending scripted input ---")
	w.display.send(ui.Focus{Window: w.id})

	// A click in the middle of the window, or wherever -click aimed it.
	// Against the fake server this opens a menu; against a real one it
	// does whatever the program does there.
	if at := w.display.click; at != nil {
		midX, midY = at.X, at.Y
	}
	w.display.clickAt(midX, midY)

	// If a menu did open, click where it overlaps this window. That
	// click belongs to the menu, and nothing but the stacking order
	// says so -- it is the case place.go exists for, checked here
	// against the real client rather than only in its own test.
	time.Sleep(200 * time.Millisecond)
	w.clickThroughAnyMenu(box)

	// Typing, through the same translation gridterm would use. The
	// default exercises both halves of it: named keys for the letters
	// and the space, and text for the digits and the punctuation, which
	// gridterm does not name.
	w.typeText(w.display.text)

	// A pane being split down the middle is this. Only the event goes
	// out: the client calls Resized itself when it handles a Configure,
	// and it compares against Geometry() first, so a backend that
	// applied the change before announcing it gets silently ignored.
	//
	// Half the width rather than a fixed number of pixels, so it means
	// the same thing against a 64-pixel fake window and a real one.
	time.Sleep(200 * time.Millisecond)
	w.display.send(ui.Configure{Window: w.id, X: x, Y: y, Width: max(width/2, 16), Height: height})

	// And the window everything is drawn in being dragged narrower.
	// Two things go out: the desktop size, so the server resizes its
	// virtual display and a remote toolkit keeps placing menus somewhere
	// visible, and a Configure for the pane so the application reflows.
	time.Sleep(200 * time.Millisecond)
	w.display.resizeDesktop(startingDesk.Width*3/4, startingDesk.Height*3/4)

	log.Printf("--- scripted input done ---")
}

// clickThroughAnyMenu clicks where an open menu overlaps the window
// under it, and complains if the click lands on the wrong one.
func (w *window) clickThroughAnyMenu(under Box) {
	menu, ok := w.display.topFloating()
	if !ok {
		log.Printf("no menu opened, so there is nothing to click through")
		return
	}
	over, ok := menu.Box.Intersect(under)
	if !ok {
		log.Printf("the menu at %+v does not overlap the window at %+v", menu.Box, under)
		return
	}
	x, y := over.Centre()
	got, found := w.display.clickAt(x, y)
	switch {
	case !found:
		log.Printf("the overlap at %d,%d belongs to no window", x, y)
	case got != menu.ID:
		log.Printf("WRONG: the click at %d,%d went to window %d, not the menu %d",
			x, y, got, menu.ID)
	default:
		log.Printf("the click at %d,%d went to the menu, over the window beneath it", x, y)
	}
}

// typeText pretends somebody typed a string.
//
// It goes through keys.go rather than building ui.Key values directly,
// so that running the spike against a real server tests the mapping
// gridterm would rely on and not a hand-written stand-in.
func (w *window) typeText(text string) {
	if text == "" {
		return
	}
	log.Printf("typing %q", text)
	board := newKeyboard()
	var source input.Source
	for _, r := range text {
		source++
		for _, event := range typedAs(r, source) {
			for _, key := range board.handle(event, w.id) {
				w.display.send(key)
			}
		}
		// Slowly enough that a real application's own key handling and
		// repaint keep up, and the screenshot at the end shows the lot.
		time.Sleep(25 * time.Millisecond)
	}
}

// typedAs is what gridterm's input layer reports for one typed
// character: a key transition, the text it produced, and the release,
// all sharing a Source.
//
// The key is named only when gridterm has a name for it. The digits
// past zero and every piece of punctuation have none, so they arrive as
// text alone -- which is the case that would be silently missed by a
// test written only against the letters.
func typedAs(r rune, source input.Source) []input.Event {
	key, mods := input.KeyNone, input.Mods(0)
	switch {
	case r >= 'a' && r <= 'z':
		key = input.KeyA + input.Key(r-'a')
	case r >= 'A' && r <= 'Z':
		key, mods = input.KeyA+input.Key(r-'A'), input.ModShift
	case r == ' ':
		key = input.KeySpace
	case r == '0':
		key = input.Key0
	}
	if _, shifted := expShifted[r]; shifted {
		// A shifted symbol: the user held shift to type it, and
		// gridterm would report that.
		mods = input.ModShift
	}
	return []input.Event{
		{Kind: input.KeyPress, Key: key, Mods: mods, Source: source},
		{Kind: input.Text, Rune: r, Mods: mods, Source: source, NormalText: true},
		{Kind: input.KeyRelease, Key: key, Mods: mods, Source: source},
	}
}
