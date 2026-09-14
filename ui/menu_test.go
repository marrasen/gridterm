package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// menuStyled returns colours a test can tell apart from a blank cell.
func menuStyled() MenuStyle {
	return MenuStyle{
		FG:         fg,
		BG:         bg,
		SelectedFG: bg,
		SelectedBG: fg,
		ChordFG:    fg,
		DisabledFG: fg,
	}
}

// items turns command ids into ordinary menu lines. An empty id gives a
// separator, so a test can write the shape of a menu in one line.
func items(ids ...string) []MenuItem {
	out := make([]MenuItem, len(ids))
	for i, id := range ids {
		out[i] = MenuItem{Command: id}
	}
	return out
}

// newTestMenu returns a menu over the given commands and a count of how
// often it asked to be closed.
func newTestMenu(t *testing.T, cmds *Commands, lines []MenuItem) (*Menu, *int) {
	t.Helper()
	closed := 0
	m := NewMenu(cmds, NewKeymap(), lines, func() { closed++ })
	m.Style = menuStyled()
	m.Layout(Size{Cols: 40, Rows: 20})
	return m, &closed
}

// drawMenu paints a menu onto a see-through grid, the way its own layer
// is made.
func drawMenu(m *Menu, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	m.Layout(Size{Cols: cols, Rows: rows})
	m.Draw(g.View())
	return g
}

func TestMenuStartsOnTheFirstThingItCanRun(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	// A separator first, so landing on line zero would be wrong.
	m, _ := newTestMenu(t, cmds, []MenuItem{MenuSeparator(), {Command: "copy"}, {Command: "paste"}})

	cmd, ok := m.Selected()
	if !ok {
		t.Fatal("nothing was selected")
	}
	if cmd.ID != "copy" {
		t.Errorf("selected %q, want the first line that can be run", cmd.ID)
	}
}

func TestMenuArrowsSkipSeparators(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, []MenuItem{{Command: "copy"}, MenuSeparator(), {Command: "paste"}})

	m.HandleKey(press(input.KeyDown, 0))

	if got := m.SelectedIndex(); got != 2 {
		t.Errorf("down landed on %d, want 2: the separator was not stepped over", got)
	}
	m.HandleKey(press(input.KeyUp, 0))
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("up landed on %d, want 0", got)
	}
}

func TestMenuArrowsStopAtTheEnds(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, items("copy", "paste"))

	for i := 0; i < 5; i++ {
		m.HandleKey(press(input.KeyDown, 0))
	}
	if got := m.SelectedIndex(); got != 1 {
		t.Errorf("selected %d after running off the bottom, want 1", got)
	}
	for i := 0; i < 5; i++ {
		m.HandleKey(press(input.KeyUp, 0))
	}
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("selected %d after running off the top, want 0", got)
	}
}

func TestMenuHomeAndEnd(t *testing.T) {
	cmds := testCommands("Copy", "Paste", "Quit")
	// A separator in the middle, which Home and End must step past.
	m, _ := newTestMenu(t, cmds, []MenuItem{
		{Command: "copy"}, MenuSeparator(), {Command: "paste"}, {Command: "quit"},
	})

	m.HandleKey(press(input.KeyEnd, 0))
	if got := m.SelectedIndex(); got != 3 {
		t.Errorf("End landed on %d, want the last line that can be run", got)
	}
	m.HandleKey(press(input.KeyHome, 0))
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("Home landed on %d, want the first line that can be run", got)
	}
}

// TestMenuDropsAStrandedSeparator checks that a rule which separates
// nothing is not drawn. A menu opening with a line across the top, or
// two rules together where a line between them was dropped, looks broken.
func TestMenuDropsAStrandedSeparator(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, []MenuItem{
		MenuSeparator(),
		{Command: "copy"},
		MenuSeparator(),
		MenuSeparator(),
		{Command: "paste"},
		MenuSeparator(),
	})

	got := m.Items()

	want := []string{"copy", "", "paste"}
	if len(got) != len(want) {
		t.Fatalf("items = %+v, want %v", got, want)
	}
	for i, id := range want {
		if got[i].Command != id {
			t.Errorf("item %d = %q, want %q", i, got[i].Command, id)
		}
	}
}

