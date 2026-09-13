// Package ebitenin adapts ebitengine's input events to the toolkit-free
// types in package input.
//
// It is the only place in the project that knows about ebiten's input
// API. Swapping the window toolkit means rewriting this file and nothing
// else. Keeping it separate also means the encoding rules in package
// input can be tested on a machine with no GPU and no X11 headers.
package ebitenin

import (
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marcus/gridterm/input"
)

// Reader collects a frame's input events from the window. Reuse one
// across frames so polling does not allocate.
type Reader struct {
	raw []ebiten.InputEvent
	out []input.Event
}

// Poll returns the events since the previous call. The returned slice is
// only valid until the next Poll.
func (r *Reader) Poll() []input.Event {
	r.raw = ebiten.AppendInputEvents(r.raw[:0])
	r.out = r.out[:0]
	for _, ev := range r.raw {
		switch ev.Kind {
		case ebiten.InputEventKindKey:
			r.out = append(r.out, input.Event{
				Kind:   actionKind(ev.Action),
				Key:    fromKey(ev.Key),
				Mods:   fromMods(ev.Mods),
				Source: input.Source(ev.Source),
			})
		case ebiten.InputEventKindText:
			r.out = append(r.out, input.Event{
				Kind:       input.Text,
				Rune:       ev.Rune,
				Mods:       fromMods(ev.Mods),
				Source:     input.Source(ev.Source),
				NormalText: ev.NormalText,
			})
		}
	}
	return r.out
}

func actionKind(a ebiten.KeyAction) input.Kind {
	switch a {
	case ebiten.KeyActionPress:
		return input.KeyPress
	case ebiten.KeyActionRepeat:
		return input.KeyRepeat
	default:
		return input.KeyRelease
	}
}

func fromMods(m ebiten.KeyModifier) input.Mods {
	var out input.Mods
	if m&ebiten.KeyModShift != 0 {
		out |= input.ModShift
	}
	if m&ebiten.KeyModAlt != 0 {
		out |= input.ModAlt
	}
	if m&ebiten.KeyModControl != 0 {
		out |= input.ModCtrl
	}
	if m&ebiten.KeyModSuper != 0 {
		out |= input.ModSuper
	}
	return out
}

// fromKey maps only the keys package input encodes specially. Anything
// else returns KeyNone and reaches the application as a Text event.
func fromKey(k ebiten.Key) input.Key {
	if k >= ebiten.KeyA && k <= ebiten.KeyZ {
		return input.KeyA + input.Key(k-ebiten.KeyA)
	}
	if v, ok := keys[k]; ok {
		return v
	}
	return input.KeyNone
}

var keys = map[ebiten.Key]input.Key{
	ebiten.KeyEnter:          input.KeyEnter,
	ebiten.KeyNumpadEnter:    input.KeyEnter,
	ebiten.KeyTab:            input.KeyTab,
	ebiten.KeyBackspace:      input.KeyBackspace,
	ebiten.KeyEscape:         input.KeyEscape,
	ebiten.KeySpace:          input.KeySpace,
	ebiten.KeyBracketLeft:    input.KeyBracketLeft,
	ebiten.KeyBracketRight:   input.KeyBracketRight,
	ebiten.KeyBackslash:      input.KeyBackslash,
	ebiten.KeyEqual:          input.KeyEquals,
	ebiten.KeyNumpadAdd:      input.KeyEquals,
	ebiten.KeyMinus:          input.KeyMinus,
	ebiten.KeyNumpadSubtract: input.KeyMinus,
	ebiten.KeyDigit0:         input.Key0,
	ebiten.KeyArrowUp:        input.KeyUp,
	ebiten.KeyArrowDown:      input.KeyDown,
	ebiten.KeyArrowRight:     input.KeyRight,
	ebiten.KeyArrowLeft:      input.KeyLeft,
	ebiten.KeyHome:           input.KeyHome,
	ebiten.KeyEnd:            input.KeyEnd,
	ebiten.KeyInsert:         input.KeyInsert,
	ebiten.KeyDelete:         input.KeyDelete,
	ebiten.KeyPageUp:         input.KeyPageUp,
	ebiten.KeyPageDown:       input.KeyPageDown,
	ebiten.KeyF1:             input.KeyF1,
	ebiten.KeyF2:             input.KeyF2,
	ebiten.KeyF3:             input.KeyF3,
	ebiten.KeyF4:             input.KeyF4,
	ebiten.KeyF5:             input.KeyF5,
	ebiten.KeyF6:             input.KeyF6,
	ebiten.KeyF7:             input.KeyF7,
	ebiten.KeyF8:             input.KeyF8,
	ebiten.KeyF9:             input.KeyF9,
	ebiten.KeyF10:            input.KeyF10,
	ebiten.KeyF11:            input.KeyF11,
	ebiten.KeyF12:            input.KeyF12,
}
