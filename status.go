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
	// past it, and a zero time is a line that holds.
	until time.Time
}

// held says this line stays up until what it is about has finished.
func (s status) held() bool { return s.text != "" && s.until.IsZero() }

// say puts a line on the bottom row for a few seconds.
//
// The deadline is taken from the frame rather than the wall, so a line
// set while the window was not drawing does not start counting down
// before it has been seen.
func (a *app) say(text string) {
	a.status = status{text: text, until: a.frameTime().Add(statusFor)}
	a.markDirty()
}

// sayWhile puts a line on the bottom row and leaves it there until what
// it is about has finished.
//
// For a line that says what the window is doing rather than what it has
// done. A few seconds is long enough to read that something worked, and
// not long enough for a login: the row would go blank half way through
// the wait, which is the thing the line is up to prevent.
func (a *app) sayWhile(text string) {
	a.status = status{text: text}
	a.markDirty()
}

// doneSaying takes a held line away, if it is still the one showing.
//
// Checked, because anything the user did in the meantime has its own
// line and that one is not this one's to clear.
func (a *app) doneSaying(text string) {
	if a.status.held() && a.status.text == text {
		a.status = status{}
		a.markDirty()
	}
}

// saying is the line to draw, and empty once its time is up.
func (a *app) saying() string {
	if a.status.text == "" {
		return ""
	}
	if a.status.held() {
		return a.status.text
	}
	if !a.frameTime().Before(a.status.until) {
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
	if a.status.text == "" || a.status.held() {
		return
	}
	if a.frameTime().Before(a.status.until) {
		return
	}
	a.status = status{}
	a.markDirty()
}
