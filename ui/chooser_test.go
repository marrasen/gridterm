package ui

import (
	"errors"
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// chooserStyled colours a chooser so a test can tell its parts apart.
// chooserLines is what a chooser is showing, with a heading marked.
func chooserLines(c *Chooser) []string {
	var out []string
	for _, row := range c.Rows() {
		if row.Header {
			out = append(out, "# "+row.Text)
			continue
		}
		out = append(out, row.Text)
	}
	return out
}

// typeIntoChooser gives a chooser letters, the way a user types them.
func typeIntoChooser(t *testing.T, c *Chooser, text string) {
	t.Helper()
	for _, r := range text {
		took, err := c.HandleKey(input.Event{Kind: input.Text, Rune: r})
		if err != nil {
			t.Fatalf("typing %q: %v", r, err)
		}
		if !took {
			t.Fatalf("the chooser did not take %q", r)
		}
	}
}

// A chooser groups its lines under the headings they were added with.
func TestChooserGroupsLinesUnderHeadings(t *testing.T) {
	c := NewChooser("Split with", func() {})
	c.Add("New terminal", "Local", nil)
	c.Under("Local")
	c.Add("Move Command Prompt", "", nil)
	c.Under("margit")
	c.Add("Move Terminal vim", "", nil)
	c.Under("")
	c.Add("Terminal on margit", "connected", nil)

	want := []string{
		"New terminal",
		"# Local", "Move Command Prompt",
		"# margit", "Move Terminal vim",
		"Terminal on margit",
	}
	if got := chooserLines(c); !slices.Equal(got, want) {
		t.Errorf("the chooser shows %v, want %v", got, want)
	}
}

// Typing keeps the lines that match and the headings they sit under, and
// drops a heading with nothing left under it.
func TestChooserNarrowsToWhatWasTyped(t *testing.T) {
	c := NewChooser("Split with", func() {})
	c.Under("Local")
	c.Add("Move Command Prompt", "", nil)
	c.Under("margit")
	c.Add("Move Terminal vim", "", nil)
	c.Add("Move make deploy", "", nil)
	c.Layout(Size{Cols: 40, Rows: 12})

	typeIntoChooser(t, c, "vim")
	want := []string{"# margit", "Move Terminal vim"}
	if got := chooserLines(c); !slices.Equal(got, want) {
		t.Errorf("after typing vim the chooser shows %v, want %v", got, want)
	}
	if c.Query() != "vim" {
		t.Errorf("the chooser holds %q, want what was typed", c.Query())
	}
}

// Backspace gives a letter back, and Escape gives the whole list back
// before it puts the chooser away.
func TestChooserGivesBackWhatWasTyped(t *testing.T) {
	gone := 0
	c := NewChooser("Split with", func() { gone++ })
	c.Under("Local")
	c.Add("Move Command Prompt", "", nil)
	c.Under("margit")
	c.Add("Move Terminal vim", "", nil)
	c.Layout(Size{Cols: 40, Rows: 12})

	typeIntoChooser(t, c, "vimx")
	if got := chooserLines(c); len(got) != 0 {
		t.Fatalf("the chooser shows %v for a query nothing matches", got)
	}
	if _, err := c.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyBackspace}); err != nil {
		t.Fatalf("backspace: %v", err)
	}
	if got := chooserLines(c); !slices.Equal(got, []string{"# margit", "Move Terminal vim"}) {
		t.Errorf("after a backspace the chooser shows %v", got)
	}

	if _, err := c.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape}); err != nil {
		t.Fatalf("escape: %v", err)
	}
	if c.Query() != "" {
		t.Errorf("Escape left %q typed in", c.Query())
	}
	if gone != 0 {
		t.Error("Escape put the chooser away instead of giving the list back")
	}
	if _, err := c.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape}); err != nil {
		t.Fatalf("escape again: %v", err)
	}
	if gone != 1 {
		t.Errorf("Escape on the whole list put the chooser away %d times", gone)
	}
}

// A heading is not a line to take. Enter on the first row takes the
// first thing under it.
func TestChooserTakesALineAndNotAHeading(t *testing.T) {
	took := ""
	c := NewChooser("Split with", func() {})
	c.Under("margit")
	c.Add("Move Terminal vim", "", func() error { took = "vim"; return nil })
	c.Layout(Size{Cols: 40, Rows: 12})

	if _, err := c.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter}); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if took != "vim" {
		t.Errorf("Enter took %q, want the line under the heading", took)
	}
}

