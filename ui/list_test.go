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
	var ran int
	l.OnActivate = func(ListRow) error { ran++; return nil }

	if _, ok := l.Selected(); ok {
		t.Fatal("something is selected in an empty list")
	}
	if l.SelectedIndex() != -1 {
		t.Fatalf("the selection is row %d in an empty list", l.SelectedIndex())
	}

	l.HandleKey(press(input.KeyDown, 0))
	l.HandleKey(press(input.KeyEnter, 0))
	l.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelDown})
	took, _ := l.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 0,
	})
	if !took {
		t.Error("a click on an empty list travelled on")
	}
	if ran != 0 {
		t.Fatalf("an empty list ran a row %d times", ran)
	}
	if l.top != 0 {
		t.Fatalf("an empty list scrolled to row %d", l.top)
	}

	g := drawList(l, 20, 5)
	for y := 0; y < 5; y++ {
		if got := strings.TrimSpace(rowOf(g, y)); got != "" {
			t.Errorf("an empty list drew %q on row %d", got, y)
		}
	}
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

// Keys with a modifier belong to whatever is around the list, including
// the ones the list would otherwise take for itself.
func TestListLetsModifiedKeysThrough(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	var ran int
	l.OnActivate = func(ListRow) error { ran++; return nil }
	before, _ := l.Selected()

	for _, key := range []input.Key{
		input.KeyDown, input.KeyUp, input.KeyHome, input.KeyEnd, input.KeyEnter,
		input.KeySpace, input.KeyPageDown, input.KeyTab,
	} {
		for _, mods := range []input.Mods{
			input.ModCtrl, input.ModAlt, input.ModCtrl | input.ModShift,
		} {
			if took, _ := l.HandleKey(press(key, mods)); took {
				t.Errorf("the list swallowed %s with %v", key, mods)
			}
		}
	}
	if after, _ := l.Selected(); after.Key != before.Key {
		t.Fatalf("a modified key moved the selection to %v", after.Key)
	}
	if ran != 0 {
		t.Fatalf("a modified key ran a row %d times", ran)
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
	// A header is a name, not a row. Reporting success while the
	// selection stays where it was would be worse than saying no.
	before, _ := l.Selected()
	if l.Select("h:margit") {
		t.Fatal("a header was selected")
	}
	if after, _ := l.Selected(); after.Key != before.Key {
		t.Fatalf("selecting a header moved the selection to %v", after.Key)
	}
}

// The panel is rebuilt from the registry on every frame, so a rebuild
// must not undo the wheel: the user would turn it and see nothing move.
func TestListWheelSurvivesARebuild(t *testing.T) {
	var rows []ListRow
	for i := 0; i < 40; i++ {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)

	for i := 0; i < 3; i++ {
		l.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelDown})
	}
	scrolled := l.top
	if scrolled == 0 {
		t.Fatal("the wheel did not scroll")
	}

	l.SetRows(rows)
	if l.top != scrolled {
		t.Fatalf("a rebuild moved the list from row %d back to %d", scrolled, l.top)
	}

	// Moving the selection still brings it into view: that is what
	// moving it means.
	l.HandleKey(press(input.KeyHome, 0))
	if l.top != 0 {
		t.Fatalf("Home left the list showing from row %d", l.top)
	}
}

// A key that cannot be compared would panic where it is compared, on the
// goroutine that draws.
func TestListSurvivesAKeyThatCannotBeCompared(t *testing.T) {
	l := NewList()
	l.Style = listStyled()
	l.Layout(Size{Cols: 20, Rows: 5})

	rows := []ListRow{{Text: "one", Key: []int{1}}, {Text: "two", Key: "two"}}
	l.SetRows(rows)
	l.HandleKey(press(input.KeyDown, 0))
	l.SetRows(rows)
	if l.Select([]string{"x"}) {
		t.Fatal("a key that cannot be compared was found")
	}
	drawList(l, 20, 5)
}

// buttonRows is a panel-shaped list where each header offers something
// to do, the way the connections panel offers what to open on a machine.
func buttonRows() []ListRow {
	rows := panelRows()
	for i := range rows {
		if rows[i].Header {
			rows[i].Button = '+'
		}
	}
	return rows
}

