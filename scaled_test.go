package main

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui/term"
)

// aHeldScreen is a window serving one shell and a second window, of the
// test's own size, that has taken it over from the dialog.
//
// The pane on the second window sets the size of the shell on the first,
// so a second window bigger than the first is what makes the first draw
// a screen it has no room for.
func aHeldScreen(t *testing.T, cols, rows int) (host, client *testApp, hostPane *term.Terminal) {
	t.Helper()
	host = newTestApp(t, 90, 30)
	withDialogs(t, host)
	withPanel(t, host)
	withScreen(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	hostPane = onlyPaneOn(t, host)

	client = newTestApp(t, cols, rows)
	withDialogs(t, client)
	withPanel(t, client)
	withMenubar(t, client)
	// The host key, so taking the window over does not stop to ask about
	// it.
	if err := os.WriteFile(client.windows.knownAt, []byte(knownLine(t, host)), 0o600); err != nil {
		t.Fatalf("write the known window: %v", err)
	}
	addr := host.serving.addr()
	takeOverFromTheDialog(t, client, addr, keyFile)

	// The shell says something and this window draws it where its own
	// layout put it, so that what the window does once the size is taken
	// is a change rather than a first frame.
	host.shells[0].out <- []byte("before anybody was watching")
	waitFor(t, host, "the shell to say something", func() bool {
		return strings.Contains(screenText(hostPane), "before anybody was watching")
	}, client)
	frame(t, host)

	// And the shell already running over there, watched from the row the
	// sidebar draws for it: that is what gives its size away.
	row := remoteKey{window: windowAt(t, client, addr), id: host.panes[hostPane].ID()}
	waitFor(t, host, "the row for the shell over there", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		_, there := client.openOver(row)
		return there
	}, client)
	attachFromTheSidebar(t, client, row)
	watching := newestPane(t, client)
	waitFor(t, host, "the screen here to take the size of the window watching it", func() bool {
		return hostPane.Held() && hostPane.Size() == watching.Size()
	}, client)
	return host, client, hostPane
}

// withScreen gives a test app a compositor that can draw, so the
// window's own frame runs the way it does in the program.
func withScreen(t *testing.T, a *testApp) {
	t.Helper()
	a.comp = render.NewCompositor(a.renderer)
	a.comp.OnError = func(err error) { t.Errorf("compositor: %v", err) }
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	if a.sideRegion != nil {
		a.comp.Add(a.sideRegion.layer)
	}
}

// frame runs one of the window's frames: the window's own Update, and
// then the draw.
//
// Update itself rather than the same calls written out again. A list
// written out here drifts from the one the window runs, and a step
// added to the window and not to the list is a step no test takes.
func frame(t *testing.T, a *testApp) {
	t.Helper()
	if err := a.Update(); err != nil {
		t.Fatalf("the frame: %v", err)
	}
	if a.screen == nil {
		cw, ch := a.renderer.CellSize()
		cols, rows := a.g.Size()
		a.screen = ebiten.NewImage(cols*cw, rows*ch)
	}
	a.Draw(a.screen)
}

// fillScreen writes a different rune in every cell of a shell's screen,
// so a test can tell one cell of it from another.
func fillScreen(t *testing.T, a *testApp, which int, pane *term.Terminal) {
	t.Helper()
	size := pane.Size()
	var b strings.Builder
	for y := 0; y < size.Rows; y++ {
		// Placed row by row rather than written as one run: a line that
		// reached the last column would wrap, and the last one would
		// scroll the pattern up out of the screen.
		fmt.Fprintf(&b, "\x1b[%d;1H", y+1)
		for x := 0; x < size.Cols; x++ {
			b.WriteRune(cellRune(x, y))
		}
	}
	a.shells[which].out <- []byte(b.String())
	waitFor(t, a, "the shell's screen to hold the pattern", func() bool {
		g := screenOf(pane)
		return g.At(size.Cols-1, size.Rows-1).Rune == cellRune(size.Cols-1, size.Rows-1)
	})
}

