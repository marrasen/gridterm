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
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// exitQueue is how many "a shell has gone" notices are held before the
// drawing goroutine collects them. One per pane is plenty; the channel
// only has to avoid blocking the goroutine reporting it.
const exitQueue = 64

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

	// modals is the dialog stack, each with a layer of its own above the
	// tree so that closing one costs a blit rather than a repaint of
	// everything underneath.
	modals []*modal

	// palette is the command dialog while it is open, and dismissPalette
	// is what takes it away.
	palette        *ui.Palette
	dismissPalette func()

	// bar is the row of menu titles at the top of the window.
	bar *ui.Menubar

	// panes is every live terminal, so a shell that exits can be found
	// wherever it sits in the tree.
	panes map[*term.Terminal]struct{}

	// exits carries "a shell has gone" from the goroutines reading them
	// to the drawing goroutine, which is the only one that may touch the
	// widget tree.
	exits chan struct{}

	// newSession starts the shell a new pane runs. It is a field so a
	// test can drive the tree without spawning anything.
	newSession func(cols, rows int) (session.Session, error)

	// What a new pane is started with, kept from the flags.
	scrollback int
	colours    vt.Palette
	clip       clipboardWriter

	// fontSize is the current size in points.
	fontSize float64

	// lastPixels is the window size in device pixels, kept so a font
	// size change can re-derive the grid size from it.
	lastPixels [2]int
	lastSize   [2]int

	// title is what the window is currently called, so it is only set
	// again when it changes.
	title string

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

	a.reapExited()

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

	a.updateTitle()
	return nil
}

// updateTitle shows the focused pane's title. Read rather than pushed:
// a build running in a pane you are not looking at should not rename the
// window.
func (a *app) updateTitle() {
	title := ""
	if t := a.focusedTerminal(); t != nil {
		title = t.Title()
	}
	if title == a.title {
		return
	}
	a.title = title
	if title == "" {
		ebiten.SetWindowTitle("gridterm")
		return
	}
	ebiten.SetWindowTitle("gridterm — " + title)
}

func (a *app) Draw(screen *ebiten.Image) {
	a.root.Draw(a.g.View())
	a.drawModals()
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
	a.setGridSize(cols, rows)
}

// setGridSize tells the grids and the widget tree about a new size in
// cells. Every layer is resized, not just the tree's: a dialog on its
// own layer has to follow the window or it is measured for the old one.
func (a *app) setGridSize(cols, rows int) {
	if a.lastSize == [2]int{cols, rows} {
		return
	}
	a.lastSize = [2]int{cols, rows}

	a.g.Resize(cols, rows)
	a.resizeModals(cols, rows)
	a.root.Layout(ui.Rect{Cols: cols, Rows: rows})
	a.markDirty()
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

// logError reports a failure a pane could not return: the goroutines
// moving bytes have nowhere to hand one back to.
func (a *app) logError(err error) { log.Print(err) }

// onFocused wraps a command that acts on the focused pane, doing nothing
// when the focus is somewhere that is not a terminal.
func (a *app) onFocused(fn func(*term.Terminal) error) func() error {
	return func() error {
		t := a.focusedTerminal()
		if t == nil {
			return nil
		}
		return fn(t)
	}
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
		ui.Command{ID: "edit.copy", Title: "Copy", Run: a.onFocused(
			func(t *term.Terminal) error { t.Copy(); return nil })},
		ui.Command{ID: "edit.paste", Title: "Paste", Run: a.onFocused(
			func(t *term.Terminal) error { t.PasteClipboard(); return nil })},
		ui.Command{ID: "view.scrollUp", Title: "Scroll back", Run: a.onFocused(
			func(t *term.Terminal) error { t.ScrollPages(1); return nil })},
		ui.Command{ID: "view.scrollDown", Title: "Scroll forward", Run: a.onFocused(
			func(t *term.Terminal) error { t.ScrollPages(-1); return nil })},
		ui.Command{ID: "pane.splitRight", Title: "Split right", Run: func() error {
			return a.splitFocused(ui.Columns)
		}},
		ui.Command{ID: "pane.splitDown", Title: "Split down", Run: func() error {
			return a.splitFocused(ui.Rows)
		}},
		ui.Command{ID: "pane.close", Title: "Close pane", Run: a.closeFocused},
		ui.Command{ID: "tab.open", Title: "New tab", Run: a.openTab},
		ui.Command{ID: "palette.open", Title: "Show all commands", Run: a.openPalette},
		ui.Command{ID: "menu.open", Title: "Show the menu bar", Run: a.openMenu},
		ui.Command{ID: "tab.next", Title: "Next tab", Run: func() error {
			return a.focusTab(1)
		}},
		ui.Command{ID: "tab.previous", Title: "Previous tab", Run: func() error {
			return a.focusTab(-1)
		}},
		ui.Command{ID: "pane.next", Title: "Next pane", Run: func() error {
			return a.focusPane(1)
		}},
		ui.Command{ID: "pane.previous", Title: "Previous pane", Run: func() error {
			return a.focusPane(-1)
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
		{Key: input.KeyD, Mods: input.ModCtrl | input.ModShift}:      "pane.splitRight",
		{Key: input.KeyE, Mods: input.ModCtrl | input.ModShift}:      "pane.splitDown",
		{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}:      "pane.close",
		{Key: input.KeyTab, Mods: input.ModCtrl}:                     "pane.next",
		{Key: input.KeyTab, Mods: input.ModCtrl | input.ModShift}:    "pane.previous",
		{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}:      "tab.open",
		{Key: input.KeyPageDown, Mods: input.ModCtrl}:                "tab.next",
		{Key: input.KeyPageUp, Mods: input.ModCtrl}:                  "tab.previous",
		{Key: input.KeyK, Mods: input.ModCtrl}:                       "palette.open",
		{Key: input.KeyF10}:                                          "menu.open",
	})

	a.root.Commands = cmds
	// These are accelerators rather than ordinary bindings because the
	// terminal has a meaning for every key and would swallow them.
	a.root.Accelerators = keys
}