// TestMenuDropsALineItCannotName checks an item naming a command that is
// not registered. There is nothing to show but the id, which means
// nothing to the person reading it.
func TestMenuDropsALineItCannotName(t *testing.T) {
	cmds := testCommands("Copy")
	m, _ := newTestMenu(t, cmds, items("gone", "copy"))

	if got := len(m.Items()); got != 1 {
		t.Fatalf("%d items, want the unnamed one dropped: %+v", got, m.Items())
	}
	g := drawMenu(m, 40, 20)
	for y := 0; y < 20; y++ {
		if strings.Contains(rowOf(g, y), "gone") {
			t.Errorf("row %d = %q, want no command id on screen", y, rowOf(g, y))
		}
	}
}

func TestMenuEnterRunsAndCloses(t *testing.T) {
	ran := ""
	cmds := NewCommands()
	cmds.MustRegister(
		Command{ID: "copy", Title: "Copy", Run: func() error { ran = "copy"; return nil }},
		Command{ID: "paste", Title: "Paste", Run: func() error { ran = "paste"; return nil }},
	)
	m, closed := newTestMenu(t, cmds, items("copy", "paste"))

	m.HandleKey(press(input.KeyDown, 0))
	handled, err := m.HandleKey(press(input.KeyEnter, 0))

	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	if !handled {
		t.Error("enter was not handled")
	}
	if ran != "paste" {
		t.Errorf("ran %q, want the selected line", ran)
	}
	if *closed != 1 {
		t.Errorf("closed %d times, want once", *closed)
	}
}

func TestMenuEscapeClosesWithoutRunning(t *testing.T) {
	ran := false
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "copy", Title: "Copy", Run: func() error { ran = true; return nil }})
	m, closed := newTestMenu(t, cmds, items("copy"))

	if handled, _ := m.HandleKey(press(input.KeyEscape, 0)); !handled {
		t.Error("escape was not handled")
	}

	if ran {
		t.Error("escape ran the selected command")
	}
	if *closed != 1 {
		t.Errorf("closed %d times, want once", *closed)
	}
}

// TestMenuUnknownCommandCannotBeChosen checks a line the program named
// itself whose command is not registered. Panes and tabs register
// commands as they open, so a menu written once holds lines that are not
// always available. Such a line is shown greyed out and cannot be run.
func TestMenuUnknownCommandCannotBeChosen(t *testing.T) {
	cmds := testCommands("Copy")
	m, closed := newTestMenu(t, cmds, []MenuItem{
		{Command: "gone", Title: "Not today"},
		{Command: "copy"},
	})

	if got := len(m.Items()); got != 2 {
		t.Fatalf("%d items, want the named line kept", got)
	}
	if got := m.SelectedIndex(); got != 1 {
		t.Errorf("selected %d, want the line that names a real command", got)
	}
	// Aiming at it with the mouse must not run it either.
	if _, err := m.HandleMouse(pressAt(menuLines(m).X+1, menuLines(m).Y)); err != nil {
		t.Fatalf("press: %v", err)
	}
	if *closed != 0 {
		t.Error("pressing a line with no command behind it closed the menu")
	}
	// And it says what it is rather than showing the command id.
	g := drawMenu(m, 40, 20)
	if row := rowOf(g, menuLines(m).Y); !strings.Contains(row, "Not today") {
		t.Errorf("row = %q, want the title the program gave it", row)
	}
}

func TestMenuTitleOverridesTheCommand(t *testing.T) {
	cmds := testCommands("Copy")
	m, _ := newTestMenu(t, cmds, []MenuItem{{Command: "copy", Title: "Copy to clipboard"}})

	g := drawMenu(m, 40, 20)

	if !strings.Contains(rowOf(g, menuLines(m).Y), "Copy to clipboard") {
		t.Errorf("row = %q, want the item's own title", rowOf(g, menuLines(m).Y))
	}
}

func TestMenuHangsUnderItsAnchor(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, items("copy", "paste"))
	m.Anchor = func() Rect { return Rect{X: 7, Y: 3, Cols: 6, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 20})

	box := m.box()
	if box.X != 7 {
		t.Errorf("box.X = %d, want it lined up with the anchor at 7", box.X)
	}
	if box.Y != 4 {
		t.Errorf("box.Y = %d, want it just under the anchor", box.Y)
	}
}

