package ui

import (
	"errors"
	"image/color"
	"strings"
	"sync"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

var (
	fg = color.RGBA{0xff, 0xff, 0xff, 0xff}
	bg = color.RGBA{0x00, 0x00, 0x00, 0xff}
)

// fake is a widget that records what it was told and answers keys it was
// asked to answer.
type fake struct {
	name string
	size Size
	// sizes is every size Layout was given, for checking a widget is not
	// told it has no room.
	sizes   []Size
	drawn   int
	drawSz  Size
	focus   bool
	focused []bool // every SetFocus value, in order
	takes   input.Key
	fails   error
	seen    []input.Key
}

func (f *fake) Layout(size Size) {
	f.size = size
	f.sizes = append(f.sizes, size)
}
func (f *fake) Draw(v grid.View) {
	f.drawn++
	cols, rows := v.Size()
	f.drawSz = Size{Cols: cols, Rows: rows}
	v.SetString(0, 0, f.name, fg, bg, 0)
}
func (f *fake) SetFocus(on bool) { f.focus = on; f.focused = append(f.focused, on) }
func (f *fake) HandleKey(ev input.Event) (bool, error) {
	f.seen = append(f.seen, ev.Key)
	if ev.Key != f.takes {
		return false, nil
	}
	return true, f.fails
}

func press(k input.Key, mods input.Mods) input.Event {
	return input.Event{Kind: input.KeyPress, Key: k, Mods: mods}
}

func TestRectContainsAndEmpty(t *testing.T) {
	r := Rect{X: 2, Y: 1, Cols: 3, Rows: 2}

	for _, tc := range []struct {
		x, y int
		want bool
	}{
		{2, 1, true}, {4, 2, true},
		{1, 1, false}, {5, 1, false}, {2, 0, false}, {2, 3, false},
	} {
		if got := r.Contains(tc.x, tc.y); got != tc.want {
			t.Errorf("Contains(%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	if r.Empty() {
		t.Error("a 3x2 rectangle reported empty")
	}
	for _, e := range []Rect{{}, {Cols: 4}, {Rows: 4}, {Cols: -1, Rows: 2}} {
		if !e.Empty() {
			t.Errorf("%+v reported non-empty", e)
		}
	}
}

// TestRectInClipsToTheParent checks that a widget handed more room than
// its parent has still cannot draw outside it.
func TestRectInClipsToTheParent(t *testing.T) {
	g := grid.New(10, 4, fg, bg)
	area := Rect{X: 8, Y: 3, Cols: 6, Rows: 6}

	v := area.In(g.View())

	if cols, rows := v.Size(); cols != 2 || rows != 1 {
		t.Errorf("view = %dx%d, want 2x1", cols, rows)
	}
	v.SetString(0, 0, "xy", fg, bg, 0)
	if got := g.At(8, 3).Rune; got != 'x' {
		t.Errorf("grid 8,3 = %q, want 'x'", got)
	}
}

func TestCommandsRegisterRejectsBadCommands(t *testing.T) {
	c := NewCommands()
	ok := Command{ID: "a", Title: "A", Run: func() error { return nil }}
	if err := c.Register(ok); err != nil {
		t.Fatalf("registering a good command: %v", err)
	}

	for _, tc := range []struct {
		name string
		cmd  Command
	}{
		{"no id", Command{Title: "T", Run: func() error { return nil }}},
		{"no title", Command{ID: "b", Run: func() error { return nil }}},
		{"does nothing", Command{ID: "b", Title: "B"}},
		{"duplicate id", Command{ID: "a", Title: "Other", Run: func() error { return nil }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := c.Register(tc.cmd); err == nil {
				t.Error("accepted")
			}
		})
	}
	// The duplicate must not have replaced the original.
	if got, _ := c.Lookup("a"); got.Title != "A" {
		t.Errorf("command a = %q, want the first registration to survive", got.Title)
	}
	if c.Len() != 1 {
		t.Errorf("registry holds %d commands, want 1", c.Len())
	}
}

func TestCommandsRunReportsFailure(t *testing.T) {
	c := NewCommands()
	boom := errors.New("boom")
	ran := 0
	c.MustRegister(
		Command{ID: "good", Title: "Good", Run: func() error { ran++; return nil }},
		Command{ID: "bad", Title: "Bad", Run: func() error { return boom }},
	)

	if err := c.Run("good"); err != nil {
		t.Errorf("running a good command: %v", err)
	}
	if ran != 1 {
		t.Errorf("ran %d times, want 1", ran)
	}
	if err := c.Run("bad"); !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap the command's own", err)
	}
	if err := c.Run("missing"); err == nil {
		t.Error("running an unknown command succeeded: a binding pointing at nothing is a bug")
	}
}

// TestCommandsAllIsStable checks the order a menu and the palette list
// commands in, which must not depend on map iteration.
func TestCommandsAllIsStable(t *testing.T) {
	nop := func() error { return nil }
	c := NewCommands()
	c.MustRegister(
		Command{ID: "z", Title: "Bravo", Run: nop},
		Command{ID: "a", Title: "Alpha", Run: nop},
		Command{ID: "m", Title: "Bravo", Run: nop},
	)

	want := []string{"a", "m", "z"} // by title, then id
	for i := 0; i < 20; i++ {
		got := c.All()
		if len(got) != len(want) {
			t.Fatalf("got %d commands, want %d", len(got), len(want))
		}
		for j, id := range want {
			if got[j].ID != id {
				t.Fatalf("order = %v, want %v", ids(got), want)
			}
		}
	}
}

func ids(cmds []Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.ID
	}
	return out
}

func TestKeymapBindAndLookup(t *testing.T) {
	k := NewKeymap()
	c := Chord{Key: input.KeyK, Mods: input.ModCtrl}
	if err := k.Bind(c, "palette.open"); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if id, ok := k.Lookup(c); !ok || id != "palette.open" {
		t.Errorf("Lookup = %q, %v, want palette.open", id, ok)
	}
	if _, ok := k.Lookup(Chord{Key: input.KeyK}); ok {
		t.Error("a chord with different modifiers matched")
	}

	// Rebinding is ordinary, unlike registering a command twice.
	if err := k.Bind(c, "other"); err != nil {
		t.Fatalf("rebinding: %v", err)
	}
	if id, _ := k.Lookup(c); id != "other" {
		t.Errorf("after rebinding, Lookup = %q, want other", id)
	}

	k.Unbind(c)
	if _, ok := k.Lookup(c); ok {
		t.Error("the binding survived Unbind")
	}
}

func TestKeymapRejectsBadBindings(t *testing.T) {
	k := NewKeymap()
	if err := k.Bind(Chord{Key: input.KeyNone}, "x"); err == nil {
		t.Error("bound the empty chord, which no key event can produce")
	}
	if err := k.Bind(Chord{Key: input.KeyA}, ""); err == nil {
		t.Error("bound a chord to no command")
	}
}

// TestKeymapChordForIsStable checks the chord shown beside a menu item
// does not change between runs when several are bound.
func TestKeymapChordForIsStable(t *testing.T) {
	k := NewKeymap()
	k.MustBind(map[Chord]string{
		{Key: input.KeyZ, Mods: input.ModCtrl}: "same",
		{Key: input.KeyA, Mods: input.ModCtrl}: "same",
		{Key: input.KeyA, Mods: input.ModAlt}:  "same",
	})

	first, ok := k.ChordFor("same")
	if !ok {
		t.Fatal("no chord found for a bound command")
	}
	for i := 0; i < 20; i++ {
		if got, _ := k.ChordFor("same"); got != first {
			t.Fatalf("ChordFor returned %s then %s", first, got)
		}
	}
	if _, ok := k.ChordFor("unbound"); ok {
		t.Error("found a chord for a command with no binding")
	}
}

func TestChordString(t *testing.T) {
	for _, tc := range []struct {
		chord Chord
		want  string
	}{
		{Chord{Key: input.KeyK, Mods: input.ModCtrl}, "ctrl+K"},
		{Chord{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}, "ctrl+shift+C"},
		{Chord{Key: input.KeyEnter}, "Enter"},
		{Chord{}, "-"},
	} {
		if got := tc.chord.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestParseChord(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Chord
	}{
		{"ctrl+K", Chord{Key: input.KeyK, Mods: input.ModCtrl}},
		{"CTRL+k", Chord{Key: input.KeyK, Mods: input.ModCtrl}},
		{"ctrl+shift+C", Chord{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}},
		{"Enter", Chord{Key: input.KeyEnter}},
		{"enter", Chord{Key: input.KeyEnter}},
		{"alt+PageUp", Chord{Key: input.KeyPageUp, Mods: input.ModAlt}},
		{"cmd+F1", Chord{Key: input.KeyF1, Mods: input.ModSuper}},
	} {
		got, err := ParseChord(tc.in)
		if err != nil {
			t.Errorf("ParseChord(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseChord(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}

	for _, bad := range []string{"", "ctrl+", "hyper+K", "ctrl+Nope", "Nope"} {
		if got, err := ParseChord(bad); err == nil {
			t.Errorf("ParseChord(%q) = %+v, want an error", bad, got)
		}
	}
}

// TestChordRoundTrip checks that what a menu shows is what a settings
// file can say.
func TestChordRoundTrip(t *testing.T) {
	for _, c := range []Chord{
		{Key: input.KeyK, Mods: input.ModCtrl},
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift},
		{Key: input.KeyF12, Mods: input.ModAlt | input.ModSuper},
		{Key: input.KeyEquals, Mods: input.ModCtrl},
		{Key: input.KeyEnter},
	} {
		back, err := ParseChord(c.String())
		if err != nil {
			t.Errorf("ParseChord(%q): %v", c.String(), err)
			continue
		}
		if back != c {
			t.Errorf("%+v rendered as %q parsed back as %+v", c, c.String(), back)
		}
	}
}

func TestChordOfIgnoresText(t *testing.T) {
	if got := ChordOf(input.Event{Kind: input.Text, Rune: 'k'}); got.Key != input.KeyNone {
		t.Errorf("a text event produced chord %+v, want none", got)
	}
	if got := ChordOf(press(input.KeyK, input.ModCtrl)); got.Key != input.KeyK {
		t.Errorf("a press produced chord %+v, want ctrl+K", got)
	}
	repeat := input.Event{Kind: input.KeyRepeat, Key: input.KeyLeft}
	if got := ChordOf(repeat); got.Key != input.KeyLeft {
		t.Error("a key repeat produced no chord, so held shortcuts would not repeat")
	}
}

func TestRootRoutesToTheWidgetFirst(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	ran := 0
	r.Commands.MustRegister(Command{ID: "x", Title: "X", Run: func() error { ran++; return nil }})
	r.Keys.MustBind(map[Chord]string{{Key: input.KeyK, Mods: input.ModCtrl}: "x"})
	w := &fake{name: "w", takes: input.KeyK}
	r.SetWidget(w)

	handled, err := r.HandleKey(press(input.KeyK, input.ModCtrl))

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled {
		t.Error("the widget consumed the key but HandleKey reported it unhandled")
	}
	if ran != 0 {
		t.Error("the binding ran even though the widget consumed the key")
	}
}

func TestRootFallsThroughToABinding(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	ran := 0
	r.Commands.MustRegister(Command{ID: "x", Title: "X", Run: func() error { ran++; return nil }})
	r.Keys.MustBind(map[Chord]string{{Key: input.KeyK, Mods: input.ModCtrl}: "x"})
	w := &fake{name: "w", takes: input.KeyQ}
	r.SetWidget(w)

	handled, err := r.HandleKey(press(input.KeyK, input.ModCtrl))

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled || ran != 1 {
		t.Errorf("handled = %v, ran = %d, want true and 1", handled, ran)
	}
	if len(w.seen) != 1 || w.seen[0] != input.KeyK {
		t.Errorf("the widget saw %v, want the key offered to it first", w.seen)
	}
}

// TestRootReportsAFailedCommandAsHandled checks that a key which ran
// something and failed is not then passed on to the shell as typing.
func TestRootReportsAFailedCommandAsHandled(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	boom := errors.New("boom")
	r.Commands.MustRegister(Command{ID: "x", Title: "X", Run: func() error { return boom }})
	r.Keys.MustBind(map[Chord]string{{Key: input.KeyK, Mods: input.ModCtrl}: "x"})
	r.SetWidget(&fake{name: "w"})

	handled, err := r.HandleKey(press(input.KeyK, input.ModCtrl))

	if !handled {
		t.Error("a key that ran a command was reported unhandled")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the command's own", err)
	}
}

func TestRootUnboundKeyIsNotHandled(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	r.SetWidget(&fake{name: "w"})

	handled, err := r.HandleKey(press(input.KeyQ, 0))

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if handled {
		t.Error("an unbound key nothing wanted was reported handled, so it would never reach the shell")
	}
}

// TestRootWithoutBindingsIsSafe checks the zero Root, which a caller
// gets before it has set anything up.
func TestRootWithoutBindingsIsSafe(t *testing.T) {
	var r Root

	handled, err := r.HandleKey(press(input.KeyK, input.ModCtrl))

	if handled || err != nil {
		t.Errorf("handled = %v, err = %v, want false and nil", handled, err)
	}
	r.Layout(Rect{Cols: 4, Rows: 2})
	r.Draw(grid.New(4, 2, fg, bg).View())
	if r.Modal() != nil || r.PopModal() != nil {
		t.Error("the zero Root produced a modal")
	}
}

func TestRootModalTakesTheWidgetsKeys(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	w := &fake{name: "w", takes: input.KeyQ}
	dialog := &fake{name: "d", takes: input.KeyEscape}
	r.SetWidget(w)
	r.PushModal(dialog)

	if handled, _ := r.HandleKey(press(input.KeyQ, 0)); handled {
		t.Error("the widget under a modal consumed a key")
	}
	if len(w.seen) != 0 {
		t.Errorf("the widget under a modal saw %v, want nothing", w.seen)
	}
	if handled, _ := r.HandleKey(press(input.KeyEscape, 0)); !handled {
		t.Error("the modal did not get its own key")
	}
}

// TestRootModalStillFallsThroughToBindings checks that a dialog does not
// have to reimplement quit and close for itself.
func TestRootModalStillFallsThroughToBindings(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	ran := 0
	r.Commands.MustRegister(Command{ID: "quit", Title: "Quit", Run: func() error { ran++; return nil }})
	r.Accelerators.MustBind(map[Chord]string{{Key: input.KeyQ, Mods: input.ModCtrl}: "quit"})
	r.SetWidget(&fake{name: "w"})
	r.PushModal(&fake{name: "d", takes: input.KeyEscape})

	handled, err := r.HandleKey(press(input.KeyQ, input.ModCtrl))

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled || ran != 1 {
		t.Errorf("handled = %v, ran = %d, want true and 1", handled, ran)
	}
}

func TestRootModalStack(t *testing.T) {
	r := &Root{}
	w := &fake{name: "w"}
	first := &fake{name: "1"}
	second := &fake{name: "2"}
	r.SetWidget(w)

	r.PushModal(first)
	r.PushModal(second)
	if got := r.Modals(); len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("stack = %v, want [first second]", got)
	}
	if r.Modal() != second {
		t.Fatalf("top = %v, want the second", r.Modal())
	}

	if got := r.PopModal(); got != second {
		t.Errorf("PopModal returned %v, want the second", got)
	}
	if r.Modal() != first {
		t.Errorf("top = %v, want the first back", r.Modal())
	}
	r.PopModal()
	if r.Modal() != nil || len(r.Modals()) != 0 {
		t.Error("the stack did not empty")
	}
	if got := r.PopModal(); got != nil {
		t.Errorf("popping an empty stack returned %v, want nil", got)
	}
}

func TestRootFocusFollowsWhoGetsKeys(t *testing.T) {
	r := &Root{}
	w := &fake{name: "w"}
	dialog := &fake{name: "d"}

	r.SetWidget(w)
	if !w.focus {
		t.Fatal("the widget did not take focus")
	}

	r.PushModal(dialog)
	if w.focus {
		t.Error("the widget kept focus under a modal")
	}
	if !dialog.focus {
		t.Error("the modal did not take focus")
	}

	r.PopModal()
	if !w.focus {
		t.Error("focus did not come back when the modal closed")
	}
	if dialog.focus {
		t.Error("the closed modal kept focus")
	}

	// SetFocus must never be called twice with the same value.
	for _, f := range []*fake{w, dialog} {
		for i := 1; i < len(f.focused); i++ {
			if f.focused[i] == f.focused[i-1] {
				t.Errorf("%s saw SetFocus(%v) twice in a row: %v", f.name, f.focused[i], f.focused)
				break
			}
		}
	}
}

// TestRootSetWidgetTwiceDoesNotRefocus checks the guard against telling a
// widget it gained focus it already had.
func TestRootSetWidgetTwiceDoesNotRefocus(t *testing.T) {
	r := &Root{}
	w := &fake{name: "w"}

	r.SetWidget(w)
	r.SetWidget(w)

	if len(w.focused) != 1 {
		t.Errorf("SetFocus calls = %v, want one", w.focused)
	}
}

// TestRootAModalTakesFocusFromANewWidget checks that swapping the widget
// under an open dialog does not steal the dialog's keys.
func TestRootAModalTakesFocusFromANewWidget(t *testing.T) {
	r := &Root{}
	dialog := &fake{name: "d"}
	r.SetWidget(&fake{name: "old"})
	r.PushModal(dialog)

	replacement := &fake{name: "new"}
	r.SetWidget(replacement)

	if replacement.focus {
		t.Error("the new widget took focus while a modal was open")
	}
	if !dialog.focus {
		t.Error("the modal lost focus when the widget under it changed")
	}
}

func TestRootLayoutReachesWidgetAndModals(t *testing.T) {
	r := &Root{}
	w := &fake{name: "w"}
	dialog := &fake{name: "d"}
	r.SetWidget(w)
	r.PushModal(dialog)

	area := Rect{X: 7, Y: 3, Cols: 20, Rows: 6}
	r.Layout(area)

	want := Size{Cols: 20, Rows: 6}
	if w.size != want {
		t.Errorf("widget size = %+v, want %+v: the offset leaked into the size", w.size, want)
	}
	if dialog.size != want {
		t.Errorf("modal size = %+v, want %+v", dialog.size, want)
	}
	if r.Area() != area {
		t.Errorf("Area() = %+v, want %+v", r.Area(), area)
	}
}

// TestRootLaysOutAWidgetSetAfterLayout checks a widget arriving once the
// area is already known, which is what happens when a pane is swapped.
func TestRootLaysOutAWidgetSetAfterLayout(t *testing.T) {
	r := &Root{}
	area := Rect{X: 7, Y: 3, Cols: 20, Rows: 6}
	r.Layout(area)

	w := &fake{name: "w"}
	r.SetWidget(w)

	if want := (Size{Cols: 20, Rows: 6}); w.size != want {
		t.Errorf("widget size = %+v, want %+v: it was never told its size", w.size, want)
	}
}

func TestRootDrawPaintsTheWidget(t *testing.T) {
	r := &Root{}
	w := &fake{name: "hi"}
	r.SetWidget(w)
	r.Layout(Rect{Cols: 6, Rows: 2})
	g := grid.New(6, 2, fg, bg)

	r.Draw(g.View())

	if w.drawn != 1 {
		t.Errorf("drawn %d times, want 1", w.drawn)
	}
	if got := g.At(0, 0).Rune; got != 'h' {
		t.Errorf("grid 0,0 = %q, want 'h'", got)
	}
}

// TestRootDrawLeavesModalsToTheirOwnLayer checks that a dialog is not
// painted into the tree's grid, which would force a repaint underneath
// it every time it closed.
func TestRootDrawLeavesModalsToTheirOwnLayer(t *testing.T) {
	r := &Root{}
	dialog := &fake{name: "d"}
	r.SetWidget(&fake{name: "w"})
	r.PushModal(dialog)

	r.Draw(grid.New(6, 2, fg, bg).View())

	if dialog.drawn != 0 {
		t.Errorf("the modal was drawn %d times into the tree's grid", dialog.drawn)
	}
}

// TestRootWidgetWithoutHandlersIsSafe checks a widget that only draws,
// which is the whole point of keeping Widget to two methods.
func TestRootWidgetWithoutHandlersIsSafe(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	r.SetWidget(plain{})

	handled, err := r.HandleKey(press(input.KeyQ, 0))

	if handled || err != nil {
		t.Errorf("handled = %v, err = %v, want false and nil", handled, err)
	}
}

type plain struct{}

func (plain) Layout(Size)    {}
func (plain) Draw(grid.View) {}

// TestRootDrawClipsToItsArea checks that the tree cannot draw outside
// the area it was given, and that the widget is handed a view of exactly
// that size rather than the whole grid.
func TestRootDrawClipsToItsArea(t *testing.T) {
	r := &Root{}
	w := &fake{name: "hi"}
	r.SetWidget(w)
	r.Layout(Rect{X: 3, Y: 1, Cols: 4, Rows: 2})
	g := grid.New(20, 6, fg, bg)

	r.Draw(g.View())

	if w.drawSz != (Size{Cols: 4, Rows: 2}) {
		t.Errorf("the widget got a %+v view, want 4x2", w.drawSz)
	}
	if got := g.At(3, 1).Rune; got != 'h' {
		t.Errorf("grid 3,1 = %q, want 'h': the area offset was dropped", got)
	}
	if got := g.At(0, 0).Rune; got != ' ' {
		t.Errorf("grid 0,0 = %q, want a blank: the widget drew outside its area", got)
	}
}

// TestRootAcceleratorBeatsAGreedyWidget is the case the two tiers exist
// for. A terminal has a meaning for every key, so a binding that only
// ran after it would never run.
func TestRootAcceleratorBeatsAGreedyWidget(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	accel, after := 0, 0
	r.Commands.MustRegister(
		Command{ID: "quit", Title: "Quit", Run: func() error { accel++; return nil }},
		Command{ID: "other", Title: "Other", Run: func() error { after++; return nil }},
	)
	r.Accelerators.MustBind(map[Chord]string{{Key: input.KeyQ, Mods: input.ModCtrl}: "quit"})
	r.Keys.MustBind(map[Chord]string{{Key: input.KeyW, Mods: input.ModCtrl}: "other"})
	r.SetWidget(&greedy{})

	if handled, err := r.HandleKey(press(input.KeyQ, input.ModCtrl)); !handled || err != nil {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	if accel != 1 {
		t.Errorf("the accelerator ran %d times, want 1: a greedy widget swallowed it", accel)
	}

	if handled, _ := r.HandleKey(press(input.KeyW, input.ModCtrl)); !handled {
		t.Error("the greedy widget did not consume the key")
	}
	if after != 0 {
		t.Error("an ordinary binding ran even though the widget consumed the key")
	}
}

// TestRootModalBeatsAnAccelerator checks that a dialog keeps the keys
// it needs. Escape cancelling a dialog is the commonest interaction
// there is, and it must still work when Escape is also a global
// shortcut: suspending the usual meanings is what makes a modal modal.
func TestRootModalBeatsAnAccelerator(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap()}
	quit := 0
	r.Commands.MustRegister(Command{ID: "quit", Title: "Quit", Run: func() error { quit++; return nil }})
	r.Accelerators.MustBind(map[Chord]string{{Key: input.KeyEscape}: "quit"})
	r.SetWidget(&fake{name: "w"})
	dialog := &fake{name: "d", takes: input.KeyEscape}
	r.PushModal(dialog)

	handled, err := r.HandleKey(press(input.KeyEscape, 0))

	if err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if !handled {
		t.Error("the dialog did not get its own key")
	}
	if quit != 0 {
		t.Error("the accelerator ran, so the program quit instead of closing the dialog")
	}
}

// TestRootAcceleratorReachesPastAModalThatPassesItOn checks the other
// half: a dialog that does not want a key still lets the global
// shortcut through, so it need not reimplement quit for itself.
func TestRootAcceleratorReachesPastAModalThatPassesItOn(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap()}
	quit := 0
	r.Commands.MustRegister(Command{ID: "quit", Title: "Quit", Run: func() error { quit++; return nil }})
	r.Accelerators.MustBind(map[Chord]string{{Key: input.KeyQ, Mods: input.ModCtrl}: "quit"})
	r.SetWidget(&fake{name: "w"})
	r.PushModal(&fake{name: "d", takes: input.KeyEscape})

	if _, err := r.HandleKey(press(input.KeyQ, input.ModCtrl)); err != nil {
		t.Fatalf("HandleKey: %v", err)
	}

	if quit != 1 {
		t.Errorf("the accelerator ran %d times, want 1", quit)
	}
}

// TestRootAcceleratorErrorIsReported checks that a failing accelerator
// does not swallow its error, the way the after-bindings do not.
func TestRootAcceleratorErrorIsReported(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap()}
	boom := errors.New("boom")
	r.Commands.MustRegister(Command{ID: "x", Title: "X", Run: func() error { return boom }})
	r.Accelerators.MustBind(map[Chord]string{{Key: input.KeyK, Mods: input.ModCtrl}: "x"})
	r.SetWidget(&fake{name: "w"})

	handled, err := r.HandleKey(press(input.KeyK, input.ModCtrl))

	if !handled {
		t.Error("a key that ran an accelerator was reported unhandled")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the command's own", err)
	}
}

// TestRootWidgetErrorDoesNotStopTheKey checks that a widget reporting a
// failure has not thereby consumed the key. The two answers are
// separate: what broke, and who took it.
func TestRootWidgetErrorDoesNotStopTheKey(t *testing.T) {
	r := &Root{Commands: NewCommands(), Keys: NewKeymap()}
	ran := 0
	r.Commands.MustRegister(Command{ID: "x", Title: "X", Run: func() error { ran++; return nil }})
	r.Keys.MustBind(map[Chord]string{{Key: input.KeyK, Mods: input.ModCtrl}: "x"})
	boom := errors.New("autosave failed")
	r.SetWidget(&noisyPass{err: boom})

	handled, err := r.HandleKey(press(input.KeyK, input.ModCtrl))

	if !handled {
		t.Error("the key was reported unhandled, but the binding took it")
	}
	if ran != 1 {
		t.Errorf("the binding ran %d times, want 1: a widget's error stopped the key", ran)
	}
	// The failure still has to come out. It just does not decide who
	// gets the key.
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the widget's failure carried out", err)
	}
}

