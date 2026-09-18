package main

import (
	"testing"
)

// What one pass of the widget tree costs when nothing in it changed,
// which is what drawing the panes behind the switcher costs.
func BenchmarkIdleTreeDraw(b *testing.B) {
	t := &testing.T{}
	a := aWindowOfPanes(t, 8)
	frame(t, a)
	frame(t, a)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.root.Draw(a.g.View())
	}
}

// And what one whole frame costs beside it, so the share is clear.
func BenchmarkIdleFrame(b *testing.B) {
	t := &testing.T{}
	a := aWindowOfPanes(t, 8)
	frame(t, a)
	frame(t, a)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.Update(); err != nil {
			b.Fatal(err)
		}
		a.Draw(a.screen)
	}
}

// And the same with the switcher open, which is the case in question.
func BenchmarkSwitcherFrame(b *testing.B) {
	t := &testing.T{}
	a := aWindowOfPanes(t, 8)
	frame(t, a)
	openTiles(t, a)
	frame(t, a)
	frame(t, a)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.Update(); err != nil {
			b.Fatal(err)
		}
		a.Draw(a.screen)
	}
}