func chooserStyled() ChooserStyle {
	return ChooserStyle{
		FG: fg, BG: bg, TitleFG: fg,
		SelectedFG: bg, SelectedBG: fg, NoteFG: fg,
	}
}

// newTestChooser returns a chooser of three lines and what each took.
func newTestChooser(t *testing.T) (*Chooser, *[]string, *int) {
	t.Helper()
	var took []string
	closed := 0
	c := NewChooser("Split with…", func() { closed++ })
	c.Style = chooserStyled()
	for _, name := range []string{"New terminal", "Move vim", "Terminal on margit"} {
		at := name
		c.Add(at, "here", func() error { took = append(took, at); return nil })
	}
	c.Layout(Size{Cols: 60, Rows: 20})
	return c, &took, &closed
}

// drawChooser paints a chooser onto a see-through grid, the way its own
// layer is made.
func drawChooser(c *Chooser, cols, rows int) *grid.Grid {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	c.Layout(Size{Cols: cols, Rows: rows})
	c.Draw(g.View())
	return g
}

// A chooser opens on its first line, so Enter straight away takes the
// answer it leads with.
func TestChooserOpensOnItsFirstLine(t *testing.T) {
	c, took, closed := newTestChooser(t)

	if _, err := c.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if len(*took) != 1 || (*took)[0] != "New terminal" {
		t.Fatalf("it took %v, want the first line", *took)
	}
	if *closed != 1 {
		t.Fatalf("it closed %d times, want once", *closed)
	}
}

// The arrows move down the list and Enter takes what they landed on.
func TestChooserArrowsAndEnter(t *testing.T) {
	c, took, _ := newTestChooser(t)

	keyTo(t, c, press(input.KeyDown, 0))
	keyTo(t, c, press(input.KeyDown, 0))
	keyTo(t, c, press(input.KeyEnter, 0))

	if len(*took) != 1 || (*took)[0] != "Terminal on margit" {
		t.Fatalf("it took %v, want the third line", *took)
	}
}

// Escape leaves without taking anything.
func TestChooserEscapeTakesNothing(t *testing.T) {
	c, took, closed := newTestChooser(t)

	if took2, err := c.HandleKey(press(input.KeyEscape, 0)); !took2 || err != nil {
		t.Fatalf("Escape took %v, err %v", took2, err)
	}
	if len(*took) != 0 {
		t.Fatalf("Escape took %v", *took)
	}
	if *closed != 1 {
		t.Fatalf("it closed %d times, want once", *closed)
	}
}

// It goes away before it does the thing: what was picked may open a
// dialog of its own, and closing one takes everything stacked above it.
func TestChooserClosesBeforeItActs(t *testing.T) {
	var order []string
	c := NewChooser("Pick", func() { order = append(order, "closed") })
	c.Style = chooserStyled()
	c.Add("one", "", func() error { order = append(order, "did"); return nil })
	c.Layout(Size{Cols: 40, Rows: 12})

	keyTo(t, c, press(input.KeyEnter, 0))
	if len(order) != 2 || order[0] != "closed" || order[1] != "did" {
		t.Fatalf("it went %v, want it closed first", order)
	}
}

// A failure from what was picked reaches whoever asked.
func TestChooserReportsAFailure(t *testing.T) {
	boom := errors.New("no room to split")
	c := NewChooser("Pick", func() {})
	c.Style = chooserStyled()
	c.Add("one", "", func() error { return boom })
	c.Layout(Size{Cols: 40, Rows: 12})

	_, err := c.HandleKey(press(input.KeyEnter, 0))
	if !errors.Is(err, boom) {
		t.Fatalf("it reported %v, want the failure", err)
	}
}

// A click takes the line it landed on, and one on the title or the rule
// takes nothing.
func TestChooserClicks(t *testing.T) {
	c, took, closed := newTestChooser(t)
	drawChooser(c, 60, 20)

	lines := c.lines()
	for _, at := range []struct {
		x, y int
		why  string
	}{
		{c.box().X, c.box().Y, "the rule"},
		{lines.X, lines.Y - 1, "the blank under the title"},
		{lines.X, lines.Y - 2, "the title"},
	} {
		mouseTo(t, c, pressAt(at.x, at.y))
		if len(*took) != 0 {
			t.Fatalf("a press on %s took %v", at.why, *took)
		}
		if *closed != 0 {
			t.Fatalf("a press on %s closed it", at.why)
		}
	}

	mouseTo(t, c, pressAt(lines.X+1, lines.Y+1))
	if len(*took) != 1 || (*took)[0] != "Move vim" {
		t.Fatalf("clicking the second line took %v", *took)
	}
}

