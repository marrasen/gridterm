package ebitenin

import (
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/input"
)

// MouseReader turns ebiten's polled mouse state into the press, release
// and motion events a terminal reports.
//
// ebiten exposes the mouse as state rather than events, so the edges
// have to be derived by comparing against the previous frame. Reuse one
// reader across frames or every frame looks like a fresh press.
type MouseReader struct {
	pressed  [3]bool
	lastCol  int
	lastRow  int
	haveLast bool
	out      []input.MouseEvent
}

var mouseButtons = [3]struct {
	eb ebiten.MouseButton
	in input.MouseButton
}{
	{ebiten.MouseButtonLeft, input.MouseLeft},
	{ebiten.MouseButtonMiddle, input.MouseMiddle},
	{ebiten.MouseButtonRight, input.MouseRight},
}

// Poll returns this frame's mouse events in grid coordinates. at says
// which cell a pixel falls in.
//
// The caller does the conversion rather than being asked for a cell
// size, because a grid is not quite a grid: a column or a row can have
// padding around it, and dividing by the cell size would put every
// click after the padding one cell out.
func (r *MouseReader) Poll(at func(px, py int) (col, row int)) []input.MouseEvent {
	r.out = r.out[:0]
	if at == nil {
		return r.out
	}
	px, py := ebiten.CursorPosition()
	col, row := at(px, py)
	mods := currentMods()

	// The wheel comes first: a program that scrolls on the wheel should
	// see the scroll before any click that lands in the same frame.
	if _, dy := ebiten.Wheel(); dy != 0 {
		b := input.MouseWheelDown
		if dy > 0 {
			b = input.MouseWheelUp
		}
		r.out = append(r.out, input.MouseEvent{
			Kind: input.MousePress, Button: b, Col: col, Row: row, Mods: mods,
		})
	}

	held := input.MouseNone
	for i, b := range mouseButtons {
		down := ebiten.IsMouseButtonPressed(b.eb)
		switch {
		case down && !r.pressed[i]:
			r.out = append(r.out, input.MouseEvent{
				Kind: input.MousePress, Button: b.in, Col: col, Row: row, Mods: mods,
			})
		case !down && r.pressed[i]:
			r.out = append(r.out, input.MouseEvent{
				Kind: input.MouseRelease, Button: b.in, Col: col, Row: row, Mods: mods,
			})
		}
		r.pressed[i] = down
		if down && held == input.MouseNone {
			held = b.in
		}
	}

	// Motion is only worth reporting when the pointer changed cell. A
	// program in any-event mode would otherwise get one report per frame
	// for a stationary pointer.
	if r.haveLast && (col != r.lastCol || row != r.lastRow) {
		r.out = append(r.out, input.MouseEvent{
			Kind: input.MouseMove, Button: held, Col: col, Row: row, Mods: mods,
		})
	}
	r.lastCol, r.lastRow, r.haveLast = col, row, true
	return r.out
}

// Mods reads the modifier keys held now.
//
// Polled rather than delivered as events. A mouse event carries no
// modifier mask of its own, and a window that wants to know whether a
// key is still down cannot wait for a release that may land in another
// window.
func Mods() input.Mods { return currentMods() }

// currentMods reads the modifier keys.
//
// It reads nothing on Windows. IsKeyPressed is deprecated in the ebiten
// this builds against and answers from a map only the browser and
// mobile backends fill, so every mouse event here carries no modifier
// at all. Written up in TODO.md under "The modifier keys are dead on
// the mouse"; the answer is the modifiers that AppendInputEvents
// already carries for the keyboard.
//
//nolint:staticcheck // deprecated, and the replacement is the fix above
func currentMods() input.Mods {
	var m input.Mods
	if ebiten.IsKeyPressed(ebiten.KeyShift) {
		m |= input.ModShift
	}
	if ebiten.IsKeyPressed(ebiten.KeyControl) {
		m |= input.ModCtrl
	}
	if ebiten.IsKeyPressed(ebiten.KeyAlt) {
		m |= input.ModAlt
	}
	if ebiten.IsKeyPressed(ebiten.KeyMeta) {
		m |= input.ModSuper
	}
	return m
}
