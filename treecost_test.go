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

// And a frame with a dialog over the panes, which is where the shadow
// and the frosted panel behind it are drawn.
func BenchmarkDialogFrame(b *testing.B) {
	t := &testing.T{}
	// aWindowOfPanes already gives the window its dialogs.
	a := aWindowOfPanes(t, 8)
	frame(t, a)
	a.showNotice("Could not reach margit", "The connection was refused.", false)
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

// A frame where nothing moved allocates nothing, with a dialog up and
// without one.
//
// A window sitting still is the case the whole display is built for, and
// an allocation a frame is rubbish for the collector to clear at sixty
// frames a second for as long as the window is open.
func TestAFrameThatChangedNothingAllocatesNothing(t *testing.T) {
	for what, withNotice := range map[string]bool{
		"idle":          false,
		"with a dialog": true,
	} {
		a := aWindowOfPanes(t, 8)
		frame(t, a)
		if withNotice {
			a.showNotice("Could not reach margit", "The connection was refused.", false)
		}
		frame(t, a)
		frame(t, a)

		got := testing.AllocsPerRun(20, func() {
			if err := a.Update(); err != nil {
				t.Fatalf("%s: a frame: %v", what, err)
			}
			a.Draw(a.screen)
		})

		if got > 0 {
			t.Errorf("%s: a settled frame allocates %v times, want none", what, got)
		}
	}
}
