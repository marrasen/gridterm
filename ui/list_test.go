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

// hoverRows is a list where the second row offers something only while
// the pointer is on it.
func hoverRows() []ListRow {
	rows := panelRows()
	rows[1].HoverButton = '×'
	return rows
}

// A row draws its HoverButton only while the pointer is on it.
func TestListDrawsAHoverButtonOnlyUnderThePointer(t *testing.T) {
	l := newTestList(t, hoverRows(), 30, 6)
	at := l.ButtonCol()
	if at < 0 {
		t.Fatal("the list is too narrow to draw a button")
	}

	if got := drawList(l, 30, 6).At(at, 1).Rune; got == '×' {
		t.Error("the row draws its button with the pointer on no row")
	}
	l.SetHover(1)
	if got := drawList(l, 30, 6).At(at, 1).Rune; got != '×' {
		t.Errorf("the row under the pointer draws %q, want the button", got)
	}
	// And the rows either side of it draw none.
	for _, y := range []int{0, 2} {
		if got := drawList(l, 30, 6).At(at, y).Rune; got == '×' {
			t.Errorf("row %d draws a button, and the pointer is on row 1", y)
		}
	}
	l.SetHover(-1)
	if got := drawList(l, 30, 6).At(at, 1).Rune; got == '×' {
		t.Error("the row still draws its button once the pointer has gone")
	}
}

// The pointer stays on the row it is over when the list scrolls under
// it, rather than being carried along with the rows.
func TestListHoverStaysWhereThePointerIsWhenItScrolls(t *testing.T) {
	// Every row offers something, so which row draws it says where the
	// pointer is rather than which rows can draw at all.
	rows := panelRows()
	for i := range rows {
		rows[i].HoverButton = '×'
	}
	l := newTestList(t, rows, 30, 3)
	at := l.ButtonCol()
	l.SetHover(1)
	if got := drawList(l, 30, 3).At(at, 1).Rune; got != '×' {
		t.Fatalf("the row under the pointer draws %q, want the button", got)
	}

	// The list scrolls under a pointer that has not moved. The row it is
	// on is a different one of the list, in the same place on screen.
	if _, err := l.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseWheelDown,
	}); err != nil {
		t.Fatalf("the wheel: %v", err)
	}
	g := drawList(l, 30, 3)
	if got := g.At(at, 1).Rune; got != '×' {
		t.Errorf("the row under the pointer draws %q after scrolling: the pointer "+
			"was carried along with the rows", got)
	}
	for _, y := range []int{0, 2} {
		if got := g.At(at, y).Rune; got == '×' {
			t.Errorf("row %d draws a button after scrolling, and the pointer is on row 1", y)
		}
	}
}

// Pressing the button column runs OnButton only while the pointer is on
// the row, so a row that draws nothing there is chosen instead.
func TestListPressesAHoverButtonOnlyUnderThePointer(t *testing.T) {
	l := newTestList(t, hoverRows(), 30, 6)
	at := l.ButtonCol()
	var pressed, chosen []string
	l.OnButton = func(row ListRow) error {
		pressed = append(pressed, row.Text)
		return nil
	}
	l.OnActivate = func(row ListRow) error {
		chosen = append(chosen, row.Text)
		return nil
	}

	press := input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: at, Row: 1}
	if _, err := l.HandleMouse(press); err != nil {
		t.Fatalf("pressing with the pointer away: %v", err)
	}
	if len(pressed) != 0 {
		t.Errorf("the button ran with the pointer on no row: %v", pressed)
	}
	if len(chosen) != 1 {
		t.Errorf("the press chose %v, want the row it landed on", chosen)
	}

	l.SetHover(1)
	if _, err := l.HandleMouse(press); err != nil {
		t.Fatalf("pressing with the pointer on the row: %v", err)
	}
	if len(pressed) != 1 {
		t.Errorf("the button ran %d times with the pointer on the row", len(pressed))
	}
	if len(chosen) != 1 {
		t.Errorf("the press chose a row as well as running its button: %v", chosen)
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

	keyTo(t, l, press(input.KeyDown, 0))
	if got, _ := l.Selected(); got.Key != "margit-vim" {
		t.Fatalf("Down went to %v, want the row past the header", got.Key)
	}
	keyTo(t, l, press(input.KeyUp, 0))
	if got, _ := l.Selected(); got.Key != "local-files" {
		t.Fatalf("Up went to %v, want back past the header", got.Key)
	}
}

