package main

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

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

	// frame and icons number the PNGs.
	frame int
	icons int

	// limits are what the application said about its own size. A
	// terminal resizes in whole character cells, so a pane's exact
	// pixel size is usually not one it will take.
	limits ui.SizeConstraints
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
	log.Printf("window %d: icon %dx%d%s", w.id, icon.Width, icon.Height, premultiplied(icon.Pixels))
	return w.writeIcon(icon)
}

// writeIcon dumps an icon beside the frames, so that what arrived can
// be looked at rather than counted.
//
// The pixels are alpha-premultiplied BGRA, which is what Go's own
// image.RGBA holds once the blue and red ends are swapped -- so the PNG
// encoder needs no un-premultiplying and a wrong assumption about that
// would show up as a dark halo round the edges.
func (w *window) writeIcon(icon *ui.Icon) error {
	w.mu.Lock()
	n := w.icons
	w.icons++
	w.mu.Unlock()

	img := image.NewRGBA(image.Rect(0, 0, icon.Width, icon.Height))
	for i := 0; i+3 < len(icon.Pixels); i += 4 {
		b, g, r, a := icon.Pixels[i], icon.Pixels[i+1], icon.Pixels[i+2], icon.Pixels[i+3]
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, a
	}

	path := filepath.Join(w.display.dir, fmt.Sprintf("window%d-icon-%d.png", w.id, n))
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

	// Announce the local clipboard, if there is one to announce. The
	// far application can then paste it.
	w.display.copyLocally(w.display.copy)

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
	w.sendChords(w.display.chords)

	// A pane being split down the middle is this. Only the event goes
	// out: the client calls Resized itself when it handles a Configure,
	// and it compares against Geometry() first, so a backend that
	// applied the change before announcing it gets silently ignored.
	//
	// Half the width rather than a fixed number of pixels, so it means
	// the same thing against a 64-pixel fake window and a real one.
	time.Sleep(200 * time.Millisecond)
	half, tall := w.fit(max(width/2, 16), height)
	w.display.send(ui.Configure{Window: w.id, X: x, Y: y, Width: half, Height: tall})

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
	if shiftedOnAUSKeyboard(r) {
		// A shifted symbol: the user held shift to type it, and
		// gridterm would report that. Which symbols need shift depends
		// on the layout in front of the user, and this stand-in assumes
		// the usual one -- it is the scripted typist guessing, not the
		// translation.
		mods = input.ModShift
	}
	return []input.Event{
		{Kind: input.KeyPress, Key: key, Mods: mods, Source: source},
		{Kind: input.Text, Rune: r, Mods: mods, Source: source, NormalText: true},
		{Kind: input.KeyRelease, Key: key, Mods: mods, Source: source},
	}
}

// shiftedOnAUSKeyboard reports whether a character is typed with shift.
//
// Only the scripted typist needs this, and it is a guess: the real
// gridterm is told which modifiers were actually held. Capitals are
// unicode's, not ASCII's, or the guess loses every accented capital --
// which is how "Öland" first arrived as "öland".
func shiftedOnAUSKeyboard(r rune) bool {
	if unicode.IsUpper(r) {
		return true
	}
	return strings.ContainsRune(`~!@#$%^&*()_+{}|:"<>?`, r)
}

// SetSizeConstraints takes the limits the application put on its size.
//
// This is the optional half of ui.Window. Without it the server applies
// them anyway and does not reliably say so -- one correction arrives as
// a geometry change, the next only shows up in the size of the next
// damage rectangle -- and the pane ends up drawing a rectangle the
// window does not fill.
func (w *window) SetSizeConstraints(c ui.SizeConstraints) {
	w.mu.Lock()
	w.limits = c
	w.mu.Unlock()
	if c.Empty() {
		return
	}
	log.Printf("window %d: size limits, min %dx%d, base %dx%d, steps of %dx%d",
		w.id, c.MinWidth, c.MinHeight, c.BaseWidth, c.BaseHeight, c.IncWidth, c.IncHeight)
}

// fit rounds a size to one this window will accept.
func (w *window) fit(width, height int) (int, int) {
	w.mu.Lock()
	limits := w.limits
	w.mu.Unlock()
	return limits.Fit(width, height)
}

// premultiplied describes whether pixels keep the promise ui.Icon makes
// about them, for the log line.
//
// In alpha-premultiplied pixels no colour channel can exceed the alpha:
// a pixel half transparent has had its colour halved already. A decoder
// that handed over straight alpha instead would break that, and the
// only sign on screen would be a pale fringe round the icon -- easy to
// miss and easy to blame on the artwork.
func premultiplied(pixels []byte) string {
	var translucent, broken int
	for i := 0; i+3 < len(pixels); i += 4 {
		a := pixels[i+3]
		if a == 0xff {
			continue
		}
		translucent++
		if pixels[i] > a || pixels[i+1] > a || pixels[i+2] > a {
			broken++
		}
	}
	switch {
	case translucent == 0:
		return ", fully opaque"
	case broken > 0:
		return fmt.Sprintf(", %d of %d translucent pixels are NOT premultiplied", broken, translucent)
	default:
		return fmt.Sprintf(", %d translucent pixels, all premultiplied", translucent)
	}
}
