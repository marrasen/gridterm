package ui

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// stage stands in for whatever shows a menu: it records the menus it was
// given and how often each was taken away again.
type stage struct {
	shown  []*Menu
	closed int
	refuse bool
}

func (s *stage) present(m *Menu) func() {
	if s.refuse {
		return nil
	}
	s.shown = append(s.shown, m)
	// Laying it out is what a real one does, and the bar's anchor is only
	// asked for then.
	m.Layout(Size{Cols: 40, Rows: 20})
	return func() { s.closed++ }
}

func (s *stage) top() *Menu {
	if len(s.shown) == 0 {
		return nil
	}
	return s.shown[len(s.shown)-1]
}

// newTestBar returns a bar with two menus over a filler, and the stage
// its menus are shown on.
func newTestBar(t *testing.T, child Widget) (*Menubar, *stage) {
	t.Helper()
	cmds := testCommands("Copy", "Paste", "Quit")
	b := NewMenubar(cmds, NewKeymap(), child)
	b.Style = MenubarStyle{FG: fg, BG: bg, OpenFG: bg, OpenBG: fg}
	b.MenuStyle = menuStyled()
	b.Menus = []MenuDef{
		{Title: "File", Items: items("quit")},
		{Title: "Edit", Items: items("copy", "paste")},
	}
	st := &stage{}
	b.Present = st.present
	b.Layout(Size{Cols: 40, Rows: 20})
	return b, st
}

func TestMenubarGivesTheChildEverythingBelowTheBar(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)

	if got := (Size{Cols: 40, Rows: 19}); child.size != got {
		t.Errorf("child was told %+v, want %+v", child.size, got)
	}
	area, ok := b.ChildArea(child)
	if !ok {
		t.Fatal("the child has no area")
	}
	if want := (Rect{Y: 1, Cols: 40, Rows: 19}); area != want {
		t.Errorf("child area = %+v, want %+v", area, want)
	}
}

func TestMenubarDrawsTitlesAboveTheChild(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)
	g := grid.New(40, 20, fg, bg)

	b.Draw(g.View())

	if row := rowOf(g, 0); !strings.Contains(row, "File") || !strings.Contains(row, "Edit") {
		t.Errorf("bar row = %q, want both titles", row)
	}
	if row := rowOf(g, 1); !strings.HasPrefix(row, "xxxx") {
		t.Errorf("row under the bar = %q, want the child", row)
	}
}

func TestMenubarClickOpensAMenu(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})

	handled, err := b.HandleMouse(pressAt(1, 0))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press on a title was passed on")
	}
	if b.OpenIndex() != 0 {
		t.Errorf("open = %d, want the first menu", b.OpenIndex())
	}
	if len(st.shown) != 1 {
		t.Fatalf("%d menus shown, want one", len(st.shown))
	}
}

func TestMenubarClickOnTheOpenTitleCloses(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.HandleMouse(pressAt(1, 0))

	b.HandleMouse(pressAt(1, 0))

	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want nothing open", b.OpenIndex())
	}
	if st.closed != 1 {
		t.Errorf("closed %d times, want once", st.closed)
	}
}

// TestMenubarClickBesideTheTitlesMeansNothing checks the empty part of
// the bar. Claiming it would swallow a press that belongs to nobody.
func TestMenubarClickBesideTheTitlesMeansNothing(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})

	handled, _ := b.HandleMouse(pressAt(39, 0))

	if handled {
		t.Error("a press on the empty part of the bar was claimed")
	}
	if len(st.shown) != 0 {
		t.Error("a press beside the titles opened a menu")
	}
}

func TestMenubarPassesAClickBelowToTheChild(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)

	b.HandleMouse(pressAt(5, 3))

	if len(child.seen) != 1 {
		t.Fatalf("the child saw %d events, want one", len(child.seen))
	}
	if got := child.seen[0]; got.Col != 5 || got.Row != 2 {
		t.Errorf("child saw %d,%d, want 5,2 in its own coordinates", got.Col, got.Row)
	}
}

func TestMenubarKeysGoStraightThroughToTheChild(t *testing.T) {
	child := &filler{ch: 'x', takes: input.KeyA}
	b, _ := newTestBar(t, child)

	handled, _ := b.HandleKey(press(input.KeyA, 0))

	if !handled {
		t.Error("the child's key was not offered to it")
	}
	if len(child.keys) != 1 || child.keys[0] != input.KeyA {
		t.Errorf("child saw %v, want the key", child.keys)
	}
}

// TestMenubarNeverTakesFocus checks that the bar is chrome. A bar that
// held focus would fight every program in a pane for the arrow keys.
func TestMenubarNeverTakesFocus(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)

	b.SetFocus(true)

	if !child.focused {
		t.Error("focus stopped at the bar instead of reaching the child")
	}
	if got := b.Focused(); got != Widget(child) {
		t.Errorf("Focused = %v, want the child", got)
	}
}