// A header is the one row nothing can be done with, so the button on it
// is the only thing a click there can mean.
func TestListClickingAHeaderButtonRunsIt(t *testing.T) {
	l := newTestList(t, buttonRows(), 40, 10)
	var ran []any
	l.OnButton = func(row ListRow) error {
		ran = append(ran, row.Key)
		return nil
	}
	l.OnActivate = func(ListRow) error {
		t.Error("the button chose the row as well")
		return nil
	}
	was, _ := l.Selected()

	took, err := l.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 2, Col: 38,
	})
	if !took || err != nil {
		t.Fatalf("the press was taken %v, err %v", took, err)
	}
	if len(ran) != 1 || ran[0] != "h:margit" {
		t.Fatalf("ran %v, want the header that was clicked", ran)
	}
	if got, _ := l.Selected(); got.Key != was.Key {
		t.Fatalf("the selection moved to %v", got.Key)
	}
}

// The rest of a header is still a name rather than a button.
func TestListClickingBesideAButtonRunsNothing(t *testing.T) {
	l := newTestList(t, buttonRows(), 40, 10)
	var ran int
	l.OnButton = func(ListRow) error { ran++; return nil }

	for _, col := range []int{0, 5, 37, 39} {
		took, _ := l.HandleMouse(input.MouseEvent{
			Kind: input.MousePress, Button: input.MouseLeft, Row: 2, Col: col,
		})
		if !took {
			t.Errorf("the press at column %d travelled on", col)
		}
	}
	if ran != 0 {
		t.Fatalf("the button ran %d times", ran)
	}
}

// A button on a row that can be chosen is still the button: the two are
// different columns of the same row.
func TestListButtonBeatsChoosingTheRow(t *testing.T) {
	rows := panelRows()
	rows[1].Button = '+'
	l := newTestList(t, rows, 40, 10)
	var button, activate int
	l.OnButton = func(ListRow) error { button++; return nil }
	l.OnActivate = func(ListRow) error { activate++; return nil }

	l.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 1, Col: 38,
	})
	if button != 1 || activate != 0 {
		t.Fatalf("the button ran %d times and the row %d", button, activate)
	}
}

// A failure from the button reaches whoever asked, rather than being
// swallowed by the list.
func TestListButtonReportsAFailure(t *testing.T) {
	l := newTestList(t, buttonRows(), 40, 10)
	boom := errors.New("no room for a menu")
	l.OnButton = func(ListRow) error { return boom }

	took, err := l.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 0, Col: 38,
	})
	if !took || !errors.Is(err, boom) {
		t.Fatalf("took %v, err %v", took, err)
	}
}

// The button is drawn where the click looks for it, and the text and
// the note make room for it.
func TestListDrawsTheButtonAtItsColumn(t *testing.T) {
	l := newTestList(t, []ListRow{
		{Text: "margit", Header: true, Button: '+', Key: 1},
		{Text: "Terminal", Depth: 1, Note: "12 kB/s", Button: '+', Key: 2},
	}, 20, 4)
	g := drawList(l, 20, 4)

	for _, y := range []int{0, 1} {
		if got := g.At(18, y).Rune; got != '+' {
			t.Errorf("row %d has %q where the button is clicked", y, got)
		}
	}
	// The note stops short of it rather than running under it.
	if got := rowOf(g, 1); !strings.Contains(got, "12 kB/s") {
		t.Fatalf("row 1 = %q, want the note as well", got)
	}
	if got := g.At(19, 1).Rune; got != ' ' {
		t.Errorf("the column past the button holds %q", got)
	}
}

// A list too narrow for a button draws none, and a click where one
// would have been is an ordinary click.
func TestListTooNarrowForAButton(t *testing.T) {
	l := newTestList(t, []ListRow{
		{Text: "margit", Header: true, Button: '+', Key: 1},
		{Text: "sh", Depth: 1, Key: 2},
	}, 5, 4)
	g := drawList(l, 5, 4)
	if got := rowOf(g, 0); strings.Contains(got, "+") {
		t.Fatalf("row 0 = %q, want no button in a list this narrow", got)
	}

	var ran int
	l.OnButton = func(ListRow) error { ran++; return nil }
	for col := 0; col < 5; col++ {
		l.HandleMouse(input.MouseEvent{
			Kind: input.MousePress, Button: input.MouseLeft, Row: 0, Col: col,
		})
	}
	if ran != 0 {
		t.Fatalf("the button ran %d times in a list with no room for one", ran)
	}
}

