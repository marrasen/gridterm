package ui

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

func nop() error { return nil }

// testCommands builds a registry of the given titles, ids taken from the
// titles so a test can name them.
func testCommands(titles ...string) *Commands {
	c := NewCommands()
	for _, title := range titles {
		c.MustRegister(Command{ID: strings.ToLower(title), Title: title, Run: nop})
	}
	return c
}

func matchIDs(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Command.ID
	}
	return out
}

func TestMatchCommandsFindsASubsequence(t *testing.T) {
	cmds := testCommands("Close pane", "Copy", "New tab", "Next pane").All()

	got := matchIDs(MatchCommands(cmds, "cp"))

	// "Close pane" has the query as two word starts; "Copy" has it as
	// two letters in one word.
	if len(got) != 2 || got[0] != "close pane" || got[1] != "copy" {
		t.Errorf("matches = %v, want the word-start match first", got)
	}
}

func TestMatchCommandsEmptyQueryMatchesEverything(t *testing.T) {
	cmds := testCommands("One", "Two", "Three").All()

	got := MatchCommands(cmds, "")

	if len(got) != 3 {
		t.Errorf("%d matches for an empty query, want all 3", len(got))
	}
}

func TestMatchCommandsRejectsWhatIsNotThere(t *testing.T) {
	cmds := testCommands("Copy", "Paste").All()

	if got := MatchCommands(cmds, "zz"); len(got) != 0 {
		t.Errorf("matches = %v, want none", matchIDs(got))
	}
	// Every letter has to appear, in order.
	if got := MatchCommands(cmds, "yp"); len(got) != 0 {
		t.Errorf("matches = %v, want none: the letters are in the wrong order", matchIDs(got))
	}
}

func TestMatchCommandsIgnoresCase(t *testing.T) {
	cmds := testCommands("Increase font size").All()

	for _, query := range []string{"ifs", "IFS", "iFs", "increase"} {
		if got := MatchCommands(cmds, query); len(got) != 1 {
			t.Errorf("query %q found %d, want 1", query, len(got))
		}
	}
}

// TestMatchCommandsOrdering pins what makes one match better than
// another, which is what decides whether Enter does the right thing.
func TestMatchCommandsOrdering(t *testing.T) {
	for _, tc := range []struct {
		name   string
		titles []string
		query  string
		want   string
	}{
		{
			name:   "word starts beat letters in the middle",
			titles: []string{"Scroll back", "Split right"},
			query:  "sr", want: "split right",
		},
		{
			name:   "consecutive letters beat scattered ones",
			titles: []string{"Close pane", "Copy"},
			query:  "co", want: "copy",
		},
		{
			name:   "a shorter title wins a tie",
			titles: []string{"New tab", "New tab somewhere else"},
			query:  "nt", want: "new tab",
		},
		{
			name:   "the start of the title counts",
			titles: []string{"Reset font size", "Increase font size"},
			query:  "fs", want: "reset font size",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchIDs(MatchCommands(testCommands(tc.titles...).All(), tc.query))

			if len(got) == 0 || got[0] != tc.want {
				t.Errorf("matches = %v, want %q first", got, tc.want)
			}
		})
	}
}

// TestMatchCommandsIsStable checks the order does not shuffle between
// runs, which would move a line out from under the pointer.
func TestMatchCommandsIsStable(t *testing.T) {
	cmds := testCommands("Alpha", "Beta", "Gamma", "Delta").All()

	first := matchIDs(MatchCommands(cmds, "a"))
	for i := 0; i < 20; i++ {
		got := matchIDs(MatchCommands(cmds, "a"))
		if len(got) != len(first) {
			t.Fatalf("match count changed: %v then %v", first, got)
		}
		for j := range first {
			if got[j] != first[j] {
				t.Fatalf("order changed: %v then %v", first, got)
			}
		}
	}
}

// TestMatchCommandsReportsWhereItMatched checks the positions the
// palette uses to pick out the matched letters.
func TestMatchCommandsReportsWhereItMatched(t *testing.T) {
	cmds := testCommands("Close pane").All()

	got := MatchCommands(cmds, "cp")

	if len(got) != 1 {
		t.Fatalf("%d matches, want 1", len(got))
	}
	// "Close pane": C at 0, p at 6.
	if want := []int{0, 6}; len(got[0].At) != 2 || got[0].At[0] != want[0] || got[0].At[1] != want[1] {
		t.Errorf("matched at %v, want %v", got[0].At, want)
	}
}

// newTestPalette returns a palette and a count of how often it asked to
// be closed.
func newTestPalette(t *testing.T, cmds *Commands) (*Palette, *int) {
	t.Helper()
	closed := 0
	p := NewPalette(cmds, NewKeymap(), func() { closed++ })
	p.Layout(Size{Cols: 40, Rows: 12})
	// The modal stack focuses a dialog when it pushes it.
	p.SetFocus(true)
	return p, &closed
}