func TestMenubarAnchorsAMenuUnderItsTitle(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})

	// "Edit" is the second title: "File" takes six columns with a space
	// each side.
	b.HandleMouse(pressAt(7, 0))

	menu := st.top()
	if menu == nil {
		t.Fatal("no menu was shown")
	}
	if got := menu.box().X; got != 6 {
		t.Errorf("menu starts at column %d, want it under the title at 6", got)
	}
	if got := menu.box().Y; got != 1 {
		t.Errorf("menu starts at row %d, want it just under the bar", got)
	}
}

// TestMenubarAnchorFollowsTheBarsOwnPosition checks a bar that is not at
// the top left. A menu is laid out over the whole window while the bar
// is told only about its own corner of it, so without the offset the
// menu hangs under the wrong column.
func TestMenubarAnchorFollowsTheBarsOwnPosition(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.Origin = func() Rect { return Rect{X: 10, Y: 4, Cols: 40, Rows: 20} }

	b.HandleMouse(pressAt(1, 0))

	menu := st.top()
	if menu == nil {
		t.Fatal("no menu was shown")
	}
	if got := menu.box().X; got != 10 {
		t.Errorf("menu starts at column %d, want it under the bar's own origin", got)
	}
	if got := menu.box().Y; got != 5 {
		t.Errorf("menu starts at row %d, want it under the bar's own row", got)
	}
}

func TestMenubarArrowsMoveBetweenMenus(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.HandleMouse(pressAt(1, 0))

	st.top().HandleKey(press(input.KeyRight, 0))

	if b.OpenIndex() != 1 {
		t.Errorf("open = %d, want the second menu", b.OpenIndex())
	}
	// And it wraps at the ends rather than stopping.
	st.top().HandleKey(press(input.KeyRight, 0))
	if b.OpenIndex() != 0 {
		t.Errorf("open = %d after stepping past the last, want the first", b.OpenIndex())
	}
	st.top().HandleKey(press(input.KeyLeft, 0))
	if b.OpenIndex() != 1 {
		t.Errorf("open = %d after stepping back from the first, want the last", b.OpenIndex())
	}
}

// TestMenubarOpeningAnotherClosesTheFirst checks that moving along the
// bar leaves one menu on screen, not two.
func TestMenubarOpeningAnotherClosesTheFirst(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.HandleMouse(pressAt(1, 0))

	b.HandleMouse(pressAt(7, 0))

	if st.closed != 1 {
		t.Errorf("closed %d times, want the first menu taken away", st.closed)
	}
	if len(st.shown) != 2 {
		t.Errorf("%d menus shown, want two", len(st.shown))
	}
	if b.OpenIndex() != 1 {
		t.Errorf("open = %d, want the second menu", b.OpenIndex())
	}
}

// TestMenubarPressOnAnotherTitleSwitchesMenus checks the press the menu
// itself reports. A menu covers the whole window, so the bar never sees
// that press directly.
func TestMenubarPressOnAnotherTitleSwitchesMenus(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.HandleMouse(pressAt(1, 0))
	menu := st.top()

	// A press on "Edit", which lands outside the open menu.
	handled, err := menu.HandleMouse(pressAt(7, 0))

	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !handled {
		t.Error("the press reached past the menu")
	}
	if b.OpenIndex() != 1 {
		t.Errorf("open = %d, want the menu whose title was pressed", b.OpenIndex())
	}
}

// TestMenubarPressBesideTheTitlesClosesTheMenu checks the empty part of
// the bar while a menu is open. It belongs to the bar, so it closes the
// menu rather than reaching the pane underneath.
func TestMenubarPressBesideTheTitlesClosesTheMenu(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.HandleMouse(pressAt(1, 0))

	handled, _ := st.top().HandleMouse(pressAt(39, 0))

	if !handled {
		t.Error("the press reached past the menu")
	}
	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want nothing open", b.OpenIndex())
	}
	if st.closed != 1 {
		t.Errorf("closed %d times, want once", st.closed)
	}
}

// TestMenubarPressBelowTheBarDoesNotSwitch checks that only the bar's
// own row switches menus. A press in a pane closes the menu, and the
// bar must not claim it as a title.
func TestMenubarPressBelowTheBarDoesNotSwitch(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	b.HandleMouse(pressAt(1, 0))

	st.top().HandleMouse(pressAt(1, 10))

	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want the menu closed", b.OpenIndex())
	}
}

// TestMenubarClosingWhenNothingIsOpenIsHarmless checks the double close
// that a dialog taken away from underneath produces.
func TestMenubarClosingWhenNothingIsOpenIsHarmless(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})

	b.Close()
	b.HandleMouse(pressAt(1, 0))
	b.Close()
	b.Close()

	if st.closed != 1 {
		t.Errorf("closed %d times, want the one open menu taken away once", st.closed)
	}
	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want nothing open", b.OpenIndex())
	}
}