// A press outside it leaves without taking anything.
func TestChooserPressOutsideCloses(t *testing.T) {
	c, took, closed := newTestChooser(t)
	drawChooser(c, 60, 20)

	mouseTo(t, c, pressAt(0, 0))
	if len(*took) != 0 {
		t.Fatalf("a press outside took %v", *took)
	}
	if *closed != 1 {
		t.Fatalf("it closed %d times", *closed)
	}
}

// What it draws is the title and every line, each with its note.
func TestChooserDrawsItsLines(t *testing.T) {
	c, _, _ := newTestChooser(t)
	c.Style.BorderFG = fg
	g := drawChooser(c, 60, 20)

	box := c.box()
	if got := g.At(box.X, box.Y).Rune; got != single.topLeft {
		t.Errorf("the top left is %q, want the rule", got)
	}
	whole := ""
	for y := box.Y; y < box.Y+box.Rows; y++ {
		whole += rowOf(g, y) + "\n"
	}
	for _, want := range []string{"Split with", "New terminal", "Move vim", "Terminal on margit", "here"} {
		if !strings.Contains(whole, want) {
			t.Errorf("it drew %q, missing %q", whole, want)
		}
	}
}

// A window with no room for it shows nothing, and it takes nothing but
// Escape: Enter would otherwise take a line nobody has read.
func TestAChooserWithNoRoom(t *testing.T) {
	for _, rows := range []int{1, 2, 4} {
		c, took, closed := newTestChooser(t)
		c.Layout(Size{Cols: 60, Rows: rows})
		if got := c.box(); !got.Empty() {
			t.Fatalf("in %d rows the box is %+v, want none", rows, got)
		}
		if got, _ := c.HandleKey(press(input.KeyEnter, 0)); !got {
			t.Fatalf("in %d rows Enter travelled on", rows)
		}
		if len(*took) != 0 {
			t.Fatalf("in %d rows Enter took %v", rows, *took)
		}
		keyTo(t, c, press(input.KeyEscape, 0))
		if *closed != 1 {
			t.Fatalf("in %d rows Escape closed it %d times", rows, *closed)
		}
	}
}

// A chooser nobody is touching leaves its layer alone.
func TestAnIdleChooserDirtiesNothing(t *testing.T) {
	c, _, _ := newTestChooser(t)
	c.Style.BorderFG = fg
	c.Style.ShadowBG = color.RGBA{A: 0x60}

	g := grid.New(60, 20, color.RGBA{}, color.RGBA{})
	c.Layout(Size{Cols: 60, Rows: 20})
	c.Draw(g.View())
	g.ClearDirty()
	for range 5 {
		c.Draw(g.View())
	}
	if g.AnyDirty() {
		t.Fatal("an idle chooser dirtied its layer")
	}
}

// A chooser lets go of the keys when it is told to.
//
// It is the top modal while it is up, but a dialog pushed over it takes
// the keys: a chooser still drawing an active bar under that dialog
// would say two things have them.
func TestAChooserToldItLostTheKeysStopsMarkingItsLine(t *testing.T) {
	c, _, _ := newTestChooser(t)
	g := drawChooser(c, 60, 20)

	lines := c.lines()
	if got := g.At(lines.X, lines.Y).BG; got != c.Style.SelectedBG {
		t.Fatalf("with the keys the first line is %v, want it marked", got)
	}

	c.SetFocus(false)
	g = drawChooser(c, 60, 20)
	if got := g.At(lines.X, lines.Y).BG; got == c.Style.SelectedBG {
		t.Fatal("a chooser that lost the keys still marks its line")
	}

	// And it takes them back.
	c.SetFocus(true)
	g = drawChooser(c, 60, 20)
	if got := g.At(lines.X, lines.Y).BG; got != c.Style.SelectedBG {
		t.Fatalf("the line is %v once the keys come back", got)
	}
}

