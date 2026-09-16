// Package agent lets a program the user is talking to work in a pane of
// this window.
//
// It is not a second window taking over. An agent is given one pane at a
// time, by the user, and what it can do with it is read the screen, type
// into it, and wait for the screen to settle -- which is what watching
// somebody work looks like from the outside. It cannot reach a pane it
// was not handed, open a connection of this window's, or read a file
// through it.
//
// That is a narrow way in, not a fence around what happens next. What it
// types goes into a live shell, so anything that shell can run it can
// ask for, and it runs as whoever the user set that pane up as. The
// point is that it happens in one pane, in front of the user, who is
// watching and can take it back.
//
// The user picks a pane and gets a code. The code is the whole of what
// lets anything in. It is made fresh and taking the pane back makes it
// useless. The window writes it nowhere; showing it puts it on the
// clipboard, which on Windows is kept in the clipboard history and may
// be sent to the user's Microsoft account, so a code that has been
// shown has been out of this process.
//
// The listener is on the loopback address and nothing else. That is not
// the same as this user: a loopback port on Windows is reachable by
// every session on the machine. What it is worth reaching is bounded by
// the code, and what it costs to reach without one is bounded by
// mostAgents and by hanging up on anything that does not say what it
// is.
package agent

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strconv"
	"strings"
)

// Window is what an agent may do with the panes it has been handed.
//
// Every method is called from a goroutine serving one agent, so an
// implementation that touches the window has to hand the work to
// whatever draws.
//
// Use is the only way in. It takes a code the user gave the agent and
// gives back the pane that code names; a code that names nothing, or one
// the user has taken back, fails. Look and Send then work on the id Use
// gave, and fail for any other.
//
// Look takes how many lines to give back, ending at the bottom of the
// screen, and zero for the screen. Send types text and then presses the
// named keys; a name it does not know is refused and nothing is typed.
type Window interface {
	Use(code string) (Pane, error)
	Look(id string, lines int) (Look, error)
	Send(id, text string, keys []string) error
}

// Pane is what a code named.
type Pane struct {
	// ID names the pane for as long as the user leaves it handed over.
	ID string `json:"id"`

	// Label is what the window calls it, so an agent holding two panes
	// can tell them apart.
	Label string `json:"label"`

	// Cols and Rows are the size of its screen.
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Look is a pane as it stands.
type Look struct {
	// Screen is what is on it now, as plain text: one line per row,
	// trailing spaces cut, no escape sequences, and more rows than the
	// screen has when lines asked for history. An agent reads it the
	// way the user does.
	Screen string `json:"screen"`

	// Gone says the program in the pane has finished, so waiting for it
	// to say more is waiting for nothing.
	Gone bool `json:"gone"`

	// Changed counts how many times the pane has said anything. Waiting
	// for a screen to settle watches this rather than comparing text,
	// so output that redraws the same picture still counts as movement.
	Changed uint64 `json:"changed"`

	// Row and Col are where the cursor is on the screen, counted from
	// zero at the top left. They are the screen's own rows, so they say
	// nothing about lines that have scrolled off.
	Row int `json:"row"`
	Col int `json:"col"`

	// Alt says a full-screen program is drawing, such as vim, top or
	// mc. Nothing has scrolled off while one is, so a read of more than
	// the screen gives the screen.
	Alt bool `json:"alt,omitempty"`
}

// NewCode makes a code for one handed-over pane.
//
// It carries the port to reach this window on, because the agent is
// given nothing else and has no way to find the window otherwise. The
// secret half is twenty bytes from the system's random source, which is
// not guessable and is never written anywhere.
func NewCode(port int) (string, error) {
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("agent: no randomness for a code: %w", err)
	}
	secret := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
	return fmt.Sprintf("%s-%d-%s", codePrefix, port, secret), nil
}

// ReadCode takes a code apart into the port to dial and the code itself.
//
// The whole code is what the window is given back, not the secret half:
// two windows on one machine hand out codes of the same shape, and the
// port is what says which one this is for.
func ReadCode(code string) (port int, err error) {
	parts := strings.Split(strings.TrimSpace(code), "-")
	if len(parts) != 3 || parts[0] != codePrefix || parts[2] == "" {
		return 0, fmt.Errorf("%q is not a gridterm session code", code)
	}
	port, err = strconv.Atoi(parts[1])
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%q does not name a port to reach gridterm on", code)
	}
	return port, nil
}

// codePrefix begins every code, so something pasted by mistake is
// turned away by name rather than tried against the window.
const codePrefix = "gt1"
