package serve

// What one gridterm says to another.
//
// Kept to SSH's own shapes wherever there is one that fits. A session
// channel carries the bytes of a program, a window-change request
// carries the size, and an exit-status request carries how it ended --
// the same three things an SSH shell is made of, so both ends can use
// the library's own framing rather than inventing a frame to put inside
// it.
//
// The names carry @gridterm so that an ordinary SSH client connecting
// to a serving window is refused by name rather than being handed a
// shell it did not ask for.
const (
	// chanSession opens something to work in on the machine being
	// served. Its payload is openSession.
	chanSession = "session@gridterm"

	// reqWindowChange says the pane the session is drawn in has changed
	// size. Its payload is windowChange, which is the shape OpenSSH
	// uses for the same thing.
	reqWindowChange = "window-change"

	// reqExitStatus says how the program ended. Its payload is
	// exitStatus.
	reqExitStatus = "exit-status"
)

// openSession is what a client asks for when it opens a session
// channel: the size of the pane it will be drawn in.
//
// The size comes with the request rather than in one that follows it. A
// program reads the size as it starts, and one that started at eighty
// by twenty-four and was told the truth afterwards has already drawn
// its first screen wrong.
type openSession struct {
	Cols uint32
	Rows uint32
}

// windowChange is the size of the pane a session is drawn in.
//
// The pixel sizes are part of the shape OpenSSH uses and are sent as
// zero: a pane is measured in cells, and the two ends do not share a
// font to turn those into pixels with.
type windowChange struct {
	Cols     uint32
	Rows     uint32
	WidthPx  uint32
	HeightPx uint32
}

// exitStatus is how a program ended.
type exitStatus struct {
	Status uint32
}
