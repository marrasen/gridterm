package vt

import (
	"bytes"
	"strconv"
	"strings"
)

// MostNoticeRunes is how much of a message a program sends is kept. A
// row has one line for it, and the rest would only be dropped later.
const MostNoticeRunes = 200

// ProgressState is what a program says about the work it is doing, from
// OSC 9;4.
type ProgressState int

const (
	// NoProgress is a program that has said nothing, or has said it has
	// finished. It is the zero value, so a pane nobody has told anything
	// is a pane with no progress.
	NoProgress ProgressState = iota

	// Working is a share of the work done, which Percent gives.
	Working

	// ProgressFailed is work that went wrong. Percent is where it was.
	ProgressFailed

	// Indeterminate is work going on with no way to say how far.
	Indeterminate

	// ProgressWarning is work going on that the program is unhappy
	// about. Percent is where it is.
	ProgressWarning
)

// Progress is how far along a program says it is.
type Progress struct {
	State ProgressState

	// Percent is 0 to 100, and means nothing while State is NoProgress
	// or Indeterminate.
	Percent int
}

// Notice is the last message a program asked to have shown, and a count
// that rises each time a new one arrives.
//
// The count is what tells a second message that reads the same as the
// first from the first one being shown again.
func (t *Terminal) Notice() (string, uint64) { return t.notice, t.noticeNum }

// Progress is how far along the program in this pane says it is.
func (t *Terminal) Progress() Progress { return t.progress }

// setNotify takes OSC 9, which ConEmu made and the terminals after it
// copied.
//
// "9;4;..." is how far along the program is. "9;9;..." is where the
// shell is, which setDirPath reads. Anything else is a message the
// program wants shown.
func (t *Terminal) setNotify(params [][]byte) {
	if len(params) < 2 {
		return
	}
	switch string(params[1]) {
	case "4":
		t.setProgress(params)
	case "9":
		t.setDirPath(params)
	default:
		t.setNotice(params)
	}
}

// setNotice keeps a message a program asked to have shown.
//
// Kept rather than shown: what shows it is the window, and a message
// that took the keyboard would let any program that can write to a pane
// stop the window by sending them one after another.
func (t *Terminal) setNotice(params [][]byte) {
	// The parser cuts on semicolons and a message may hold one, so what
	// was sent is the rest of the parameters joined back up.
	text := string(bytes.Join(params[1:], []byte(";")))
	text = strings.TrimSpace(sanitiseLine(text))
	if text == "" {
		return
	}
	if r := []rune(text); len(r) > MostNoticeRunes {
		text = string(r[:MostNoticeRunes])
	}
	t.notice, t.noticeNum = text, t.noticeNum+1
}

// sanitiseLine drops the control characters out of a line a program
// sent, so a message cannot draw over the window it is shown in.
func sanitiseLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// setProgress takes OSC 9;4, which is "9;4;<state>;<percent>".
//
// The states are ConEmu's: 0 nothing, 1 a share done, 2 failed, 3
// working with no number, 4 working with a warning.
func (t *Terminal) setProgress(params [][]byte) {
	if len(params) < 3 {
		return
	}
	state, err := strconv.Atoi(strings.TrimSpace(string(params[2])))
	if err != nil || state < int(NoProgress) || state > int(ProgressWarning) {
		return
	}
	percent := 0
	if len(params) > 3 {
		// A percent that is not a number is no percent. The state still
		// counts: a program saying it failed has said something.
		if n, err := strconv.Atoi(strings.TrimSpace(string(params[3]))); err == nil {
			percent = min(max(n, 0), 100)
		}
	}
	t.progress = Progress{State: ProgressState(state), Percent: percent}
	if t.progress.State == NoProgress {
		t.progress.Percent = 0
	}
}

// answerColour takes OSC 10 and OSC 11, which ask what the text and the
// background are drawn in.
//
// Only the question is answered. A program may also use these to set
// the colours, and this pane's colours are the window's theme: a
// program that changed them would leave a pane looking unlike every
// other one, with nothing to put it back.
func (t *Terminal) answerColour(params [][]byte, which string, bell bool) {
	if len(params) < 2 || strings.TrimSpace(string(params[1])) != "?" {
		return
	}
	c := t.scr.palette.FG
	if which == "11" {
		c = t.scr.palette.BG
	}
	end := "\x1b\\"
	if bell {
		end = "\x07"
	}
	// Four hex digits a channel, which is what xterm answers and what
	// everything reading one expects.
	t.reply("\x1b]" + which + ";rgb:" +
		hex4(c.R) + "/" + hex4(c.G) + "/" + hex4(c.B) + end)
}

// hex4 writes one channel the way an X colour name does: the byte
// twice, which spreads 8 bits across the 16 the name has room for.
func hex4(v uint8) string {
	const digits = "0123456789abcdef"
	hi, lo := digits[v>>4], digits[v&0xf]
	return string([]byte{hi, lo, hi, lo})
}
