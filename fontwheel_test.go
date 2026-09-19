package main

import (
	"path/filepath"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
)

// wheel is a turn of the wheel over the middle of the window.
func wheel(b input.MouseButton, mods input.Mods) input.MouseEvent {
	return input.MouseEvent{Kind: input.MousePress, Button: b, Col: 5, Row: 5, Mods: mods}
}

// Ctrl and the wheel change the font size, which is what every other
// window does and what a mouse with modifiers makes possible.
func TestCtrlAndTheWheelChangesTheFontSize(t *testing.T) {
	a := newTestApp(t, 80, 24)
	was := a.fontSize

	took, err := a.zoomedFont(wheel(input.MouseWheelUp, input.ModCtrl))
	if err != nil {
		t.Fatalf("bigger: %v", err)
	}

	if !took {
		t.Fatal("the wheel was left for the pane to scroll")
	}
	if a.fontSize <= was {
		t.Errorf("the font is %v, want bigger than the %v it was", a.fontSize, was)
	}
}

// And down makes it smaller.
func TestCtrlAndTheWheelDownMakesItSmaller(t *testing.T) {
	a := newTestApp(t, 80, 24)
	was := a.fontSize

	if _, err := a.zoomedFont(wheel(input.MouseWheelDown, input.ModCtrl)); err != nil {
		t.Fatalf("smaller: %v", err)
	}

	if a.fontSize >= was {
		t.Errorf("the font is %v, want smaller than the %v it was", a.fontSize, was)
	}
}

// The wheel on its own is the pane's, to scroll with.
func TestTheWheelWithoutCtrlIsLeftForThePane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	was := a.fontSize

	took, err := a.zoomedFont(wheel(input.MouseWheelUp, 0))
	if err != nil {
		t.Fatalf("the wheel: %v", err)
	}

	if took {
		t.Error("the window took a plain turn of the wheel")
	}
	if a.fontSize != was {
		t.Errorf("the font changed to %v without ctrl", a.fontSize)
	}
}

// And ctrl with a button is not the wheel.
func TestCtrlAndAClickIsNotAZoom(t *testing.T) {
	a := newTestApp(t, 80, 24)
	was := a.fontSize

	took, err := a.zoomedFont(wheel(input.MouseLeft, input.ModCtrl))
	if err != nil {
		t.Fatalf("the click: %v", err)
	}

	if took {
		t.Error("the window took a click as a zoom")
	}
	if a.fontSize != was {
		t.Errorf("the font changed to %v on a click", a.fontSize)
	}
}

// A frame's mouse events reach the zoom, which is what makes the wheel
// change the font rather than only the function being able to.
func TestAFramesWheelReachesTheZoom(t *testing.T) {
	a := newTestApp(t, 80, 24)
	was := a.fontSize

	a.handleMouse([]input.MouseEvent{wheel(input.MouseWheelUp, input.ModCtrl)})

	if a.fontSize <= was {
		t.Errorf("the font is %v, want bigger than the %v it was", a.fontSize, was)
	}
}

// And a frame's ordinary click still reaches the tree.
func TestAFramesClickStillReachesTheTree(t *testing.T) {
	a := newTestApp(t, 80, 24)
	was := a.fontSize

	a.handleMouse([]input.MouseEvent{wheel(input.MouseWheelUp, 0)})

	if a.fontSize != was {
		t.Errorf("a plain wheel changed the font to %v", a.fontSize)
	}
}

// The font size is reachable on a keyboard with a plus key of its own,
// as well as on one where plus is shift and equals.
//
// A Swedish keyboard has plus where a US one has minus, so without the
// plus binding the only way to a bigger font was a key that layout does
// not have.
func TestTheFontSizeIsBoundForEveryShapeOfKeyboard(t *testing.T) {
	a := aRealWindow(t)

	for _, tc := range []struct {
		chord ui.Chord
		want  string
	}{
		{ui.Chord{Key: input.KeyPlus, Mods: input.ModCtrl}, "font.increase"},
		{ui.Chord{Key: input.KeyEquals, Mods: input.ModCtrl}, "font.increase"},
		{ui.Chord{Key: input.KeyMinus, Mods: input.ModCtrl}, "font.decrease"},
	} {
		got, on := a.root.Accelerators.Lookup(tc.chord)
		if !on || got != tc.want {
			t.Errorf("%s runs %q (%v), want %q", tc.chord, got, on, tc.want)
		}
	}
}

// The size the text is drawn at survives a restart.
//
// Marcus set the size he wanted and gridterm opened at the default every
// time: nothing ever wrote it down.
func TestTheFontSizeIsRememberedBetweenRuns(t *testing.T) {
	a := newTestApp(t, 80, 24)
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
	}
	was := a.fontSize

	if err := a.setFontSize(was + fontStep); err != nil {
		t.Fatalf("make it bigger: %v", err)
	}

	// Written down, and read back by a window opening again.
	again, err := settings.Load(path)
	if err != nil {
		t.Fatalf("load the settings again: %v", err)
	}
	got, saved := again.FontSize()
	if !saved {
		t.Fatal("the size was not written down")
	}
	if got != was+fontStep {
		t.Errorf("it remembered %v, want the %v it was set to", got, was+fontStep)
	}

	next := newTestApp(t, 80, 24)
	next.font.remember(again)
	next.useStartFontSize()

	if next.fontSize != was+fontStep {
		t.Errorf("a window opening again is at %v, want the %v it was left at",
			next.fontSize, was+fontStep)
	}
}

// A size named on the command line is the size for that run, whatever
// the last run ended on.
func TestASizeOnTheCommandLineBeatsTheOneRemembered(t *testing.T) {
	a := newTestApp(t, 80, 24)
	path := filepath.Join(t.TempDir(), "settings.json")
	set, err := withSettings(t, a, path)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := set.PutFontSize(defaultFontSize + 4*fontStep); err != nil {
		t.Fatalf("write one down: %v", err)
	}

	next := newTestApp(t, 80, 24)
	next.font = newFontPick(true)
	next.font.remember(set)
	was := next.fontSize
	next.useStartFontSize()

	if next.fontSize != was {
		t.Errorf("the window opened at %v, want the %v the command line asked for",
			next.fontSize, was)
	}
}

// A window with no settings behind it keeps working: changing the size
// is not a failure just because there is nowhere to write it.
func TestTheFontSizeChangesWithNowhereToKeepIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	was := a.fontSize

	if err := a.setFontSize(was + fontStep); err != nil {
		t.Fatalf("make it bigger: %v", err)
	}
	if a.fontSize == was {
		t.Error("the size did not change")
	}
}
