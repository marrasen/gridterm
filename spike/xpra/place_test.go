package main

import (
	"testing"

	"github.com/Xpra-org/go-xpra/ui"
)

// The window these tests work in: 800 by 600 pixels, split down the
// middle into two panes.
var (
	desk  = Desk{Width: 800, Height: 600}
	paneA = Box{X: 0, Y: 0, W: 400, H: 600}
	paneB = Box{X: 400, Y: 0, W: 400, H: 600}
)

const (
	appA ui.WindowID = 1
	appB ui.WindowID = 2
	menu ui.WindowID = 3
)

// TestPlaceIsIdentity says out loud that an application's main window
// is its pane and nothing else.
//
// It looks like a test of nothing. It is a test that the scheme has not
// quietly stopped being the scheme: the moment a main window sits
// somewhere other than its pane, every menu position needs the same
// correction applied, and the reason for arranging it this way is gone.
func TestPlaceIsIdentity(t *testing.T) {
	for _, pane := range []Box{paneA, paneB, {X: 13, Y: 7, W: 101, H: 53}} {
		if got := desk.Place(pane); got != pane {
			t.Errorf("Place(%+v) = %+v, want the pane unchanged", pane, got)
		}
	}
}

// TestFit covers a menu that would fall off an edge.
func TestFit(t *testing.T) {
	for _, c := range []struct {
		what string
		in   Box
		want Box
	}{
		{"already inside", Box{10, 10, 50, 50}, Box{10, 10, 50, 50}},
		{"flush against the right edge", Box{680, 0, 120, 50}, Box{680, 0, 120, 50}},
		{"past the right edge", Box{700, 0, 120, 50}, Box{680, 0, 120, 50}},
		{"past the bottom", Box{0, 560, 50, 100}, Box{0, 500, 50, 100}},
		{"past both", Box{790, 590, 120, 100}, Box{680, 500, 120, 100}},
		{"wider than the window", Box{10, 10, 900, 50}, Box{0, 10, 900, 50}},
		{"taller than the window", Box{10, 10, 50, 700}, Box{10, 0, 50, 700}},
	} {
		if got := desk.Fit(c.in); got != c.want {
			t.Errorf("%s: Fit(%+v) = %+v, want %+v", c.what, c.in, got, c.want)
		}
	}
}

// TestFitNeverShrinks is the rule that matters more than the numbers. A
// menu cut down to fit is a menu with its labels chopped off, and the
// program was never told it happened.
func TestFitNeverShrinks(t *testing.T) {
	for _, in := range []Box{
		{700, 0, 120, 50}, {0, 560, 50, 100}, {790, 590, 120, 100},
		{10, 10, 900, 50}, {10, 10, 50, 700},
	} {
		got := desk.Fit(in)
		if got.W != in.W || got.H != in.H {
			t.Errorf("Fit(%+v) = %+v: the size changed", in, got)
		}
	}
}

// TestMenuOverAnotherPaneTakesTheClick is the one this file exists for.
//
// A menu opened near the right edge of the left pane hangs over the
// right one. The click in the overhang belongs to the menu. Searching
// the panes first would send it to the pane underneath, and the menu
// would be unusable in exactly the place menus always are.
func TestMenuOverAnotherPaneTakesTheClick(t *testing.T) {
	var s Stack
	s.Add(Window{ID: appA, Box: desk.Place(paneA)})
	s.Add(Window{ID: appB, Box: desk.Place(paneB)})

	// Opened from the left pane, 100 pixels of it over the right one.
	overhang := Box{X: 380, Y: 100, W: 120, H: 200}
	s.Add(Window{ID: menu, Box: desk.Fit(overhang), Floating: true})

	if overhang.Right() <= paneB.X {
		t.Fatalf("the menu at %+v does not reach the right pane; the test proves nothing", overhang)
	}

	for _, c := range []struct {
		what   string
		x, y   int
		want   ui.WindowID
		inside bool
	}{
		{"in the overhang, over the right pane", 450, 150, menu, true},
		{"in the part of the menu over its own pane", 390, 150, menu, true},
		{"in the right pane, below the menu", 450, 400, appB, true},
		{"in the right pane, past the menu's right edge", 600, 150, appB, true},
		{"in the left pane, away from the menu", 100, 300, appA, true},
		{"outside every window", 900, 900, 0, false},
	} {
		id, px, py, ok := s.At(c.x, c.y)
		if ok != c.inside || id != c.want {
			t.Errorf("%s: At(%d,%d) = window %d (found=%v), want window %d (found=%v)",
				c.what, c.x, c.y, id, ok, c.want, c.inside)
		}
		if px != c.x || py != c.y {
			t.Errorf("%s: At(%d,%d) moved the point to %d,%d", c.what, c.x, c.y, px, py)
		}
	}
}

