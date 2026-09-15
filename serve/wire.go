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

	// Attach names something the served window already has open, to
	// work in that rather than start something new. Empty asks for
	// something new.
	//
	// It is the ID of an Open the served window sent down the control
	// channel, so a client can only ask for what it was told about.
	Attach string
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

// chanControl carries what the window being served has open.
//
// The client opens it; the served window writes a snapshot down it
// whenever what it has open changes, and reads nothing back. One line
// of JSON per snapshot, because a snapshot is a list of a length
// neither end knows in advance and SSH's own encoding has no good shape
// for that. It carries no bytes of anybody's program, so the cost of
// the encoding does not matter.
const chanControl = "control@gridterm"

// Open is one thing the window being served has open.
//
// Its own type rather than conns.Entry: an entry carries the closures
// that reveal and close the thing, which mean nothing on the other side
// of a wire, and the two would drift apart the moment one of them
// gained a field the other could not carry.
type Open struct {
	// ID names this one for as long as it is open, so the client can
	// ask about it again.
	ID string `json:"id"`

	// Host is the machine it is on, as the served window calls it. That
	// window's "Local" is this one's "the machine I took over".
	Host string `json:"host"`

	// Kind, Label and Note are what the panel says about it.
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Note  string `json:"note"`

	// State is what it is doing: opened, active, settled or closed.
	State string `json:"state"`
}

// Snapshot is everything the window being served has open.
type Snapshot struct {
	// Window is what the served window calls itself, for a client
	// showing several at once.
	Window string `json:"window"`

	// Open is what it has open, in the order it opened them.
	Open []Open `json:"open"`
}