// screenText is everything a pane's screen holds, as one string.
func screenText(pane *term.Terminal) string {
	g := screenOf(pane)
	cols, rows := g.Size()
	var b strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if r := g.At(x, y).Rune; r != 0 {
				b.WriteRune(r)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// cellRune is what fillScreen puts in one cell.
func cellRune(x, y int) rune { return rune('0' + (x+y)%10) }

// screenOf is a pane's whole screen, whether or not the window is
// drawing it where the layout put it.
func screenOf(pane *term.Terminal) *grid.Grid {
	size := pane.Size()
	g := grid.New(max(size.Cols, 1), max(size.Rows, 1), color.RGBA{}, color.RGBA{})
	pane.DrawScreen(g.View())
	return g
}

// A screen held bigger than the room the window has for it is drawn
// whole, on a layer of its own, shrunk to fit.
//
// Copied into that room cell by cell, which is what the window did
// before, the columns and rows past it were simply not there.
func TestAHeldScreenTooBigForItsRoomIsDrawnWhole(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)

	s := host.scaled[hostPane]
	if s == nil {
		t.Fatalf("the screen is %v in a box of %v and is not drawn on a layer of its own",
			hostPane.Size(), hostPane.Box())
	}
	if s.layer.Hidden || s.layer.Scale != s.scale || s.scale >= 1 {
		t.Errorf("the layer is hidden=%v at scale %v, want it shown at the %v it was placed at",
			s.layer.Hidden, s.layer.Scale, s.scale)
	}
	if !inStack(host.comp, s.layer) {
		t.Error("the layer is not in the stack, so nothing blits it")
	}

	// The whole screen fits in the room, and fills it one way or the
	// other: a picture that fitted with room to spare in both directions
	// would be smaller than it had to be.
	if s.width > s.boxWidth || s.height > s.boxHeight {
		t.Errorf("the picture is %dx%d pixels in a box of %dx%d",
			s.width, s.height, s.boxWidth, s.boxHeight)
	}
	if s.width < s.boxWidth-1 && s.height < s.boxHeight-1 {
		t.Errorf("the picture is %dx%d pixels and the box is %dx%d, so it could be bigger",
			s.width, s.height, s.boxWidth, s.boxHeight)
	}
	// Centred, so what is left over is a band of the same width at each
	// end rather than all of it at one.
	if got, want := s.left-s.boxLeft, (s.boxWidth-s.width)/2; got != want {
		t.Errorf("the picture starts %d pixels into the box, want %d", got, want)
	}
	if got, want := s.top-s.boxTop, (s.boxHeight-s.height)/2; got != want {
		t.Errorf("the picture starts %d pixels down the box, want %d", got, want)
	}

	// Every cell of the screen is on the layer, including the ones past
	// the corner of the room the window has for it.
	size := hostPane.Size()
	cols, rows := s.g.Size()
	if cols != size.Cols || rows != size.Rows {
		t.Fatalf("the layer's grid is %dx%d, want the screen's %dx%d", cols, rows, size.Cols, size.Rows)
	}
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if got := s.g.At(x, y).Rune; got != cellRune(x, y) {
				t.Fatalf("the layer holds %q at %d,%d, want %q", got, x, y, cellRune(x, y))
			}
		}
	}

	// And the tree does not draw it as well, at the size it does not fit.
	area, shown := host.paneArea(hostPane)
	if !shown {
		t.Fatal("the tree has nowhere for the pane")
	}
	blank := host.g.Blank()
	for y := area.Y; y < area.Y+area.Rows; y++ {
		for x := area.X; x < area.X+area.Cols; x++ {
			if got := host.g.At(x, y); !got.Equal(blank) {
				t.Fatalf("the window painted %q at %d,%d, under the pane's own layer", got.Rune, x, y)
			}
		}
	}

	// The row still says the size, which is the one thing about the
	// screen the user cannot work out from what it draws.
	want := fmt.Sprintf("at %dx%d", size.Cols, size.Rows)
	lines := panelText(host, panelNow)
	if !hasLineWith(lines, want) {
		t.Errorf("the panel says %v, want a row saying %q", lines, want)
	}
}

// Letting go of the size gives the pane back to the tree, and the layer
// goes with it.
func TestLettingGoOfAHeldScreenGivesThePaneBackToTheTree(t *testing.T) {
	host, client, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	if s == nil {
		t.Fatal("the screen is not drawn on a layer of its own to begin with")
	}

	if err := client.closePane(newestPane(t, client)); err != nil {
		t.Fatalf("stop watching: %v", err)
	}
	waitFor(t, host, "the screen to go back to its own size", func() bool {
		return !hostPane.Held()
	}, client)
	frame(t, host)

	if host.scaled[hostPane] != nil {
		t.Error("the pane is still drawn on a layer of its own")
	}
	if inStack(host.comp, s.layer) {
		t.Error("its layer is still in the stack, so the old picture is still blitted")
	}
	if hostPane.Elsewhere() {
		t.Error("the pane still leaves its room blank")
	}
	area, _ := host.paneArea(hostPane)
	blank := host.g.Blank()
	painted := false
	for y := area.Y; y < area.Y+area.Rows && !painted; y++ {
		for x := area.X; x < area.X+area.Cols; x++ {
			if !host.g.At(x, y).Equal(blank) {
				painted = true
				break
			}
		}
	}
	if !painted {
		t.Error("the tree drew nothing where the pane is, so the shell is on screen nowhere")
	}
}