// TestAtLeavesThePointAlone is the other half of the scheme: xpra wants
// pointer positions in the desktop's space, and the gridterm window is
// that space, so nothing is subtracted and nothing can be subtracted
// twice.
func TestAtLeavesThePointAlone(t *testing.T) {
	var s Stack
	s.Add(Window{ID: appA, Box: Box{X: 100, Y: 50, W: 200, H: 200}})
	for _, p := range [][2]int{{100, 50}, {150, 120}, {299, 249}} {
		id, px, py, ok := s.At(p[0], p[1])
		if !ok || id != appA {
			t.Errorf("At(%d,%d) found window %d (found=%v), want %d", p[0], p[1], id, ok, appA)
		}
		if px != p[0] || py != p[1] {
			t.Errorf("At(%d,%d) came back as %d,%d", p[0], p[1], px, py)
		}
	}
}

// TestRaiseChangesWhoTakesTheClick checks that a server-driven raise
// actually reorders the stack.
func TestRaiseChangesWhoTakesTheClick(t *testing.T) {
	var s Stack
	both := Box{X: 100, Y: 100, W: 100, H: 100}
	s.Add(Window{ID: appA, Box: both})
	s.Add(Window{ID: appB, Box: both})

	if id, _, _, _ := s.At(150, 150); id != appB {
		t.Fatalf("the top window is %d, want %d", id, appB)
	}
	if !s.Raise(appA) {
		t.Fatal("Raise said window A was not in the stack")
	}
	if id, _, _, _ := s.At(150, 150); id != appA {
		t.Errorf("after raising A the top window is %d, want %d", id, appA)
	}
	if got := len(s.Windows()); got != 2 {
		t.Errorf("the stack holds %d windows after a raise, want 2", got)
	}
	if s.Raise(menu) {
		t.Error("Raise claimed to find a window that was never added")
	}
}

// TestRemove checks a window going away, which is the server's call.
func TestRemove(t *testing.T) {
	var s Stack
	s.Add(Window{ID: appA, Box: paneA})
	s.Add(Window{ID: menu, Box: Box{X: 10, Y: 10, W: 50, H: 50}, Floating: true})

	if !s.Remove(menu) {
		t.Fatal("Remove said the menu was not there")
	}
	if id, _, _, _ := s.At(30, 30); id != appA {
		t.Errorf("with the menu gone the click goes to window %d, want %d", id, appA)
	}
	if s.Remove(menu) {
		t.Error("Remove found the menu twice")
	}
}

// TestResizeMovesPanesAndLeavesFloatsAlone is the gridterm window being
// dragged wider.
//
// The panes' windows follow their panes, because the application has to
// reflow. A dialog the user put somewhere stays put: shoving it because
// a pane moved would be rude, and it is not in a pane in the first
// place.
func TestResizeMovesPanesAndLeavesFloatsAlone(t *testing.T) {
	var s Stack
	s.Add(Window{ID: appA, Box: desk.Place(paneA)})
	s.Add(Window{ID: appB, Box: desk.Place(paneB)})
	dialog := Box{X: 300, Y: 200, W: 200, H: 150}
	s.Add(Window{ID: menu, Box: dialog, Floating: true})

	// Twice as wide, same height, split down the middle as before.
	wideA := Box{X: 0, Y: 0, W: 800, H: 600}
	wideB := Box{X: 800, Y: 0, W: 800, H: 600}
	next, moved := desk.Resize(1600, 600, &s, map[ui.WindowID]Box{appA: wideA, appB: wideB})

	if next != (Desk{Width: 1600, Height: 600}) {
		t.Errorf("the new desk is %+v, want 1600x600", next)
	}
	if len(moved) != 2 {
		t.Fatalf("%d windows moved, want 2: both panes and not the dialog", len(moved))
	}
	for _, w := range moved {
		if w.Floating {
			t.Errorf("window %d floats and should not have moved", w.ID)
		}
	}
	if got := boxOf(t, &s, appB); got != wideB {
		t.Errorf("window B is at %+v, want %+v", got, wideB)
	}
	if got := boxOf(t, &s, menu); got != dialog {
		t.Errorf("the dialog moved to %+v; it was at %+v", got, dialog)
	}
}

// TestResizeSlidesAFloatBackIn is the window shrinking past a dialog,
// which is the one case a floating window does get moved.
func TestResizeSlidesAFloatBackIn(t *testing.T) {
	var s Stack
	s.Add(Window{ID: appA, Box: desk.Place(paneA)})
	dialog := Box{X: 600, Y: 400, W: 150, H: 150}
	s.Add(Window{ID: menu, Box: dialog, Floating: true})

	small := Box{X: 0, Y: 0, W: 400, H: 300}
	_, moved := desk.Resize(400, 300, &s, map[ui.WindowID]Box{appA: small})

	if len(moved) != 2 {
		t.Fatalf("%d windows moved, want 2: the pane and the dialog it no longer fits beside", len(moved))
	}
	want := Box{X: 250, Y: 150, W: 150, H: 150}
	if got := boxOf(t, &s, menu); got != want {
		t.Errorf("the dialog slid to %+v, want %+v", got, want)
	}
}

