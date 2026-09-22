package main

import (
	"net"
	"testing"
	"time"

	"github.com/Xpra-org/go-xpra/client"
	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

// The window ids the spike's own display hands out, in creation order.
const (
	mainWindow  ui.WindowID = 1
	popupWindow ui.WindowID = 2
)

// TestSession runs a whole xpra session over a pipe and checks what came
// out of it. No xpra, no network and no display.
//
// The stages are driven by input, the way the fake server is: a click
// opens a menu, a geometry change makes the application reflow. So this
// is the same story the spike tells when it is run by hand, and either
// one failing means the same thing.
func TestSession(t *testing.T) {
	display, server, done := startSession(t)

	// --- the main window, and the four encodings it is painted in ---

	waitFor(t, "the main window to be painted", func() bool {
		return display.count(func(s tally) int { return s.paints }) == mainPaintsExpected
	})
	checkMainPixels(t, display)

	if got := display.count(func(s tally) int { return s.icons }); got != 1 {
		t.Errorf("got %d icons, want 1", got)
	}
	if got := display.count(func(s tally) int { return s.cursors }); got != 1 {
		t.Errorf("got %d cursors, want 1", got)
	}
	if got := display.count(func(s tally) int { return s.popups }); got != 0 {
		t.Errorf("got %d popups before the click, want 0", got)
	}

	// --- focus raises the window ---

	display.send(ui.Focus{Window: mainWindow})
	waitFor(t, "the raise", func() bool {
		return display.count(func(s tally) int { return s.raises }) == 1
	})

	// --- a click opens a menu, which is the case a pane cannot hold ---

	display.clickAt(mainX+mainW/2, mainY+mainH/2)
	waitFor(t, "the menu to open and paint", func() bool {
		return display.count(func(s tally) int { return s.paints }) == mainPaintsExpected+popupPaintsExpected
	})
	checkPopup(t, display)
	checkTheMenuTakesTheClick(t, display)

	// --- a geometry change makes the application reflow ---

	display.send(ui.Configure{
		Window: mainWindow,
		X:      mainX, Y: mainY,
		Width: mainW - 8, Height: mainH,
	})
	waitFor(t, "the reflow", func() bool {
		return display.count(func(s tally) int { return s.bells }) == 1
	})
	waitFor(t, "the server's move", func() bool {
		x, y, _, _ := display.window(t, mainWindow).Geometry()
		return x == movedX && y == movedY
	})
	// One for each window at creation, and one more when the application
	// retitled itself on the reflow.
	if got := display.count(func(s tally) int { return s.titles }); got != 3 {
		t.Errorf("got %d titles, want 3", got)
	}

	// --- the gridterm window is resized, and the server is told ---

	checkTheDesktopSizeIsReported(t, display, server)

	// --- closing it takes both windows with it ---

	display.closeEverything()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("running the client: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the client never stopped")
	}

	if got := display.count(func(s tally) int { return s.windows }); got != 2 {
		t.Errorf("got %d windows, want 2", got)
	}
	if got := display.count(func(s tally) int { return s.destroys }); got != 2 {
		t.Errorf("got %d destroys, want 2: the server owns both windows", got)
	}
	for _, coding := range []string{"BGRX"} {
		if got := display.formatCount(coding); got == 0 {
			t.Errorf("no paints arrived as %s", coding)
		}
	}
}