// TestMenubarWithNowhereToShowAMenuOpensNothing checks a program that
// cannot put a menu on screen. The bar must not be left marking a title
// as open with nothing under it.
func TestMenubarWithNowhereToShowAMenuOpensNothing(t *testing.T) {
	b, st := newTestBar(t, &filler{ch: 'x'})
	st.refuse = true

	if b.Open(0) {
		t.Error("opening reported success with nowhere to show the menu")
	}
	if b.OpenIndex() != -1 {
		t.Errorf("open = %d, want nothing open", b.OpenIndex())
	}
}

func TestMenubarOpenRejectsATitleThatIsNotThere(t *testing.T) {
	b, _ := newTestBar(t, &filler{ch: 'x'})

	for _, i := range []int{-1, 2, 99} {
		if b.Open(i) {
			t.Errorf("Open(%d) reported success", i)
		}
	}
}

// TestMenubarMarksTheOpenTitle checks that the bar shows which menu is
// down, so the two do not look unrelated.
func TestMenubarMarksTheOpenTitle(t *testing.T) {
	b, _ := newTestBar(t, &filler{ch: 'x'})
	b.HandleMouse(pressAt(1, 0))
	g := grid.New(40, 20, fg, bg)

	b.Draw(g.View())

	if got := g.At(1, 0).BG; got != b.Style.OpenBG {
		t.Errorf("title cell background = %+v, want the open colour %+v", got, b.Style.OpenBG)
	}
	if got := g.At(7, 0).BG; got != b.Style.BG {
		t.Errorf("the closed title is marked open too: %+v", got)
	}
}

func TestMenubarContainerContract(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)
	other := &filler{ch: 'y'}

	if got := b.Children(); len(got) != 1 || got[0] != Widget(child) {
		t.Errorf("Children = %v, want the one child", got)
	}
	if !b.Focus(child) {
		t.Error("Focus refused the child")
	}
	if b.Focus(other) {
		t.Error("Focus accepted a widget that is not the child")
	}
	if b.Replace(other, child) {
		t.Error("Replace accepted an old widget that is not the child")
	}
	if !b.Replace(child, other) {
		t.Fatal("Replace refused the child")
	}
	if b.Focused() != Widget(other) {
		t.Errorf("Focused = %v, want the replacement", b.Focused())
	}
	if _, ok := b.ChildArea(child); ok {
		t.Error("the widget that was replaced still has an area")
	}
	if _, ok := b.Remove(child); ok {
		t.Error("Remove accepted a widget that is not the child")
	}
	stands, ok := b.Remove(other)
	if !ok {
		t.Fatal("Remove refused the child")
	}
	if stands != nil {
		t.Errorf("Remove reported %v should stand in the bar's place, want nothing", stands)
	}
}

// TestMenubarDoesNotTellTheChildItHasNoRoom checks the rule a terminal
// depends on: a size of nothing is not a size. A shell told it has no
// rows reflows its scrollback, and the next layout cannot undo that.
func TestMenubarDoesNotTellTheChildItHasNoRoom(t *testing.T) {
	child := &fake{name: "x"}
	b, _ := newTestBar(t, child)

	b.Layout(Size{Cols: 40, Rows: 1})
	b.Layout(Size{})

	for _, size := range child.sizes {
		if size.Empty() {
			t.Fatalf("the child was told it has %+v", size)
		}
	}
}

// TestMenubarLabelsAreMeasuredInColumns checks a title of double-width
// characters. Measured in runes the next title would be drawn over it
// and every click area after it would be wrong.
func TestMenubarLabelsAreMeasuredInColumns(t *testing.T) {
	b, _ := newTestBar(t, &filler{ch: 'x'})
	b.Menus = []MenuDef{
		{Title: "世界", Items: items("quit")},
		{Title: "Edit", Items: items("copy")},
	}

	got := b.labels()

	if want := grid.StringWidth("世界") + 2; got[0].Cols != want {
		t.Errorf("first label is %d columns, want %d", got[0].Cols, want)
	}
	if got[1].X != got[0].Cols {
		t.Errorf("second label starts at %d, want %d", got[1].X, got[0].Cols)
	}
}

// TestMenubarWithNoRoomForTheBarDrawsNone checks a window one row tall.
// A bar drawn there would leave nothing for the widget under it.
func TestMenubarWithNoRoomForTheBarDrawsNone(t *testing.T) {
	child := &filler{ch: 'x'}
	b, _ := newTestBar(t, child)

	b.Layout(Size{Cols: 40, Rows: 1})
	g := grid.New(40, 1, fg, bg)
	b.Draw(g.View())

	if row := rowOf(g, 0); !strings.HasPrefix(row, "xxxx") {
		t.Errorf("row = %q, want the child to have the only row there is", row)
	}
	if got := b.body(); got.Y != 0 || got.Rows != 1 {
		t.Errorf("body = %+v, want the whole window", got)
	}
}