// TestResizeLeavesAnOrphanAlone covers a window whose pane has gone.
// The caller closes it; there is nothing to move it to meanwhile.
func TestResizeLeavesAnOrphanAlone(t *testing.T) {
	var s Stack
	s.Add(Window{ID: appA, Box: desk.Place(paneA)})
	_, moved := desk.Resize(800, 600, &s, map[ui.WindowID]Box{})
	if len(moved) != 0 {
		t.Errorf("%d windows moved, want none", len(moved))
	}
	if got := boxOf(t, &s, appA); got != paneA {
		t.Errorf("the orphan moved to %+v", got)
	}
}

func boxOf(t *testing.T, s *Stack, id ui.WindowID) Box {
	t.Helper()
	for _, w := range s.Windows() {
		if w.ID == id {
			return w.Box
		}
	}
	t.Fatalf("there is no window %d in the stack", id)
	return Box{}
}

// TestIntersect covers the overlap between a menu and the window under
// it, which is the only place a click's owner is ever in doubt.
func TestIntersect(t *testing.T) {
	base := Box{X: 100, Y: 100, W: 100, H: 100}
	for _, c := range []struct {
		what string
		with Box
		want Box
		any  bool
	}{
		{"overlapping a corner", Box{150, 150, 100, 100}, Box{150, 150, 50, 50}, true},
		{"wholly inside", Box{120, 120, 20, 20}, Box{120, 120, 20, 20}, true},
		{"wholly around", Box{0, 0, 300, 300}, base, true},
		{"identical", base, base, true},
		// Touching is not overlapping: the pixel at x=200 is the first
		// one outside the box, so a click there is not in it.
		{"touching the right edge", Box{200, 100, 50, 50}, Box{}, false},
		{"touching the bottom edge", Box{100, 200, 50, 50}, Box{}, false},
		{"nowhere near", Box{500, 500, 10, 10}, Box{}, false},
	} {
		got, any := base.Intersect(c.with)
		if any != c.any {
			t.Errorf("%s: Intersect reported overlap=%v, want %v", c.what, any, c.any)
			continue
		}
		if any && got != c.want {
			t.Errorf("%s: Intersect(%+v) = %+v, want %+v", c.what, c.with, got, c.want)
		}
	}

	// It has to say the same thing whichever way round it is asked, or
	// the answer depends on which window was looked at first.
	other := Box{150, 150, 100, 100}
	a, okA := base.Intersect(other)
	b, okB := other.Intersect(base)
	if a != b || okA != okB {
		t.Errorf("Intersect is not symmetric: %+v/%v one way, %+v/%v the other", a, okA, b, okB)
	}
}

// TestCentre checks the middle of a box, which is where a scripted
// click goes.
func TestCentre(t *testing.T) {
	for _, c := range []struct {
		in   Box
		x, y int
	}{
		{Box{0, 0, 100, 100}, 50, 50},
		{Box{100, 50, 64, 48}, 132, 74},
		{Box{10, 10, 1, 1}, 10, 10},
	} {
		if x, y := c.in.Centre(); x != c.x || y != c.y {
			t.Errorf("Centre(%+v) = %d,%d, want %d,%d", c.in, x, y, c.x, c.y)
		}
	}
}

// TestStackMissesAreQuiet covers asking the stack about windows that
// are not in it. The server can name a window we have already dropped,
// so every lookup has to answer rather than panic.
func TestStackMissesAreQuiet(t *testing.T) {
	var s Stack
	if s.MoveResize(appA, paneA) {
		t.Error("MoveResize claimed to find a window in an empty stack")
	}
	if _, ok := s.TopFloating(); ok {
		t.Error("TopFloating found a menu in an empty stack")
	}

	s.Add(Window{ID: appA, Box: paneA})
	if _, ok := s.TopFloating(); ok {
		t.Error("TopFloating returned a pane; only floating windows count")
	}
	if s.MoveResize(menu, paneB) {
		t.Error("MoveResize claimed to find a window that was never added")
	}
	if !s.MoveResize(appA, paneB) {
		t.Fatal("MoveResize did not find the window that is there")
	}
	if got := boxOf(t, &s, appA); got != paneB {
		t.Errorf("after MoveResize the window is at %+v, want %+v", got, paneB)
	}
}
