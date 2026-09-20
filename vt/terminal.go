// Package vt turns a terminal byte stream into a character grid. It
// wraps a DEC-compatible escape-sequence parser and drives a Screen,
// which holds the primary and alternate buffers, the cursor and the
// modes.
//
// Nothing here touches a GPU or a pty, so the whole emulator is testable
// by writing bytes in and reading cells out.
package vt

import (
	"bytes"
	"encoding/base64"
	"net/url"
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
	// CommandDone fires when the shell says a command finished, with the
	// exit status and whether the shell gave one.
	CommandDone func(status int, ok bool)
}

// Command is what the shell's OSC 133 and OSC 633 marks say about the
// command line. The zero value is a shell that has never sent one.
//
// A shell started inside the pane, over ssh or in a container, sends
// marks of its own. So this is what a shell reported, not what the
// command did.
type Command struct {
	// Integrated says at least one of the marks read here has arrived,
	// and any program that writes to the pane can set it. Everything
	// else here means nothing without it.
	Integrated bool

	// Running says a command is running, from its C mark to its D mark.
	// A full-screen program killed without restoring the ordinary screen
	// leaves this true for ever.
	Running bool

	// status is the exit status of the last command that finished, and
	// hasStatus says the shell gave one at all.
	status    int
	hasStatus bool

	// from is the line the last command's output began on, and hasFrom
	// says a command has started at all. It is where the shell put its C
	// mark, which is after the command line on every shell that sends
	// one.
	from    uint64
	hasFrom bool

	// Done counts the commands that have finished, however they ended:
	// with a D mark, or with the next prompt arriving while one was
	// running. It only moves forward, so a caller that reads it before
	// sending keys can tell a real finish from a Running that is stuck.
	Done uint64
}

// Exit returns the exit status of the last command that finished, and
// whether the shell gave one at all.
func (c Command) Exit() (status int, ok bool) { return c.status, c.hasStatus }

// Output returns the line the last command's output began on, and
// whether the shell has marked one.
//
// It is a line number of the screen it came from, which goes on naming
// that line as the screen scrolls. The command line itself is above it.
func (c Command) Output() (from uint64, ok bool) { return c.from, c.hasFrom }