// A chord the chooser never offered moves nothing, and does not reach
// what is behind the dialog either.
//
// Ctrl+Down is not Down: acting on it moves the choice on a key the user
// pressed for something else. Handing it on is worse, because the
// window's own chords act on a pane the chooser is covering.
func TestChooserTakesNoChordItNeverOffered(t *testing.T) {
	for _, ev := range []input.Event{
		press(input.KeyDown, input.ModCtrl),
		press(input.KeyUp, input.ModAlt),
		press(input.KeyEnter, input.ModCtrl),
		press(input.KeyV, input.ModCtrl|input.ModShift),
		// Ctrl+Escape is not Escape: a chooser that closed on it would
		// answer a chord the user pressed for something else.
		press(input.KeyEscape, input.ModCtrl),
	} {
		c, took, closed := newTestChooser(t)
		was := c.list.place.at

		taken, err := c.HandleKey(ev)
		if err != nil {
			t.Fatalf("%v: %v", ev.Key, err)
		}
		if !taken {
			t.Errorf("%v went past the dialog to whatever is behind it", ev.Key)
		}
		if len(*took) != 0 {
			t.Errorf("%v picked %q", ev.Key, *took)
		}
		if *closed != 0 {
			t.Errorf("%v closed the chooser", ev.Key)
		}
		if now := c.list.place.at; now != was {
			t.Errorf("%v moved the choice from %d to %d", ev.Key, was, now)
		}
	}
}

// A dialog with nowhere to draw itself still closes on Escape, and still
// only on Escape: Ctrl+Escape is a different chord.
func TestAnInvisibleDialogClosesOnPlainEscapeOnly(t *testing.T) {
	c, _, closed := newTestChooser(t)
	c.Layout(Size{Cols: 4, Rows: 2})
	if !c.box().Empty() {
		t.Fatal("the test needs a chooser with nowhere to draw itself")
	}

	if _, err := c.HandleKey(press(input.KeyEscape, input.ModCtrl)); err != nil {
		t.Fatalf("ctrl+escape: %v", err)
	}
	if *closed != 0 {
		t.Error("Ctrl+Escape closed an invisible chooser")
	}
	if _, err := c.HandleKey(press(input.KeyEscape, 0)); err != nil {
		t.Fatalf("escape: %v", err)
	}
	if *closed != 1 {
		t.Errorf("Escape closed an invisible chooser %d times, want 1", *closed)
	}
}

// aForgettableChooser is a chooser whose lines can be taken off it, the
// way a list of things the user keeps can.
func aForgettableChooser(t *testing.T, lines ...string) *Chooser {
	t.Helper()
	c := NewChooser("Kept", func() {})
	c.Style = chooserStyled()
	c.Button = '\u00d7'
	c.OnPress = func(i int) error {
		c.Forget(i)
		return nil
	}
	for _, line := range lines {
		c.Add(line, "", func() error { return nil })
	}
	return c
}

// Taking a line off leaves the bar on the line it was on. A line's key
// is where it sits, so without this the bar lands on the next one and
// Enter runs something the user did not pick.
func TestForgettingALineLeavesTheBarWhereItWas(t *testing.T) {
	for what, tc := range map[string]struct {
		forget int
		want   string
	}{
		"above the bar":  {0, "three"},
		"below the bar":  {3, "three"},
		"the bar itself": {2, "four"},
	} {
		c := aForgettableChooser(t, "one", "two", "three", "four")
		if !c.Select(2) {
			t.Fatalf("%s: the bar could not be put on the third line", what)
		}

		if err := c.Press(tc.forget); err != nil {
			t.Fatalf("%s: the button: %v", what, err)
		}

		on, ok := c.Selected()
		if !ok {
			t.Errorf("%s: the bar is on nothing, want %q", what, tc.want)
			continue
		}
		if on.Text != tc.want {
			t.Errorf("%s: the bar is on %q, want %q", what, on.Text, tc.want)
		}
	}
}

// A chooser with a button on its lines is wide enough for it, so the
// longest line keeps its end.
func TestAChooserLeavesRoomForItsButton(t *testing.T) {
	const long = "deployment-manifest.yaml"
	c := aForgettableChooser(t, long)

	g := drawChooser(c, 60, 12)

	if !strings.Contains(gridText(g), long) {
		t.Errorf("the chooser shows %q cut short:\n%s", long, gridText(g))
	}
}