// A list you can run off the end of is hard to aim at.
func TestListStopsAtTheEnds(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	for range 10 {
		keyTo(t, l, press(input.KeyDown, 0))
	}
	if got, _ := l.Selected(); got.Key != "margit-tunnel" {
		t.Fatalf("selected %v after running down, want the last row", got.Key)
	}
	for range 10 {
		keyTo(t, l, press(input.KeyUp, 0))
	}
	if got, _ := l.Selected(); got.Key != "local-files" {
		t.Fatalf("selected %v after running up, want the first row", got.Key)
	}
}

func TestListHomeAndEnd(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	keyTo(t, l, press(input.KeyEnd, 0))
	if got, _ := l.Selected(); got.Key != "margit-tunnel" {
		t.Fatalf("End selected %v", got.Key)
	}
	keyTo(t, l, press(input.KeyHome, 0))
	if got, _ := l.Selected(); got.Key != "local-files" {
		t.Fatalf("Home selected %v", got.Key)
	}
}

// The list is rebuilt from the registry on every frame. Losing the
// user's place each time would make it impossible to use.
func TestListKeepsTheSelectionAcrossARebuild(t *testing.T) {
	l := newTestList(t, panelRows(), 40, 10)
	keyTo(t, l, press(input.KeyDown, 0))
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
	keyTo(t, l, press(input.KeyEnd, 0))

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

	keyTo(t, l, press(input.KeyEnter, 0))
	keyTo(t, l, press(input.KeyDown, 0))
	keyTo(t, l, press(input.KeySpace, 0))

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
	mouseTo(t, l, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Row: 3})
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
	for i := range 40 {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)

	mouseTo(t, l, input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelDown})
	if l.place.top == 0 {
		t.Fatal("the wheel did not scroll")
	}
	// The selection stays put: the wheel moves the view, not the choice.
	if got, _ := l.Selected(); got.Key != 0 {
		t.Fatalf("the wheel moved the selection to %v", got.Key)
	}
	for range 40 {
		mouseTo(t, l, input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelDown})
	}
	if l.place.top > len(rows)-5 {
		t.Fatalf("scrolled to %d, past the end of %d rows", l.place.top, len(rows))
	}
	for range 60 {
		mouseTo(t, l, input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelUp})
	}
	if l.place.top != 0 {
		t.Fatalf("scrolled to %d, past the start", l.place.top)
	}
}

// Moving down a long list has to bring the selection into view, or Enter
// acts on something nobody can see.
func TestListScrollsToKeepTheSelectionInView(t *testing.T) {
	var rows []ListRow
	for i := range 40 {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)

	keyTo(t, l, press(input.KeyEnd, 0))
	at := l.SelectedIndex()
	if at < l.place.top || at >= l.place.top+5 {
		t.Fatalf("the selection is row %d with rows %d..%d on screen", at, l.place.top, l.place.top+4)
	}
	keyTo(t, l, press(input.KeyHome, 0))
	if l.place.top != 0 {
		t.Fatalf("after Home the list shows from row %d", l.place.top)
	}
}