// noisyPass reports a failure without consuming anything, which is what
// a widget doing background work looks like.
type noisyPass struct{ err error }

func (*noisyPass) Layout(Size)    {}
func (*noisyPass) Draw(grid.View) {}
func (n *noisyPass) HandleKey(input.Event) (bool, error) {
	return false, n.err
}

// TestRootStaleBindingDoesNotEatTheKey checks a binding left pointing at
// a command that has gone. Bindings outlive the panes and tabs that
// register commands, and a settings file can name one that no longer
// exists.
func TestRootStaleBindingDoesNotEatTheKey(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap()}
	r.Commands.MustRegister(Command{ID: "pane.close", Title: "Close", Run: func() error { return nil }})
	r.Accelerators.MustBind(map[Chord]string{{Key: input.KeyW, Mods: input.ModCtrl}: "pane.close"})
	w := &fake{name: "w"}
	r.SetWidget(w)

	r.Commands.Unregister("pane.close")
	handled, err := r.HandleKey(press(input.KeyW, input.ModCtrl))

	if handled {
		t.Error("a stale binding consumed the key, killing it for good")
	}
	if err == nil {
		t.Error("a stale binding was passed over silently")
	}
	if len(w.seen) != 1 {
		t.Errorf("the widget saw %v, want the key offered to it", w.seen)
	}
}