// checkMainPixels reads the backing store back.
//
// This is the assertion worth keeping when any of it moves into
// gridterm: a texture upload is where a BGRX buffer meets ebiten's RGBA
// one, and nothing above it would notice the two being confused.
func checkMainPixels(t *testing.T, d *display) {
	t.Helper()
	w := d.window(t, mainWindow)

	// The raw BGRX paint, outside every patch.
	wantsColour(t, w, "the raw BGRX base", 0, 0, baseColour.R, baseColour.G, baseColour.B, 0)
	wantsColour(t, w, "the raw BGRX base", mainW-1, mainH-1, baseColour.R, baseColour.G, baseColour.B, 0)

	// PNG and WebP are both lossless, so these are exact. Red and green
	// are the pair that a channel swap turns into each other's
	// neighbours, and blue below is the one it swaps outright.
	wantsColour(t, w, "the PNG patch", pngAtX+1, pngAtY+1, 0xff, 0x00, 0x00, 0)
	wantsColour(t, w, "the WebP patch", webpAtX+1, webpAtY+1, 0x00, 0xff, 0x00, 0)

	// JPEG is the one lossy encoding here, so it is near blue rather
	// than equal to it.
	wantsColour(t, w, "the JPEG patch", jpegAtX+1, jpegAtY+1, 0x00, 0x00, 0xff, 24)

	// One pixel past a patch's right edge must still be the base, or a
	// damage rectangle was painted at the wrong offset.
	wantsColour(t, w, "just past the PNG patch",
		pngAtX+patchSide, pngAtY+1, baseColour.R, baseColour.G, baseColour.B, 0)
}

// checkPopup is the design claim in REMOTE-APPS.md, made into a test: a
// menu is a window of its own, and it hangs past the edge of the window
// that opened it.
func checkPopup(t *testing.T, d *display) {
	t.Helper()
	w := d.window(t, popupWindow)

	w.mu.Lock()
	popup := w.popup
	w.mu.Unlock()
	if !popup {
		t.Error("the menu did not arrive as an override-redirect window")
	}

	x, y, width, _ := w.Geometry()
	if x != popupX || y != popupY {
		t.Errorf("the menu is at %d,%d, want %d,%d", x, y, popupX, popupY)
	}
	if over := x + width - (mainX + mainW); over <= 0 {
		t.Errorf("the menu ends %d pixels inside the main window's right edge; "+
			"it is meant to hang past it, which is why it cannot be clipped to a pane", -over)
	}
	if d.count(func(s tally) int { return s.popups }) != 1 {
		t.Error("the menu was not counted as a popup")
	}
}

// checkTheDesktopSizeIsReported is the reason this module points at a
// fork of go-xpra rather than the released one.
//
// The released client puts the desktop size in the hello and never
// mentions it again. That is survivable for a desktop whose monitors do
// not move, and wrong for gridterm, whose whole desktop is one window
// somebody drags about: the server would go on placing windows, and
// letting remote toolkits place their menus, against a screen that is
// no longer there.
//
// So both have to arrive: the size at connection time, and a fresh one
// every time the window changes.
func checkTheDesktopSizeIsReported(t *testing.T, d *display, server *fakeServer) {
	t.Helper()

	first := server.desktopSizes()
	if len(first) != 1 {
		t.Fatalf("the server heard %d desktop sizes before the resize, want 1 from the hello: %v",
			len(first), first)
	}
	if want := [2]int{startingDesk.Width, startingDesk.Height}; first[0] != want {
		t.Errorf("the hello said the desktop is %v, want %v", first[0], want)
	}

	// The window is dragged narrower. The pane follows it, so the
	// application is told to reflow as well.
	const w, h = 700, 500
	before := d.count(func(s tally) int { return s.moves })
	d.resizeDesktop(w, h)

	waitFor(t, "the server to hear the new desktop size", func() bool {
		return len(server.desktopSizes()) == 2
	})
	if got, want := server.desktopSizes()[1], [2]int{w, h}; got != want {
		t.Errorf("the server heard the desktop is %v, want %v", got, want)
	}
	waitFor(t, "the pane to be told to reflow", func() bool {
		return d.count(func(s tally) int { return s.moves }) > before
	})

	// Reporting the same size again is not news, and a window being
	// dragged reports one on every frame. Proving a packet was *not*
	// sent needs something that was, so a different size follows it: if
	// the repeat went out, the server heard four sizes and not three.
	d.resizeDesktop(w, h)
	d.resizeDesktop(w, h-40)
	waitFor(t, "the size after the repeat", func() bool {
		return len(server.desktopSizes()) >= 3
	})
	got := server.desktopSizes()
	if len(got) != 3 {
		t.Errorf("the server heard %d desktop sizes, want 3: a repeat is not news: %v", len(got), got)
	}
	if want := [2]int{w, h - 40}; got[2] != want {
		t.Errorf("the third size the server heard is %v, want %v", got[2], want)
	}
}

