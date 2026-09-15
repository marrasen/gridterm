// Package vt turns a terminal byte stream into a character grid. It
// wraps a DEC-compatible escape-sequence parser and drives a Screen,
// which holds the primary and alternate buffers, the cursor and the
// modes.
//
// Nothing here touches a GPU or a pty, so the whole emulator is testable
// by writing bytes in and reading cells out.
package vt

import (
	"encoding/base64"
	"strconv"
	"strings"

	vte "github.com/danielgatis/go-vte"

	"github.com/marrasen/gridterm/grid"
)

// Callbacks are the side effects a terminal has on the world around it.
// All are optional.
type Callbacks struct {
	// Bell fires on BEL.
	Bell func()
	// Title fires when the window title changes (OSC 0 or 2).
	Title func(string)
	// Reply sends bytes back to the program, for device reports.
	Reply func([]byte)
	// ClipboardSet fires on OSC 52 with the decoded text.
	ClipboardSet func(string)
}

// Terminal is a VT emulator: write bytes in, render cells out.
//
// A Terminal is not safe for concurrent use. Feed it from one goroutine
// and render from the same one, or hold a lock around both.
type Terminal struct {
	parser *vte.Parser
	scr    *Screen
	cb     Callbacks

	title string

	// lastRune is the most recent printable character, which REP repeats.
	lastRune rune
}

// New returns a terminal of the given size.
func New(cols, rows int, pal Palette, scrollback int, cb Callbacks) *Terminal {
	t := &Terminal{
		scr: NewScreen(cols, rows, pal, scrollback),
		cb:  cb,
	}
	t.parser = vte.NewParser(t)
	return t
}

// Screen returns the underlying screen, for scrolling the view and
// reading modes.
func (t *Terminal) Screen() *Screen { return t.scr }

// Title returns the last title set by the program.
func (t *Terminal) Title() string { return t.title }

// Write feeds bytes to the emulator. It never returns an error: a
// terminal has no way to reject what a program sends it.
func (t *Terminal) Write(p []byte) (int, error) {
	for _, b := range p {
		t.parser.Advance(b)
	}
	return len(p), nil
}

// Resize changes the screen size.
func (t *Terminal) Resize(cols, rows int) { t.scr.Resize(cols, rows) }

// Render copies the visible screen into g.
func (t *Terminal) Render(g *grid.Grid) { t.scr.Render(g) }

// ---------------------------------------------------------------- //
// vte.Performer
// ---------------------------------------------------------------- //

func (t *Terminal) Print(r rune) {
	w := grid.RuneWidth(r)
	// REP repeats the last printable character. A combining mark is not
	// one: repeating it would stack marks on a cell rather than repeat
	// anything visible.
	if w > 0 {
		t.lastRune = r
	}
	t.scr.print(r, w)
}

func (t *Terminal) Execute(b byte) {
	switch b {
	case 0x07: // BEL
		if t.cb.Bell != nil {
			t.cb.Bell()
		}
	case 0x08: // BS
		t.scr.Backspace()
	case 0x09: // HT
		t.scr.Tab(1)
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		t.scr.LineFeed()
	case 0x0d: // CR
		t.scr.CarriageReturn()
	}
}

// Hook, Put and Unhook cover DCS sequences. Nothing here implements one,
// but they must be consumed or their payload would be printed as text.
func (t *Terminal) Hook(_ [][]uint16, _ []byte, _ bool, _ rune) {}
func (t *Terminal) Put(_ byte)                                  {}
func (t *Terminal) Unhook()                                     {}

// SosPmApcDispatch covers SOS, PM and APC strings, which are consumed
// and ignored for the same reason.
func (t *Terminal) SosPmApcDispatch(_ vte.SosPmApcKind, _ []byte, _ bool) {}

func (t *Terminal) EscDispatch(intermediates []byte, _ bool, b byte) {
	// Character-set selection is parsed and ignored: the intermediate
	// byte distinguishes G0 from G1, and without consuming it the final
	// byte would be mistaken for another sequence.
	if len(intermediates) > 0 {
		switch intermediates[0] {
		case '(', ')', '*', '+':
			return
		case '#':
			if b == '8' {
				t.scr.DecAln()
			}
			return
		}
	}
	switch b {
	case 'D': // IND
		t.scr.LineFeed()
	case 'E': // NEL
		t.scr.CarriageReturn()
		t.scr.LineFeed()
	case 'M': // RI
		t.scr.ReverseIndex()
	case 'H': // HTS
		t.scr.SetTab(true)
	case '7': // DECSC
		t.scr.SaveCursor()
	case '8': // DECRC
		t.scr.RestoreCursor()
	case 'c': // RIS
		t.scr.Reset()
		t.lastRune = 0
		t.title = ""
	case '=': // DECKPAM
		t.scr.mode.AppKeypad = true
	case '>': // DECKPNM
		t.scr.mode.AppKeypad = false
	}
}

