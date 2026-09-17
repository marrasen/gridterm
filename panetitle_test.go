package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// aTitledWindow is a window whose settings are in a file the test owns.
func aTitledWindow(t *testing.T) *testApp {
	t.Helper()
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if _, err := withSettings(t, a, filepath.Join(t.TempDir(), "settings.json")); err != nil {
		t.Fatalf("settings: %v", err)
	}
	return a
}

// paneRow is what a pane draws on one of its rows.
func titleRow(t *testing.T, pane *term.Terminal, row int) string {
	t.Helper()
	lines := strings.Split(paneText(pane), "\n")
	if row >= len(lines) {
		t.Fatalf("the pane drew %d rows", len(lines))
	}
	return strings.TrimRight(lines[row], " ")
}

// Nothing is above a pane until the user asks for it.
func TestAPaneHasNoLineAboveItUntilItIsAskedFor(t *testing.T) {
	a := aTitledWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	a.refreshCaptions()

	if got := pane.Caption(); got != "" {
		t.Errorf("the pane says %q above itself", got)
	}
	if got := pane.Size().Rows; got != 24 {
		t.Errorf("the program has %d rows, want the whole pane", got)
	}
}

// Turned on, the line names the machine and what the program calls
// itself, and the program gets one row fewer.
func TestTheLineNamesTheMachineAndTheProgram(t *testing.T) {
	a := aTitledWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	was := pane.Size().Rows
	a.panes[pane].Label = "bash"

	if err := a.togglePaneTitles(); err != nil {
		t.Fatalf("turn them on: %v", err)
	}
	a.refreshCaptions()

	if got := pane.Caption(); !strings.Contains(got, "bash") {
		t.Errorf("the line says %q, want what the program calls itself", got)
	}
	if got := pane.Caption(); !strings.HasPrefix(got, "Local: ") {
		t.Errorf("the line says %q, want the machine first", got)
	}
	if got := pane.Size().Rows; got != was-1 {
		t.Errorf("the program has %d rows, want the %d it had less the line", got, was)
	}
}

// The line is drawn above the screen, and the screen starts below it.
func TestTheLineIsDrawnAboveTheScreen(t *testing.T) {
	a := aTitledWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	a.panes[pane].Label = "bash"
	if err := a.togglePaneTitles(); err != nil {
		t.Fatalf("turn them on: %v", err)
	}
	a.refreshCaptions()
	a.relayout()
	a.shells[0].out <- []byte("the first line the program printed")
	waitFor(t, a, "the program to print", func() bool {
		return strings.Contains(paneText(pane), "the first line")
	})

	if got := titleRow(t, pane, 0); !strings.Contains(got, "bash") {
		t.Errorf("the first row says %q, want the line naming the pane", got)
	}
	if got := titleRow(t, pane, 1); !strings.Contains(got, "the first line") {
		t.Errorf("the second row says %q, want what the program printed", got)
	}
}

// Turning them off gives the row back.
func TestTurningTheLinesOffGivesTheRowBack(t *testing.T) {
	a := aTitledWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	was := pane.Size().Rows
	if err := a.togglePaneTitles(); err != nil {
		t.Fatalf("turn them on: %v", err)
	}
	a.refreshCaptions()
	if pane.Size().Rows != was-1 {
		t.Fatal("the line took no row, so turning it off proves nothing")
	}

	if err := a.togglePaneTitles(); err != nil {
		t.Fatalf("turn them off: %v", err)
	}
	a.refreshCaptions()

	if got := pane.Size().Rows; got != was {
		t.Errorf("the program has %d rows, want the %d it started with", got, was)
	}
	if got := pane.Caption(); got != "" {
		t.Errorf("the pane still says %q above itself", got)
	}
}

// The answer survives a restart: it is a setting, not a thing to turn
// on again every time.
func TestTheLinesAreRememberedBetweenRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := a.togglePaneTitles(); err != nil {
		t.Fatalf("turn them on: %v", err)
	}

	again, err := settings.Load(path)
	if err != nil {
		t.Fatalf("read it again: %v", err)
	}
	if !again.PaneTitles() {
		t.Error("the file does not say the lines are on")
	}
}

// A setting that cannot be written is said so, and nothing changes.
func TestALineThatCannotBeRememberedIsRefused(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	was := pane.Size().Rows
	a.paneTitles.remember(settings.Unusable(errors.New("the settings file is unreadable")))

	err := a.togglePaneTitles()

	if err == nil {
		t.Fatal("it said nothing about a setting it could not write")
	}
	a.refreshCaptions()
	if got := pane.Size().Rows; got != was {
		t.Errorf("the program has %d rows, want the %d it had", got, was)
	}
}