func TestPaletteTypingFilters(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste", "Close pane"))
	if len(p.Matches()) != 3 {
		t.Fatalf("%d matches before typing, want all 3", len(p.Matches()))
	}

	typeInto(t, p, "cp")

	got := matchIDs(p.Matches())
	if len(got) != 2 {
		t.Errorf("matches = %v, want the two that contain c then p", got)
	}
	if p.Query() != "cp" {
		t.Errorf("query = %q, want %q", p.Query(), "cp")
	}
}

func TestPaletteBackspace(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste"))
	typeInto(t, p, "co")

	press(input.KeyBackspace, 0)
	if _, err := p.HandleKey(press(input.KeyBackspace, 0)); err != nil {
		t.Fatalf("backspace: %v", err)
	}

	if p.Query() != "c" {
		t.Errorf("query = %q, want %q", p.Query(), "c")
	}
	// And backspacing an empty query is harmless.
	p.Reset()
	if _, err := p.HandleKey(press(input.KeyBackspace, 0)); err != nil {
		t.Fatalf("backspace on an empty query: %v", err)
	}
	if p.Query() != "" {
		t.Errorf("query = %q, want it still empty", p.Query())
	}
}

func TestPaletteMoveStopsAtTheEnds(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("One", "Two", "Three"))

	// Up from the first stays on the first.
	p.HandleKey(press(input.KeyUp, 0))
	if got, _ := p.Selected(); got.ID != matchIDs(p.Matches())[0] {
		t.Error("moving up from the first line went somewhere")
	}

	for i := 0; i < 10; i++ {
		p.HandleKey(press(input.KeyDown, 0))
	}
	last := p.Matches()[len(p.Matches())-1].Command.ID
	if got, _ := p.Selected(); got.ID != last {
		t.Errorf("selected %q after running off the end, want %q", got.ID, last)
	}
}

func TestPaletteEnterRunsTheSelected(t *testing.T) {
	ran := ""
	cmds := NewCommands()
	cmds.MustRegister(
		Command{ID: "one", Title: "One", Run: func() error { ran = "one"; return nil }},
		Command{ID: "two", Title: "Two", Run: func() error { ran = "two"; return nil }},
	)
	p, closed := newTestPalette(t, cmds)
	typeInto(t, p, "tw")

	handled, err := p.HandleKey(press(input.KeyEnter, 0))

	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if !handled {
		t.Error("Enter was not handled")
	}
	if ran != "two" {
		t.Errorf("ran %q, want two", ran)
	}
	if *closed != 1 {
		t.Errorf("the dialog asked to close %d times, want once", *closed)
	}
}

// TestPaletteEnterWithNoMatchJustCloses checks a query that found
// nothing: Enter must not run whatever happens to be first.
func TestPaletteEnterWithNoMatchJustCloses(t *testing.T) {
	ran := 0
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "one", Title: "One", Run: func() error { ran++; return nil }})
	p, closed := newTestPalette(t, cmds)
	typeInto(t, p, "zzz")

	if _, err := p.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	if ran != 0 {
		t.Error("Enter ran a command the query did not find")
	}
	if *closed != 1 {
		t.Errorf("the dialog asked to close %d times, want once", *closed)
	}
}

// TestPaletteReportsAFailedCommand checks that a command which failed
// says so rather than being swallowed by the dialog.
func TestPaletteReportsAFailedCommand(t *testing.T) {
	boom := errors.New("boom")
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "bad", Title: "Bad", Run: func() error { return boom }})
	p, _ := newTestPalette(t, cmds)

	_, err := p.HandleKey(press(input.KeyEnter, 0))

	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the command's own", err)
	}
}

func TestPaletteEscapeCloses(t *testing.T) {
	p, closed := newTestPalette(t, testCommands("One"))

	handled, err := p.HandleKey(press(input.KeyEscape, 0))

	if err != nil || !handled {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	if *closed != 1 {
		t.Errorf("the dialog asked to close %d times, want once", *closed)
	}
}

// TestPaletteLetsOtherKeysThrough checks that a shortcut the dialog has
// no use for still reaches the bindings, so quit and close keep working
// while it is open.
func TestPaletteLetsOtherKeysThrough(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("One"))

	for _, ev := range []input.Event{
		press(input.KeyQ, input.ModCtrl),
		press(input.KeyTab, input.ModCtrl),
		{Kind: input.KeyRelease, Key: input.KeyA},
		{Kind: input.Text, Rune: '\x01', NormalText: false},
	} {
		if handled, _ := p.HandleKey(ev); handled {
			t.Errorf("%+v was swallowed by the dialog", ev)
		}
	}
}

func TestPaletteResetEmptiesTheQuery(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste"))
	typeInto(t, p, "co")

	p.Reset()

	if p.Query() != "" {
		t.Errorf("query = %q, want it empty", p.Query())
	}
	if len(p.Matches()) != 2 {
		t.Errorf("%d matches after reset, want all of them back", len(p.Matches()))
	}
}