// Terminal is a VT emulator: write bytes in, render cells out.
//
// A Terminal is not safe for concurrent use. Feed it from one goroutine
// and render from the same one, or hold a lock around both.
type Terminal struct {
	parser *vte.Parser
	scr    *Screen
	cb     Callbacks

	title string

	// dir is where the shell last said it was, from OSC 7, and empty
	// until one says. Its host is kept beside it: a shell on a machine
	// at the far end reports that machine's path, which means nothing
	// here.
	dir     string
	dirHost string

	// links are the hyperlinks OSC 8 has named, by the number cells
	// carry. byURL finds the number one already has, so a listing of
	// fifty links to the same page costs one entry.
	links []string
	byURL map[string]uint32

	// images are the pictures a program put in the output, oldest
	// first, each one holding the line it sits on.
	images []Image

	// cmd is what the shell's prompt marks have said so far.
	cmd Command

	// lastRune is the most recent printable character, which REP repeats.
	lastRune rune

	// long reads the sequences carrying a picture, which are longer
	// than the parser will hold.
	long longOSC
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

// Dir is where the program last said it was, and the machine it said
// it about. Both are empty until a shell sends OSC 7.
//
// The host is what the shell put in the URL, which is its own machine
// rather than this one. A caller that means to use the path locally
// has to decide whether it believes that name.
func (t *Terminal) Dir() (dir, host string) { return t.dir, t.dirHost }

// Command returns what the shell's prompt marks say about the command
// line, all from one moment so the parts cannot disagree.
func (t *Terminal) Command() Command { return t.cmd }

// Write feeds bytes to the emulator. It never returns an error: a
// terminal has no way to reject what a program sends it.
func (t *Terminal) Write(p []byte) (int, error) {
	t.long.feed(p, t.parser.Advance, t.longOSCDone)
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

// Hook takes the start of a DCS sequence, as Put takes its payload and
// Unhook its end. Nothing here implements one, but they must be consumed
// or the payload would be printed as text.
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
		t.dir, t.dirHost = "", ""
		t.links, t.byURL = nil, nil
		t.images = nil
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

// setCursorStyle handles DECSCUSR. The odd numbers blink and the even
// ones are steady; 0 is the default, a blinking block.
func (t *Terminal) setCursorStyle(n int) {
	switch n {
	case 0, 1, 2:
		t.scr.SetCursorStyle(grid.CursorBlock, n != 2)
	case 3, 4:
		t.scr.SetCursorStyle(grid.CursorUnderline, n == 3)
	case 5, 6:
		t.scr.SetCursorStyle(grid.CursorBar, n == 5)
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
	case "7":
		t.setDir(params)
	case "9":
		t.setDirPath(params)
	case "8":
		t.setLink(params)
	case "1338":
		t.setWirePic(params)
	case "1337":
		t.setImage(params)
	case "52":
		t.clipboard(params)
	case "133", "633":
		t.semanticPrompt(params)
	}
}

// MostLinks is how many hyperlinks a pane remembers.
//
// A page of output can name a great many, and each one is a string
// held for as long as the pane is. Past this, a new one is not taken
// and the text is drawn without a link: the words are still there to
// read and to copy.
const MostLinks = 4096

// setLink takes OSC 8, which is how a program puts a hyperlink under
// the text it is about to print.
//
// It is "OSC 8 ; params ; URI", and a URI of nothing ends the link.
// The parameters carry an id the program uses to join two runs of one
// link, which nothing here needs: what is under the cells is the
// address.
func (t *Terminal) setLink(params [][]byte) {
	if len(params) < 3 {
		// Not enough to be a link at all. The safe reading is the end
		// of one, which is what a program that sends a short one on
		// the way out means.
		t.scr.SetPenLink(0)
		return
	}
	// The address may hold a semicolon, and the parser cuts on those.
	uri := string(bytes.Join(params[2:], []byte(";")))
	t.scr.SetPenLink(t.linkID(uri))
}

// linkID is the number cells carry for an address, taking a new one
// the first time an address is seen.
func (t *Terminal) linkID(uri string) uint32 {
	uri = strings.TrimSpace(uri)
	if uri == "" || !safeLink(uri) {
		return 0
	}
	if id, had := t.byURL[uri]; had {
		return id
	}
	if len(t.links) >= MostLinks {
		return 0
	}
	if t.byURL == nil {
		t.byURL = map[string]uint32{}
	}
	t.links = append(t.links, uri)
	id := uint32(len(t.links))
	t.byURL[uri] = id
	return id
}

// LinkURL is the address a cell's link number names, and empty for a
// cell with no link or a number this terminal does not know.
func (t *Terminal) LinkURL(id uint32) string {
	if id == 0 || int(id) > len(t.links) {
		return ""
	}
	return t.links[id-1]
}

// safeLink reports whether an address is one worth offering to open.
//
// The program at the far end of a pane may be anything, and a link is
// a thing the user clicks. Only the schemes a browser is the right
// answer for, and nothing that could hand a local program a command
// line: no file, no javascript, no data.
func safeLink(uri string) bool {
	scheme, _, found := strings.Cut(uri, ":")
	if !found {
		return false
	}
	switch strings.ToLower(scheme) {
	case "http", "https", "mailto", "ftp", "ftps":
		return true
	}
	return false
}

// setDir takes OSC 7, which is how a shell says where it is.
//
// The payload is a file URL: "file://host/path", with the path
// percent-encoded. An empty payload means the shell no longer knows,
// which is what one sends before handing over to something else.
func (t *Terminal) setDir(params [][]byte) {
	if len(params) < 2 {
		return
	}
	// The path may hold a semicolon, and the parser cuts on those, so
	// what was sent is the rest of the parameters joined back up.
	raw := string(bytes.Join(params[1:], []byte(";")))
	if strings.TrimSpace(raw) == "" {
		t.dir, t.dirHost = "", ""
		return
	}
	dir, host, ok := parseFileURL(raw)
	if !ok {
		// A shell that sends something else is not one to believe. The
		// last directory stays rather than being replaced by nonsense.
		return
	}
	t.dir, t.dirHost = dir, host
}

// setDirPath takes OSC 9;9, which says where the shell is as a plain
// path rather than as a URL.
//
// It is what the Command Prompt can send: its prompt is built from the
// pieces cmd.exe substitutes, and none of them makes a URL. The path
// is the machine's own, so no host comes with it.
func (t *Terminal) setDirPath(params [][]byte) {
	if len(params) < 3 || string(params[1]) != "9" {
		return
	}
	// A path may hold a semicolon and the parser cuts on those, so what
	// was sent is the rest of the parameters joined back up.
	raw := string(bytes.Join(params[2:], []byte(";")))
	raw = strings.TrimRight(raw, "\r\n")
	// Windows Terminal quotes the path, and some shells copy that.
	raw = strings.Trim(raw, `"`)
	if strings.TrimSpace(raw) == "" {
		return
	}
	t.dir, t.dirHost = raw, ""
}

// parseFileURL reads the "file://host/path" a shell sends for OSC 7
// and returns the path and the host it named.
//
// A path with no scheme is taken as a path, because some shells send
// one, and a Windows path is unwound from the leading slash a URL
// puts in front of the drive letter.
func parseFileURL(raw string) (dir, host string, ok bool) {
	rest := raw
	if after, cut := strings.CutPrefix(raw, "file://"); cut {
		host, rest, _ = strings.Cut(after, "/")
		rest = "/" + rest
	} else if strings.Contains(raw, "://") {
		// Some other scheme. Not a directory on any machine.
		return "", "", false
	}
	unescaped, err := url.PathUnescape(rest)
	if err != nil {
		return "", "", false
	}
	unescaped = strings.TrimRight(unescaped, "\r\n")
	if unescaped == "" {
		return "", "", false
	}
	// "/C:/Users/x" is how a URL spells a Windows path. The slash in
	// front of the drive letter is the URL's, not the path's.
	if len(unescaped) > 2 && unescaped[0] == '/' && unescaped[2] == ':' {
		unescaped = unescaped[1:]
	}
	return unescaped, host, true
}

// semanticPrompt handles OSC 133 and VS Code's OSC 633: A is a prompt
// starting, B its end, C the start of the command's output, and D the
// command finishing. Extra parameters are ignored, as are marks sent on
// the alternate screen.
func (t *Terminal) semanticPrompt(params [][]byte) {
	if len(params) < 2 || t.scr.OnAltBuffer() {
		return
	}
	switch string(params[1]) {
	case "A", "B":
		// A prompt ends a running command, and says nothing about how it went. It
		// counts as finished all the same: a shell printing its next prompt is a
		// shell whose command is over, which is what a caller waiting for one is
		// asking about. A command stopped with ctrl+c ends this way.
		t.cmd.Integrated = true
		if t.cmd.Running {
			t.cmd.Running = false
			t.cmd.status, t.cmd.hasStatus = 0, false
			t.cmd.Done++
		}
	case "C":
		t.cmd.Integrated = true
		t.cmd.Running = true
		// Where the output starts, which is where the cursor is when the
		// shell says the command is about to run.
		_, row := t.scr.CursorPos()
		t.cmd.from, t.cmd.hasFrom = t.scr.LineNumber(row), true
	case "D":
		t.cmd.Integrated = true
		t.commandDone(params)
	}
}

// commandDone handles the D mark. A D with no command running finishes
// nothing and is ignored.
func (t *Terminal) commandDone(params [][]byte) {
	if !t.cmd.Running {
		return
	}
	t.cmd.Running = false
	t.cmd.status, t.cmd.hasStatus = 0, false
	// The status is optional, and a shell may send one that is not a number.
	if len(params) > 2 {
		if n, err := strconv.Atoi(string(params[2])); err == nil {
			t.cmd.status, t.cmd.hasStatus = n, true
		}
	}
	t.cmd.Done++
	if t.cb.CommandDone != nil {
		t.cb.CommandDone(t.cmd.status, t.cmd.hasStatus)
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

// RenderBack copies the screen as it stands back lines into history into
// g, for a reader asking for more than the screen holds.
func (t *Terminal) RenderBack(g *grid.Grid, back int) { t.scr.RenderBack(g, back) }

// History is how many lines have scrolled off the top and are kept.
func (t *Terminal) History() int { return t.scr.History() }

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
