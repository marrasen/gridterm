package ui

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// listStyled returns colours a test can tell apart from a blank cell.
func listStyled() ListStyle {
	return ListStyle{
		FG: fg, BG: bg,
		SelectedFG: bg, SelectedBG: fg,
		HeaderFG: fg, NoteFG: fg,
	}
}

// panelRows is a list shaped the way the connections panel shapes one: a
// header per machine, and the things open on it under it.
func panelRows() []ListRow {
	return []ListRow{
		{Text: "Local", Header: true, Key: "h:local"},
		{Text: "Files ~/Workspace", Depth: 1, Note: "settled", Key: "local-files"},
		{Text: "margit", Header: true, Key: "h:margit"},
		{Text: "Terminal vim", Depth: 1, Note: "active", Key: "margit-vim"},
		{Text: "Tunnel :5432", Depth: 1, Note: "active", Key: "margit-tunnel"},
	}
}

func newTestList(t *testing.T, rows []ListRow, cols, lines int) *List {
	t.Helper()
	l := NewList()
	l.Style = listStyled()
	l.SetRows(rows)
	l.Layout(Size{Cols: cols, Rows: lines})
	l.SetFocus(true)
	return l
}

// drawList paints a list the way its own layer is made.
func drawList(l *List, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	l.Layout(Size{Cols: cols, Rows: rows})
	l.Draw(g.View())
	return g
}

// A header names the rows under it and is not one of them.
func TestListOpensOnTheFirstRowThatIsNotAHeader(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	got, ok := l.Selected()
	if !ok {
		t.Fatal("nothing is selected")
	}
	if got.Key != "local-files" {
		t.Fatalf("selected %v, want the first row under a header", got.Key)
	}
}

func TestListArrowsSkipHeaders(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)

	l.HandleKey(press(input.KeyDown, 0))
	if got, _ := l.Selected(); got.Key != "margit-vim" {
		t.Fatalf("Down went to %v, want the row past the header", got.Key)
	}
	l.HandleKey(press(input.KeyUp, 0))
	if got, _ := l.Selected(); got.Key != "local-files" {
		t.Fatalf("Up went to %v, want back past the header", got.Key)
	}
}

// A list you can run off the end of is hard to aim at.
func TestListStopsAtTheEnds(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	for i := 0; i < 10; i++ {
		l.HandleKey(press(input.KeyDown, 0))
	}
	if got, _ := l.Selected(); got.Key != "margit-tunnel" {
		t.Fatalf("selected %v after running down, want the last row", got.Key)
	}
	for i := 0; i < 10; i++ {
		l.HandleKey(press(input.KeyUp, 0))
	}
	if got, _ := l.Selected(); got.Key != "local-files" {
		t.Fatalf("selected %v after running up, want the first row", got.Key)
	}
}

func TestListHomeAndEnd(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	l.HandleKey(press(input.KeyEnd, 0))
	if got, _ := l.Selected(); got.Key != "margit-tunnel" {
		t.Fatalf("End selected %v", got.Key)
	}
	l.HandleKey(press(input.KeyHome, 0))
	if got, _ := l.Selected(); got.Key != "local-files" {
		t.Fatalf("Home selected %v", got.Key)
	}
}

// The list is rebuilt from the registry on every frame. Losing the
// user's place each time would make it impossible to use.
func TestListKeepsTheSelectionAcrossARebuild(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	l.HandleKey(press(input.KeyDown, 0))
	if got, _ := l.Selected(); got.Key != "margit-vim" {
		t.Fatalf("selected %v", got.Key)
	}

	// Something opens above it, which moves it down the list.
	rows := panelRows()
	rows = append(rows[:1], append([]ListRow{
		{Text: "Terminal bash", Depth: 1, Key: "local-bash"},
	}, rows[1:]...)...)
	l.SetRows(rows)

	if got, _ := l.Selected(); got.Key != "margit-vim" {
		t.Fatalf("after a rebuild, selected %v, want the same row", got.Key)
	}
	// And its label can change without losing it.
	rows[l.SelectedIndex()].Text = "Terminal make"
	l.SetRows(rows)
	if got, _ := l.Selected(); got.Key != "margit-vim" {
		t.Fatalf("after a relabel, selected %v", got.Key)
	}
}

