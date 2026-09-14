package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// menuLines is where a menu's items are drawn: its box, less the rule
// around the outside.
func menuLines(m *Menu) Rect {
	box := m.box()
	if box.Empty() {
		return box
	}
	return Rect{
		X: box.X + menuFrame, Y: box.Y + menuFrame,
		Cols: max(box.Cols-menuFrame*2, 0), Rows: max(box.Rows-menuFrame*2, 0),
	}
}

// longMenu returns a menu of n numbered commands, for the scrolling
// tests. The titles differ in their first character so a drawn row says
// which item it is.
func longMenu(t *testing.T, n int) *Menu {
	t.Helper()
	cmds := NewCommands()
	lines := make([]MenuItem, 0, n)
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		cmds.MustRegister(Command{ID: id, Title: id + " command", Run: nop})
		lines = append(lines, MenuItem{Command: id})
	}
	m := NewMenu(cmds, NewKeymap(), lines, func() {})
	m.Style = menuStyled()
	return m
}

// TestMenuLineLayout pins where a title and its key binding are drawn,
// column by column. Every other test looks for text somewhere in the
// row, which cannot see parts that have moved or run into each other.
func TestMenuLineLayout(t *testing.T) {
	const title = "Copy to clipboard" // 17 columns
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "copy", Title: title, Run: nop})
	keys := NewKeymap()
	keys.MustBind(map[Chord]string{{Key: input.KeyF5}: "copy"})
	m := NewMenu(cmds, keys, items("copy"), func() {})
	m.Style = menuStyled()

	g := drawMenu(m, 60, 20)

	box := menuLines(m)
	// A blank column each side, the title, the gap, then "F5".
	if want := menuPad + 17 + menuGap + 2 + menuPad; box.Cols != want {
		t.Fatalf("box is %d columns, want %d", box.Cols, want)
	}
	for _, tc := range []struct {
		at   int
		want rune
		why  string
	}{
		{0, ' ', "the blank before the title"},
		{menuPad, 'C', "the title"},
		{menuPad + 17, ' ', "the gap after the title"},
		{box.Cols - menuPad - 2, 'F', "the binding"},
		{box.Cols - 1, ' ', "the blank after the binding"},
	} {
		if got := g.At(box.X+tc.at, box.Y).Rune; got != tc.want {
			t.Errorf("column %d = %q, want %q: %s", tc.at, got, tc.want, tc.why)
		}
	}
}

// TestMenuTooNarrowDropsTheBinding checks which of the two goes when
// they will not both fit. A line showing only a key binding does not say
// what the key does.
func TestMenuTooNarrowDropsTheBinding(t *testing.T) {
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "copy", Title: "Copy", Run: nop})
	keys := NewKeymap()
	keys.MustBind(map[Chord]string{{Key: input.KeyF5}: "copy"})
	m := NewMenu(cmds, keys, items("copy"), func() {})
	m.Style = menuStyled()

	// Seven columns: the rule each side, a blank each side, the two the
	// binding wants, and one left over. Wide enough that a guard one
	// column out would show the binding and leave the title nowhere to
	// go.
	g := drawMenu(m, 5+menuFrame*2, 20)

	row := rowOf(g, menuLines(m).Y)
	if strings.Contains(row, "F5") {
		t.Errorf("row = %q, want the binding dropped for want of room", row)
	}
	if !strings.Contains(row, "Co") {
		t.Errorf("row = %q, want what fits of the title", row)
	}
}

// TestMenuDrawsTheScrolledItems checks that a scrolled menu shows the
// lines it thinks it does. Drawing from the top of the list while
// selecting from the middle puts the highlight on the wrong command.
func TestMenuDrawsTheScrolledItems(t *testing.T) {
	m := longMenu(t, 20)
	m.Layout(Size{Cols: 40, Rows: 6})
	for i := 0; i < 19; i++ {
		m.HandleKey(press(input.KeyDown, 0))
	}
	if m.top == 0 {
		t.Fatal("the list did not scroll, so this proves nothing")
	}

	g := drawMenu(m, 40, 6)

	box := menuLines(m)
	want := m.Items()[m.top].Command
	if got := strings.TrimSpace(rowOf(g, box.Y)); !strings.HasPrefix(got, want) {
		t.Errorf("first drawn row = %q, want item %d, which is %q", got, m.top, want)
	}
}

