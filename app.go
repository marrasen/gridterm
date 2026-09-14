package main

import (
	"log"
	"sync/atomic"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/input/ebitenin"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// Font size limits and the step the zoom commands move by.
const (
	defaultFontSize = 15
	minFontSize     = 6
	maxFontSize     = 72
	fontStep        = 1
)

// app is the window: it owns the atlas, the compositor and the widget
// tree, and turns ebiten's callbacks into toolkit ones. Everything a
// terminal does lives in the widget, not here.
type app struct {
	atlas    *glyph.Atlas
	renderer *render.Renderer
	comp     *render.Compositor
	root     ui.Root

	// g is the layer the widget tree draws on. Splits and tabs divide it
	// up; a dialog gets a layer of its own above it.
	g     *grid.Grid
	layer *render.Layer

	reader ebitenin.Reader
	mouse  ebitenin.MouseReader

	// t is the terminal the window was opened for. It is also the tree's
	// only widget until splits arrive.
	t *term.Terminal

	// fontSize is the current size in points.
	fontSize float64

	// lastPixels is the window size in device pixels, kept so a font
	// size change can re-derive the grid size from it.
	lastPixels [2]int
	lastSize   [2]int

	// title carries a title set by the program back to the drawing
	// goroutine, which is the only one allowed to touch the window.
	title atomic.Pointer[string]

	// quit is set once the shell is gone; the window closes on the next
	// frame.
	quit atomic.Bool

	// drewFinal records that the frame after the shell exited has been
	// drawn. ebiten returns from Update before Draw, so terminating the
	// moment the shell goes would discard its last output.
	drewFinal bool
}

func (a *app) Update() error {
	if a.quit.Load() {
		// Give Draw one more frame to paint what the shell wrote last.
		if a.drewFinal {
			return ebiten.Termination
		}
		a.drewFinal = true
		return nil
	}

	for _, ev := range a.reader.Poll() {
		if _, err := a.root.HandleKey(ev); err != nil {
			log.Printf("key %s: %v", ui.ChordOf(ev), err)
		}
	}
	cw, ch := a.renderer.CellSize()
	for _, ev := range a.mouse.Poll(cw, ch) {
		if _, err := a.root.HandleMouse(ev); err != nil {
			log.Printf("mouse: %v", err)
		}
	}

	if t := a.title.Swap(nil); t != nil {
		ebiten.SetWindowTitle("gridterm — " + *t)
	}
	return nil
}

func (a *app) Draw(screen *ebiten.Image) {
	a.root.Draw(a.g.View())
	a.comp.Draw(screen)
}

// Layout satisfies ebiten.Game; LayoutF below takes precedence when the
// runtime supports it.
func (a *app) Layout(w, h int) (int, int) {
	a.resizeTo(w, h)
	return w, h
}

func (a *app) LayoutF(logicalW, logicalH float64) (float64, float64) {
	s := ebiten.Monitor().DeviceScaleFactor()
	a.resizeTo(int(logicalW*s), int(logicalH*s))
	return logicalW * s, logicalH * s
}

// resizeTo tells the grid and the widget tree about a new window size.
func (a *app) resizeTo(pxW, pxH int) {
	a.lastPixels = [2]int{pxW, pxH}
	cols, rows := a.renderer.GridSizeFor(pxW, pxH)
	if a.lastSize == [2]int{cols, rows} {
		return
	}
	a.lastSize = [2]int{cols, rows}

	a.g.Resize(cols, rows)
	a.root.Layout(ui.Rect{Cols: cols, Rows: rows})
}

// setFontSize rebuilds the atlas and re-derives the grid size, because
// changing the font changes how many cells fit in the window.
func (a *app) setFontSize(pt float64) error {
	pt = min(max(pt, minFontSize), maxFontSize)
	if pt == a.fontSize {
		return nil
	}
	if err := a.atlas.SetSize(pt); err != nil {
		return err
	}
	a.fontSize = pt
	// The cell box changed, so the cached size is meaningless and every
	// glyph quad has to be re-measured.
	a.lastSize = [2]int{0, 0}
	a.resizeTo(a.lastPixels[0], a.lastPixels[1])
	a.g.MarkAllDirty()
	return nil
}

// commands registers everything the window can do and binds the default
// keys to it. Accelerators are the ones the terminal must not swallow.
func (a *app) commands() {
	cmds := ui.NewCommands()
	cmds.MustRegister(
		ui.Command{ID: "font.increase", Title: "Increase font size", Run: func() error {
			return a.setFontSize(a.fontSize + fontStep)
		}},
		ui.Command{ID: "font.decrease", Title: "Decrease font size", Run: func() error {
			return a.setFontSize(a.fontSize - fontStep)
		}},
		ui.Command{ID: "font.reset", Title: "Reset font size", Run: func() error {
			return a.setFontSize(defaultFontSize)
		}},
		ui.Command{ID: "edit.copy", Title: "Copy", Run: func() error {
			a.t.Copy()
			return nil
		}},
		ui.Command{ID: "edit.paste", Title: "Paste", Run: func() error {
			a.t.PasteClipboard()
			return nil
		}},
		ui.Command{ID: "view.scrollUp", Title: "Scroll back", Run: func() error {
			a.t.ScrollPages(1)
			return nil
		}},
		ui.Command{ID: "view.scrollDown", Title: "Scroll forward", Run: func() error {
			a.t.ScrollPages(-1)
			return nil
		}},
	)

	// Ctrl+Shift is the usual escape hatch: Ctrl+C has to stay available
	// to the program, so copy cannot live there.
	keys := ui.NewKeymap()
	keys.MustBind(map[ui.Chord]string{
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}: "edit.copy",
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModShift}: "edit.paste",
		{Key: input.KeyEquals, Mods: input.ModCtrl}:             "font.increase",
		// Ctrl+plus is Ctrl+Shift+= on a US layout, and the shift shows
		// up in the modifiers, so the obvious way to ask for a bigger
		// font needs its own binding.
		{Key: input.KeyEquals, Mods: input.ModCtrl | input.ModShift}: "font.increase",
		{Key: input.KeyMinus, Mods: input.ModCtrl}:                   "font.decrease",
		{Key: input.Key0, Mods: input.ModCtrl}:                       "font.reset",
		{Key: input.KeyPageUp, Mods: input.ModShift}:                 "view.scrollUp",
		{Key: input.KeyPageDown, Mods: input.ModShift}:               "view.scrollDown",
	})

	a.root.Commands = cmds
	// These are accelerators rather than ordinary bindings because the
	// terminal has a meaning for every key and would swallow them.
	a.root.Accelerators = keys
}