// A press on the line does nothing, rather than starting a selection in
// the program's first row.
func TestAPressOnTheLineSelectsNothing(t *testing.T) {
	a, pane := aTitledPane(t)
	fillScreen(t, a, 0, pane)

	// Pressed on the line and dragged down into the screen: a press the
	// line did not take would leave the drag anchored above the first
	// row and select everything down to here.
	for _, ev := range []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseLeft, Col: 3, Row: 0},
		{Kind: input.MouseMove, Button: input.MouseLeft, Col: 5, Row: 5},
		{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 5, Row: 5},
	} {
		if _, err := pane.HandleMouse(ev); err != nil {
			t.Fatalf("%v on the line: %v", ev.Kind, err)
		}
	}

	if got := pane.SelectionText(); got != "" {
		t.Errorf("the press on the line selected %q", got)
	}
}

// A press below the line lands on the cell under it, not one row up.
func TestAPressBelowTheLineLandsWhereItIsPointed(t *testing.T) {
	a, pane := aTitledPane(t)
	fillScreen(t, a, 0, pane)

	dragAcross(t, pane, 5)

	// Row 5 of the pane is row 4 of the screen: the line above took one.
	want := string([]rune{cellRune(3, 4), cellRune(4, 4), cellRune(5, 4)})
	if got := pane.SelectionText(); got != want {
		t.Errorf("the press selected %q, want %q from the row under it", got, want)
	}
}

// aTitledPane is a window showing the line above its one pane.
func aTitledPane(t *testing.T) (*testApp, *term.Terminal) {
	t.Helper()
	a := aTitledWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	a.panes[pane].Label = "bash"
	if err := a.togglePaneTitles(); err != nil {
		t.Fatalf("turn them on: %v", err)
	}
	a.refreshCaptions()
	a.relayout()
	return a, pane
}

// dragAcross selects three cells along one row of a pane, the way a
// pointer does.
func dragAcross(t *testing.T, pane *term.Terminal, row int) {
	t.Helper()
	for _, ev := range []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseLeft, Col: 3, Row: row},
		{Kind: input.MouseMove, Button: input.MouseLeft, Col: 5, Row: row},
		{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 5, Row: row},
	} {
		if _, err := pane.HandleMouse(ev); err != nil {
			t.Fatalf("%v on row %d: %v", ev.Kind, row, err)
		}
	}
}

// A pane with no room to spare keeps its one row for the program.
func TestAPaneWithOneRowKeepsItForTheProgram(t *testing.T) {
	a, pane := aTitledPane(t)
	a.shells[0].out <- []byte("what the program printed")
	waitFor(t, a, "the program to print", func() bool {
		return strings.Contains(paneText(pane), "what the program")
	})

	pane.Layout(ui.Size{Cols: 40, Rows: 1})

	if got := pane.Size().Rows; got != 1 {
		t.Errorf("the program has %d rows in a pane one row tall", got)
	}
	// And that row shows the program rather than the line naming it: a
	// pane with nowhere to put the program is not a pane.
	if got := titleRow(t, pane, 0); !strings.Contains(got, "what the program") {
		t.Errorf("the one row says %q, want what the program printed", got)
	}
}

// A screen whose size belongs to somebody watching is not squeezed to
// make room: the line goes rather than the watcher's bottom row.
func TestAHeldScreenKeepsItsSizeAndLosesTheLine(t *testing.T) {
	_, pane := aTitledPane(t)
	if got := pane.Caption(); got == "" {
		t.Fatal("the pane has no line, so taking it away proves nothing")
	}

	// Somebody watching takes the size, at the whole of the pane's room.
	pane.Hold(pane.Box().Cols, pane.Box().Rows)

	if got := pane.Size().Rows; got != pane.Box().Rows {
		t.Errorf("the screen is %d rows, want the %d the watcher asked for",
			got, pane.Box().Rows)
	}
	if got := titleRow(t, pane, 0); strings.Contains(got, "bash") {
		t.Errorf("the first row says %q, and the screen needs every row", got)
	}
}

// And turning the line on while it is held does not take the size back
// from the watcher.
func TestTurningTheLineOnDoesNotTakeAHeldSize(t *testing.T) {
	a := aTitledWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	a.panes[pane].Label = "bash"
	pane.Hold(40, 12)
	was := pane.Size()

	if err := a.togglePaneTitles(); err != nil {
		t.Fatalf("turn them on: %v", err)
	}
	a.refreshCaptions()

	if got := pane.Size(); got != was {
		t.Errorf("the screen is %v, want the %v the watcher asked for", got, was)
	}
}