// TestPaletteDrawsItsBox checks the dialog appears where it says it
// does, with the query line and the matches under it.
func TestPaletteDrawsItsBox(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste"))
	p.Style = PaletteStyle{FG: fg, BG: bg, MatchFG: fg, SelectedFG: bg, SelectedBG: fg, ChordFG: fg}
	typeInto(t, p, "co")
	g := grid.New(40, 12, fg, bg)

	p.Draw(g.View())

	box := p.box()
	if box.Empty() {
		t.Fatal("the dialog has no box")
	}
	query := strings.TrimRight(rowOf(g, box.Y)[box.X:box.X+box.Cols], " ")
	if query != "> co" {
		t.Errorf("query line = %q, want %q", query, "> co")
	}
	first := rowOf(g, box.Y+1)[box.X : box.X+box.Cols]
	if !strings.Contains(first, "Copy") {
		t.Errorf("first line = %q, want it to hold the match", first)
	}
	// Nothing outside the box was touched.
	if got := strings.TrimSpace(rowOf(g, 0)); got != "" {
		t.Errorf("row 0 = %q, want the dialog to stay in its box", got)
	}
}

// TestPaletteCursorSitsAfterTheQuery checks the dialog looks like
// somewhere to type. It holds focus, so the cursor is its to place.
func TestPaletteCursorSitsAfterTheQuery(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy"))
	typeInto(t, p, "co")
	g := grid.New(40, 12, fg, bg)

	p.Draw(g.View())

	box := p.box()
	cur := g.Cursor()
	if !cur.Visible {
		t.Fatal("the dialog drew no cursor")
	}
	if want := box.X + 2 + 2; cur.X != want || cur.Y != box.Y {
		t.Errorf("cursor at %d,%d, want %d,%d", cur.X, cur.Y, want, box.Y)
	}
}

// TestPaletteTooSmallDrawsNothing checks the sizes a layout passes
// through while it settles.
func TestPaletteTooSmallDrawsNothing(t *testing.T) {
	for _, size := range []Size{{}, {Cols: 4, Rows: 10}, {Cols: 40, Rows: 1}} {
		p, _ := newTestPalette(t, testCommands("One"))
		p.Style = PaletteStyle{FG: fg, BG: bg}
		p.Layout(size)
		g := grid.New(40, 12, fg, bg)

		p.Draw(g.View())

		// The layer is still cleared, but nothing opaque is put on it.
		for y := 0; y < 12; y++ {
			for x := 0; x < 40; x++ {
				if got := g.At(x, y); got.BG.A != 0 || got.Rune != ' ' {
					t.Fatalf("size %+v: cell %d,%d = %+v, want the layer left clear",
						size, x, y, got)
				}
			}
		}
	}
}

func TestPaletteClickRunsALine(t *testing.T) {
	ran := ""
	cmds := NewCommands()
	cmds.MustRegister(
		Command{ID: "one", Title: "One", Run: func() error { ran = "one"; return nil }},
		Command{ID: "two", Title: "Two", Run: func() error { ran = "two"; return nil }},
	)
	p, closed := newTestPalette(t, cmds)
	box := p.box()

	// The second line of the list, which is the second match.
	_, err := p.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + 3, Row: box.Y + 2,
	})

	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if ran != "two" {
		t.Errorf("ran %q, want the line that was clicked", ran)
	}
	if *closed != 1 {
		t.Errorf("the dialog asked to close %d times, want once", *closed)
	}
}

func TestPaletteClickOutsideCloses(t *testing.T) {
	p, closed := newTestPalette(t, testCommands("One"))

	handled, err := p.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 0, Row: 0,
	})

	if err != nil || !handled {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	if *closed != 1 {
		t.Errorf("the dialog asked to close %d times, want once", *closed)
	}
}

// TestPaletteSwallowsTheMouse checks that a drag does not reach what is
// behind the dialog, which would select text nobody can see.
func TestPaletteSwallowsTheMouse(t *testing.T) {
	p, closed := newTestPalette(t, testCommands("One"))

	for _, ev := range []input.MouseEvent{
		{Kind: input.MouseMove, Col: 1, Row: 1},
		{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 1, Row: 1},
		{Kind: input.MousePress, Button: input.MouseWheelUp, Col: 1, Row: 1},
	} {
		handled, err := p.HandleMouse(ev)
		if err != nil {
			t.Fatalf("HandleMouse: %v", err)
		}
		if !handled {
			t.Errorf("%+v reached what is behind the dialog", ev)
		}
	}
	if *closed != 0 {
		t.Error("something other than a press closed the dialog")
	}
}

// TestPaletteWithNoCommandsIsSafe checks a registry nothing is
// registered in, which is what a program has before it sets itself up.
func TestPaletteWithNoCommandsIsSafe(t *testing.T) {
	p, closed := newTestPalette(t, NewCommands())

	if got, ok := p.Selected(); ok {
		t.Errorf("selected %+v, want nothing", got)
	}
	if _, err := p.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("Enter with nothing to run: %v", err)
	}
	if *closed != 1 {
		t.Error("Enter did not close the dialog")
	}
	p.HandleKey(press(input.KeyDown, 0))
	p.Draw(grid.New(40, 12, fg, bg).View())
}