func (t *Terminal) CsiDispatch(params [][]uint16, intermediates []byte, ignore bool, r rune) {
	if ignore {
		return
	}
	// arg returns parameter i with a default, treating an omitted or
	// zero parameter as the default. Most CSI sequences define 0 to mean
	// "the default", which for counts is 1.
	arg := func(i, def int) int {
		if i >= len(params) || len(params[i]) == 0 || params[i][0] == 0 {
			return def
		}
		return int(params[i][0])
	}
	// argRaw keeps an explicit 0, which the erase sequences need.
	argRaw := func(i, def int) int {
		if i >= len(params) || len(params[i]) == 0 {
			return def
		}
		return int(params[i][0])
	}

	private := len(intermediates) > 0 && intermediates[0] == '?'
	if private {
		switch r {
		case 'h':
			t.setPrivateModes(params, true)
		case 'l':
			t.setPrivateModes(params, false)
		case 'n':
			t.deviceStatus(argRaw(0, 0), true)
		}
		return
	}
	// Other intermediates change the meaning of the final byte; the only
	// one handled is the space that makes 'q' DECSCUSR.
	if len(intermediates) > 0 {
		if intermediates[0] == ' ' && r == 'q' {
			t.setCursorStyle(argRaw(0, 0))
		}
		return
	}

	switch r {
	case '@': // ICH
		t.scr.InsertChars(arg(0, 1))
	case 'A': // CUU
		t.scr.MoveRel(0, -arg(0, 1))
	case 'B', 'e': // CUD, VPR
		t.scr.MoveRel(0, arg(0, 1))
	case 'C', 'a': // CUF, HPR
		t.scr.MoveRel(arg(0, 1), 0)
	case 'D': // CUB
		t.scr.MoveRel(-arg(0, 1), 0)
	case 'E': // CNL
		t.scr.MoveRel(0, arg(0, 1))
		t.scr.CarriageReturn()
	case 'F': // CPL
		t.scr.MoveRel(0, -arg(0, 1))
		t.scr.CarriageReturn()
	case 'G', '`': // CHA, HPA
		t.scr.MoveToCol(arg(0, 1) - 1)
	case 'H', 'f': // CUP, HVP
		t.scr.MoveTo(arg(1, 1)-1, arg(0, 1)-1)
	case 'I': // CHT
		t.scr.Tab(arg(0, 1))
	case 'J': // ED
		t.scr.EraseInDisplay(argRaw(0, 0))
	case 'K': // EL
		t.scr.EraseInLine(argRaw(0, 0))
	case 'L': // IL
		t.scr.InsertLines(arg(0, 1))
	case 'M': // DL
		t.scr.DeleteLines(arg(0, 1))
	case 'P': // DCH
		t.scr.DeleteChars(arg(0, 1))
	case 'S': // SU
		t.scr.ScrollUp(arg(0, 1))
	case 'T': // SD
		t.scr.ScrollDown(arg(0, 1))
	case 'X': // ECH
		t.scr.EraseChars(arg(0, 1))
	case 'Z': // CBT
		t.scr.BackTab(arg(0, 1))
	case 'b': // REP
		t.repeat(arg(0, 1))
	case 'd': // VPA
		t.scr.MoveToRow(arg(0, 1) - 1)
	case 'g': // TBC
		switch argRaw(0, 0) {
		case 0:
			t.scr.SetTab(false)
		case 3:
			t.scr.ClearTabs()
		}
	case 'h': // SM
		t.setModes(params, true)
	case 'l': // RM
		t.setModes(params, false)
	case 'm': // SGR
		t.applySGR(params)
	case 'n': // DSR
		t.deviceStatus(argRaw(0, 0), false)
	case 'r': // DECSTBM
		top := arg(0, 1) - 1
		bot := arg(1, t.scr.rows) - 1
		t.scr.SetScrollRegion(top, bot)
	case 's': // SCOSC
		t.scr.SaveCursor()
	case 'u': // SCORC
		t.scr.RestoreCursor()
	case 'c': // DA
		t.reply("\x1b[?6c") // a VT102, which is what most programs expect
	}
}

// repeat implements REP, which repeats the previous printable character.
func (t *Terminal) repeat(n int) {
	if t.lastRune == 0 {
		return
	}
	// A runaway count would let eight bytes of input buy a screenful of
	// work. xterm bounds REP by what is left of the current line, which
	// caps the amplification at the terminal width.
	cols, _ := t.scr.Size()
	x, _ := t.scr.CursorPos()
	n = min(n, max(cols-x, 1))
	w := grid.RuneWidth(t.lastRune)
	for i := 0; i < n; i++ {
		t.scr.print(t.lastRune, w)
	}
}

func (t *Terminal) setCursorStyle(n int) {
	switch n {
	case 0, 1, 2:
		t.scr.SetCursorStyle(grid.CursorBlock)
	case 3, 4:
		t.scr.SetCursorStyle(grid.CursorUnderline)
	case 5, 6:
		t.scr.SetCursorStyle(grid.CursorBar)
	}
}

