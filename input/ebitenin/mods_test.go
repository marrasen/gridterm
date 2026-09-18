package ebitenin

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/input"
)

// fresh starts each test from nothing held, since the state is the
// package's.
func fresh(t *testing.T) {
	t.Helper()
	forgetMods()
	t.Cleanup(forgetMods)
}

// Holding shift is read by whatever asks next, which is what a click
// asks. Nothing did before: the window was asked which keys were down
// and answered that none were, so every shift-click read as plain.
func TestAModifierHeldIsReadByWhateverAsksNext(t *testing.T) {
	fresh(t)

	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionPress, ebiten.KeyModShift)

	if got := Mods(); got != input.ModShift {
		t.Errorf("the modifiers are %v, want shift", got)
	}
}

// Letting it go clears it.
//
// Counted by what the key did rather than by the mask the release
// carries: platforms disagree about whether that mask still holds the
// key being let go of, and one that did would leave shift down for
// ever.
func TestLettingAModifierGoClearsIt(t *testing.T) {
	fresh(t)
	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionPress, ebiten.KeyModShift)

	// The mask still claims shift, the way a platform reporting the
	// state before the transition would.
	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionRelease, ebiten.KeyModShift)

	if got := Mods(); got != 0 {
		t.Errorf("the modifiers are %v, want nothing", got)
	}
}

// Each modifier is its own, and either side of the keyboard is the same
// modifier.
func TestEachModifierIsHeldOnItsOwn(t *testing.T) {
	for _, tc := range []struct {
		key  ebiten.Key
		want input.Mods
	}{
		{ebiten.KeyShiftLeft, input.ModShift},
		{ebiten.KeyShiftRight, input.ModShift},
		{ebiten.KeyControlLeft, input.ModCtrl},
		{ebiten.KeyControlRight, input.ModCtrl},
		{ebiten.KeyAltLeft, input.ModAlt},
		{ebiten.KeyAltRight, input.ModAlt},
		{ebiten.KeyMetaLeft, input.ModSuper},
		{ebiten.KeyMetaRight, input.ModSuper},
	} {
		t.Run(tc.want.String(), func(t *testing.T) {
			fresh(t)

			sawKey(tc.key, ebiten.KeyActionPress, 0)

			if got := Mods(); got != tc.want {
				t.Errorf("holding that key reads as %v, want %v", got, tc.want)
			}
		})
	}
}

// Two at once are both held, and letting one go leaves the other.
func TestTwoModifiersAtOnce(t *testing.T) {
	fresh(t)

	sawKey(ebiten.KeyControlLeft, ebiten.KeyActionPress, 0)
	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionPress, 0)
	if got, want := Mods(), input.ModCtrl|input.ModShift; got != want {
		t.Fatalf("the modifiers are %v, want %v", got, want)
	}

	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionRelease, 0)

	if got := Mods(); got != input.ModCtrl {
		t.Errorf("after letting shift go the modifiers are %v, want ctrl", got)
	}
}

// An ordinary key carries the window's own mask, which is the whole
// truth: it puts right anything held before this window was read.
func TestAnOrdinaryKeyTakesTheWindowsOwnMask(t *testing.T) {
	fresh(t)

	sawKey(ebiten.KeyA, ebiten.KeyActionPress, ebiten.KeyModControl|ebiten.KeyModAlt)

	if got, want := Mods(), input.ModCtrl|input.ModAlt; got != want {
		t.Errorf("the modifiers are %v, want %v", got, want)
	}
}

// And an ordinary key with nothing held clears what was there, so a
// modifier this window never saw let go of does not stick.
func TestAnOrdinaryKeyWithNothingHeldClearsThem(t *testing.T) {
	fresh(t)
	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionPress, ebiten.KeyModShift)

	sawKey(ebiten.KeyA, ebiten.KeyActionPress, 0)

	if got := Mods(); got != 0 {
		t.Errorf("the modifiers are %v, want nothing", got)
	}
}

// A repeat keeps it held. The OS repeats a key held down, and a repeat
// read as anything but held would drop the modifier mid-press.
func TestARepeatKeepsAModifierHeld(t *testing.T) {
	fresh(t)
	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionPress, ebiten.KeyModShift)

	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionRepeat, ebiten.KeyModShift)

	if got := Mods(); got != input.ModShift {
		t.Errorf("the modifiers are %v, want shift", got)
	}
}

// A window that is not being typed into holds nothing, because a key
// let go of somewhere else is one it never sees come up.
func TestAWindowNobodyIsTypingIntoHoldsNothing(t *testing.T) {
	fresh(t)
	sawKey(ebiten.KeyShiftLeft, ebiten.KeyActionPress, ebiten.KeyModShift)

	forgetMods()

	if got := Mods(); got != 0 {
		t.Errorf("the modifiers are %v, want nothing", got)
	}
}

// Reading a frame's keys is what puts the modifiers where the mouse can
// see them. Without this the counting is there and nothing calls it.
func TestReadingAFrameCountsTheModifiers(t *testing.T) {
	fresh(t)
	var r Reader

	r.take([]ebiten.InputEvent{{
		Kind: ebiten.InputEventKindKey, Key: ebiten.KeyShiftLeft,
		Action: ebiten.KeyActionPress, Mods: ebiten.KeyModShift,
	}}, true)

	if got := Mods(); got != input.ModShift {
		t.Errorf("after a frame holding shift the modifiers are %v, want shift", got)
	}
}

// And a frame read while nobody is typing into the window holds nothing.
func TestReadingAFrameUnfocusedHoldsNothing(t *testing.T) {
	fresh(t)
	var r Reader
	r.take([]ebiten.InputEvent{{
		Kind: ebiten.InputEventKindKey, Key: ebiten.KeyShiftLeft,
		Action: ebiten.KeyActionPress, Mods: ebiten.KeyModShift,
	}}, true)

	r.take(nil, false)

	if got := Mods(); got != 0 {
		t.Errorf("the modifiers are %v, want nothing", got)
	}
}

// The events still come out, which is what the window types from.
func TestReadingAFrameStillGivesTheEvents(t *testing.T) {
	fresh(t)
	var r Reader

	out := r.take([]ebiten.InputEvent{{
		Kind: ebiten.InputEventKindKey, Key: ebiten.KeyA,
		Action: ebiten.KeyActionPress, Mods: ebiten.KeyModControl,
	}}, true)

	if len(out) != 1 {
		t.Fatalf("it gave %d events, want the one", len(out))
	}
	if out[0].Key != input.KeyA || out[0].Mods != input.ModCtrl {
		t.Errorf("the event is %v with %v, want A with ctrl", out[0].Key, out[0].Mods)
	}
}