// TestPaletteIsAWidget checks the interfaces the toolkit routes on.
func TestPaletteIsAWidget(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("One"))

	var w Widget = p
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"keys", func() bool { _, ok := w.(KeyHandler); return ok }()},
		{"mouse", func() bool { _, ok := w.(MouseHandler); return ok }()},
		{"gestures", func() bool { _, ok := w.(GestureCanceller); return ok }()},
	} {
		if !tc.ok {
			t.Errorf("the palette does not take %s", tc.name)
		}
	}
}

// typeInto sends a string to a palette as ordinary typing.
func typeInto(t *testing.T, p *Palette, s string) {
	t.Helper()
	for _, r := range s {
		handled, err := p.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
		if err != nil {
			t.Fatalf("typing %q: %v", r, err)
		}
		if !handled {
			t.Fatalf("typing %q was not handled", r)
		}
	}
}

// TestPaletteLeavesTheRestOfItsLayerClear checks that what is behind the
// dialog shows through. The layer is composited over the widget tree, so
// an opaque cell anywhere outside the box blanks the window.
func TestPaletteLeavesTheRestOfItsLayerClear(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste"))
	p.Style = PaletteStyle{FG: fg, BG: bg, MatchFG: fg, SelectedFG: bg, SelectedBG: fg, ChordFG: fg}
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})

	p.Draw(g.View())

	box := p.box()
	for y := 0; y < 12; y++ {
		for x := 0; x < 40; x++ {
			if box.Contains(x, y) {
				continue
			}
			if got := g.At(x, y); got.BG.A != 0 {
				t.Fatalf("cell %d,%d outside the box = %+v, want it see-through", x, y, got)
			}
		}
	}
}

// TestPaletteClearsWhatItDrewLastTime checks the box shrinking as the
// query narrows. The layer is the dialog's own, so a list that no longer
// matches anything must not be left standing beside the new box.
func TestPaletteClearsWhatItDrewLastTime(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste", "Close pane", "New tab"))
	p.Style = PaletteStyle{FG: fg, BG: bg, MatchFG: fg, SelectedFG: bg, SelectedBG: fg, ChordFG: fg}
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})
	p.Draw(g.View())

	typeInto(t, p, "zzz")
	p.Draw(g.View())

	box := p.box()
	for y := 0; y < 12; y++ {
		if box.Contains(box.X, y) {
			continue
		}
		if got := strings.TrimSpace(rowOf(g, y)); got != "" {
			t.Errorf("row %d = %q, want the old list gone", y, got)
		}
	}
}

// TestPaletteClearsAfterTheWindowResizes checks the same rule when the
// box moves rather than shrinks: two dialogs side by side is what a
// widened window would otherwise show.
func TestPaletteClearsAfterTheWindowResizes(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste"))
	p.Style = PaletteStyle{FG: fg, BG: bg, MatchFG: fg, SelectedFG: bg, SelectedBG: fg, ChordFG: fg}
	g := grid.New(80, 12, color.RGBA{}, color.RGBA{})
	p.Draw(g.View())
	narrow := p.box()

	p.Layout(Size{Cols: 80, Rows: 12})
	p.Draw(g.View())

	wide := p.box()
	if wide.X == narrow.X {
		t.Skip("the box did not move, so there is nothing to have left behind")
	}
	// Every cell of the old box that the new one does not cover is clear.
	for y := narrow.Y; y < narrow.Y+narrow.Rows; y++ {
		for x := narrow.X; x < narrow.X+narrow.Cols; x++ {
			if wide.Contains(x, y) {
				continue
			}
			if got := g.At(x, y); got.BG.A != 0 || got.Rune != ' ' {
				t.Fatalf("cell %d,%d = %+v, want the old box gone", x, y, got)
			}
		}
	}
}

// TestPaletteScrollsToKeepTheSelectionInView checks a list longer than
// the box. Without scrolling the selection walks off the bottom and
// Enter runs a command nobody can see.
func TestPaletteScrollsToKeepTheSelectionInView(t *testing.T) {
	titles := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		titles = append(titles, "Command "+string(rune('a'+i)))
	}
	p, _ := newTestPalette(t, testCommands(titles...))
	p.Style = PaletteStyle{FG: fg, BG: bg, MatchFG: fg, SelectedFG: bg, SelectedBG: fg, ChordFG: fg}
	if len(p.Matches()) <= p.rows() {
		t.Fatalf("%d matches fit in %d rows, too few for the test", len(p.Matches()), p.rows())
	}

	for i := 0; i < len(p.Matches()); i++ {
		want, _ := p.Selected()
		if !p.drawsSelection() {
			t.Fatalf("line %d: the selection is not on screen", i)
		}
		g := grid.New(40, 12, color.RGBA{}, color.RGBA{})
		p.Draw(g.View())
		if !strings.Contains(rowOf(g, p.box().Y+p.at-p.top+1), want.Title) {
			t.Fatalf("line %d: %q is not drawn where the selection is", i, want.Title)
		}
		p.HandleKey(press(input.KeyDown, 0))
	}
	// And back up to the top again.
	for i := 0; i < len(p.Matches()); i++ {
		p.HandleKey(press(input.KeyUp, 0))
	}
	if p.top != 0 || p.at != 0 {
		t.Errorf("back at the top, top = %d and at = %d, want 0 and 0", p.top, p.at)
	}
}