// TestMenuFlipsAboveAnAnchorAtTheBottom checks a menu that would hang
// off the end of the window. Drawn where it was asked it would be cut
// off, and the lines that fell off could not be chosen at all.
func TestMenuFlipsAboveAnAnchorAtTheBottom(t *testing.T) {
	cmds := testCommands("Copy", "Paste", "Quit")
	m, _ := newTestMenu(t, cmds, items("copy", "paste", "quit"))
	m.Anchor = func() Rect { return Rect{X: 0, Y: 9, Cols: 4, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 12})

	box := m.box()
	if box.Y+box.Rows > 12 {
		t.Errorf("box at %d is %d rows, which runs past the 12 the window has", box.Y, box.Rows)
	}
	if box.Y+box.Rows != 9 {
		t.Errorf("box ends at %d, want it to sit on top of the anchor at 9", box.Y+box.Rows)
	}
}

// TestMenuSlidesOntoTheWindowFromTheRight checks a title near the right
// edge. A menu is narrower than the window, so it can always be moved
// onto it rather than being cut off.
func TestMenuSlidesOntoTheWindowFromTheRight(t *testing.T) {
	cmds := testCommands("Copy")
	m, _ := newTestMenu(t, cmds, items("copy"))
	m.Anchor = func() Rect { return Rect{X: 38, Y: 0, Cols: 2, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 12})

	box := m.box()
	if box.X+box.Cols > 40 {
		t.Errorf("box at %d is %d wide, which runs past the 40 columns there are", box.X, box.Cols)
	}
}

// TestMenuTallerThanTheWindowScrolls checks the case that fits neither
// above nor below. Without scrolling the selection walks off the box and
// Enter runs a line nobody can see.
func TestMenuTallerThanTheWindowScrolls(t *testing.T) {
	titles := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		titles = append(titles, "Command "+string(rune('a'+i)))
	}
	cmds := testCommands(titles...)
	lines := make([]MenuItem, 0, 20)
	for _, cmd := range cmds.All() {
		lines = append(lines, MenuItem{Command: cmd.ID})
	}
	m, _ := newTestMenu(t, cmds, lines)
	m.Anchor = func() Rect { return Rect{X: 0, Y: 0, Cols: 4, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 8})

	for i := 0; i < len(lines); i++ {
		m.HandleKey(press(input.KeyDown, 0))
	}

	box := m.box()
	if box.Rows > 8 {
		t.Fatalf("box is %d rows in a window of 8", box.Rows)
	}
	if m.at < m.top || m.at >= m.top+box.Rows {
		t.Errorf("selected %d with lines %d..%d showing: it is off the box",
			m.at, m.top, m.top+box.Rows-1)
	}
}

// TestMenuScrollFollowsTheWindow checks a window resized while the list
// is scrolled, the way the palette had to be fixed.
func TestMenuScrollFollowsTheWindow(t *testing.T) {
	titles := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		titles = append(titles, "Command "+string(rune('a'+i)))
	}
	cmds := testCommands(titles...)
	lines := make([]MenuItem, 0, 20)
	for _, cmd := range cmds.All() {
		lines = append(lines, MenuItem{Command: cmd.ID})
	}
	m, _ := newTestMenu(t, cmds, lines)
	m.Layout(Size{Cols: 40, Rows: 24})
	for i := 0; i < len(lines); i++ {
		m.HandleKey(press(input.KeyDown, 0))
	}

	m.Layout(Size{Cols: 40, Rows: 6})

	box := m.box()
	if m.at < m.top || m.at >= m.top+box.Rows {
		t.Errorf("after shrinking the window, selected %d with lines %d..%d showing",
			m.at, m.top, m.top+box.Rows-1)
	}
	// And growing must not leave the list scrolled past its end.
	m.Layout(Size{Cols: 40, Rows: 24})
	if m.top != 0 {
		t.Errorf("top = %d in a window with room for every line, want 0", m.top)
	}
}

func TestMenuLeftAndRightAskForTheNeighbour(t *testing.T) {
	cmds := testCommands("Copy")
	m, _ := newTestMenu(t, cmds, items("copy"))
	var steps []int
	m.OnEdge = func(step int) { steps = append(steps, step) }

	m.HandleKey(press(input.KeyRight, 0))
	m.HandleKey(press(input.KeyLeft, 0))

	if len(steps) != 2 || steps[0] != 1 || steps[1] != -1 {
		t.Errorf("steps = %v, want [1 -1]", steps)
	}
}