// setModes handles the non-private SM and RM sequences.
func (t *Terminal) setModes(params [][]uint16, on bool) {
	for _, sub := range params {
		if len(sub) == 0 {
			continue
		}
		if sub[0] == 4 { // IRM
			t.scr.mode.Insert = on
		}
	}
}

// setPrivateModes handles DECSET and DECRST.
func (t *Terminal) setPrivateModes(params [][]uint16, on bool) {
	for _, sub := range params {
		if len(sub) == 0 {
			continue
		}
		switch sub[0] {
		case 1:
			t.scr.mode.AppCursor = on
		case 5:
			t.scr.mode.ReverseVid = on
			t.scr.touchAll()
		case 6:
			t.scr.cursor.Origin = on
			t.scr.MoveTo(0, 0)
		case 7:
			t.scr.mode.Wrap = on
		case 25:
			t.scr.mode.CursorVis = on
		case 1000:
			t.scr.mode.MouseClick = on
		case 1002:
			t.scr.mode.MouseDrag = on
		case 1003:
			t.scr.mode.MouseMotion = on
		case 1004:
			t.scr.mode.FocusEvents = on
		case 1006:
			t.scr.mode.MouseSGR = on
		case 47, 1047:
			// xterm clears the alternate screen for 1047 on the way
			// out, not on the way in; 47 does not clear at all.
			if !on && sub[0] == 1047 {
				t.scr.clearAlt()
			}
			t.scr.UseAltBuffer(on, false)
		case 1048:
			if on {
				t.scr.SaveCursor()
			} else {
				t.scr.RestoreCursor()
			}
		case 1049:
			// The combined form: save the cursor, switch, and on the way
			// back restore it. Doing both is what makes vim leave the
			// prompt exactly where it found it.
			if on {
				t.scr.SaveCursor()
				t.scr.UseAltBuffer(true, true)
			} else {
				t.scr.UseAltBuffer(false, false)
				t.scr.RestoreCursor()
			}
		case 2004:
			t.scr.mode.Bracketed = on
		}
	}
}

// deviceStatus answers DSR. 5 asks whether the terminal is healthy, 6
// asks where the cursor is.
func (t *Terminal) deviceStatus(n int, private bool) {
	switch n {
	case 5:
		if !private {
			t.reply("\x1b[0n")
		}
	case 6:
		x, _ := t.scr.CursorPos()
		y := t.scr.CursorRow()
		prefix := "\x1b["
		if private {
			prefix = "\x1b[?"
		}
		t.reply(prefix + strconv.Itoa(y+1) + ";" + strconv.Itoa(x+1) + "R")
	}
}

// reply sends bytes back to the program. The slice is freshly allocated
// on each call: reports are rare and short, and handing out a reused
// buffer means a callback that queues the slice sees it change under it.
func (t *Terminal) reply(s string) {
	if t.cb.Reply != nil {
		t.cb.Reply([]byte(s))
	}
}

func (t *Terminal) OscDispatch(params [][]byte, _ bool) {
	if len(params) == 0 {
		return
	}
	switch string(params[0]) {
	case "0", "1", "2":
		if len(params) < 2 {
			return
		}
		// OSC 1 sets the icon name, which has no separate home here.
		title := string(params[1])
		if title != t.title {
			t.title = title
			if t.cb.Title != nil {
				t.cb.Title(title)
			}
		}
	case "52":
		t.clipboard(params)
	}
}

// clipboard handles OSC 52, which lets a program put text on the system
// clipboard. Reads are deliberately not answered: replying would let any
// program that can write to the terminal exfiltrate the clipboard.
func (t *Terminal) clipboard(params [][]byte) {
	if len(params) < 3 || t.cb.ClipboardSet == nil {
		return
	}
	data := string(params[2])
	if data == "?" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(data))
	if err != nil {
		return
	}
	t.cb.ClipboardSet(string(raw))
}

// RenderLive copies the live screen into g, whatever the person at this
// machine has scrolled back to.
//
// Render shows what they are looking at, which may be history. Somebody
// being handed the screen from elsewhere wants the screen itself: what
// is on it now, and what the next output will land on.
func (t *Terminal) RenderLive(g *grid.Grid) { t.scr.RenderLive(g) }

// RenderUnder draws the ordinary screen that an alternate one is
// covering, and reports whether there was one.
func (t *Terminal) RenderUnder(g *grid.Grid) bool { return t.scr.RenderUnder(g) }

// Screenful is what the screen carries that its grid does not, for
// sending it somewhere else.
func (t *Terminal) Screenful() Screenful {
	return Screenful{
		Alt:       t.scr.OnAltBuffer(),
		Wrap:      t.scr.Wrap(),
		AppCursor: t.scr.AppCursor(),
		WrapNext:  t.scr.WrapNext(),
	}
}