type greedy struct{}

func (*greedy) Layout(Size)    {}
func (*greedy) Draw(grid.View) {}
func (*greedy) HandleKey(input.Event) (bool, error) {
	return true, nil
}

// TestRootReportsAWidgetError checks that a widget which ran something
// and failed can say so, rather than having to swallow it.
func TestRootReportsAWidgetError(t *testing.T) {
	r := &Root{Commands: NewCommands(), Keys: NewKeymap()}
	boom := errors.New("boom")
	w := &fake{name: "w", takes: input.KeyQ, fails: boom}
	r.SetWidget(w)

	handled, err := r.HandleKey(press(input.KeyQ, 0))

	if !handled {
		t.Error("a key the widget acted on was reported unhandled")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the widget's own", err)
	}
}

// TestChordOfIgnoresKeyRelease checks the guard that stops every bound
// shortcut firing twice, once on press and once on release. A release
// carries a real key and real modifiers, unlike a text event.
func TestChordOfIgnoresKeyRelease(t *testing.T) {
	release := input.Event{Kind: input.KeyRelease, Key: input.KeyK, Mods: input.ModCtrl}

	if got := ChordOf(release); got.Key != input.KeyNone {
		t.Errorf("a release produced chord %+v, want none", got)
	}
}

// TestRootRunsABindingOncePerKeystroke checks the same guard through the
// routing, where a double run would be visible to the user.
func TestRootRunsABindingOncePerKeystroke(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap()}
	ran := 0
	r.Commands.MustRegister(Command{ID: "x", Title: "X", Run: func() error { ran++; return nil }})
	r.Accelerators.MustBind(map[Chord]string{{Key: input.KeyK, Mods: input.ModCtrl}: "x"})
	r.SetWidget(&fake{name: "w"})

	if _, err := r.HandleKey(press(input.KeyK, input.ModCtrl)); err != nil {
		t.Fatalf("HandleKey: %v", err)
	}
	if _, err := r.HandleKey(input.Event{
		Kind: input.KeyRelease, Key: input.KeyK, Mods: input.ModCtrl,
	}); err != nil {
		t.Fatalf("HandleKey: %v", err)
	}

	if ran != 1 {
		t.Errorf("ran %d times for one keystroke, want 1", ran)
	}
}