// Giving the size back gives the program the room the line leaves, not
// the whole pane.
func TestReleasingASizeLeavesTheLineItsRow(t *testing.T) {
	_, pane := aTitledPane(t)
	want := pane.Size().Rows
	pane.Hold(40, 12)

	pane.Release()

	if got := pane.Size().Rows; got != want {
		t.Errorf("the program has %d rows, want the %d the line leaves", got, want)
	}
}

// A release that wandered onto the line ends the drag, rather than
// leaving the selection following a pointer with nothing held.
func TestAReleaseOnTheLineEndsTheDrag(t *testing.T) {
	a, pane := aTitledPane(t)
	fillScreen(t, a, 0, pane)

	for _, ev := range []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseLeft, Col: 3, Row: 5},
		{Kind: input.MouseMove, Button: input.MouseLeft, Col: 5, Row: 5},
		{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 5, Row: 0},
	} {
		if _, err := pane.HandleMouse(ev); err != nil {
			t.Fatalf("%v: %v", ev.Kind, err)
		}
	}
	was := pane.SelectionText()

	// A move with nothing held. A drag that never ended would take it.
	if _, err := pane.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove, Col: 20, Row: 10,
	}); err != nil {
		t.Fatalf("moving: %v", err)
	}

	if got := pane.SelectionText(); got != was {
		t.Errorf("the selection grew to %q after the button came up", got)
	}
}

// A drag up onto the line selects to the first row of the screen rather
// than stopping where it left.
func TestADragOntoTheLineSelectsToTheTop(t *testing.T) {
	a, pane := aTitledPane(t)
	fillScreen(t, a, 0, pane)

	for _, ev := range []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseLeft, Col: 3, Row: 3},
		{Kind: input.MouseMove, Button: input.MouseLeft, Col: 3, Row: 0},
	} {
		if _, err := pane.HandleMouse(ev); err != nil {
			t.Fatalf("%v: %v", ev.Kind, err)
		}
	}

	// From the screen's first row down to where the press was.
	if got := pane.SelectionText(); !strings.HasPrefix(got, string(cellRune(3, 0))) {
		t.Errorf("the drag selected %q, want it to reach the first row", got)
	}
}

// A pane whose program is started again gets the room the line leaves,
// not the whole pane.
func TestARestartedPaneLeavesTheLineItsRow(t *testing.T) {
	a, pane := aTitledPane(t)
	want := pane.Size().Rows

	if err := a.shells[0].Close(); err != nil {
		t.Fatalf("end the program: %v", err)
	}
	waitFor(t, a, "the program to end", func() bool { return pane.Exited() })
	if err := pane.Restart(newPipeSession()); err != nil {
		t.Fatalf("start it again: %v", err)
	}

	if got := pane.Size().Rows; got != want {
		t.Errorf("the program has %d rows, want the %d the line leaves", got, want)
	}
}

// A pane the host paints itself has no line and no row taken, so the
// rows it is given are the screen's own.
func TestAPanePaintedElsewhereTakesNoRow(t *testing.T) {
	a, pane := aTitledPane(t)
	fillScreen(t, a, 0, pane)
	pane.SetElsewhere(true)

	dragAcross(t, pane, 5)

	want := string([]rune{cellRune(3, 5), cellRune(4, 5), cellRune(5, 5)})
	if got := pane.SelectionText(); got != want {
		t.Errorf("the press selected %q, want %q from the row it names", got, want)
	}
}

// A held screen that no longer fits under the line is drawn on a layer
// of its own, rather than having its bottom row cut off.
func TestAHeldScreenThatNoLongerFitsIsScaled(t *testing.T) {
	_, pane := aTitledPane(t)
	room := pane.ScreenRoom()

	// One row taller than the line leaves, which is the size the pane
	// itself has.
	pane.Hold(room.Cols, room.Rows+1)

	if !overflows(pane) {
		t.Errorf("a screen of %v in room for %v is not being scaled", pane.Size(), room)
	}
}

// And one that still fits under it is not.
func TestAHeldScreenThatFitsUnderTheLineIsNotScaled(t *testing.T) {
	_, pane := aTitledPane(t)
	room := pane.ScreenRoom()

	pane.Hold(room.Cols, room.Rows)

	if overflows(pane) {
		t.Errorf("a screen of %v in room for %v is being scaled", pane.Size(), room)
	}
}