// TestMenuWithNoNeighboursSwallowsLeftAndRight checks that the arrows do
// not fall through to a pane underneath when there is no bar to move
// along. A menu is modal: keys it does not use are the only ones that
// travel on.
func TestMenuWithNoNeighboursSwallowsLeftAndRight(t *testing.T) {
	cmds := testCommands("Copy")
	m, _ := newTestMenu(t, cmds, items("copy"))

	if handled, _ := m.HandleKey(press(input.KeyRight, 0)); !handled {
		t.Error("right was passed on, so a shell underneath would see it")
	}
}

func TestMenuPressOutsideCloses(t *testing.T) {
	cmds := testCommands("Copy")
	m, closed := newTestMenu(t, cmds, items("copy"))
	m.Anchor = func() Rect { return Rect{X: 0, Y: 0, Cols: 4, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 20})

	handled, err := m.HandleMouse(pressAt(39, 19))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press was passed on to what is under the menu")
	}
	if *closed != 1 {
		t.Errorf("closed %d times, want once", *closed)
	}
}

func TestMenuPressOutsideCanBeClaimed(t *testing.T) {
	cmds := testCommands("Copy")
	m, closed := newTestMenu(t, cmds, items("copy"))
	var at [2]int
	m.OnOutside = func(col, row int) bool { at = [2]int{col, row}; return true }

	m.HandleMouse(pressAt(39, 19))

	if at != [2]int{39, 19} {
		t.Errorf("reported %v, want the point that was pressed", at)
	}
	if *closed != 0 {
		t.Error("the menu closed even though the press was claimed")
	}
}

// TestMenuIgnoresAReleaseOutside checks the press that opened the menu.
// Its release lands over the menu or beside it, and closing on that
// would make a menu impossible to open with a click.
func TestMenuIgnoresAReleaseOutside(t *testing.T) {
	cmds := testCommands("Copy")
	m, closed := newTestMenu(t, cmds, items("copy"))

	handled, _ := m.HandleMouse(releaseAt(39, 19))

	if !handled {
		t.Error("the release was passed on to what is under the menu")
	}
	if *closed != 0 {
		t.Error("a release closed the menu")
	}
}

func TestMenuClickRunsALine(t *testing.T) {
	ran := ""
	cmds := NewCommands()
	cmds.MustRegister(
		Command{ID: "copy", Title: "Copy", Run: func() error { ran = "copy"; return nil }},
		Command{ID: "paste", Title: "Paste", Run: func() error { ran = "paste"; return nil }},
	)
	m, closed := newTestMenu(t, cmds, items("copy", "paste"))
	box := menuLines(m)

	if _, err := m.HandleMouse(pressAt(box.X+1, box.Y+1)); err != nil {
		t.Fatalf("press: %v", err)
	}

	if ran != "paste" {
		t.Errorf("ran %q, want the line that was pressed", ran)
	}
	if *closed != 1 {
		t.Errorf("closed %d times, want once", *closed)
	}
}

func TestMenuPointerHighlightsALine(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, items("copy", "paste"))
	box := menuLines(m)

	m.HandleMouse(moveTo(box.X+1, box.Y+1))

	if got := m.SelectedIndex(); got != 1 {
		t.Errorf("selected %d, want the line under the pointer", got)
	}
}

// TestMenuPressOnASeparatorDoesNothing checks that a rule cannot be
// chosen with the mouse either, and that pressing one does not close the
// menu out from under a slip of the hand.
func TestMenuPressOnASeparatorDoesNothing(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, closed := newTestMenu(t, cmds, []MenuItem{{Command: "copy"}, MenuSeparator(), {Command: "paste"}})
	box := menuLines(m)

	m.HandleMouse(moveTo(box.X+1, box.Y+1))
	m.HandleMouse(pressAt(box.X+1, box.Y+1))

	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("selected %d, want the separator to have been left alone", got)
	}
	if *closed != 0 {
		t.Error("pressing a separator closed the menu")
	}
}

