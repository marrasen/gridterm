package main

import "github.com/Xpra-org/go-xpra/ui"

// This file is the coordinate scheme REMOTE-APPS.md proposes, written
// out so it can be tested before anything in gridterm depends on it.
//
// The problem it solves: xpra believes in one desktop, and puts both
// window positions and pointer positions in it. gridterm has a window of
// pixels holding a grid of panes. Something has to relate the two.
//
// The proposal is to make xpra's desktop *be* the gridterm window's
// pixels. Then an application's main window is placed at its pane's own
// box, a menu's absolute position is already the place on screen it goes,
// and a mouse position needs no conversion in either direction. The
// arithmetic below is mostly identity on purpose: that is the argument
// for the scheme, not an accident of it.
//
// What is left over, and what this file is really for, is the part that
// is not identity: a menu that would fall off the edge, and working out
// which window a click belongs to when a menu is hanging over a pane
// that is not its own.

// Box is a rectangle of pixels. The same type does for a pane, a window
// and a menu, because under this scheme they are all in one space.
type Box struct {
	X, Y, W, H int
}

// Empty reports whether a box covers nothing.
func (b Box) Empty() bool { return b.W <= 0 || b.H <= 0 }

// Right and Bottom are the edges just past the box.
func (b Box) Right() int  { return b.X + b.W }
func (b Box) Bottom() int { return b.Y + b.H }

// Contains reports whether a point is inside the box.
func (b Box) Contains(x, y int) bool {
	return x >= b.X && y >= b.Y && x < b.Right() && y < b.Bottom()
}

// Centre is the middle of the box.
func (b Box) Centre() (int, int) { return b.X + b.W/2, b.Y + b.H/2 }

// Intersect is the part of two boxes that is both, and whether there is
// any. It is how the overlap between a menu and the window under it is
// found, which is the only place a click's owner is in doubt.
func (b Box) Intersect(o Box) (Box, bool) {
	x, y := max(b.X, o.X), max(b.Y, o.Y)
	right, bottom := min(b.Right(), o.Right()), min(b.Bottom(), o.Bottom())
	got := Box{X: x, Y: y, W: right - x, H: bottom - y}
	return got, !got.Empty()
}

// Desk is the desktop one xpra session believes it is drawing on: the
// gridterm window, in pixels.
//
// One per session. Two sessions both think the desktop starts at 0,0,
// and that is fine, because neither is ever told about the other's
// windows.
type Desk struct {
	Width, Height int
}

// Place is where an application's main window goes: its pane, exactly.
//
// This is the whole of the scheme. It is identity, and the test beside
// it exists to say so out loud, because the moment it stops being
// identity every popup position starts needing a correction and the
// reason for choosing this arrangement is gone.
func (d Desk) Place(pane Box) Box { return pane }

// Fit slides a window back inside the desktop.
//
// A toolkit that knows the screen size flips its own menus away from
// the edge, and telling the server the desktop size is what lets it.
// This is the fallback for when it does not, or for when the gridterm
// window shrank after the menu opened.
//
// It slides and never shrinks. A menu narrowed to fit is a menu with
// its labels cut off, and the program was never asked. One that is
// genuinely wider than the window is pinned to the top left and clipped
// by whatever draws it -- there is nothing better to do with it.
func (d Desk) Fit(b Box) Box {
	if b.Right() > d.Width {
		b.X = d.Width - b.W
	}
	if b.Bottom() > d.Height {
		b.Y = d.Height - b.H
	}
	b.X = max(b.X, 0)
	b.Y = max(b.Y, 0)
	return b
}

// Window is one thing on screen for a session.
type Window struct {
	ID  ui.WindowID
	Box Box

	// Floating is a window that is not a pane: a menu, a tooltip, a
	// dialog. It sits above the grid and is clipped to the gridterm
	// window rather than to any pane.
	Floating bool
}

// Stack is what a session has on screen, in drawing order: the bottom
// of the slice is drawn first and the top of it takes the clicks.
//
// Panes do not overlap, so their order among themselves means nothing.
// Floating windows are the reason this is a stack at all.
type Stack struct {
	windows []Window
}

// Add puts a window on top.
func (s *Stack) Add(w Window) { s.windows = append(s.windows, w) }

// Remove takes a window out, reporting whether it was there.
func (s *Stack) Remove(id ui.WindowID) bool {
	for i, w := range s.windows {
		if w.ID == id {
			s.windows = append(s.windows[:i], s.windows[i+1:]...)
			return true
		}
	}
	return false
}

// Raise moves a window to the top, which is what the server asks for
// with a window-raise packet.
//
// A pane raised above a menu would be wrong, but it is the server's
// call and not ours: it is the thing running the window manager.
func (s *Stack) Raise(id ui.WindowID) bool {
	for i, w := range s.windows {
		if w.ID != id {
			continue
		}
		s.windows = append(append(s.windows[:i], s.windows[i+1:]...), w)
		return true
	}
	return false
}

// MoveResize records a window's new box.
func (s *Stack) MoveResize(id ui.WindowID, b Box) bool {
	for i := range s.windows {
		if s.windows[i].ID == id {
			s.windows[i].Box = b
			return true
		}
	}
	return false
}

// Windows returns the stack from the bottom up, which is the order they
// are drawn in.
func (s *Stack) Windows() []Window { return s.windows }

// At names the window a point in the gridterm window belongs to, and
// the point to send with it.
//
// The point comes back unchanged, and that is the result worth having:
// xpra wants pointer positions in the desktop's own space, and under
// this scheme the gridterm window *is* that space. Nothing is
// subtracted, so nothing can be subtracted twice.
//
// The search runs from the top down, so a menu hanging over a pane that
// is not its own takes the click. Getting that backwards would send the
// click to the pane underneath, which is the bug this whole file exists
// to avoid.
func (s *Stack) At(x, y int) (id ui.WindowID, px, py int, ok bool) {
	for i := len(s.windows) - 1; i >= 0; i-- {
		if w := s.windows[i]; w.Box.Contains(x, y) {
			return w.ID, x, y, true
		}
	}
	return 0, x, y, false
}

// Resize is the gridterm window changing size.
//
// It returns the desk to declare to the server and the panes' windows
// to move, because both have to go out: the desktop size so a toolkit
// keeps placing its menus somewhere visible, and each main window's
// geometry so the application reflows to the pane it is now in.
//
// Floating windows are left where they are. A menu will be closed and
// reopened by the program; a dialog the user has dragged somewhere is
// theirs, and shoving it because a pane moved would be rude. They are
// slid back inside only if the window shrank past them.
func (d Desk) Resize(width, height int, s *Stack, panes map[ui.WindowID]Box) (Desk, []Window) {
	desk := Desk{Width: width, Height: height}

	var moved []Window
	for i := range s.windows {
		w := &s.windows[i]
		if w.Floating {
			if fitted := desk.Fit(w.Box); fitted != w.Box {
				w.Box = fitted
				moved = append(moved, *w)
			}
			continue
		}
		pane, ok := panes[w.ID]
		if !ok {
			// A window whose pane has gone. The caller closes it; there
			// is nothing sensible to move it to in the meantime.
			continue
		}
		if box := desk.Place(pane); box != w.Box {
			w.Box = box
			moved = append(moved, *w)
		}
	}
	return desk, moved
}

// TopFloating is the frontmost floating window: the menu, if one is
// open. Panes are never it.
func (s *Stack) TopFloating() (Window, bool) {
	for i := len(s.windows) - 1; i >= 0; i-- {
		if s.windows[i].Floating {
			return s.windows[i], true
		}
	}
	return Window{}, false
}