// TestMenuClickIsScrollAware checks a click on a menu that is both
// scrolled and not at the top of the window. Forgetting either the
// scroll or the box position runs a different command from the one under
// the pointer.
func TestMenuClickIsScrollAware(t *testing.T) {
	ran := ""
	cmds := NewCommands()
	lines := make([]MenuItem, 0, 20)
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		cmds.MustRegister(Command{ID: id, Title: id + " command", Run: func() error {
			ran = id
			return nil
		}})
		lines = append(lines, MenuItem{Command: id})
	}
	m := NewMenu(cmds, NewKeymap(), lines, func() {})
	m.Style = menuStyled()
	m.Anchor = func() Rect { return Rect{X: 0, Y: 2, Cols: 4, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 9})
	for i := 0; i < 19; i++ {
		m.HandleKey(press(input.KeyDown, 0))
	}
	box := menuLines(m)
	if box.Y == 0 || m.top == 0 {
		t.Fatalf("lines at row %d with top %d: this proves nothing", box.Y, m.top)
	}

	// The second line of the menu.
	if _, err := m.HandleMouse(pressAt(box.X+1, box.Y+1)); err != nil {
		t.Fatalf("press: %v", err)
	}

	if want := m.Items()[m.top+1].Command; ran != want {
		t.Errorf("ran %q, want %q: the click ignored the scroll or the box position", ran, want)
	}
}

// TestMenuHighlightsTheSelectedRow checks the selection is drawn on the
// line it is on, not one either side of it.
func TestMenuHighlightsTheSelectedRow(t *testing.T) {
	cmds := testCommands("Copy", "Paste", "Quit")
	m, _ := newTestMenu(t, cmds, items("copy", "paste", "quit"))
	m.HandleKey(press(input.KeyDown, 0))

	g := drawMenu(m, 40, 20)

	box := menuLines(m)
	for row := 0; row < box.Rows; row++ {
		got := g.At(box.X+1, box.Y+row).BG
		want := m.Style.BG
		if row == m.at-m.top {
			want = m.Style.SelectedBG
		}
		if got != want {
			t.Errorf("row %d background = %+v, want %+v (selected is %d)", row, got, want, m.at)
		}
	}
}

// TestMenuNeverCoversItsAnchor is the rule a menu bar depends on: the
// title stays visible, so a click on another title reaches the bar
// rather than running a line of the menu drawn over it.
func TestMenuNeverCoversItsAnchor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rows    int
		items   int
		anchorY int
	}{
		{"room below", 20, 3, 0},
		{"exactly fits below", 4, 3, 0},
		{"one short below", 4, 4, 0},
		{"anchor at the bottom", 12, 3, 9},
		{"more items than the window", 6, 30, 0},
		{"anchor in the middle, list too long", 9, 30, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := longMenu(t, tc.items)
			anchor := Rect{X: 0, Y: tc.anchorY, Cols: 4, Rows: 1}
			m.Anchor = func() Rect { return anchor }
			m.Layout(Size{Cols: 40, Rows: tc.rows})

			box := m.box()
			if box.Empty() {
				t.Fatal("no box at all")
			}
			if box.Y < 0 || box.Y+box.Rows > tc.rows {
				t.Errorf("box holds rows %d..%d, the window has 0..%d",
					box.Y, box.Y+box.Rows-1, tc.rows-1)
			}
			below := box.Y >= anchor.Y+anchor.Rows
			above := box.Y+box.Rows <= anchor.Y
			if !below && !above {
				t.Errorf("box holds rows %d..%d, which covers the anchor at row %d",
					box.Y, box.Y+box.Rows-1, anchor.Y)
			}
		})
	}
}

// TestMenuShrunkToFitStillReachesEveryLine checks that a menu with less
// room than it wants can still be scrolled to the end.
func TestMenuShrunkToFitStillReachesEveryLine(t *testing.T) {
	m := longMenu(t, 30)
	m.Anchor = func() Rect { return Rect{X: 0, Y: 0, Cols: 4, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 6})

	m.HandleKey(press(input.KeyEnd, 0))

	if m.at != 29 {
		t.Fatalf("End landed on %d, want the last line", m.at)
	}
	if lines := m.lines(); m.at < m.top || m.at >= m.top+lines {
		t.Errorf("selected %d with lines %d..%d showing", m.at, m.top, m.top+lines-1)
	}
}