// drawsSelection reports whether the selected line is one of the drawn
// ones.
func (p *Palette) drawsSelection() bool {
	return p.at >= p.top && p.at < p.top+p.rows()
}

// TestPaletteTypingSnapsBackToTheTop checks that a new query gives a new
// answer: the line the old selection sat on means nothing now.
func TestPaletteTypingSnapsBackToTheTop(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Close pane", "Cut"))
	p.HandleKey(press(input.KeyDown, 0))
	p.HandleKey(press(input.KeyDown, 0))
	if p.at == 0 {
		t.Fatal("moving down did not move the selection")
	}

	typeInto(t, p, "c")

	if p.at != 0 || p.top != 0 {
		t.Errorf("after typing, at = %d and top = %d, want the best match selected", p.at, p.top)
	}
}

// TestPaletteRunsAfterClosing checks the order the dialog does two
// things in. A command that opens another dialog would otherwise be
// fighting this one for the modal stack.
func TestPaletteRunsAfterClosing(t *testing.T) {
	order := []string{}
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "one", Title: "One", Run: func() error {
		order = append(order, "ran")
		return nil
	}})
	p := NewPalette(cmds, NewKeymap(), func() { order = append(order, "closed") })
	p.Layout(Size{Cols: 40, Rows: 12})

	if _, err := p.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	if len(order) != 2 || order[0] != "closed" || order[1] != "ran" {
		t.Errorf("order = %v, want the dialog closed before the command ran", order)
	}
}

// TestPaletteHighlightsTheSelectedLine checks that the line Enter would
// run is the one that looks selected.
func TestPaletteHighlightsTheSelectedLine(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("One", "Two", "Three"))
	p.Style = PaletteStyle{
		FG: fg, BG: bg, MatchFG: fg,
		SelectedFG: bg, SelectedBG: fg, ChordFG: fg,
	}
	p.HandleKey(press(input.KeyDown, 0))
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})

	p.Draw(g.View())

	box := p.box()
	selected := box.Y + 1 + (p.at - p.top)
	if got := g.At(box.X+1, selected).BG; got != fg {
		t.Errorf("the selected line's background = %v, want the selected colour %v", got, fg)
	}
	other := box.Y + 1
	if got := g.At(box.X+1, other).BG; got == fg {
		t.Error("a line that is not selected looks selected too")
	}
}

// TestPaletteTitleKeepsGraphemeClusters checks a title whose characters
// are not one rune each. Picking out matched letters rune by rune would
// give a combining mark a cell of its own.
func TestPaletteTitleKeepsGraphemeClusters(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Café x"))
	p.Style = PaletteStyle{FG: fg, BG: bg, MatchFG: fg, SelectedFG: fg, SelectedBG: bg, ChordFG: fg}
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})

	p.Draw(g.View())

	box := p.box()
	line := rowOf(g, box.Y+1)
	if !strings.Contains(line, "Cafe x") {
		t.Errorf("line = %q, want the accent in its base character's cell", line)
	}
	// The accent shares the cell with the e rather than taking its own.
	at := strings.Index(line, "Cafe")
	if got := g.At(at+3, box.Y+1); len(got.Comb) != 1 {
		t.Errorf("the e cell = %+v, want it carrying the combining mark", got)
	}
}

// TestPaletteLongQueryShowsItsEnd checks that typing past the width of
// the box keeps the caret in view rather than typing blind.
func TestPaletteLongQueryShowsItsEnd(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("One"))
	p.Style = PaletteStyle{FG: fg, BG: bg, MatchFG: fg, SelectedFG: fg, SelectedBG: bg, ChordFG: fg}
	p.Layout(Size{Cols: 16, Rows: 8})
	typeInto(t, p, "abcdefghijklmnopqrstuvwxyz")
	g := grid.New(16, 8, color.RGBA{}, color.RGBA{})

	p.Draw(g.View())

	box := p.box()
	line := rowOf(g, box.Y)[box.X : box.X+box.Cols]
	if !strings.Contains(line, "z") {
		t.Errorf("query line = %q, want the end of what was typed", line)
	}
	cur := g.Cursor()
	if !cur.Visible || cur.X < box.X || cur.X >= box.X+box.Cols {
		t.Errorf("cursor at %d, want it inside the box %+v", cur.X, box)
	}
}

