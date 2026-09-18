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
	// SessionChannel opens something to work in on the machine being
	// served. Its payload is openSession.
	//
	// Exported because a machine that refuses an ordinary SSH session
	// names it in the refusal, and the client that has to recognise that
	// refusal must match the name rather than a copy of it.
	SessionChannel = "session@gridterm"

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
// SSH's own encoding is positional: there are no field names and no
// room for a field one end knows and the other does not. So both
// windows have to be the same build, and one that is not is refused by
// name when this fails to parse.
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

	// AttachHost and AttachKind are the machine and the sort of thing
	// that Open said it was.
	//
	// They are not authorisation: the key the client signed with is what
	// decides whether it may be here at all. They are integrity. An ID
	// names a place in a list the served window builds afresh, and if
	// ids ever stop being a counter that is never reused, one that has
	// been handed out again would silently connect a client to something
	// else. The label is deliberately not among them: a shell sets its
	// own title, so it changes at every prompt.
	AttachHost string
	AttachKind string
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

// chanFiles carries a file session on the machine being served, or on
// a machine that window is connected to. Its payload is openFiles, and
// no payload at all asks for the machine being served. The channel
// carries the bytes; what runs on it is the window's business.
const chanFiles = "files@gridterm"

// chanClipboard carries a picture from a client to the clipboard of the
// window being served, so a program running there can be pasted one.
//
// A channel of its own rather than a line down the control channel: a
// screenshot is megabytes, and the control channel is what says what a
// window has open. One would hold up the other.
//
// The client writes the picture as a PNG and stops writing. The window
// answers with nothing when the picture landed, and with why it did not
// when it did not, so there is no framing to agree on beyond that.
const chanClipboard = "clipboard@gridterm"

// mostClipboardBytes is the largest picture a window will take. A
// screenshot of a large screen is a few megabytes; this is well past
// that, and it is what stops a client filling this window's memory.
const mostClipboardBytes = 64 << 20

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

	// Window says the machine it is on is another gridterm the served
	// window has taken over, rather than a machine it has a shell on.
	//
	// A client cannot open anything there: it is a window, with panes of
	// its own, and the files behind it are that window's to serve. So a
	// client shows it as a window and offers nothing on it.
	Window bool `json:"window,omitempty"`

	// Cols and Rows are how big the screen is over there, for something
	// with a screen, and zero for anything else.
	//
	// Watching does not resize it: it is drawn on that machine too, and
	// a window that shrank somebody's shell to fit a pane they are not
	// looking at would reach further than it was asked to. So the size
	// is sent instead, and the pane watching it can say what it is.
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`
}

// HasScreen reports whether this is something a watcher can be shown.
//
// The size is what says so: only something with a screen carries one.
// A connection, a tunnel or a client working in that window is a row on
// its panel and nothing a second pair of eyes can be put on.
func (o Open) HasScreen() bool { return o.Cols > 0 && o.Rows > 0 }

// Snapshot is everything the window being served has open.
type Snapshot struct {
	// Window is what the served window calls itself, for a client
	// showing several at once.
	Window string `json:"window"`

	// Open is what it has open, in the order it opened them.
	Open []Open `json:"open"`

	// Going says the window is about to close the connection on
	// purpose, and why. Empty in an ordinary snapshot, and the only
	// field set when it is not: a client that reads one of these leaves
	// its list of what is open alone.
	//
	// Without it a window that stopped sharing and a network that
	// dropped look the same at the other end, and the client shows a
	// socket error for something the user did on purpose.
	Going string `json:"going,omitempty"`
}

// Why a window closes a connection on purpose. A client that is told
// one of these says what happened instead of showing a network error.
const (
	// GoingStopped says the window stopped sharing.
	GoingStopped = "stopped"

	// GoingKicked says this client in particular was thrown out, while
	// the window goes on serving anybody else.
	GoingKicked = "kicked"
)

// openFiles is what a client asks for when it opens a file session: the
// machine whose files it wants.
//
// Empty, and no payload at all, both ask for the machine the served
// window is on. That is what a client of an older build sends, and what
// a client asking for the window's own disk sends.
type openFiles struct {
	// Host is the machine as the served window calls it, which is the
	// name that window sent down the control channel.
	Host string
}
