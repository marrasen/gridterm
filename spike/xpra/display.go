package main

import (
	"image"
	"log"
	"sync"

	"github.com/Xpra-org/go-xpra/ui"

	"github.com/marrasen/gridterm/input"
)

// display is a desktop that draws nothing.
//
// go-xpra's client state machine talks to the desktop through ui.Display
// and ui.Window and nothing else, so this file is the whole of what
// gridterm would have to write: five methods here and ten on a window.
// Everything above it -- the handshake, the packet decoding, the damage
// sequencing, the draw acknowledgements the server waits for -- is the
// library's.
type display struct {
	dir   string
	drive bool

	// text is what the scripted input types, and empty for none.
	text string

	// layoutName is the XKB layout the server is asked to load.
	layoutName string

	// board is the shared text clipboard, and copy is what this side
	// announces having copied once it is connected.
	board  *clipboard
	copy   string
	chords []input.Event

	// click aims the scripted click, for a real application whose menus
	// are not in the middle of its window. Nil clicks the middle.
	click *image.Point

	events chan ui.Event

	mu      sync.Mutex
	windows map[ui.WindowID]*window
	nextID  ui.WindowID

	// desk and stack are the coordinate scheme in place.go, kept up to
	// date from the session so that it is exercised against the real
	// client rather than only in its own tests. gridterm would own
	// exactly these two.
	desk  Desk
	stack Stack

	// driven records that the scripted input has run. It belongs to the
	// session and not to a window: a real application opens more than
	// one top-level window -- an xpra session with xclock in it also
	// carries a notification window -- and running the script once per
	// window sends everything twice.
	driven bool

	// closed guards the events channel, which the client reads until it
	// is closed and several goroutines may want to close.
	closed bool

	// seen counts what arrived, for the report at the end.
	seen tally
}

// tally is what one run saw, so the run says something even when the
// PNGs are never looked at.
type tally struct {
	windows  int
	paints   int
	pixels   int64
	formats  map[string]int
	titles   int
	icons    int
	cursors  int
	moves    int
	bells    int
	raises   int
	destroys int
	popups   int
}

// deskMargin is how much of the window is not the pane, so that a pane
// and the desktop it is in are never accidentally the same rectangle.
// A real grid would have several panes and this would be the layout.
const deskMargin = 20

// startingDesk is how big the spike says the window it draws in is. It
// stands in for the gridterm window.
var startingDesk = Desk{Width: 1024, Height: 768}

// paneFor is the one pane's box in a desktop of a given size. A grid
// would work this out from its layout; here it is the desktop inset by
// a margin, which is enough to tell the two apart.
func paneFor(d Desk) Box {
	return Box{
		X: deskMargin, Y: deskMargin,
		W: d.Width - 2*deskMargin, H: d.Height - 2*deskMargin,
	}
}

func newDisplay(dir string, drive bool) *display {
	return &display{
		desk:    startingDesk,
		board:   &clipboard{},
		dir:     dir,
		drive:   drive,
		events:  make(chan ui.Event, 64),
		windows: map[ui.WindowID]*window{},
		nextID:  1,
		seen:    tally{formats: map[string]int{}},
	}
}

// NewWindow makes an unmapped window. The geometry is the content area
// in screen coordinates: xpra places windows on a desktop it believes
// in, which is the first thing gridterm has to decide what to do with.
func (d *display) NewWindow(x, y, width, height int, overrideRedirect bool) (ui.Window, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	w := &window{
		display: d,
		id:      d.nextID,
		x:       x,
		y:       y,
		width:   width,
		height:  height,
		popup:   overrideRedirect,
		pixels:  make([]byte, width*height*ui.BytesPerPixel),
	}
	d.nextID++
	d.windows[w.id] = w
	d.stack.Add(Window{
		ID:       w.id,
		Box:      Box{X: x, Y: y, W: width, H: height},
		Floating: overrideRedirect,
	})
	d.seen.windows++
	if overrideRedirect {
		d.seen.popups++
	}

	kind := "window"
	if overrideRedirect {
		kind = "override-redirect popup"
	}
	log.Printf("window %d: new %s, %dx%d at %d,%d", w.id, kind, width, height, x, y)
	return w, nil
}

func (d *display) Events() <-chan ui.Event { return d.events }

// DesktopSize is how big the window gridterm draws in is, which the
// client puts in the hello so the server sizes its virtual display to
// match. Without it the server picks its own and every window the
// remote window manager places lands somewhere unrelated to the grid.
func (d *display) DesktopSize() (int, int, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.desk.Width, d.desk.Height, true
}