// TestRootNilCommandsWithBindingsIsSafe checks the guard against a
// keymap with no registry behind it.
func TestRootNilCommandsWithBindingsIsSafe(t *testing.T) {
	r := &Root{Accelerators: NewKeymap(), Keys: NewKeymap()}
	r.Accelerators.MustBind(map[Chord]string{{Key: input.KeyK, Mods: input.ModCtrl}: "x"})
	r.SetWidget(&fake{name: "w"})

	handled, err := r.HandleKey(press(input.KeyK, input.ModCtrl))

	if handled || err != nil {
		t.Errorf("handled = %v, err = %v, want false and nil", handled, err)
	}
}

// TestRootSetWidgetUnderAModalLeavesFocusAlone checks that replacing the
// widget under an open dialog does not tell the old one it lost focus it
// never had.
func TestRootSetWidgetUnderAModalLeavesFocusAlone(t *testing.T) {
	r := &Root{}
	w := &fake{name: "w"}
	r.SetWidget(w)
	r.PushModal(&fake{name: "d"})

	r.SetWidget(&fake{name: "new"})

	want := []bool{true, false} // focused, then lost it to the modal
	if len(w.focused) != len(want) {
		t.Fatalf("SetFocus calls = %v, want %v", w.focused, want)
	}
	for i, v := range want {
		if w.focused[i] != v {
			t.Fatalf("SetFocus calls = %v, want %v", w.focused, want)
		}
	}
}