// A window that shrank must not leave the list scrolled past its end.
func TestListDoesNotStayScrolledPastItsEnd(t *testing.T) {
	var rows []ListRow
	for i := range 40 {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)
	keyTo(t, l, press(input.KeyEnd, 0))

	l.SetRows(rows[:6])
	if l.place.top > 1 {
		t.Fatalf("the list shows from row %d of 6 with 5 on screen", l.place.top)
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

	keyTo(t, l, press(input.KeyDown, 0))
	keyTo(t, l, press(input.KeyEnter, 0))
	mouseTo(t, l, input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelDown})
	took, _ := l.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 0,
	})
	if !took {
		t.Error("a click on an empty list travelled on")
	}
	if ran != 0 {
		t.Fatalf("an empty list ran a row %d times", ran)
	}
	if l.place.top != 0 {
		t.Fatalf("an empty list scrolled to row %d", l.place.top)
	}

	g := drawList(l, 20, 5)
	for y := range 5 {
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
	keyTo(t, l, press(input.KeyDown, 0))
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
	for i := range 40 {
		rows = append(rows, ListRow{Text: "row", Key: i})
	}
	l := newTestList(t, rows, 20, 5)

	for range 3 {
		mouseTo(t, l, input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelDown})
	}
	scrolled := l.place.top
	if scrolled == 0 {
		t.Fatal("the wheel did not scroll")
	}

	l.SetRows(rows)
	if l.place.top != scrolled {
		t.Fatalf("a rebuild moved the list from row %d back to %d", scrolled, l.place.top)
	}

	// Moving the selection still brings it into view: that is what
	// moving it means.
	keyTo(t, l, press(input.KeyHome, 0))
	if l.place.top != 0 {
		t.Fatalf("Home left the list showing from row %d", l.place.top)
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
	keyTo(t, l, press(input.KeyDown, 0))
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

	mouseTo(t, l, input.MouseEvent{
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
	for col := range 5 {
		mouseTo(t, l, input.MouseEvent{
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
	for i := range 40 {
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
	for y := range 10 {
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

// A row's icon goes where its text would start, and the text moves along
// to make room.
//
// The picture is not a character in the text: nothing copies it out,
// and nothing measures the row by it.
func TestListDrawsAnIconInFrontOfTheText(t *testing.T) {
	l := newTestList(t, []ListRow{
		{Text: "margit", Header: true, Key: 1},
		{Text: "vim", Depth: 1, Mark: '•', MarkFG: fg, Icon: grid.Icon(grid.IconTerminal), Key: 2},
	}, 30, 4)
	g := drawList(l, 30, 4)

	// The mark's column stays blank: the icon says what the mark did, in
	// the same colour.
	if got := g.At(0, 1).Rune; got != ' ' {
		t.Fatalf("column 0 holds %q, want a blank beside the icon", got)
	}
	// Then the icon where the text used to start, a blank, then the text.
	if got := g.At(2, 1).Art; got != grid.Icon(grid.IconTerminal) {
		t.Fatalf("column 2 carries %v, want the icon", got)
	}
	if got := g.At(3, 1).Art.Kind; got != grid.ArtNone {
		t.Fatalf("column 3 carries %v, want the blank after the icon", got)
	}
	if got := rowOf(g, 1); !strings.Contains(got, "vim") {
		t.Fatalf("row = %q, want the text as well", got)
	}
	// In columns, not bytes: the icon's blank is one column.
	if at := columnOf(rowOf(g, 1), "vim"); at != 4 {
		t.Fatalf("the text starts at column %d, want it past the icon", at)
	}
	// A row with no icon keeps its text where it was.
	if at := columnOf(rowOf(g, 0), "margit"); at != 0 {
		t.Fatalf("a header with no icon starts at column %d", at)
	}
}

// columnOf is where a word starts in a drawn row, counted in columns.
func columnOf(row, want string) int {
	before, _, ok := strings.Cut(row, want)
	if !ok {
		return -1
	}
	return len([]rune(before))
}

// An icon is drawn in its own colour, which is how one mark says what a
// row is and what state it is in at once.
func TestListDrawsAnIconInItsOwnColour(t *testing.T) {
	green := color.RGBA{R: 0, G: 200, B: 0, A: 255}
	l := newTestList(t, []ListRow{
		{Text: "margit", Header: true, Key: 1},
		{Text: "vim", Depth: 1, Icon: grid.Icon(grid.IconTerminal), IconFG: green, Key: 2},
		{Text: "scp", Depth: 1, Icon: grid.Icon(grid.IconFiles), Key: 3},
	}, 30, 4)
	l.SetFocus(false)
	g := drawList(l, 30, 4)

	if got := g.At(2, 1).FG; got != green {
		t.Fatalf("the icon is drawn in %v, want the colour the row gave it", got)
	}
	// The blank after it is not the icon, so the colour ends with the
	// picture.
	if got := g.At(3, 1).FG; got == green {
		t.Fatal("the blank after the icon took the icon's colour")
	}
	// A row that gave no colour draws its icon in the row's own.
	if got := g.At(2, 2).FG; got != fg {
		t.Fatalf("an icon with no colour is drawn in %v, want the row's %v", got, fg)
	}
}

// A colour picked to stand out against the other rows can disappear
// against the selected one, so the selected row draws its icon in
// whatever it writes its text in.
func TestListWashesTheIconOnTheSelectedRow(t *testing.T) {
	green := color.RGBA{R: 0, G: 200, B: 0, A: 255}
	l := newTestList(t, []ListRow{
		{Text: "margit", Header: true, Key: 1},
		{Text: "vim", Depth: 1, Icon: grid.Icon(grid.IconTerminal), IconFG: green, Key: 2},
	}, 30, 4)
	l.SetFocus(true)
	if l.SelectedIndex() != 1 {
		t.Fatalf("row %d is selected, want the one with the icon", l.SelectedIndex())
	}
	g := drawList(l, 30, 4)

	if got := g.At(2, 1).FG; got != l.Style.SelectedFG {
		t.Fatalf("the icon on the selected row is %v, want the row's own %v",
			got, l.Style.SelectedFG)
	}
	// And without the keys it keeps its colour: the wash is about the
	// selected row's ground, not about the selection.
	l.SetFocus(false)
	g = drawList(l, 30, 4)
	if got := g.At(2, 1).FG; got != green {
		t.Fatalf("the icon is %v once the list has lost the keys, want %v", got, green)
	}
}

// A frame where nothing moved leaves the layer alone, and the icon is
// still there afterwards: a buffer that dirtied nothing by drawing
// nothing would pass the first half of this and fail the user.
func TestListWithMarksDoesNotDirtyAnIdleFrame(t *testing.T) {
	green := color.RGBA{R: 0, G: 200, B: 0, A: 255}
	l := newTestList(t, []ListRow{
		{Text: "margit", Header: true, Mark: '•', MarkFG: green, Depth: 1, Key: 1},
		{Text: "vim", Depth: 1, Icon: grid.Icon(grid.IconTerminal), IconFG: green, Key: 2},
	}, 30, 4)
	l.SetFocus(false)
	g := grid.New(30, 4, color.RGBA{}, color.RGBA{})
	l.Draw(g.View())
	g.ClearDirty()
	l.Draw(g.View())
	if g.AnyDirty() {
		t.Fatal("drawing an unchanged list dirtied the layer")
	}

	// The second draw wrote the same cells, not no cells.
	if got := g.At(2, 1); got.Art != grid.Icon(grid.IconTerminal) || got.FG != green {
		t.Fatalf("after an idle frame the icon cell is %v in %v, want the terminal icon in %v",
			got.Art, got.FG, green)
	}
	if got := g.At(0, 0); got.Rune != '•' || got.FG != green {
		t.Fatalf("after an idle frame the mark cell is %q in %v, want the dot in %v",
			got.Rune, got.FG, green)
	}
}

// A sidebar dragged too narrow to draw an icon still says what state its
// rows are in: the mark stands in, in the colour the icon would have had.
func TestListFallsBackToTheMarkWithNoRoomForAnIcon(t *testing.T) {
	green := color.RGBA{R: 0, G: 200, B: 0, A: 255}
	rows := []ListRow{{
		Text: "vim", Depth: 1,
		Icon: grid.Icon(grid.IconTerminal), IconFG: green,
		Mark: '•', MarkFG: green,
		// The run of the last few seconds takes a column of its own, and
		// it is what leaves the icon with none.
		Art: grid.Graph([]int{1, 2, 3}),
		Key: 1,
	}}

	// Wide enough for both: the icon is drawn and the mark is not, or
	// the row would say the same thing twice.
	l := newTestList(t, rows, 30, 4)
	l.SetFocus(false)
	wide := drawList(l, 30, 4)
	if got := wide.At(2, 0).Art; got != grid.Icon(grid.IconTerminal) {
		t.Fatalf("at 30 columns the row carries %v, want the icon", got)
	}
	if got := wide.At(0, 0).Rune; got == '•' {
		t.Fatal("the row draws its mark as well as its icon")
	}

	// Narrow enough that the graph takes the icon's room, so the mark is
	// back and carries the colour.
	l = newTestList(t, rows, 7, 4)
	l.SetFocus(false)
	narrow := drawList(l, 7, 4)
	for x := range 7 {
		if got := narrow.At(x, 0).Art.Kind; got == grid.ArtIcon {
			t.Fatalf("column %d carries the icon in a list this narrow", x)
		}
	}
	got := narrow.At(0, 0)
	if got.Rune != '•' {
		t.Fatalf("the narrow row holds %q where the mark goes, want the dot", got.Rune)
	}
	if got.FG != green {
		t.Fatalf("the mark is %v, want the %v the icon would have been", got.FG, green)
	}
}

// A header with a colour of its own is drawn in it. The style's header
// colour is what a header falls back to, not what it is held to: a
// heading that has to read differently from the other headings has
// nowhere else to say so.
func TestListDrawsAHeaderInItsOwnColour(t *testing.T) {
	purple := color.RGBA{R: 180, G: 0, B: 200, A: 255}
	l := newTestList(t, []ListRow{
		{Text: "margit", Header: true, Depth: 1, Key: 1},
		{Text: "statio", Header: true, Depth: 1, FG: purple, Key: 2},
		{Text: "vim", Depth: 1, Key: 3},
	}, 30, 4)
	l.SetFocus(false)
	g := drawList(l, 30, 4)

	if got := g.At(2, 1).FG; got != purple {
		t.Fatalf("the header with a colour is drawn in %v, want %v", got, purple)
	}
	// And one without keeps the style's.
	if got := g.At(2, 0).FG; got != l.Style.HeaderFG {
		t.Fatalf("the plain header is drawn in %v, want the style's %v",
			got, l.Style.HeaderFG)
	}
}

// A list too narrow for an icon and a word draws the word.
func TestListWithNoRoomForAnIcon(t *testing.T) {
	l := newTestList(t, []ListRow{
		{Text: "vim", Depth: 1, Icon: grid.Icon(grid.IconTerminal), Key: 1},
	}, 4, 4)
	g := drawList(l, 4, 4)
	for x := range 4 {
		if got := g.At(x, 0).Art.Kind; got != grid.ArtNone {
			t.Fatalf("column %d carries %v in a list this narrow", x, got)
		}
	}
}

// edgedRow is the row edged stripes: one that is not the selected one,
// whose ground is its own.
const edgedRow = 3

// edged is the panel's rows with a stripe down the left of one of them.
func edged(one, two color.RGBA) []ListRow {
	rows := panelRows()
	rows[edgedRow].Edge = [2]color.RGBA{one, two}
	return rows
}

// notSelected fails the test if the striped row is the selected one,
// whose ground is its own and would hide what the stripe did.
func notSelected(t *testing.T, l *List) {
	t.Helper()
	if got, ok := l.Selected(); ok && got.Key == panelRows()[edgedRow].Key {
		t.Fatalf("row %d is selected, and these tests want a row with the list's own ground", edgedRow)
	}
}

// A row's stripe colours the ground of its first cells and reaches no
// further along the row.
func TestARowsEdgeColoursItsFirstCells(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0x80}
	l := newTestList(t, edged(red, color.RGBA{}), 40, 10)
	notSelected(t, l)
	g := drawList(l, 40, 10)
	plain := drawList(newTestList(t, panelRows(), 40, 10), 40, 10)

	was, got := plain.At(0, 3).BG, g.At(0, 3).BG
	if got == was {
		t.Errorf("the striped row's first cell is %v, the same as it is with no stripe", got)
	}
	if got.R <= was.R {
		t.Errorf("the stripe took the ground from %v to %v, and the colour on it is red", was, got)
	}
	if second := g.At(1, 3).BG; second != plain.At(1, 3).BG {
		t.Errorf("the second cell is %v with only one colour on the row, want the ordinary %v",
			second, plain.At(1, 3).BG)
	}
	if rest := g.At(6, 3).BG; rest != plain.At(6, 3).BG {
		t.Errorf("the stripe reached column 6, which is %v", rest)
	}
	if got.A != was.A {
		t.Errorf("the stripe left the ground %#x see-through, and it was %#x", got.A, was.A)
	}
}

// Two colours take a cell each, in the order they are given.
func TestTwoEdgeColoursTakeACellEach(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0x80}
	blue := color.RGBA{B: 0xff, A: 0x80}
	l := newTestList(t, edged(red, blue), 40, 10)
	g := drawList(l, 40, 10)

	first, second := g.At(0, 3).BG, g.At(1, 3).BG
	if first.R <= second.R {
		t.Errorf("the first cell is %v and the second %v, want the red one first", first, second)
	}
	if second.B <= first.B {
		t.Errorf("the first cell is %v and the second %v, want the blue one second", first, second)
	}
}

// The stripe changes the ground and nothing else, so what is written on
// the row reads as it did. On the selected row, which has a ground of
// its own for the stripe to go over.
func TestARowsEdgeLeavesWhatIsDrawnOnIt(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0xff}
	rows := panelRows()
	rows[1].Depth = 0
	striped := make([]ListRow, len(rows))
	copy(striped, rows)
	striped[1].Edge = [2]color.RGBA{red, red}

	g := drawList(newTestList(t, striped, 40, 10), 40, 10)
	want := drawList(newTestList(t, rows, 40, 10), 40, 10)
	for x := range 2 {
		if got := g.At(x, 1).Rune; got != want.At(x, 1).Rune {
			t.Errorf("column %d says %q under the stripe and %q without it",
				x, got, want.At(x, 1).Rune)
		}
		if got := g.At(x, 1).FG; got != want.At(x, 1).FG {
			t.Errorf("column %d is written in %v under the stripe and %v without it",
				x, got, want.At(x, 1).FG)
		}
	}
	if g.At(0, 1).BG == want.At(0, 1).BG {
		t.Error("the stripe changed no ground at all")
	}
}

// A second colour with no room for it draws nothing, rather than
// spilling onto the first colour's cell.
func TestASecondEdgeColourWithNoRoomDrawsNothing(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0x80}
	blue := color.RGBA{B: 0xff, A: 0x80}
	one := drawList(newTestList(t, edged(red, blue), 1, 10), 1, 10)
	only := drawList(newTestList(t, edged(red, color.RGBA{}), 1, 10), 1, 10)

	if got, want := one.At(0, 3).BG, only.At(0, 3).BG; got != want {
		t.Errorf("the one column that fits is %v with a second colour and %v without it", got, want)
	}
}

// A row that says how far something has got keeps its stripe. The fill
// washes the same cells, so the stripe has to go on after it.
func TestARowsEdgeSurvivesItsFill(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0x80}
	filling := func(rows []ListRow) *grid.Grid {
		rows[edgedRow].Fill = 1
		l := newTestList(t, rows, 40, 10)
		l.Style.FillBG = color.RGBA{G: 0x60, A: 0xff}
		return drawList(l, 40, 10)
	}
	g, want := filling(edged(red, color.RGBA{})), filling(panelRows())

	if got := want.At(0, 3).BG; got.G == 0 {
		t.Fatalf("the fill washed nothing, so this proves nothing: the cell is %v", got)
	}
	if got := g.At(0, 3).BG; got == want.At(0, 3).BG {
		t.Errorf("a filled row's first cell is %v with a stripe and without one", got)
	}
}

// A double-width character takes one ground, not two. The stripe stops
// short rather than colouring half of it, which is what the fill does.
func TestARowsEdgeDoesNotSplitAWideCharacter(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0x80}
	blue := color.RGBA{B: 0xff, A: 0x80}
	rows := edged(red, blue)
	// Depth 0 so the text starts in the first column, and a character
	// two columns wide standing across the two the stripe wants.
	rows[edgedRow].Depth = 0
	rows[edgedRow].Text = "世界"
	g := drawList(newTestList(t, rows, 40, 10), 40, 10)

	if g.At(1, 3).Width != 0 {
		t.Fatalf("column 1 is not the far half of a wide character, so this proves nothing")
	}
	if first, second := g.At(0, 3).BG, g.At(1, 3).BG; first != second {
		t.Errorf("the two halves of one character stand on %v and %v", first, second)
	}
}

// A list drawn on a see-through ground gets no stripe: there is nothing
// under it for the wash to change, and writing the cell would dirty the
// row for no pixels.
func TestARowsEdgeLeavesASeeThroughGroundAlone(t *testing.T) {
	red := color.RGBA{R: 0xff, A: 0x80}
	clear := newTestList(t, edged(red, color.RGBA{}), 40, 10)
	clear.Style.BG = color.RGBA{}
	g := drawList(clear, 40, 10)

	if got := g.At(0, 3).BG; got != (color.RGBA{}) {
		t.Errorf("the first cell is %v, want it left see-through", got)
	}
}
