package ui

import (
	"image/color"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// eightTiles is a switcher's worth of tiles on a grid to draw them on.
func eightTiles() (*Tiles, *grid.Grid) {
	t := NewTiles([]string{"one", "two", "three", "four", "five", "six", "seven", "eight"})
	t.Style = TilesStyle{
		FG: color.RGBA{A: 0xff}, BG: color.RGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff},
		Border: color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
		Marked: color.RGBA{G: 0xff, A: 0xff},
	}
	t.Layout(Size{Cols: 140, Rows: 44})
	return t, grid.New(140, 44, color.RGBA{}, color.RGBA{})
}

// Drawing the tiles asks the heap for nothing.
//
// They draw through a buffer, which is a second grid the size of the
// view. Made once and kept: a frame that made one would be asking the
// collector for six thousand cells sixty times a second.
func TestDrawingTheTilesAsksTheHeapForNothing(t *testing.T) {
	tiles, g := eightTiles()
	// Once before the count, so the buffer is already there. What is
	// measured is a frame after the first.
	tiles.Draw(g.View())

	got := testing.AllocsPerRun(20, func() { tiles.Draw(g.View()) })

	if got != 0 {
		t.Errorf("a draw allocated %v times, want none", got)
	}
}

// What one settled draw of the tiles costs.
func BenchmarkTilesDraw(b *testing.B) {
	tiles, g := eightTiles()
	tiles.Draw(g.View())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tiles.Draw(g.View())
	}
}