// RowTop is how a caller hangs something off a row: a menu under the
// line that was clicked.
func TestListRowTopFindsARowOnScreen(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	if got := l.RowTop("h:margit"); got != 2 {
		t.Fatalf("the margit header is drawn at %d, want row 2", got)
	}
	if got := l.RowTop("nothing of the sort"); got != -1 {
		t.Fatalf("a key that is not there is at %d, want -1", got)
	}
}

// A row scrolled out of sight has no place to hang anything off.
func TestListRowTopSaysNothingForARowOutOfSight(t *testing.T) {
	var rows []ListRow
	for i := 0; i < 40; i++ {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)
	if got := l.RowTop(0); got != 0 {
		t.Fatalf("the first row is at %d", got)
	}
	l.scrollBy(10)
	if got := l.RowTop(0); got != -1 {
		t.Fatalf("a row scrolled away is at %d, want -1", got)
	}
	if got := l.RowTop(12); got != 2 {
		t.Fatalf("row 12 with ten scrolled past is at %d, want 2", got)
	}
}

// A note and a button do not run into one another.
//
// Headers carry both now: the machine's name, what its connection is
// carrying, and the plus that says what can be opened on it.
func TestListKeepsTheNoteOffTheButton(t *testing.T) {
	l := newTestList(t, []ListRow{
		{Text: "margit", Header: true, Note: "12 kB/s", Button: '+', Key: 1},
	}, 30, 4)
	g := drawList(l, 30, 4)

	at := buttonCol(30)
	if got := g.At(at, 0).Rune; got != '+' {
		t.Fatalf("the button is %q", got)
	}
	if got := g.At(at-1, 0).Rune; got != ' ' {
		t.Fatalf("the column before the button holds %q, want a blank", got)
	}
	// And the note is still there, ending a column short of it.
	row := rowOf(g, 0)
	if !strings.Contains(row, "12 kB/s") {
		t.Fatalf("row = %q, want the note", row)
	}
}

// A list that is a list of what is open marks the row in front even
// while the keys are somewhere else.
//
// The sidebar is that list: which pane is being looked at has to show
// whether or not the user is looking at the sidebar.
func TestListMarksTheCurrentRowWithoutTheKeys(t *testing.T) {
	current := color.RGBA{R: 40, G: 40, B: 90, A: 255}
	l := newTestList(t, panelRows(), 40, 10)
	l.Style.CurrentFG = fg
	l.Style.CurrentBG = current
	l.SetFocus(false)
	// The row in front is told to the list; the bar is the user's and is
	// somewhere else entirely.
	l.SetCurrent("margit-tunnel")
	l.Move(-len(l.Rows()))

	g := drawList(l, 40, 10)
	at := -1
	for i, row := range l.Rows() {
		if row.Key == "margit-tunnel" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("no row for the one in front")
	}
	if at == l.SelectedIndex() {
		t.Fatal("the bar is on the same row, so this proves nothing")
	}
	if got := g.At(1, at).BG; got != current {
		t.Fatalf("the current row is drawn on %v, want %v", got, current)
	}
	// And every other row is the ordinary ground.
	for y := 0; y < 10; y++ {
		if y == at {
			continue
		}
		if got := g.At(1, y).BG; got == current {
			t.Fatalf("row %d is marked as well", y)
		}
	}

	// With the keys, the bar is marked as well, on its own row: the two
	// say different things and both are worth saying.
	l.SetFocus(true)
	g = drawList(l, 40, 10)
	if got := g.At(1, at).BG; got != current {
		t.Fatalf("the row in front is %v once the list has the keys", got)
	}
	if got := g.At(1, l.SelectedIndex()).BG; got != l.Style.SelectedBG {
		t.Fatalf("the bar is drawn on %v, want the selected colour", got)
	}
}