// checkTheMenuTakesTheClick is place.go's rule run against the real
// client: a click where the menu overlaps the window beneath it belongs
// to the menu, and only the stacking order says so.
//
// The unit tests in place_test.go check the same rule against boxes
// made up on the spot. This one checks it against boxes that came off
// the wire, through go-xpra's own window bookkeeping.
func checkTheMenuTakesTheClick(t *testing.T, d *display) {
	t.Helper()
	menu, ok := d.topFloating()
	if !ok {
		t.Fatal("no menu is open")
	}
	x, y, w, h := d.window(t, mainWindow).Geometry()
	over, ok := menu.Box.Intersect(Box{X: x, Y: y, W: w, H: h})
	if !ok {
		t.Fatalf("the menu at %+v does not overlap the main window at %d,%d %dx%d, "+
			"so there is nothing to route", menu.Box, x, y, w, h)
	}

	at, at2 := over.Centre()
	got, found := d.clickAt(at, at2)
	if !found {
		t.Fatalf("the overlap at %d,%d belongs to no window", at, at2)
	}
	if got != menu.ID {
		t.Errorf("the click at %d,%d went to window %d, want the menu %d: "+
			"a menu over a pane has to take the click", at, at2, got, menu.ID)
	}
}

// wantsColour checks one pixel, in the R,G,B a viewer would see. slack
// is how far off each channel may be, for the lossy encoding.
func wantsColour(t *testing.T, w *window, what string, x, y int, r, g, b byte, slack int) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	i := (y*w.width + x) * ui.BytesPerPixel
	if i+3 > len(w.pixels) {
		t.Fatalf("%s: pixel %d,%d is outside a %dx%d window", what, x, y, w.width, w.height)
	}
	// The buffer is B,G,R,X.
	gotB, gotG, gotR := w.pixels[i], w.pixels[i+1], w.pixels[i+2]
	off := func(got, want byte) bool { return int(got)-int(want) > slack || int(want)-int(got) > slack }
	if off(gotR, r) || off(gotG, g) || off(gotB, b) {
		t.Errorf("%s: pixel %d,%d is r=%#02x g=%#02x b=%#02x, want r=%#02x g=%#02x b=%#02x (±%d)",
			what, x, y, gotR, gotG, gotB, r, g, b, slack)
	}
}

// startSession wires a client to the fake server over a pipe.
func startSession(t *testing.T) (*display, *fakeServer, <-chan error) {
	t.Helper()
	serverEnd, clientEnd := net.Pipe()
	server := &fakeServer{}
	go server.serve(protocol.New(serverEnd))

	d := newDisplay(t.TempDir(), false)
	done := make(chan error, 1)
	go func() { done <- client.New(protocol.New(clientEnd), d, false, "", "").Run() }()
	t.Cleanup(d.Close)
	return d, server, done
}

// count reads one number off the tally under the lock.
func (d *display) count(of func(tally) int) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return of(d.seen)
}

func (d *display) formatCount(format string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seen.formats[format]
}

// window returns one of the display's windows, failing if it has gone.
func (d *display) window(t *testing.T, id ui.WindowID) *window {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	w := d.windows[id]
	if w == nil {
		t.Fatalf("there is no window %d", id)
	}
	return w
}

// waitFor polls until the condition holds, because the client runs on
// its own goroutine and there is nothing to synchronise on.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