// TestMenuLeavesTheRestOfItsLayerClear checks that what is behind the
// menu shows through. The layer is composited over the widget tree, so
// an opaque cell anywhere outside the box blanks the window.
func TestMenuLeavesTheRestOfItsLayerClear(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, items("copy", "paste"))
	m.Anchor = func() Rect { return Rect{X: 2, Y: 1, Cols: 4, Rows: 1} }

	g := drawMenu(m, 40, 20)

	box := m.box()
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			if box.Contains(x, y) {
				continue
			}
			if got := g.At(x, y); got.BG.A != 0 {
				t.Fatalf("cell %d,%d = %+v, want it clear outside the box", x, y, got)
			}
		}
	}
}

// TestMenuDrawnTwiceLeavesTheLayerClean is the idle-frame rule: a menu
// that has not changed must not dirty a row, or the compositor can never
// skip a frame while one is open.
func TestMenuDrawnTwiceLeavesTheLayerClean(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, items("copy", "paste"))
	g := drawMenu(m, 40, 20)

	g.ClearDirty()
	for i := 0; i < 2; i++ {
		m.Draw(g.View())
		if g.AnyDirty() {
			t.Fatalf("draw %d of an unchanged menu dirtied the layer", i)
		}
	}

	// And a real change still gets through.
	m.HandleKey(press(input.KeyDown, 0))
	m.Draw(g.View())
	if !g.AnyDirty() {
		t.Error("moving the selection did not change anything on the layer")
	}
}

// TestMenuShowsTheKeyBinding checks the third way into the registry: a
// menu teaches the shortcut for what it is about to run.
func TestMenuShowsTheKeyBinding(t *testing.T) {
	cmds := testCommands("Copy")
	keys := NewKeymap()
	keys.MustBind(map[Chord]string{{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}: "copy"})
	m := NewMenu(cmds, keys, items("copy"), func() {})
	m.Style = menuStyled()

	g := drawMenu(m, 40, 20)

	row := rowOf(g, menuLines(m).Y)
	want := Chord{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}.String()
	if !strings.Contains(row, want) {
		t.Errorf("row = %q, want it to show %q", row, want)
	}
}

func TestMenuSeparatorDrawsARule(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, []MenuItem{{Command: "copy"}, MenuSeparator(), {Command: "paste"}})

	g := drawMenu(m, 40, 20)

	row := rowOf(g, menuLines(m).Y+1)
	if !strings.Contains(row, string(separatorRune)) {
		t.Errorf("row = %q, want a rule", row)
	}
}

// TestMenuWidthIsMeasuredInColumns checks a title of double-width
// characters. Measured in runes the box would be half the width it needs
// and the title would be drawn with its end cut off.
func TestMenuWidthIsMeasuredInColumns(t *testing.T) {
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "wide", Title: "世界世界", Run: nop})
	m, _ := newTestMenu(t, cmds, items("wide"))

	if got, want := menuLines(m).Cols, grid.StringWidth("世界世界")+menuPad*2; got < want {
		t.Errorf("box is %d columns, want at least %d for the title", got, want)
	}
}

// TestMenuWithNoItemsDrawsNothing checks the empty case: a box with no
// lines would be a blank rectangle over a pane for no reason.
func TestMenuWithNoItemsDrawsNothing(t *testing.T) {
	m, _ := newTestMenu(t, NewCommands(), nil)
	// A new grid starts dirty, so the check below needs a clean one.
	g := grid.New(40, 20, color.RGBA{}, color.RGBA{})
	g.ClearDirty()

	m.Layout(Size{Cols: 40, Rows: 20})
	m.Draw(g.View())

	if !m.box().Empty() {
		t.Errorf("box = %+v, want nothing", m.box())
	}
	if g.AnyDirty() {
		t.Error("an empty menu drew onto its layer")
	}
	if got := m.SelectedIndex(); got != -1 {
		t.Errorf("selected %d, want -1 when there is nothing to select", got)
	}
}

// TestMenuWithNoRoomDrawsNothing checks a window too small to hold the
// menu at all, which must not panic or draw a sliver.
func TestMenuWithNoRoomDrawsNothing(t *testing.T) {
	cmds := testCommands("Copy")
	m, _ := newTestMenu(t, cmds, items("copy"))

	m.Layout(Size{})

	if !m.box().Empty() {
		t.Errorf("box = %+v, want nothing in a window of no size", m.box())
	}
	m.Draw(grid.New(0, 0, color.RGBA{}, color.RGBA{}).View())
}