// A held screen that fits the room it is given is drawn in the tree, the
// way it always was. Nothing needs a layer.
func TestAHeldScreenThatFitsIsDrawnInTheTree(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 50, 16)
	frame(t, host)

	size, box := hostPane.Size(), hostPane.Box()
	if size.Cols > box.Cols || size.Rows > box.Rows {
		t.Fatalf("the screen is %v in a box of %v, so this fixture is not the case it is about", size, box)
	}
	if host.scaled[hostPane] != nil {
		t.Error("a screen that fits its room was given a layer of its own")
	}
	if hostPane.Elsewhere() {
		t.Error("a screen that fits its room leaves that room blank")
	}
}

// A click on a scaled screen lands on the cell under the pointer, past
// the corner where the room the window has for it runs out.
func TestAClickOnAScaledScreenLandsOnTheCellUnderIt(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	s := host.scaled[hostPane]
	if s == nil {
		t.Fatal("the screen is not drawn on a layer of its own")
	}

	// A run near the bottom right, well past the room the tree gave the
	// pane: a click routed by the window's cells could not name it.
	size := hostPane.Size()
	row := size.Rows - 2
	from, to := size.Cols-6, size.Cols-2
	if from < hostPane.Box().Cols {
		t.Fatalf("the screen is %v in a box of %v, so this run is inside the box", size, hostPane.Box())
	}

	dragOnScreen(t, host, s, from, row, to, row)

	want := ""
	for x := from; x <= to; x++ {
		want += string(cellRune(x, row))
	}
	if got := hostPane.SelectionText(); got != want {
		t.Errorf("the drag selected %q, want %q: the click did not land where the pointer was", got, want)
	}
	if !hostPane.Focused() {
		t.Error("the click did not move focus to the pane")
	}
}

// dragOnScreen presses on one cell of a scaled screen and releases on
// another.
func dragOnScreen(t *testing.T, a *testApp, s *scaledPane, x0, y0, x1, y1 int) {
	t.Helper()
	onCell(t, a, s, input.MousePress, x0, y0)
	onCell(t, a, s, input.MouseMove, x1, y1)
	onCell(t, a, s, input.MouseRelease, x1, y1)
}

// onCell sends one event on a cell of a scaled screen. A move carries
// the button that is down, the way the window's own mouse reader sends
// it.
func onCell(t *testing.T, a *testApp, s *scaledPane, kind input.MouseKind, x, y int) bool {
	t.Helper()
	px, py := pixelOfCell(s, x, y)
	return mouseAt(t, a, kind, input.MouseLeft, px, py)
}

// mouseAt sends one event at a pixel the way the window's own reader
// does it: the cell the window makes of the pixel, then the event
// through the window's own routing.
func mouseAt(t *testing.T, a *testApp, kind input.MouseKind, button input.MouseButton, px, py int) bool {
	t.Helper()
	col, row := a.cellAt(px, py)
	took, err := a.routeMouse(input.MouseEvent{Kind: kind, Button: button, Col: col, Row: row})
	if err != nil {
		t.Fatalf("%v at %d,%d: %v", kind, px, py, err)
	}
	return took
}

// pixelOfWindowCell is the middle of one of the window's own cells.
func pixelOfWindowCell(a *testApp, col, row int) (px, py int) {
	left, width := a.geo.ColBox(col, col+1)
	top, height := a.geo.RowBox(row, row+1)
	return left + width/2, top + height/2
}

// pixelOfCell is the middle of one cell of a scaled screen, on the
// window.
func pixelOfCell(s *scaledPane, x, y int) (px, py int) {
	left, width := s.geo.ColBox(x, x+1)
	top, height := s.geo.RowBox(y, y+1)
	px = s.left + int(float64(left+width/2)*s.scale)
	py = s.top + int(float64(top+height/2)*s.scale)
	return px, py
}

// A scaled screen with nothing happening on it costs nothing: no row is
// repainted and the screen is not wiped.
func TestAScaledScreenThatIsIdleCostsNothing(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	fillScreen(t, host, 0, hostPane)
	frame(t, host)
	if host.scaled[hostPane] == nil {
		t.Fatal("the screen is not drawn on a layer of its own")
	}
	// Two, because the frame the picture first appeared on moves the
	// layers about and the one after it settles them.
	frame(t, host)

	frame(t, host)

	got := host.comp.Stats()
	if !got.Skipped {
		t.Errorf("an idle frame = %+v, want it skipped entirely", got)
	}
	if got.Repainted != 0 || got.Cleared {
		t.Errorf("an idle frame repainted %d layers and cleared=%v the screen",
			got.Repainted, got.Cleared)
	}
}

// inStack reports whether a compositor is drawing a layer.
func inStack(c *render.Compositor, l *render.Layer) bool {
	for _, have := range c.Layers() {
		if have == l {
			return true
		}
	}
	return false
}

// hasLineWith reports whether any line contains the text.
func hasLineWith(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}