// TestMenuAnchorOffTheWindowIsClamped checks an anchor partly off the
// window, which the exported Anchor field invites. A box off the window
// is drawn nowhere while still claiming the clicks that land on it.
func TestMenuAnchorOffTheWindowIsClamped(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, _ := newTestMenu(t, cmds, items("copy", "paste"))
	m.Anchor = func() Rect { return Rect{X: -5, Y: -5, Cols: 4, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 20})

	box := m.box()

	if box.X < 0 || box.Y < 0 {
		t.Fatalf("box = %+v, want it on the window", box)
	}
	g := drawMenu(m, 40, 20)
	at := menuLines(m).Y
	if !strings.Contains(rowOf(g, at), "Copy") {
		t.Errorf("row %d = %q, want the menu drawn there", at, rowOf(g, at))
	}
}

// TestMenuWidthFitsAWideTitle checks the measurement in display columns
// with a title long enough that the minimum width is not doing the work.
func TestMenuWidthFitsAWideTitle(t *testing.T) {
	const title = "世界世界世界世界" // 8 characters, 16 columns
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "wide", Title: title, Run: nop})
	m := NewMenu(cmds, NewKeymap(), items("wide"), func() {})
	m.Style = menuStyled()

	g := drawMenu(m, 40, 20)

	box := menuLines(m)
	if want := grid.StringWidth(title) + menuPad*2; box.Cols != want {
		t.Fatalf("box is %d columns, want %d for a %d-column title",
			box.Cols, want, grid.StringWidth(title))
	}
	if got := rowOf(g, box.Y); !strings.Contains(got, title) {
		t.Errorf("row = %q, want the whole title", got)
	}
}

// TestMenuClosesBeforeItRuns checks the order. A command that opens
// another dialog has to find the stack already clear of this menu: the
// app takes away everything above whatever it is closing, so running
// first would open something and immediately tear it down.
func TestMenuClosesBeforeItRuns(t *testing.T) {
	closed := false
	openWhenRun := true
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "copy", Title: "Copy", Run: func() error {
		openWhenRun = !closed
		return nil
	}})
	m := NewMenu(cmds, NewKeymap(), items("copy"), func() { closed = true })
	m.Style = menuStyled()
	m.Layout(Size{Cols: 40, Rows: 20})

	m.HandleKey(press(input.KeyEnter, 0))

	if openWhenRun {
		t.Error("the command ran while the menu was still on the stack")
	}
}

// TestMenuIgnoresChords checks that a key with a modifier is not the
// menu's. A modal is offered keys before the accelerators are, so
// swallowing Ctrl+Enter would kill it for whatever it is bound to.
func TestMenuIgnoresChords(t *testing.T) {
	cmds := testCommands("Copy", "Paste")
	m, closed := newTestMenu(t, cmds, items("copy", "paste"))

	for _, ev := range []input.Event{
		press(input.KeyEnter, input.ModCtrl),
		press(input.KeyEscape, input.ModShift),
		press(input.KeyDown, input.ModAlt),
	} {
		if handled, _ := m.HandleKey(ev); handled {
			t.Errorf("%s was swallowed", ChordOf(ev))
		}
	}
	if *closed != 0 || m.SelectedIndex() != 0 {
		t.Error("a chord moved or closed the menu")
	}
}

