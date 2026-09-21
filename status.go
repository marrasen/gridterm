package main

import "time"

// statusFor is how long a line stays on the bottom row before it goes.
//
// Long enough to read a short sentence twice, short enough that it is
// gone by the time the user has done the next thing.
const statusFor = 4 * time.Second

// status is one line along the bottom of the window saying that
// something worked.
//
// A line rather than a dialog. An action that did what it was asked and
// has nothing to show for it needs acknowledging, not answering: a
// dialog for it costs a keypress to dismiss, and the keypress is the
// whole of what the user gets to do with it.
type status struct {
	text string

	// until is when the line goes. Nothing is showing once the frame is
	// past it.
	until time.Time
}

// say puts a line on the bottom row for a few seconds.
//
// The deadline is taken from the frame rather than the wall, so a line
// set while the window was not drawing does not start counting down
// before it has been seen.
func (a *app) say(text string) {
	a.status = status{text: text, until: a.frameTime().Add(statusFor)}
	a.markDirty()
}

// saying is the line to draw, and empty once its time is up.
func (a *app) saying() string {
	if a.status.text == "" || !a.frameTime().Before(a.status.until) {
		return ""
	}
	return a.status.text
}

// stepStatus takes the line away when its time is up.
//
// The window is marked dirty once, on the frame that finds it expired,
// so a line going costs one repaint rather than a repaint every frame
// while it is up.
func (a *app) stepStatus() {
	if a.status.text == "" {
		return
	}
	if a.frameTime().Before(a.status.until) {
		return
	}
	a.status = status{}
	a.markDirty()
}
