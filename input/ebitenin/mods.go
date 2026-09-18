package ebitenin

import (
	"sync"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/input"
)

// held is which modifier keys are down, kept from the key events the
// window reports.
//
// A mouse event carries no modifiers of its own, and asking the window
// which keys are down does not work: IsKeyPressed answers from a map
// that only the browser and mobile backends fill, so on a desktop it
// says nothing is held. Every shift-click and every alt-drag read as
// plain, and the Ctrl+Tab walk ended on the frame after it opened.
//
// So the keys are counted as they go by. The keyboard is read before the
// mouse in a frame, which is what makes this fresh by the time a click
// asks.
var held struct {
	sync.Mutex
	mods input.Mods
}

// Mods reads the modifier keys held now.
func Mods() input.Mods { return currentMods() }

// currentMods is the modifiers held as of the last key the window
// reported.
func currentMods() input.Mods {
	held.Lock()
	defer held.Unlock()
	return held.mods
}

// sawKey takes one key event into the modifiers held.
//
// A key that is itself a modifier is counted by what it did, because a
// release reports its mask differently from platform to platform and
// one that still claimed the key was down would stick. Every other key
// carries the window's own mask, which is the whole truth and corrects
// anything missed while the window was not being read.
func sawKey(k ebiten.Key, a ebiten.KeyAction, m ebiten.KeyModifier) {
	held.Lock()
	defer held.Unlock()
	if mod, is := modifierOf(k); is {
		if a == ebiten.KeyActionRelease {
			held.mods &^= mod
		} else {
			held.mods |= mod
		}
		return
	}
	held.mods = fromMods(m)
}

// forgetMods drops what is held, for a window that is not being typed
// into: a key let go of somewhere else is a key this window never sees
// come up, and it would stay down for ever.
func forgetMods() {
	held.Lock()
	defer held.Unlock()
	held.mods = 0
}

// modifierOf is the modifier a key stands for, and false for a key that
// is not one.
func modifierOf(k ebiten.Key) (input.Mods, bool) {
	switch k {
	case ebiten.KeyShiftLeft, ebiten.KeyShiftRight, ebiten.KeyShift:
		return input.ModShift, true
	case ebiten.KeyControlLeft, ebiten.KeyControlRight, ebiten.KeyControl:
		return input.ModCtrl, true
	case ebiten.KeyAltLeft, ebiten.KeyAltRight, ebiten.KeyAlt:
		return input.ModAlt, true
	case ebiten.KeyMetaLeft, ebiten.KeyMetaRight, ebiten.KeyMeta:
		return input.ModSuper, true
	}
	return 0, false
}