// TestRootPushModalTwiceIsIgnored checks that a dialog pushed again does
// not stack on itself, which would make one PopModal leave it open.
func TestRootPushModalTwiceIsIgnored(t *testing.T) {
	r := &Root{}
	d := &fake{name: "d"}
	r.SetWidget(&fake{name: "w"})

	r.PushModal(d)
	r.PushModal(d)

	if got := len(r.Modals()); got != 1 {
		t.Errorf("stack holds %d, want 1", got)
	}
	r.PopModal()
	if r.Modal() != nil {
		t.Error("one pop did not close the dialog")
	}
}

// TestCommandsUnregister checks the runtime churn a tab or a pane makes:
// its commands go away when it closes and the same ids come back when a
// new one opens.
func TestCommandsUnregister(t *testing.T) {
	c := NewCommands()
	nop := func() error { return nil }
	c.MustRegister(Command{ID: "tab.4", Title: "Tab 4", Run: nop})

	if !c.Unregister("tab.4") {
		t.Error("Unregister reported nothing was there")
	}
	if c.Unregister("tab.4") {
		t.Error("Unregister reported removing the same command twice")
	}
	if _, ok := c.Lookup("tab.4"); ok {
		t.Error("the command survived Unregister")
	}
	if err := c.Register(Command{ID: "tab.4", Title: "New Tab 4", Run: nop}); err != nil {
		t.Errorf("re-registering after Unregister: %v", err)
	}
}