// TestPaletteRejectsDelete checks that Delete is not a character to
// type, whatever the platform reports.
func TestPaletteRejectsDelete(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("One"))

	if handled, _ := p.HandleKey(input.Event{Kind: input.Text, Rune: 0x7f, NormalText: true}); handled {
		t.Error("Delete was taken as typing")
	}
	if p.Query() != "" {
		t.Errorf("query = %q, want it empty", p.Query())
	}
}

// TestMatchScoreForTheStartOfATitle pins the bonus for matching at
// position zero. Both of these match a word start, so only the leading
// bonus separates them, and without it the shorter title would win.
func TestMatchScoreForTheStartOfATitle(t *testing.T) {
	cmds := testCommands("Alphabetical ordering", "Big Apple").All()

	got := matchIDs(MatchCommands(cmds, "a"))

	if len(got) == 0 || got[0] != "alphabetical ordering" {
		t.Errorf("matches = %v, want the title that starts with the letter first", got)
	}
}

// TestMatchScoreForConsecutiveLetters pins the bonus for a letter
// following the one before it. Neither of these matches a word start, so
// only that bonus separates them, and without it the shorter title would
// win.
func TestMatchScoreForConsecutiveLetters(t *testing.T) {
	cmds := testCommands("Comfortable", "Freight").All()

	got := matchIDs(MatchCommands(cmds, "rt"))

	if len(got) == 0 || got[0] != "comfortable" {
		t.Errorf("matches = %v, want the title with the letters together first", got)
	}
}

// styled returns a palette whose colours all differ, so a test can tell
// which style a cell was drawn in.
func styled() PaletteStyle {
	return PaletteStyle{
		FG:         color.RGBA{0x10, 0x10, 0x10, 0xff},
		BG:         color.RGBA{0x20, 0x20, 0x20, 0xff},
		MatchFG:    color.RGBA{0x30, 0x30, 0x30, 0xff},
		SelectedFG: color.RGBA{0x40, 0x40, 0x40, 0xff},
		SelectedBG: color.RGBA{0x50, 0x50, 0x50, 0xff},
		ChordFG:    color.RGBA{0x60, 0x60, 0x60, 0xff},
	}
}

// TestPaletteIdleDrawDirtiesNothing checks the property the compositor
// is built around. Drawn straight onto the layer, the box background and
// the text over it would each be a change, and an unchanged dialog would
// dirty its rows on every frame for as long as it was open.
func TestPaletteIdleDrawDirtiesNothing(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy", "Paste", "Close pane"))
	p.Style = styled()
	typeInto(t, p, "c")
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})
	p.Draw(g.View())
	g.ClearDirty()

	for i := 0; i < 3; i++ {
		p.Draw(g.View())
		if g.AnyDirty() {
			t.Fatalf("draw %d of an unchanged dialog dirtied the layer", i)
		}
	}

	// And a real change still gets through.
	typeInto(t, p, "o")
	p.Draw(g.View())
	if !g.AnyDirty() {
		t.Error("typing did not change anything on the layer")
	}
}

// TestPaletteScrollFollowsTheWindow checks a window resized while the
// list is scrolled. A shorter box shows fewer lines, and the selection
// would be left below it.
func TestPaletteScrollFollowsTheWindow(t *testing.T) {
	titles := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		titles = append(titles, "Command "+string(rune('a'+i)))
	}
	p, _ := newTestPalette(t, testCommands(titles...))
	p.Style = styled()
	for i := 0; i < len(p.Matches()); i++ {
		p.HandleKey(press(input.KeyDown, 0))
	}
	if !p.drawsSelection() {
		t.Fatal("the selection is off screen before the resize")
	}

	p.Layout(Size{Cols: 40, Rows: 10})

	if !p.drawsSelection() {
		t.Errorf("after shrinking the window, at = %d and top = %d: the selection is off screen",
			p.at, p.top)
	}

	// Growing must not leave the list scrolled past its end either.
	p.Layout(Size{Cols: 40, Rows: 30})
	if got, want := p.rows(), max(p.box().Rows-1, 0); got != want {
		t.Errorf("%d lines drawn in a box with room for %d", got, want)
	}
	if !p.drawsSelection() {
		t.Error("after growing the window the selection is off screen")
	}
}

// TestPaletteClickWhileScrolledRunsTheRightLine checks that the line
// under the pointer is the one that runs, counting from the first line
// shown rather than the first there is.
func TestPaletteClickWhileScrolledRunsTheRightLine(t *testing.T) {
	ran := ""
	cmds := NewCommands()
	for i := 0; i < 30; i++ {
		title := "Command " + string(rune('a'+i))
		id := title
		cmds.MustRegister(Command{ID: id, Title: title, Run: func() error {
			ran = id
			return nil
		}})
	}
	p, _ := newTestPalette(t, cmds)
	p.Style = styled()
	for i := 0; i < len(p.Matches()); i++ {
		p.HandleKey(press(input.KeyDown, 0))
	}
	if p.top == 0 {
		t.Fatal("the list did not scroll, so there is nothing to get wrong")
	}
	want := p.Matches()[p.top].Command.ID
	box := p.box()

	// The first line of the list as drawn.
	if _, err := p.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: box.X + 2, Row: box.Y + 1,
	}); err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}

	if ran != want {
		t.Errorf("ran %q, want %q: the click counted from the top of the list", ran, want)
	}
}

