// Command gridterm is a proof of concept for a GPU-rendered character
// grid: the rendering and input foundation a native terminal or
// terminal-style IDE would sit on.
//
// It deliberately stops short of a terminal. There is no VT parser and
// no process here. What it shows is the part that has to be right first:
// glyphs batched through a texture atlas, damage-tracked repaints, and
// a key pipeline that can tell Ctrl+C from the letter c and turn it into
// the bytes you would write to a PTY or an SSH channel.
package main

import (
	"fmt"
	"image/color"
	"log"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"

	"github.com/marcus/gridterm/glyph"
	"github.com/marcus/gridterm/grid"
	"github.com/marcus/gridterm/input"
	"github.com/marcus/gridterm/input/ebitenin"
	"github.com/marcus/gridterm/render"
)

var (
	colBG     = color.RGBA{0x14, 0x17, 0x1c, 0xff}
	colFG     = color.RGBA{0xc8, 0xd0, 0xda, 0xff}
	colDim    = color.RGBA{0x6a, 0x74, 0x84, 0xff}
	colAccent = color.RGBA{0x7a, 0xc8, 0xff, 0xff}
	colGreen  = color.RGBA{0x8f, 0xd4, 0x6a, 0xff}
	colAmber  = color.RGBA{0xe6, 0xb4, 0x50, 0xff}
	colBar    = color.RGBA{0x22, 0x28, 0x32, 0xff}
)

const headerRows = 4

type app struct {
	atlas    *glyph.Atlas
	renderer *render.Renderer
	g        *grid.Grid
	reader   ebitenin.Reader

	lines  []line // scrollback of what happened
	stress bool
	tick   uint64
	encBuf []byte
}

type line struct {
	text  string
	color color.RGBA
}

func (a *app) Update() error {
	for _, ev := range a.reader.Poll() {
		a.handle(ev)
	}
	if a.stress {
		// Throughput check: push a fresh line every frame so the whole
		// body repaints and the batching numbers mean something.
		a.tick++
		a.log(fmt.Sprintf(
			"stress %06d  %s", a.tick,
			strings.Repeat("the quick brown fox jumps over the lazy dog ", 3)),
			colDim)
	}
	return nil
}

func (a *app) handle(ev input.Event) {
	switch {
	case ev.Kind == input.KeyPress && ev.Key == input.KeyF2:
		a.stress = !a.stress
		a.log(fmt.Sprintf("stress mode %v", a.stress), colAmber)
		return
	case ev.Kind == input.KeyPress && ev.Key == input.KeyF3:
		a.lines = a.lines[:0]
		a.g.MarkAllDirty()
		return
	}

	a.encBuf = input.Encode(ev, a.encBuf[:0])
	if a.encBuf == nil {
		return // release, or a shortcut that produces no input
	}

	var what string
	col := colFG
	switch ev.Kind {
	case input.Text:
		what = fmt.Sprintf("text   %-12q", ev.Rune)
		col = colGreen
	case input.KeyPress:
		what = fmt.Sprintf("press  %-12s", ev.Key)
		col = colAccent
	case input.KeyRepeat:
		what = fmt.Sprintf("repeat %-12s", ev.Key)
		col = colDim
	}
	a.log(fmt.Sprintf("%s mods=%-10s src=%-4d -> %s",
		what, ev.Mods, ev.Source, hex(a.encBuf)), col)
}

func (a *app) log(s string, c color.RGBA) {
	a.lines = append(a.lines, line{text: s, color: c})
	_, rows := a.g.Size()
	if keep := rows - headerRows - 1; keep > 0 && len(a.lines) > keep {
		a.lines = a.lines[len(a.lines)-keep:]
	}
}

func (a *app) Draw(screen *ebiten.Image) {
	a.paint()
	a.renderer.Draw(screen, a.g)
}

// paint writes the current state into the grid. It only touches cells
// whose contents changed; grid.Set drops writes that match what is
// already there, so a still screen leaves every row clean and Draw
// issues no work at all.
func (a *app) paint() {
	cols, rows := a.g.Size()
	st := a.renderer.Stats()
	cw, ch := a.renderer.CellSize()

	blank := strings.Repeat(" ", cols)
	row := func(y int, s string, fg, bg color.RGBA, attr grid.Attr) {
		a.g.SetString(0, y, blank, fg, bg, 0)
		a.g.SetString(0, y, s, fg, bg, attr)
	}

	row(0, "  gridterm — GPU character grid proof of concept",
		colBG, colAccent, grid.AttrBold)
	row(1, fmt.Sprintf(
		"  grid %dx%d  cell %dx%d px  atlas %d page(s), %d glyphs  fps %.0f",
		cols, rows, cw, ch, a.atlas.Pages(), a.atlas.Cached(), ebiten.ActualFPS()),
		colDim, colBG, 0)
	row(2, fmt.Sprintf(
		"  last frame: %d/%d rows repainted, %d quads, %d DrawTriangles calls",
		st.RowsDrawn, rows, st.Quads, st.DrawCalls),
		colDim, colBG, 0)
	row(3, "  type anything · F2 stress · F3 clear",
		colDim, colBar, 0)

	for i := 0; i < rows-headerRows-1; i++ {
		y := headerRows + i
		if i < len(a.lines) {
			row(y, "  "+a.lines[i].text, a.lines[i].color, colBG, 0)
		} else {
			row(y, "", colFG, colBG, 0)
		}
	}
	row(rows-1,
		"  keypress → VT bytes, ready to write to a PTY or an ssh.Session",
		colDim, colBar, 0)
}

// Layout satisfies ebiten.Game. LayoutF below takes precedence when the
// runtime supports it; this is the integer fallback.
func (a *app) Layout(w, h int) (int, int) {
	cols, rows := a.renderer.GridSizeFor(w, h)
	if c, r := a.g.Size(); c != cols || r != rows {
		a.g.Resize(cols, rows)
	}
	return w, h
}

// LayoutF sizes the grid to the window in device pixels, so the cell
// box lands on whole pixels on a HiDPI display instead of being scaled.
func (a *app) LayoutF(logicalW, logicalH float64) (float64, float64) {
	s := ebiten.Monitor().DeviceScaleFactor()
	pxW, pxH := int(logicalW*s), int(logicalH*s)
	cols, rows := a.renderer.GridSizeFor(pxW, pxH)
	if c, r := a.g.Size(); c != cols || r != rows {
		a.g.Resize(cols, rows)
	}
	return logicalW * s, logicalH * s
}

func main() {
	atlas, err := glyph.NewAtlas(gomono.TTF, gomonobold.TTF, 15, 96)
	if err != nil {
		log.Fatalf("build glyph atlas: %v", err)
	}
	m := atlas.Metrics()

	a := &app{atlas: atlas, renderer: render.New(atlas)}
	a.g = grid.New(80, 25, colFG, colBG)
	a.log("ready — every line below is a real input event", colAmber)

	ebiten.SetWindowTitle("gridterm")
	ebiten.SetWindowSize(m.CellW*100, m.CellH*32)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Damage tracking only pays off if ebiten keeps the previous frame.
	ebiten.SetScreenClearedEveryFrame(false)
	ebiten.SetVsyncEnabled(true)

	if err := ebiten.RunGame(a); err != nil {
		log.Fatal(err)
	}
}

func hex(b []byte) string {
	var sb strings.Builder
	for i, c := range b {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02x", c)
	}
	if printable(b) {
		fmt.Fprintf(&sb, "  %q", string(b))
	}
	return sb.String()
}

func printable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return len(b) > 0
}