// TestCommandsAllOrdersByTitleNotID checks the documented order, using
// data where sorting by id alone gives a different answer.
func TestCommandsAllOrdersByTitleNotID(t *testing.T) {
	nop := func() error { return nil }
	c := NewCommands()
	c.MustRegister(
		Command{ID: "a", Title: "Zulu", Run: nop},
		Command{ID: "z", Title: "Alpha", Run: nop},
	)

	got := c.All()

	if len(got) != 2 || got[0].ID != "z" || got[1].ID != "a" {
		t.Errorf("order = %v, want [z a]: sorted by title, not id", ids(got))
	}
}

// TestKeymapBindingsAreSortedAndCopied checks what a settings file gets:
// the same order every run, from a snapshot the keymap cannot change
// underneath it.
func TestKeymapBindingsAreSortedAndCopied(t *testing.T) {
	k := NewKeymap()
	k.MustBind(map[Chord]string{
		{Key: input.KeyZ, Mods: input.ModCtrl}: "three",
		{Key: input.KeyA, Mods: input.ModCtrl}: "two",
		{Key: input.KeyA}:                      "one",
	})

	first := k.Bindings()
	want := []string{"one", "two", "three"}
	for i, id := range want {
		if first[i].ID != id {
			t.Fatalf("order = %v, want %v", bindingIDs(first), want)
		}
	}
	for i := 0; i < 20; i++ {
		if got := k.Bindings(); !sameBindings(got, first) {
			t.Fatalf("order changed between calls: %v then %v",
				bindingIDs(first), bindingIDs(got))
		}
	}

	first[0].ID = "tampered"
	if got := k.Bindings(); got[0].ID != "one" {
		t.Error("changing the returned slice changed the keymap")
	}
}

func bindingIDs(bs []Binding) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.ID
	}
	return out
}

func sameBindings(a, b []Binding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParseChordTrimsSpaces checks the stray spaces a hand-edited
// settings file collects.
func TestParseChordTrimsSpaces(t *testing.T) {
	want := Chord{Key: input.KeyK, Mods: input.ModCtrl}
	for _, in := range []string{" ctrl+K", "ctrl+K ", "ctrl + K", "  ctrl  +  K  "} {
		got, err := ParseChord(in)
		if err != nil {
			t.Errorf("ParseChord(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseChord(%q) = %+v, want %+v", in, got, want)
		}
	}
}

// TestRectSize checks the conversion the whole layout design rests on:
// a container divides itself in Rects and tells each child only a Size.
func TestRectSize(t *testing.T) {
	got := Rect{X: 7, Y: 3, Cols: 20, Rows: 6}.Size()

	if want := (Size{Cols: 20, Rows: 6}); got != want {
		t.Errorf("Size() = %+v, want %+v", got, want)
	}
}

func TestSizeEmpty(t *testing.T) {
	if (Size{Cols: 3, Rows: 2}).Empty() {
		t.Error("a 3x2 size reported empty")
	}
	for _, s := range []Size{{}, {Cols: 4}, {Rows: 4}, {Cols: -1, Rows: 2}, {Cols: 2, Rows: -1}} {
		if !s.Empty() {
			t.Errorf("%+v reported non-empty", s)
		}
	}
}

// TestRectLocal checks the conversion a container needs to route a
// mouse event into a child's own coordinates.
func TestRectLocal(t *testing.T) {
	r := Rect{X: 4, Y: 2, Cols: 6, Rows: 3}

	x, y := r.Local(5, 4)

	if x != 1 || y != 2 {
		t.Errorf("Local(5,4) = %d,%d, want 1,2", x, y)
	}
}

// TestRootModalsIsACopy checks that a caller drawing the stack cannot
// have it shift underneath, and cannot reach in and reorder it.
func TestRootModalsIsACopy(t *testing.T) {
	r := &Root{}
	first := &fake{name: "1"}
	second := &fake{name: "2"}
	r.PushModal(first)
	r.PushModal(second)

	got := r.Modals()
	got[0] = &fake{name: "tampered"}

	if again := r.Modals(); again[0] != first {
		t.Error("changing the returned slice changed the stack")
	}
}

// TestParseChordFromSeveralGoroutines checks that a config loader may
// call it off the drawing goroutine. It builds a cache on first use.
func TestParseChordFromSeveralGoroutines(t *testing.T) {
	// Other tests have already built the cache, so the concurrent build
	// this is here to check would never run.
	keysByName, keysByNameOnce = nil, sync.Once{}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ParseChord("ctrl+K"); err != nil {
				t.Errorf("ParseChord: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestRootKeepsEveryFailureOnTheWay pins the error policy. Two stages
// can fail on one key: the one that declined it and the one that acted.
// Keeping only the first would report the stale binding and lose the
// command that actually ran and broke.
func TestRootKeepsEveryFailureOnTheWay(t *testing.T) {
	r := &Root{Commands: NewCommands(), Accelerators: NewKeymap(), Keys: NewKeymap()}
	boom := errors.New("disk full")
	r.Commands.MustRegister(Command{ID: "save", Title: "Save", Run: func() error { return boom }})
	chord := Chord{Key: input.KeyS, Mods: input.ModCtrl}
	// The accelerator is stale; the after-binding runs and fails.
	r.Accelerators.MustBind(map[Chord]string{chord: "gone"})
	r.Keys.MustBind(map[Chord]string{chord: "save"})
	r.SetWidget(&fake{name: "w"})

	handled, err := r.HandleKey(press(input.KeyS, input.ModCtrl))

	if !handled {
		t.Error("the after-binding took the key but it was reported unhandled")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the failure of the command that actually ran", err)
	}
	if !strings.Contains(err.Error(), "gone") {
		t.Errorf("error = %v, want the stale binding reported too", err)
	}
}

// TestRootKeepsAWidgetFailureAlongsideACommandFailure is the same rule
// with the failure coming from a widget rather than a binding.
func TestRootKeepsAWidgetFailureAlongsideACommandFailure(t *testing.T) {
	r := &Root{Commands: NewCommands(), Keys: NewKeymap()}
	widgetErr := errors.New("autosave failed")
	cmdErr := errors.New("disk full")
	r.Commands.MustRegister(Command{ID: "x", Title: "X", Run: func() error { return cmdErr }})
	r.Keys.MustBind(map[Chord]string{{Key: input.KeyK, Mods: input.ModCtrl}: "x"})
	r.SetWidget(&noisyPass{err: widgetErr})

	_, err := r.HandleKey(press(input.KeyK, input.ModCtrl))

	if !errors.Is(err, widgetErr) {
		t.Errorf("error = %v, want the widget's failure kept", err)
	}
	if !errors.Is(err, cmdErr) {
		t.Errorf("error = %v, want the command's failure kept", err)
	}
}

// TestRootMouseDragThatLeavesTheAreaStillFinishes checks pointer
// capture. Dragging to select and releasing past the edge is an everyday
// gesture, and dropping the release leaves the widget selecting for ever.
func TestRootMouseDragThatLeavesTheAreaStillFinishes(t *testing.T) {
	r := &Root{}
	w := &mouser{}
	r.SetWidget(w)
	r.Layout(Rect{Cols: 10, Rows: 4})

	r.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 2, Row: 1})
	r.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 40, Row: 40})
	r.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 40, Row: 40})

	kinds := w.kinds()
	if len(kinds) != 3 {
		t.Fatalf("the widget saw %d events, want all 3 of the drag", len(kinds))
	}
	if kinds[2] != input.MouseRelease {
		t.Error("the release outside the area was dropped, so the widget is still dragging")
	}
	// Past the edge clamps to the edge, so a drag selects to the end
	// rather than to a negative column.
	if got := w.last(); got.Col != 9 || got.Row != 3 {
		t.Errorf("release at %d,%d, want it clamped to 9,3", got.Col, got.Row)
	}
}