// TestMenuNeverDrawsTextItsOwnBackgroundColour is the rule the palette
// broke: a style may choose any colours, and a widget must not put a
// character in the colour of the cell behind it. A key binding drawn in
// the colour the selected line sits on simply disappears.
func TestMenuNeverDrawsTextItsOwnBackgroundColour(t *testing.T) {
	ink := color.RGBA{0xc8, 0xd0, 0xda, 0xff}
	paper := color.RGBA{0x14, 0x17, 0x1c, 0xff}
	cmds := testCommands("Copy", "Paste", "Quit")
	keys := NewKeymap()
	keys.MustBind(map[Chord]string{
		{Key: input.KeyF5}: "copy",
		{Key: input.KeyF6}: "paste",
	})
	m := NewMenu(cmds, keys, []MenuItem{
		{Command: "copy"}, MenuSeparator(), {Command: "paste"}, {Command: "quit"},
	}, func() {})
	// The chord colour is the one the selected line has behind it.
	m.Style = MenuStyle{
		FG:         ink,
		BG:         paper,
		SelectedFG: paper,
		SelectedBG: ink,
		ChordFG:    ink,
		DisabledFG: ink,
	}

	g := drawMenu(m, 40, 20)

	box := m.box()
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

// A press on the rule chooses nothing.
//
// The rule is inside the box and is not a line, so counting rows from
// the box names the line above the first or below the last. Once a menu
// is scrolled, those are real commands the user cannot see.
func TestPressingTheMenuRuleRunsNothing(t *testing.T) {
	ran := ""
	cmds := NewCommands()
	var lines []MenuItem
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		cmds.MustRegister(Command{ID: id, Title: id + " command", Run: func() error {
			ran = id
			return nil
		}})
		lines = append(lines, MenuItem{Command: id})
	}
	m := NewMenu(cmds, NewKeymap(), lines, func() {})
	m.Style = menuStyled()
	m.Anchor = func() Rect { return Rect{X: 0, Y: 2, Cols: 4, Rows: 1} }
	m.Layout(Size{Cols: 40, Rows: 9})

	box := m.box()
	if m.lines() >= len(lines) {
		t.Fatal("the menu is not truncated, so this proves nothing")
	}
	for _, y := range []int{box.Y, box.Y + box.Rows - 1} {
		m.HandleMouse(pressAt(box.X+1, y))
		if ran != "" {
			t.Fatalf("a press on the rule at row %d ran %q", y, ran)
		}
	}

	// And scrolled to the end, where the top rule would name a real line
	// as well.
	for i := 0; i < 19; i++ {
		m.HandleKey(press(input.KeyDown, 0))
	}
	if m.top == 0 {
		t.Fatal("the menu did not scroll, so this proves nothing")
	}
	box = m.box()
	was := m.SelectedIndex()
	for _, y := range []int{box.Y, box.Y + box.Rows - 1} {
		m.HandleMouse(moveTo(box.X+1, y))
		if got := m.SelectedIndex(); got != was {
			t.Fatalf("the pointer on the rule at row %d selected %d, want %d", y, got, was)
		}
		m.HandleMouse(pressAt(box.X+1, y))
		if ran != "" {
			t.Fatalf("a press on the rule at row %d ran %q", y, ran)
		}
	}
}

// A window with room for the rule and none for a line shows no menu at
// all, and the menu takes nothing but Escape.
//
// A box holding its own rule and nothing else says it has something in
// it. Enter would then run a line nobody has read.
func TestAMenuWithNoRoomForALine(t *testing.T) {
	ran := ""
	cmds := NewCommands()
	cmds.MustRegister(Command{ID: "copy", Title: "Copy", Run: func() error { ran = "copy"; return nil }})
	closed := 0
	m := NewMenu(cmds, NewKeymap(), items("copy"), func() { closed++ })
	m.Style = menuStyled()
	m.Anchor = func() Rect { return Rect{X: 0, Y: 0, Cols: 4, Rows: 1} }

	for _, rows := range []int{1, 2, 3} {
		ran, closed = "", 0
		m.Layout(Size{Cols: 40, Rows: rows})
		if got := m.box(); !got.Empty() {
			t.Fatalf("in %d rows the box is %+v, want none", rows, got)
		}
		g := drawMenu(m, 40, rows)
		for y := 0; y < rows; y++ {
			if got := strings.TrimSpace(rowOf(g, y)); got != "" {
				t.Fatalf("in %d rows it drew %q", rows, got)
			}
		}
		if took, _ := m.HandleKey(press(input.KeyEnter, 0)); !took {
			t.Fatalf("in %d rows Enter travelled on", rows)
		}
		if ran != "" {
			t.Fatalf("in %d rows Enter ran %q from a menu nobody can see", rows, ran)
		}
		m.HandleKey(press(input.KeyEscape, 0))
		if closed != 1 {
			t.Fatalf("in %d rows Escape closed it %d times", rows, closed)
		}
	}
}