// TestPaletteTypingWhileScrolledGoesBackToTheTop checks that a new query
// starts a new list rather than one scrolled where the old one was.
func TestPaletteTypingWhileScrolledGoesBackToTheTop(t *testing.T) {
	titles := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		titles = append(titles, "Command "+string(rune('a'+i)))
	}
	p, _ := newTestPalette(t, testCommands(titles...))
	for i := 0; i < len(p.Matches()); i++ {
		p.HandleKey(press(input.KeyDown, 0))
	}
	if p.top == 0 {
		t.Fatal("the list did not scroll")
	}

	typeInto(t, p, "c")

	if p.top != 0 || p.at != 0 {
		t.Errorf("after typing, top = %d and at = %d, want the list back at its start", p.top, p.at)
	}
}

// TestPaletteShowsTheKeyBinding checks the chord column, which is what
// makes the dialog teach its own shortcuts.
func TestPaletteShowsTheKeyBinding(t *testing.T) {
	cmds := testCommands("Copy")
	keys := NewKeymap()
	keys.MustBind(map[Chord]string{{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}: "copy"})
	p := NewPalette(cmds, keys, func() {})
	p.Style = styled()
	p.Layout(Size{Cols: 40, Rows: 12})
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})

	p.Draw(g.View())

	box := p.box()
	line := rowOf(g, box.Y+1)
	if !strings.Contains(line, "ctrl+shift+C") {
		t.Errorf("line = %q, want the binding shown beside the command", line)
	}
	// And a command with no binding leaves the column alone.
	plain := NewPalette(testCommands("Copy"), NewKeymap(), func() {})
	plain.Style = styled()
	plain.Layout(Size{Cols: 40, Rows: 12})
	g2 := grid.New(40, 12, color.RGBA{}, color.RGBA{})
	plain.Draw(g2.View())
	if got := strings.TrimSpace(rowOf(g2, plain.box().Y+1)); got != "Copy" {
		t.Errorf("line = %q, want just the title", got)
	}
}

// TestPalettePicksOutTheMatchedLetters checks the highlight that shows
// why a command is in the list at all.
func TestPalettePicksOutTheMatchedLetters(t *testing.T) {
	// Two matches, so there is an unselected line as well as the
	// selected one. They are drawn differently and both have to work.
	p, _ := newTestPalette(t, testCommands("Copy", "Copy paste"))
	p.Style = styled()
	typeInto(t, p, "cp")
	if len(p.Matches()) != 2 {
		t.Fatalf("%d matches, want two", len(p.Matches()))
	}
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})

	p.Draw(g.View())

	box := p.box()
	// A title starts two columns into the box; C and p matched.
	at := box.X + 2

	// An unselected line picks the letters out in the match colour.
	other := box.Y + 2
	if got := g.At(at, other).FG; got != p.Style.MatchFG {
		t.Errorf("the matched C is %v, want the match colour %v", got, p.Style.MatchFG)
	}
	if got := g.At(at+1, other).FG; got != p.Style.FG {
		t.Errorf("the o between them is %v, want the ordinary %v", got, p.Style.FG)
	}
	if got := g.At(at+2, other).FG; got != p.Style.MatchFG {
		t.Errorf("the matched p is %v, want the match colour %v", got, p.Style.MatchFG)
	}

	// The selected line has a background of its own, so the letters are
	// picked out by weight instead. The match colour is chosen to stand
	// out against the other lines, and can be exactly what is behind
	// this one.
	sel := box.Y + 1
	if got := g.At(at, sel).FG; got != p.Style.SelectedFG {
		t.Errorf("the matched C on the selected line is %v, want the line's own %v",
			got, p.Style.SelectedFG)
	}
	if g.At(at, sel).Attr&grid.AttrBold == 0 {
		t.Error("the matched C on the selected line is not bold, so nothing picks it out")
	}
	if g.At(at+1, sel).Attr&grid.AttrBold != 0 {
		t.Error("the o between them is bold, so the weight says nothing")
	}
}