// TestRootMousePressOutsideTheAreaIsIgnored checks the other half: with
// nothing held, a press that missed the tree is not routed into it.
func TestRootMousePressOutsideTheAreaIsIgnored(t *testing.T) {
	r := &Root{}
	w := &mouser{}
	r.SetWidget(w)
	r.Layout(Rect{X: 2, Y: 1, Cols: 4, Rows: 2})

	handled, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 0, Row: 0,
	})

	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if handled || len(w.kinds()) != 0 {
		t.Error("a press outside the tree was routed into it")
	}
}

// TestRootWheelDoesNotHoldThePointer checks that a wheel notch, which is
// a press with no release, does not capture the pointer for ever.
func TestRootWheelDoesNotHoldThePointer(t *testing.T) {
	r := &Root{}
	w := &mouser{}
	r.SetWidget(w)
	r.Layout(Rect{Cols: 10, Rows: 4})

	r.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseWheelUp, Col: 1, Row: 1})
	handled, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 99, Row: 99,
	})

	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if handled {
		t.Error("a wheel notch held the pointer, so later clicks anywhere reach the widget")
	}
}

// TestRootOpeningADialogReleasesThePointer checks that a dialog arriving
// mid-drag does not leave the pointer held by the widget underneath.
func TestRootOpeningADialogReleasesThePointer(t *testing.T) {
	r := &Root{}
	under := &mouser{}
	r.SetWidget(under)
	r.Layout(Rect{Cols: 10, Rows: 4})
	r.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 1, Row: 1})

	dialog := &mouser{}
	r.PushModal(dialog)
	r.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 2, Row: 1})

	if len(under.kinds()) != 1 {
		t.Errorf("the widget under the dialog saw %d events, want only its press", len(under.kinds()))
	}
	if len(dialog.kinds()) != 1 {
		t.Errorf("the dialog saw %d events, want the move", len(dialog.kinds()))
	}
}

// mouser records the mouse events it was given.
type mouser struct {
	seen []input.MouseEvent
}

func (*mouser) Layout(Size)    {}
func (*mouser) Draw(grid.View) {}
func (m *mouser) HandleMouse(ev input.MouseEvent) (bool, error) {
	m.seen = append(m.seen, ev)
	return true, nil
}

func (m *mouser) kinds() []input.MouseKind {
	out := make([]input.MouseKind, len(m.seen))
	for i, ev := range m.seen {
		out[i] = ev.Kind
	}
	return out
}

func (m *mouser) last() input.MouseEvent { return m.seen[len(m.seen)-1] }

// cursorWidget is a focused widget that places a cursor, which is what
// distinguishes clearing the cursor before drawing from after.
type cursorWidget struct {
	at grid.Cursor
}

func (*cursorWidget) Layout(Size) {}
func (c *cursorWidget) Draw(v grid.View) {
	v.SetCursor(c.at)
}

// TestRootDrawKeepsAPlacedCursor checks that a widget's cursor survives
// the pass. Clearing it after drawing without asking who placed one
// would wipe it.
func TestRootDrawKeepsAPlacedCursor(t *testing.T) {
	r := &Root{}
	want := grid.Cursor{X: 2, Y: 1, Visible: true}
	r.SetWidget(&cursorWidget{at: want})
	r.Layout(Rect{Cols: 6, Rows: 2})
	g := grid.New(6, 2, fg, bg)

	r.Draw(g.View())

	if got := g.Cursor(); got != want {
		t.Errorf("cursor = %+v, want the one the widget placed, %+v", got, want)
	}
}

// TestRootDrawHidesAnUnclaimedCursor checks the other half: with nobody
// placing one, last frame's cursor does not linger.
func TestRootDrawHidesAnUnclaimedCursor(t *testing.T) {
	r := &Root{}
	r.SetWidget(&fake{name: "w"})
	r.Layout(Rect{Cols: 6, Rows: 2})
	g := grid.New(6, 2, fg, bg)
	g.SetCursor(grid.Cursor{X: 3, Y: 1, Visible: true})

	r.Draw(g.View())

	if g.Cursor().Visible {
		t.Error("a cursor from the last frame survived, so an unfocused widget keeps one")
	}
}

// TestRootDrawIdleFrameDirtiesNothing checks the property the whole
// design rests on. Hiding the cursor and letting the widget write it
// back would change one slot twice and dirty a row every frame, on a
// terminal nobody is typing into.
func TestRootDrawIdleFrameDirtiesNothing(t *testing.T) {
	r := &Root{}
	r.SetWidget(&cursorWidget{at: grid.Cursor{X: 2, Y: 1, Visible: true}})
	r.Layout(Rect{Cols: 6, Rows: 2})
	g := grid.New(6, 2, fg, bg)
	r.Draw(g.View())
	g.ClearDirty()

	for i := 0; i < 3; i++ {
		r.Draw(g.View())
		if g.AnyDirty() {
			t.Fatalf("idle frame %d dirtied the grid", i)
		}
	}
}