// A row that goes takes the selection with it to whatever is left.
func TestListSelectionSurvivesARowGoing(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	l.HandleKey(press(input.KeyEnd, 0))

	rows := panelRows()
	rows = rows[:len(rows)-1] // the selected one goes
	l.SetRows(rows)

	got, ok := l.Selected()
	if !ok {
		t.Fatal("nothing is selected after the selected row went")
	}
	if got.Header {
		t.Fatalf("a header is selected: %v", got.Key)
	}
}

func TestListActivateRunsTheSelectedRow(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	var ran []any
	l.OnActivate = func(row ListRow) error {
		ran = append(ran, row.Key)
		return nil
	}

	l.HandleKey(press(input.KeyEnter, 0))
	l.HandleKey(press(input.KeyDown, 0))
	l.HandleKey(press(input.KeySpace, 0))

	if len(ran) != 2 || ran[0] != "local-files" || ran[1] != "margit-vim" {
		t.Fatalf("ran %v", ran)
	}
}

func TestListActivateReportsAFailure(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	want := errors.New("it would not close")
	l.OnActivate = func(ListRow) error { return want }

	took, err := l.HandleKey(press(input.KeyEnter, 0))
	if !took {
		t.Error("Enter was not taken")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want the one the row gave", err)
	}
}

func TestListClickSelectsAndRuns(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	var ran []any
	l.OnActivate = func(row ListRow) error {
		ran = append(ran, row.Key)
		return nil
	}

	// Row 3 is the terminal under margit.
	l.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Row: 3})
	if got, _ := l.Selected(); got.Key != "margit-vim" {
		t.Fatalf("clicking row 3 selected %v", got.Key)
	}
	if len(ran) != 1 || ran[0] != "margit-vim" {
		t.Fatalf("ran %v", ran)
	}
}

// A header is a name, not a row to act on, and the space under the last
// row is not one either.
func TestListClickOnAHeaderOrPastTheEndRunsNothing(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	var ran int
	l.OnActivate = func(ListRow) error { ran++; return nil }

	for _, row := range []int{0, 2, 7} {
		took, _ := l.HandleMouse(input.MouseEvent{
			Kind: input.MousePress, Button: input.MouseLeft, Row: row,
		})
		if !took {
			t.Errorf("the click on row %d travelled on", row)
		}
	}
	if ran != 0 {
		t.Fatalf("ran %d times", ran)
	}
	// And the selection did not move onto a header.
	if got, _ := l.Selected(); got.Header {
		t.Fatalf("a header is selected: %v", got.Key)
	}
}

// Unlike a menu, a list scrolls on the wheel: it is a thing to look
// through.
func TestListScrollsOnTheWheel(t *testing.T) {
	var rows []ListRow
	for i := 0; i < 40; i++ {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)

	l.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelDown})
	if l.top == 0 {
		t.Fatal("the wheel did not scroll")
	}
	// The selection stays put: the wheel moves the view, not the choice.
	if got, _ := l.Selected(); got.Key != 0 {
		t.Fatalf("the wheel moved the selection to %v", got.Key)
	}
	for i := 0; i < 40; i++ {
		l.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelDown})
	}
	if l.top > len(rows)-5 {
		t.Fatalf("scrolled to %d, past the end of %d rows", l.top, len(rows))
	}
	for i := 0; i < 60; i++ {
		l.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelUp})
	}
	if l.top != 0 {
		t.Fatalf("scrolled to %d, past the start", l.top)
	}
}

// Moving down a long list has to bring the selection into view, or Enter
// acts on something nobody can see.
func TestListScrollsToKeepTheSelectionInView(t *testing.T) {
	var rows []ListRow
	for i := 0; i < 40; i++ {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)

	l.HandleKey(press(input.KeyEnd, 0))
	at := l.SelectedIndex()
	if at < l.top || at >= l.top+5 {
		t.Fatalf("the selection is row %d with rows %d..%d on screen", at, l.top, l.top+4)
	}
	l.HandleKey(press(input.KeyHome, 0))
	if l.top != 0 {
		t.Fatalf("after Home the list shows from row %d", l.top)
	}
}