// TestPaletteNeverDrawsTextItsOwnBackgroundColour is the rule behind the
// one above. A style is free to choose any colours, and the palette must
// not put a character in the colour of the cell behind it.
//
// The window's own palette does exactly this: the colour that marks a
// match is the colour the selected line is drawn on. The matched word
// vanished, and so did the key binding beside it, leaving "Next pane"
// reading as a command called "Next".
func TestPaletteNeverDrawsTextItsOwnBackgroundColour(t *testing.T) {
	ink := color.RGBA{0xc8, 0xd0, 0xda, 0xff}
	paper := color.RGBA{0x14, 0x17, 0x1c, 0xff}
	p, _ := newTestPalette(t, testCommands("Next pane", "Close pane", "Previous pane"))
	// The arrangement the window uses: the match and chord colours are
	// the same one the selected line has behind it.
	p.Style = PaletteStyle{
		FG:         ink,
		BG:         paper,
		MatchFG:    ink,
		SelectedFG: paper,
		SelectedBG: ink,
		ChordFG:    ink,
	}
	keys := NewKeymap()
	keys.MustBind(map[Chord]string{{Key: input.KeyF5}: "next pane"})
	p.keys = keys
	typeInto(t, p, "pane")
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})

	p.Draw(g.View())

	box := p.box()
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			c := g.At(x, y)
			// A blank has no ink, so it may be any colour at all.
			if c.Rune == ' ' || c.Rune == 0 {
				continue
			}
			if c.FG == c.BG {
				t.Fatalf("%q at %d,%d is drawn in the colour behind it (%v): it is invisible",
					c.Rune, x, y, c.FG)
			}
		}
	}
}

// TestPaletteWideTitleStopsBeforeTheChord checks the bound on a title
// whose characters are two columns wide, which a check written in
// columns-so-far alone would overrun by one.
func TestPaletteWideTitleStopsBeforeTheChord(t *testing.T) {
	wide := strings.Repeat("日", 40)
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "wide", Title: wide, Run: nop})
	keys := NewKeymap()
	keys.MustBind(map[Chord]string{{Key: input.KeyW, Mods: input.ModCtrl}: "wide"})
	p := NewPalette(cmds, keys, func() {})
	p.Style = styled()
	p.Layout(Size{Cols: 40, Rows: 12})
	g := grid.New(40, 12, color.RGBA{}, color.RGBA{})

	p.Draw(g.View())

	box := p.box()
	// The binding is still readable: the title did not run into it.
	if got := rowOf(g, box.Y+1); !strings.Contains(got, "ctrl+W") {
		t.Errorf("line = %q, want the binding still whole", got)
	}
	// And no half of a wide character was left without its other half.
	for x := box.X; x < box.X+box.Cols; x++ {
		c := g.At(x, box.Y+1)
		if c.Width != 2 {
			continue
		}
		if got := g.At(x+1, box.Y+1); got.Width != 0 {
			t.Fatalf("the wide character at %d has no continuation", x)
		}
	}
}

// The query used to be append-only: the only edit was deleting the last
// character. It is a Field now, so it can be corrected in the middle.
func TestPaletteQueryCanBeEditedInTheMiddle(t *testing.T) {
	cmds := testCommands("Split right", "Split down")
	p, _ := newTestPalette(t, cmds)

	typeInto(t, p, "slpit")
	p.HandleKey(press(input.KeyLeft, 0))
	p.HandleKey(press(input.KeyLeft, 0))
	p.HandleKey(press(input.KeyLeft, 0))
	p.HandleKey(press(input.KeyBackspace, 0))
	p.HandleKey(press(input.KeyRight, 0))
	typeInto(t, p, "l")
	p.HandleKey(press(input.KeyEnd, 0))
	typeInto(t, p, " r")

	if got := p.Query(); got != "split r" {
		t.Fatalf("query = %q, want %q", got, "split r")
	}
	// The list followed the edit rather than the last keystroke.
	cmd, ok := p.Selected()
	if !ok || cmd.Title != "Split right" {
		t.Fatalf("selected %v, want Split right", cmd.Title)
	}
}

// Up and Down belong to the list. A field would ignore them, and the
// palette would become a dialog you cannot choose from with the keyboard.
func TestPaletteArrowsStillMoveTheSelection(t *testing.T) {
	cmds := testCommands("Alpha", "Beta")
	p, _ := newTestPalette(t, cmds)

	first, _ := p.Selected()
	p.HandleKey(press(input.KeyDown, 0))
	second, ok := p.Selected()
	if !ok || second.ID == first.ID {
		t.Fatalf("Down left the selection on %q", first.ID)
	}
	if p.Query() != "" {
		t.Errorf("Down typed %q into the query", p.Query())
	}
}

// The palette is a modal like any other and has to be told when it loses
// focus. Without it a menu opened over the palette leaves a caret
// blinking in a query line that no longer has the keys.
func TestPaletteGivesUpTheCursorWhenFocusLeaves(t *testing.T) {
	p, _ := newTestPalette(t, testCommands("Copy"))

	g := grid.New(40, 12, fg, bg)
	g.ResetCursorClaim()
	p.Draw(g.View())
	if !g.CursorClaimed() {
		t.Fatal("a focused palette drew no cursor")
	}

	p.SetFocus(false)
	g.ResetCursorClaim()
	p.Draw(g.View())
	if g.CursorClaimed() {
		t.Fatal("the palette still claimed the cursor after focus left")
	}
}