// resizeDesktop is the gridterm window being resized.
//
// Two things go out, and both have to. The desktop size, so the server
// resizes its virtual display and a remote toolkit keeps placing its
// menus somewhere visible. Then a Configure for every pane whose box
// moved, so each application reflows to the pane it is now in.
//
// Released go-xpra sends the first of those once and never again; the
// fork this module points at is what makes it possible at all.
func (d *display) resizeDesktop(width, height int) {
	d.mu.Lock()
	pane := paneFor(Desk{Width: width, Height: height})
	panes := map[ui.WindowID]Box{}
	for _, w := range d.stack.Windows() {
		if !w.Floating {
			panes[w.ID] = pane
		}
	}
	next, moved := d.desk.Resize(width, height, &d.stack, panes)
	d.desk = next
	d.mu.Unlock()

	log.Printf("the window is now %dx%d; %d window(s) follow it", width, height, len(moved))
	d.send(ui.DesktopResized{Width: width, Height: height})
	for _, w := range moved {
		box := w.Box
		// Ask for a size the application will actually take. The server
		// rounds it down either way; doing the sum here is the
		// difference between a pane that fits its window and one that
		// draws a strip the window never paints.
		if win := d.windowFor(w.ID); win != nil {
			box.W, box.H = win.fit(box.W, box.H)
		}
		d.send(ui.Configure{
			Window: w.ID,
			X:      box.X, Y: box.Y,
			Width: box.W, Height: box.H,
		})
	}
}

func (d *display) Bell(percent, pitch, duration int64, name string) {
	d.mu.Lock()
	d.seen.bells++
	d.mu.Unlock()
	log.Printf("bell: %d%% at %dHz for %dms %q", percent, pitch, duration, name)
}

// SetCursor takes the one pointer image the whole session shares. xpra
// sends a cursor per session rather than per window, so gridterm would
// hang it off the connection and not off a pane.
func (d *display) SetCursor(cursor *ui.Cursor) error {
	d.mu.Lock()
	d.seen.cursors++
	d.mu.Unlock()
	if cursor == nil {
		log.Printf("cursor: back to the default")
		return nil
	}
	log.Printf("cursor: %dx%d, hotspot %d,%d, %d bytes",
		cursor.Width, cursor.Height, cursor.HotspotX, cursor.HotspotY, len(cursor.Pixels))
	return nil
}

func (d *display) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	close(d.events)
}

// send offers one event to the client, dropping it if the desktop has
// gone. A backend that blocked here would deadlock the client, which is
// reading this channel and the network in the same select.
func (d *display) send(ev ui.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	select {
	case d.events <- ev:
	default:
		log.Printf("dropped %T: the client is not keeping up", ev)
	}
}

// closeEverything asks the server to close every window, which is how a
// session ends politely: nothing is destroyed locally until the server
// says so.
func (d *display) closeEverything() {
	d.mu.Lock()
	ids := make([]ui.WindowID, 0, len(d.windows))
	for id := range d.windows {
		ids = append(ids, id)
	}
	d.mu.Unlock()

	if len(ids) == 0 {
		d.Close()
		return
	}
	for _, id := range ids {
		log.Printf("window %d: asking the server to close it", id)
		d.send(ui.CloseRequest{Window: id})
	}
}

// report prints what the session carried.
func (d *display) report() {
	d.mu.Lock()
	defer d.mu.Unlock()
	t := d.seen
	log.Printf("session over: %d windows (%d of them popups), %d paints, %d pixels",
		t.windows, t.popups, t.paints, t.pixels)
	log.Printf("  %d moves, %d raises, %d destroys, %d titles, %d icons, %d cursors, %d bells",
		t.moves, t.raises, t.destroys, t.titles, t.icons, t.cursors, t.bells)
	if text, taken := d.board.Text(); taken > 0 {
		log.Printf("  the far side copied %d time(s), last %q", taken, text)
	}
	for format, n := range t.formats {
		log.Printf("  %d paints in %s", n, format)
	}
	log.Printf("frames are in %s/", d.dir)
}

// clickAt presses and releases the left button at a point in the
// gridterm window, on whichever window the stack says is there.
//
// This is the routing gridterm would have to do, run against the real
// client: a menu hanging over a pane that is not its own has to take
// the click, and nothing but the stacking order decides that.
//
// The point goes out unchanged, because under this scheme the gridterm
// window is the desktop xpra believes in.
func (d *display) clickAt(x, y int) (ui.WindowID, bool) {
	d.mu.Lock()
	id, px, py, ok := d.stack.At(x, y)
	d.mu.Unlock()
	if !ok {
		log.Printf("click at %d,%d: no window there", x, y)
		return 0, false
	}
	log.Printf("click at %d,%d: window %d", px, py, id)
	d.send(ui.Motion{Window: id, X: px, Y: py})
	d.send(ui.Button{Window: id, Button: 1, Pressed: true, X: px, Y: py})
	d.send(ui.Button{Window: id, Button: 1, Pressed: false, X: px, Y: py})
	return id, true
}

// topFloating is the frontmost menu or dialog, under the lock.
func (d *display) topFloating() (Window, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stack.TopFloating()
}

// takeTheWheel reports whether this call is the one that runs the
// scripted input, so that it runs once per session.
func (d *display) takeTheWheel() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.driven {
		return false
	}
	d.driven = true
	return true
}

// windowFor finds a window by id, and nil when it has gone.
func (d *display) windowFor(id ui.WindowID) *window {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.windows[id]
}
