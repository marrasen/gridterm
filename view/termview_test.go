package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/winkeys"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/vt"
)

// termStage puts one terminal pane on stage, with the keyboard, in a
// window of size.
func termStage(t *testing.T, size geom.Size) (*Window, *term) {
	t.Helper()
	win, sh, publish := windowStageOf(t, size)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	return win, win.terms["p1"]
}

func frames(n int) {
	for range n {
		lastWindow.Frame(time.Second / 60)
	}
}

// The click that gives a pane the keyboard only does that; the next
// one reaches the terminal.
func TestTheClickThatFocusesAPaneOnlyFocusesIt(t *testing.T) {
	_, tm := termStage(t, geom.Sz(900, 600))
	box, _ := lastUI.Bounds(tm)
	lastUI.Focus(nil)
	frames(1)
	click := func() input.MouseButton {
		lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1})
		held := tm.held
		lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
		frames(1)
		return held
	}
	if held := click(); held != input.MouseNone || !tm.focused {
		t.Fatalf("the focusing click held %v, focused %v", held, tm.focused)
	}
	if held := click(); held != input.MouseLeft {
		t.Fatalf("the next click held %v, want the left button", held)
	}
}

// A pane without the keyboard draws no cursor; with it, one, lit again
// by each key.
func TestOnlyThePaneWithTheKeyboardDrawsACursor(t *testing.T) {
	_, tm := termStage(t, geom.Sz(900, 600))
	if !tm.cursorShown {
		t.Fatal("the pane with the keyboard shows no cursor")
	}
	tm.blinkOff = true
	run := tm.blinkRun
	lastWindow.Input(gi.TextInput{Text: "x"})
	frames(1)
	if tm.blinkOff || tm.blinkRun == run {
		t.Fatal("a key left the blink where it was")
	}
	lastUI.Focus(nil)
	frames(1)
	if tm.cursorShown {
		t.Fatal("the pane without the keyboard shows a cursor")
	}
}

// Ctrl going down over a still pointer is heard, as a move would be.
func TestCtrlOverAStillPointerIsHeard(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	box, _ := lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerMove{Pos: box.Center()})
	frames(1)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	if tm.hoverMods != input.ModCtrl {
		t.Fatalf("with Ctrl down, the hover's modifiers are %v", tm.hoverMods)
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl, Mods: gi.ModControl})
	if tm.hoverMods != 0 {
		t.Fatalf("with Ctrl up, the hover's modifiers are %v", tm.hoverMods)
	}
	// With the keyboard elsewhere, the pane under the pointer still
	// hears it.
	st := withServers(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	if err := lastWindow.Client().Publish(app.WindowTopic, st); err != nil {
		t.Fatal(err)
	}
	frames(60)
	box, _ = lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerMove{Pos: box.Center()})
	lastUI.Focus(win.serversRow(lastUI))
	frames(1)
	if tm.focused {
		t.Fatal("the terminal kept the keyboard")
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	if tm.hoverMods != input.ModCtrl {
		t.Fatalf("with the keyboard elsewhere and Ctrl down, the hover's modifiers are %v", tm.hoverMods)
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl, Mods: gi.ModControl})
	// Once the pointer has gone, Ctrl leaves the pane alone.
	lastWindow.Input(gi.PointerMove{Pos: geom.Pt(20, 300)})
	frames(1)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	if tm.over || tm.hoverMods != 0 {
		t.Fatalf("with the pointer gone, the pane is over %v with modifiers %v", tm.over, tm.hoverMods)
	}
}

// A click outside the palette closes it, and the keyboard goes where
// the click landed.
func TestAClickOutsideThePaletteClosesIt(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	win.run("palette.open", lastUI)
	frames(20)
	if !win.palette.IsOpen() {
		t.Fatal("the palette did not open")
	}
	box, _ := lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1})
	lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
	frames(20)
	if win.palette.IsOpen() {
		t.Fatal("a click on the terminal left the palette open")
	}
	if !tm.focused {
		t.Fatal("after the click, the terminal has no keyboard")
	}
}