// TestRootDrawModalSettlesItsOwnCursor checks that a dialog on its own
// layer gets the same treatment as the tree, rather than its layer's
// cursor being nobody's job.
func TestRootDrawModalSettlesItsOwnCursor(t *testing.T) {
	r := &Root{}
	g := grid.New(6, 2, fg, bg)
	g.SetCursor(grid.Cursor{X: 4, Y: 1, Visible: true})

	r.DrawModal(&fake{name: "d"}, g.View())

	if g.Cursor().Visible {
		t.Error("the dialog's layer kept a cursor nobody placed")
	}

	want := grid.Cursor{X: 1, Y: 0, Visible: true}
	r.DrawModal(&cursorWidget{at: want}, g.View())

	if got := g.Cursor(); got != want {
		t.Errorf("cursor = %+v, want the dialog's own %+v", got, want)
	}
}

// TestRootMouseReleaseEndsTheCapture checks the dangerous half of
// capture. A capture that is never released means the widget under the
// pointer never sees another press.
func TestRootMouseReleaseEndsTheCapture(t *testing.T) {
	r := &Root{}
	w := &mouser{}
	r.SetWidget(w)
	r.Layout(Rect{Cols: 10, Rows: 4})

	r.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 2, Row: 1})
	r.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 2, Row: 1})

	if r.held.Holder() != nil {
		t.Fatal("the release did not end the capture")
	}
	// With the capture gone, a press outside is ignored again.
	handled, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 99, Row: 99,
	})
	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if handled {
		t.Error("a press outside was routed, so the capture is stuck")
	}
}

// TestRootSecondButtonDoesNotEndADrag checks that tapping another button
// mid-drag leaves the drag alone. Middle-click paste during a selection
// does exactly this.
func TestRootSecondButtonDoesNotEndADrag(t *testing.T) {
	r := &Root{}
	w := &mouser{}
	r.SetWidget(w)
	r.Layout(Rect{Cols: 10, Rows: 4})

	r.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 2, Row: 1})
	r.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseMiddle, Col: 3, Row: 1})
	r.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseMiddle, Col: 3, Row: 1})
	r.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 40, Row: 40})

	if r.held.Holder() == nil {
		t.Fatal("the other button's release ended the drag")
	}
	if got := w.last(); got.Kind != input.MouseMove || got.Col != 9 {
		t.Errorf("last event = %+v, want the drag still running and clamped to the edge", got)
	}
}

// TestRootWheelOutsideTheAreaStillScrolls checks the pixels left over
// below the last whole row. GridSizeFor floors, so a strip of the window
// is inside it and outside the grid, and the wheel has to work there.
func TestRootWheelOutsideTheAreaStillScrolls(t *testing.T) {
	r := &Root{}
	w := &mouser{}
	r.SetWidget(w)
	r.Layout(Rect{Cols: 10, Rows: 4})

	handled, err := r.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseWheelUp, Col: 5, Row: 9,
	})

	if err != nil {
		t.Fatalf("HandleMouse: %v", err)
	}
	if !handled || len(w.seen) != 1 {
		t.Error("a wheel notch in the leftover strip was dropped")
	}
	if got := w.last(); got.Row != 3 {
		t.Errorf("wheel at row %d, want it clamped to the last row", got.Row)
	}
}

// TestRootMotionOutsideTheAreaIsStillReported checks the same strip for
// bare motion, which a program in any-event tracking mode wants.
func TestRootMotionOutsideTheAreaIsStillReported(t *testing.T) {
	r := &Root{}
	w := &mouser{}
	r.SetWidget(w)
	r.Layout(Rect{Cols: 10, Rows: 4})

	r.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Col: 5, Row: 9})

	if len(w.seen) != 1 {
		t.Error("motion in the leftover strip was dropped")
	}
}

// TestRootSetWidgetReleasesThePointer checks that swapping the tree does
// not leave the pointer held by a widget no longer in it.
func TestRootSetWidgetReleasesThePointer(t *testing.T) {
	r := &Root{}
	old := &mouser{}
	r.SetWidget(old)
	r.Layout(Rect{Cols: 10, Rows: 4})
	r.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 1, Row: 1})

	r.SetWidget(&mouser{})

	if r.held.Holder() != nil {
		t.Error("the replaced widget still holds the pointer")
	}
}

// TestMouseCaptureIgnoresASecondPress checks that the first button to
// take the pointer keeps it, rather than the last one winning.
func TestMouseCaptureIgnoresASecondPress(t *testing.T) {
	var c MouseCapture
	first, second := &mouser{}, &mouser{}

	c.Take(first, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft})
	c.Take(second, input.MouseEvent{Kind: input.MousePress, Button: input.MouseRight})

	if c.Holder() != first {
		t.Error("a second press stole the pointer from the first")
	}
	c.Take(second, input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft})
	if c.Holder() != nil {
		t.Error("the capturing button's release did not free the pointer")
	}
}

// TestMouseCaptureIgnoresAStrayRelease checks a release with no press
// behind it, which arrives when a button comes up over the window after
// being pressed somewhere else.
func TestMouseCaptureIgnoresAStrayRelease(t *testing.T) {
	var c MouseCapture

	c.Take(&mouser{}, input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft})

	if c.Holder() != nil {
		t.Error("a stray release captured the pointer")
	}
}

// TestRootSetWidgetWithNoAreaDoesNotResize checks the trap that squashes
// a shell before the first frame. A root has no area until Layout, and
// telling a terminal it has no room resizes its shell to one column and
// reflows the scrollback, which the next Layout cannot undo.
func TestRootSetWidgetWithNoAreaDoesNotResize(t *testing.T) {
	r := &Root{}
	w := &fake{name: "w"}

	r.SetWidget(w)

	if len(w.sizes) != 0 {
		t.Errorf("the widget was told %v before the root had an area", w.sizes)
	}
	r.Layout(Rect{Cols: 20, Rows: 6})
	if got := w.size; got != (Size{Cols: 20, Rows: 6}) {
		t.Errorf("after Layout the widget has %+v, want 20x6", got)
	}
}

// TestRootLayoutToNothingTellsNobody checks the same rule on the way
// down: a window with no room yet must not resize what is in it.
func TestRootLayoutToNothingTellsNobody(t *testing.T) {
	r := &Root{}
	w := &fake{name: "w"}
	r.SetWidget(w)
	r.Layout(Rect{Cols: 20, Rows: 6})
	was := len(w.sizes)

	r.Layout(Rect{})

	if len(w.sizes) != was {
		t.Errorf("the widget was told %v, want it left at its last size", w.sizes)
	}
}

// TestRootPushModalWithNoAreaDoesNotResize checks the same for a dialog
// pushed before the window has a size.
func TestRootPushModalWithNoAreaDoesNotResize(t *testing.T) {
	r := &Root{}
	d := &fake{name: "d"}

	r.PushModal(d)

	if len(d.sizes) != 0 {
		t.Errorf("the dialog was told %v before the root had an area", d.sizes)
	}
}