// A window that shrank must not leave the list scrolled past its end.
func TestListDoesNotStayScrolledPastItsEnd(t *testing.T) {
	var rows []ListRow
	for i := 0; i < 40; i++ {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)
	l.HandleKey(press(input.KeyEnd, 0))

	l.SetRows(rows[:6])
	if l.top > 1 {
		t.Fatalf("the list shows from row %d of 6 with 5 on screen", l.top)
	}
}

func TestListDrawsTextNotesAndHeaders(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	g := drawList(l, 40, 10)

	if got := strings.TrimSpace(rowOf(g, 0)); got != "Local" {
		t.Errorf("row 0 = %q, want the header", got)
	}
	row := rowOf(g, 1)
	if !strings.Contains(row, "Files ~/Workspace") {
		t.Errorf("row 1 = %q, want the text", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "settled") {
		t.Errorf("row 1 = %q, want the note at the end", row)
	}
	// A row that belongs to the one above is indented.
	if strings.HasPrefix(row, "F") {
		t.Errorf("row 1 = %q, want it indented under its header", row)
	}
}

// A note that leaves no room for the text is dropped: a row holding
// nothing but "active" does not say what is active.
func TestListDropsANoteWithNoRoomForTheText(t *testing.T) {
	l := newTestList(t, []ListRow{
		{Text: "a long terminal title", Note: "active", Key: 1},
	}, 8, 4)
	g := drawList(l, 8, 4)
	if got := strings.TrimSpace(rowOf(g, 0)); strings.Contains(got, "active") {
		t.Fatalf("row = %q, want the note dropped for the text", got)
	}
}

// Only the widget with the keys marks its selection, or two panels both
// look like the one being used.
func TestListMarksTheSelectionOnlyWhenItHasTheKeys(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	focused := drawList(l, 40, 10)
	l.SetFocus(false)
	unfocused := drawList(l, 40, 10)

	at := l.SelectedIndex()
	if focused.At(0, at).BG == unfocused.At(0, at).BG {
		t.Fatal("the selected row looks the same whether or not the list has the keys")
	}
}

// The list is drawn through a buffer, so a frame where nothing moved
// leaves the layer alone.
func TestListDoesNotDirtyAnIdleFrame(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	g := grid.New(40, 10, color.RGBA{}, color.RGBA{})
	l.Draw(g.View())
	g.ClearDirty()
	l.Draw(g.View())
	if g.AnyDirty() {
		t.Fatal("drawing an unchanged list dirtied the layer")
	}
}

// An empty list is not a broken one.
func TestListWithNothingInIt(t *testing.T) {
	l := newTestList(t, nil, 20, 5)
	if _, ok := l.Selected(); ok {
		t.Fatal("something is selected in an empty list")
	}
	l.HandleKey(press(input.KeyDown, 0))
	l.HandleKey(press(input.KeyEnter, 0))
	l.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Row: 0})
	drawList(l, 20, 5)
}

// A list of nothing but headers has nothing to select.
func TestListOfOnlyHeaders(t *testing.T) {
	l := newTestList(t, []ListRow{
		{Text: "Local", Header: true, Key: 1},
		{Text: "margit", Header: true, Key: 2},
	}, 20, 5)
	if _, ok := l.Selected(); ok {
		t.Fatal("a header is selected")
	}
	l.HandleKey(press(input.KeyDown, 0))
	if _, ok := l.Selected(); ok {
		t.Fatal("Down selected a header")
	}
}

// Keys with a modifier belong to whatever is around the list.
func TestListLetsModifiedKeysThrough(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	for _, mods := range []input.Mods{input.ModCtrl, input.ModAlt, input.ModCtrl | input.ModShift} {
		if took, _ := l.HandleKey(press(input.KeyTab, mods)); took {
			t.Errorf("the list swallowed Tab with %v", mods)
		}
	}
}

func TestListSelectByKey(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	if !l.Select("margit-tunnel") {
		t.Fatal("the key was not found")
	}
	if got, _ := l.Selected(); got.Key != "margit-tunnel" {
		t.Fatalf("selected %v", got.Key)
	}
	if l.Select("not there") {
		t.Fatal("a key that is not there was found")
	}
}
