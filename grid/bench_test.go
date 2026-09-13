package grid

import (
	"image/color"
	"testing"
)

// A 200x60 grid is a large terminal on a 4K display: 12,000 cells.
const benchCols, benchRows = 200, 60

func benchGrid() *Grid { return New(benchCols, benchRows, fg, bg) }

// BenchmarkSetFullScreen measures repainting every cell with new
// content, the worst case for the CPU side of a frame.
func BenchmarkSetFullScreen(b *testing.B) {
	g := benchGrid()
	line := make([]rune, benchCols)
	for i := range line {
		line[i] = rune('a' + i%26)
	}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		for y := 0; y < benchRows; y++ {
			for x := 0; x < benchCols; x++ {
				g.Set(x, y, Cell{Rune: line[(x+i)%benchCols], FG: fg, BG: bg})
			}
		}
		g.ClearDirty()
	}
}

// BenchmarkSetUnchanged measures an idle screen: the same content
// written again. This should dirty nothing.
func BenchmarkSetUnchanged(b *testing.B) {
	g := benchGrid()
	c := Cell{Rune: 'x', FG: fg, BG: bg}
	for y := 0; y < benchRows; y++ {
		for x := 0; x < benchCols; x++ {
			g.Set(x, y, c)
		}
	}
	g.ClearDirty()

	b.ReportAllocs()
	for b.Loop() {
		for y := 0; y < benchRows; y++ {
			for x := 0; x < benchCols; x++ {
				g.Set(x, y, c)
			}
		}
		if g.AnyDirty() {
			b.Fatal("idle repaint dirtied the grid")
		}
	}
}

func BenchmarkScrollUp(b *testing.B) {
	g := benchGrid()
	b.ReportAllocs()
	for b.Loop() {
		g.ScrollUp(1)
	}
}

func BenchmarkBGRunsUniform(b *testing.B) {
	g := benchGrid()
	b.ReportAllocs()
	for b.Loop() {
		for y := 0; y < benchRows; y++ {
			g.BGRuns(y, func(int, int, color.RGBA) {})
		}
	}
}
