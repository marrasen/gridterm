package ebitenin

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/input"
)

// onLayout says what each key produces, for a test that is about a
// keyboard other than the one it runs on.
func onLayout(t *testing.T, produces map[ebiten.Key]string) {
	t.Helper()
	was := keyName
	keyName = func(k ebiten.Key) string { return produces[k] }
	t.Cleanup(func() { keyName = was })
}

// swedish is where a Swedish keyboard puts the punctuation this window
// binds. Plus sits where a US keyboard has minus, and minus where a US
// keyboard has slash.
var swedish = map[ebiten.Key]string{
	ebiten.KeyMinus: "+",
	ebiten.KeySlash: "-",
	ebiten.KeyEqual: "´",
}

// us is the same keys on a US keyboard.
var us = map[ebiten.Key]string{
	ebiten.KeyEqual: "=",
	ebiten.KeyMinus: "-",
	ebiten.KeySlash: "/",
}

// The key marked plus is the plus key, wherever the layout puts it.
//
// It was read as minus on a Swedish keyboard, because that is where a
// US keyboard has minus, so ctrl and the key marked plus made the font
// smaller.
func TestTheKeyMarkedPlusIsPlus(t *testing.T) {
	onLayout(t, swedish)

	if got := fromKey(ebiten.KeyMinus); got != input.KeyPlus {
		t.Errorf("the key marked plus reads as %v, want plus", got)
	}
}

// And the key marked minus is minus, which on a Swedish keyboard is
// where a US one has slash. It reached nothing at all before.
func TestTheKeyMarkedMinusIsMinus(t *testing.T) {
	onLayout(t, swedish)

	if got := fromKey(ebiten.KeySlash); got != input.KeyMinus {
		t.Errorf("the key marked minus reads as %v, want minus", got)
	}
}

// A US keyboard is unchanged, which is the whole point of going by the
// character rather than by the place.
func TestAUSKeyboardIsUnchanged(t *testing.T) {
	onLayout(t, us)

	for _, tc := range []struct {
		key  ebiten.Key
		want input.Key
	}{
		{ebiten.KeyEqual, input.KeyEquals},
		{ebiten.KeyMinus, input.KeyMinus},
	} {
		if got := fromKey(tc.key); got != tc.want {
			t.Errorf("%v reads as %v, want %v", tc.key, got, tc.want)
		}
	}
}

// A window that cannot say what its keys produce falls back to where
// they sit, which is what happens before the main loop is running.
func TestAWindowThatCannotSayFallsBackToThePlace(t *testing.T) {
	onLayout(t, nil)

	if got := fromKey(ebiten.KeyEqual); got != input.KeyEquals {
		t.Errorf("with no layout to ask, = reads as %v, want equals", got)
	}
}

// A letter is a letter, and is not looked up as punctuation.
func TestALetterIsStillALetter(t *testing.T) {
	onLayout(t, map[ebiten.Key]string{ebiten.KeyA: "a"})

	if got := fromKey(ebiten.KeyA); got != input.KeyA {
		t.Errorf("A reads as %v, want A", got)
	}
}

// A key with a name of more than one character is not punctuation.
func TestAKeyWithALongNameIsNotPunctuation(t *testing.T) {
	onLayout(t, map[ebiten.Key]string{ebiten.KeyEnter: "Enter"})

	if got := fromKey(ebiten.KeyEnter); got != input.KeyEnter {
		t.Errorf("Enter reads as %v, want enter", got)
	}
}

// The whole way through: a frame in which the key marked plus is
// pressed on a Swedish keyboard reports the plus key, which is what the
// shortcut for a bigger font is bound to.
func TestAFrameOnASwedishKeyboardReportsPlus(t *testing.T) {
	fresh(t)
	onLayout(t, swedish)
	var r Reader

	out := r.take([]ebiten.InputEvent{{
		Kind: ebiten.InputEventKindKey, Key: ebiten.KeyMinus,
		Action: ebiten.KeyActionPress, Mods: ebiten.KeyModControl,
	}}, true)

	if len(out) != 1 {
		t.Fatalf("the frame gave %d events, want the one", len(out))
	}
	if out[0].Key != input.KeyPlus {
		t.Errorf("the key marked plus arrived as %v, want plus", out[0].Key)
	}
	if !out[0].Mods.Has(input.ModCtrl) {
		t.Errorf("it arrived with %v, want ctrl", out[0].Mods)
	}
}
