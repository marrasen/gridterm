package main

import (
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vt"
)

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
		ev, ok := keyEvent(c.press)
		if !ok || ev.Key != c.want {
			t.Errorf("%+v reads as %v, %v; want %v", c.press, ev.Key, ok, c.want)
		}
	}
	ev, _ := keyEvent(gi.KeyPress{Key: gi.KeyMinus, Char: '+', Mods: gi.ModControl})
	if id, _ := shortcuts().Lookup(ui.ChordOf(ev)); id != "font.increase" {
		t.Fatalf("Ctrl and the key marked + runs %q", id)
	}
}

// termStage puts one terminal pane on stage, with the keyboard, in a
// window of size.
func termStage(t *testing.T, size geom.Size) (*window, *term) {
	t.Helper()
	win, sh, publish := windowStageOf(t, size)
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	sh.set("p1", openShell(&typed{done: make(chan struct{})}, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.get("p1").t.Close() })
	publish(State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1"})
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
	_, tm := termStage(t, geom.Sz(900, 600))
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
	if size := tm.sh.t.Size(); size.Cols != cols || size.Rows != rows {
		t.Fatalf("settled, the shell is %dx%d, want %dx%d", size.Cols, size.Rows, cols, rows)
	}
}