// The theme picker closes on a click outside it, as the palette does.
func TestAClickOutsideTheThemePickerClosesIt(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	win.themes = []string{"one", "two"}
	win.pickTheme(lastUI)
	frames(20)
	if !win.themePicker.IsOpen() {
		t.Fatal("the theme picker did not open")
	}
	box, _ := lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1})
	lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
	frames(20)
	if win.themePicker.IsOpen() || !tm.focused {
		t.Fatalf("after a click on the terminal, the picker is open %v, the terminal has the keyboard %v", win.themePicker.IsOpen(), tm.focused)
	}
}

// On a Swedish keyboard the key marked + sits where a US one has -.
// Ctrl and that key makes the font bigger, as the key says.
func TestPunctuationIsTheKeyItTypes(t *testing.T) {
	for _, c := range []struct {
		press gi.KeyPress
		want  input.Key
	}{
		{gi.KeyPress{Key: gi.KeyMinus, Char: '+'}, input.KeyPlus},
		{gi.KeyPress{Key: gi.KeySlash, Char: '-'}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyMinus, Char: '-'}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyMinus}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyA, Char: 'a'}, input.KeyA},
	} {
		ev, ok := winkeys.Event(c.press)
		if !ok || ev.Key != c.want {
			t.Errorf("%+v reads as %v, %v; want %v", c.press, ev.Key, ok, c.want)
		}
	}
	ev, _ := winkeys.Event(gi.KeyPress{Key: gi.KeyMinus, Char: '+', Mods: gi.ModControl})
	if id, _ := Shortcuts().Lookup(ui.ChordOf(ev)); id != "font.increase" {
		t.Fatalf("Ctrl and the key marked + runs %q", id)
	}
}

// A pane too small for a usable screen still resizes the shell, once it
// has stayed so a moment.
func TestATinyPaneResizesItsShellOnceSettled(t *testing.T) {
	_, tm := termStage(t, geom.Sz(900, 75))
	// Until the sidebar has slid into place.
	frames(90)
	cols, rows := tm.cells.Fit()
	if cols >= leastCols && rows >= leastRows {
		t.Fatalf("the pane fits %dx%d, which is not small", cols, rows)
	}
	if size := tm.sh.T.Size(); size.Cols != cols || size.Rows != rows {
		t.Fatalf("settled, the shell is %dx%d, want %dx%d", size.Cols, size.Rows, cols, rows)
	}
}

// The pointer over a terminal is the I-beam, and a hand over a link
// while Ctrl is down.
func TestTheHandShowsOverALinkWithCtrl(t *testing.T) {
	win, sh, publish := windowStageOf(t, geom.Sz(900, 600))
	linked := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}, Link: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.NewPrinted([]byte("https://example.com/a")), vt.DefaultPalette(), linked))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	tm := win.terms["p1"]
	frames(30)
	box, _ := lastUI.Bounds(tm.cells)
	cell := tm.cells.CellSize()
	at := box.Min.Add(geom.Pt(cell.W*3, cell.H/2))
	lastWindow.Input(gi.PointerMove{Pos: at})
	frames(2)
	if got := lastWindow.Offscreen().Cursor(); got != gi.CursorText {
		t.Fatalf("over the text the pointer is %v, want the I-beam", got)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	frames(2)
	if got := lastWindow.Offscreen().Cursor(); got != gi.CursorHand {
		t.Fatalf("over a link with Ctrl down the pointer is %v, want the hand", got)
	}
}

// A press that another program took the release of, as a browser a
// Ctrl+click opened does, ends as the window loses the keyboard, and no
// link stays lit or named.
func TestLosingTheKeyboardEndsAPressAndTheLinksLight(t *testing.T) {
	_, tm := termStage(t, geom.Sz(900, 600))
	box, _ := lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerMove{Pos: box.Center()})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1, Mods: gi.ModControl})
	frames(1)
	if tm.held == input.MouseNone || tm.hoverMods == 0 {
		t.Fatalf("pressed with Ctrl, held %v, modifiers %v", tm.held, tm.hoverMods)
	}
	lastWindow.Input(gi.WindowFocusLost{})
	frames(1)
	if tm.held != input.MouseNone || tm.hoverMods != 0 || tm.sh.T.HoveredLink() != "" {
		t.Fatalf("away, held %v, modifiers %v, link %q", tm.held, tm.hoverMods, tm.sh.T.HoveredLink())
	}
}
