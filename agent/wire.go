package agent

// What an agent says to a window, and what it hears back.
//
// One JSON object per line, each way. A line is a whole request or a
// whole answer, so neither end has to frame anything: this carries a few
// short messages about a screen, not a stream of a program's output.
type ask struct {
	// Do is what is wanted: "hello", "use", "panes", "read", "send" or
	// "wait".
	Do string `json:"do"`

	// Protocol is what the first message carries, and nothing else
	// does. See hello.
	Protocol string `json:"protocol,omitempty"`

	// Code is the session code the user gave the agent. Only "use"
	// carries one.
	Code string `json:"code,omitempty"`

	// Pane is which of the panes handed over this is about.
	Pane string `json:"pane,omitempty"`

	// Text is what to type, for "send".
	Text string `json:"text,omitempty"`

	// Keys are the keys to press after the text, by name, for "send".
	// See KeyNames.
	Keys []string `json:"keys,omitempty"`

	// Lines is how many lines of the pane to send back, ending at the
	// bottom of the screen and reaching into what has scrolled off, for
	// "read" and "wait". Zero is the screen.
	Lines int `json:"lines,omitempty"`

	// Until is what "wait" is waiting for.
	Until wait `json:"until,omitempty"`
}

// wait says what a "wait" is waiting for.
//
// Whichever happens first. A wait with nothing set waits for the screen
// to go quiet, which is what waiting for a command to finish looks like
// when the program cannot be asked.
type wait struct {
	// Contains ends the wait as soon as the screen holds this text.
	Contains string `json:"contains,omitempty"`

	// QuietMS ends it once the pane has said nothing for this long.
	// Zero means the default.
	QuietMS int `json:"quiet_ms,omitempty"`

	// TimeoutMS gives up after this long and says what is on the screen
	// anyway. Zero means the default.
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

// hello is what an agent has to say before anything else, and is the
// first thing on the connection.
//
// It is not a version check, though it is that too. A browser can be
// made to send a chosen body to a port on this machine -- it cannot
// read the answer, but it does not need to, and typing into somebody's
// shell blind is enough. What it cannot do is choose the first bytes:
// an HTTP request begins with a request line, and this window hangs up
// on a first line that is not exactly this.
//
// It says 2 because a window of the first version reads a "send" with
// keys in it and presses nothing, and says it typed: an agent turned
// away is better than one told it pressed Escape when it did not.
const hello = "gridterm-agent-2"

// said is what the window answers.
//
// Error and the rest are exclusive: a request that failed says why and
// carries nothing else.
type said struct {
	// Error says what went wrong, and is empty when nothing did.
	Error string `json:"error,omitempty"`

	// Pane is the pane a "use" opened.
	Pane *Pane `json:"pane,omitempty"`

	// Panes is what this agent has been handed, for "panes".
	Panes []Pane `json:"panes,omitempty"`

	// Look is the screen, for "read" and "wait".
	Look *Look `json:"look,omitempty"`

	// Waited says a "wait" gave up on time rather than because what it
	// was waiting for happened. The screen still comes with it: what is
	// on a screen that never settled is worth reading.
	Waited bool `json:"waited,omitempty"`

	// OK says a request that has nothing to give back succeeded.
	OK bool `json:"ok,omitempty"`
}
