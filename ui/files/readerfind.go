package files

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// asking is what a reader is waiting to be told, along the bottom row
// where the bar of keys usually is.
type asking uint8

const (
	// askingNothing is the ordinary state: the bar is the bar.
	askingNothing asking = iota

	// askingFind is a search, started with "/", and askingGoTo a line
	// number, started with ":". Both are what less asks with.
	askingFind
	askingGoTo
)

// Asking reports what the reader is waiting to be told, for a test and
// for whatever draws the bar.
func (r *Reader) Asking() (what string, typed string, on bool) {
	switch r.asking {
	case askingFind:
		return "/", r.typed, true
	case askingGoTo:
		return ":", r.typed, true
	}
	return "", "", false
}

// Find is what the reader is looking for, and is empty when it is
// looking for nothing.
func (r *Reader) Find() string { return r.finding }

// ask starts a question along the bottom row.
func (r *Reader) ask(what asking) {
	r.asking, r.typed = what, ""
}

// answer takes what was typed and does it, and reports what went wrong
// in a way the pane can show.
func (r *Reader) answer() {
	what, typed := r.asking, r.typed
	r.asking, r.typed = askingNothing, ""
	switch what {
	case askingFind:
		if typed == "" {
			// An empty search means the one before it, the way less
			// repeats the last pattern.
			r.FindNext(false)
			return
		}
		r.finding = typed
		r.findFrom(r.top, false, true)
	case askingGoTo:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			r.said = "that is not a line number: " + typed
			return
		}
		r.GoToLine(n)
	}
}

// GoToLine puts a line at the top of the pane, counting from one the way
// every other program that numbers lines does.
func (r *Reader) GoToLine(n int) {
	if len(r.shown) == 0 {
		return
	}
	r.top = max(min(n, len(r.shown))-1, 0)
	r.clampTop()
	r.stuck = r.follow && r.AtEnd()
}

// FindNext moves to the next line holding what was searched for, or the
// one before it when back is true.
//
// It steps from the last match while that is still on screen, so two
// matches on one screenful are stepped between. The match is what the
// user is reading; the top of the pane is only where it happens to sit.
func (r *Reader) FindNext(back bool) {
	if r.finding == "" {
		r.said = "there is nothing to look for yet"
		return
	}
	at := r.top
	if r.onScreen(r.found) {
		at = r.found
	}
	from := at + 1
	if back {
		from = at - 1
	}
	r.findFrom(from, back, false)
}

// onScreen reports whether a line is one of those drawn.
func (r *Reader) onScreen(at int) bool {
	rows := r.rows()
	return rows > 0 && at >= r.top && at < r.top+rows
}

// findFrom looks from a line on, wrapping once, and says so when there
// is nothing to find.
//
// here says the line the reader is already on counts as a match, which
// is what a fresh search wants and a repeat does not.
func (r *Reader) findFrom(from int, back, here bool) {
	if r.finding == "" || len(r.shown) == 0 {
		return
	}
	if here {
		from = r.top
	}
	want := r.finding
	step := 1
	if back {
		step = -1
	}
	// Every line once, starting where it was asked to and coming back
	// round to it, so a pattern on the line above is found rather than
	// reported missing.
	at := from
	for range len(r.shown) {
		at = (at%len(r.shown) + len(r.shown)) % len(r.shown)
		if i, _ := foldIndex(r.shown[at], want); i >= 0 {
			r.found = at
			r.showLine(at)
			r.said = ""
			return
		}
		at += step
	}
	r.said = "nothing else says " + r.finding
}

// showLine brings a line into view, leaving it where it is when it is
// already on screen: a search that jumped every time would move the
// page under the reader for a match they can already see.
func (r *Reader) showLine(at int) {
	rows := r.rows()
	switch {
	case rows <= 0, at < r.top, at >= r.top+rows:
		// Not on screen. A third of the way down, so what is around it
		// is readable rather than the match sitting on the top edge.
		r.top = max(at-max(rows/3, 0), 0)
	default:
		return
	}
	r.clampTop()
	r.stuck = r.follow && r.AtEnd()
}

// askKey takes a key while the reader is waiting to be told something.
func (r *Reader) askKey(ev input.Event) (bool, error) {
	if ev.Kind == input.Text && ev.NormalText && ev.Rune >= ' ' {
		r.typed += string(ev.Rune)
		return true, nil
	}
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return true, nil
	}
	switch ev.Key {
	case input.KeyEnter:
		r.answer()
	case input.KeyEscape:
		r.asking, r.typed = askingNothing, ""
	case input.KeyBackspace:
		if r.typed != "" {
			runes := []rune(r.typed)
			r.typed = string(runes[:len(runes)-1])
			return true, nil
		}
		// Taking back the last of it takes back the question, which is
		// what backspacing out of a prompt does everywhere else.
		r.asking = askingNothing
	}
	return true, nil
}

// paintAsking writes the question along the bottom row, in place of the
// bar of keys.
func (r *Reader) paintAsking(v grid.View, y, cols int) {
	what, typed, _ := r.Asking()
	v.SetString(0, y, grid.TrimTail(what+typed, cols), r.Style.FG, r.Style.BG, 0)
	// The cursor after what has been typed, so it reads as something
	// being typed rather than as a line of text.
	if at := grid.StringWidth(what + typed); at < cols {
		v.SetCursor(grid.Cursor{X: at, Y: y, Visible: true, Style: grid.CursorBar})
	}
}

// findsOn returns where the pattern sits in a line, in columns, for
// marking the match out. It returns nothing when the line has none.
func (r *Reader) findsOn(line string) (at, width int, ok bool) {
	i, n := foldIndex(line, r.finding)
	if i < 0 {
		return 0, 0, false
	}
	return grid.StringWidth(line[:i]), grid.StringWidth(line[i : i+n]), true
}

// foldIndex returns where want appears in line ignoring case, in bytes
// of line, along with how many bytes of line the match takes. It returns
// -1 when there is no match.
//
// Measured in the line itself rather than in a lowercased copy of it,
// because lowercasing can change how many bytes a character takes: "Ⱥ"
// grows from two to three, and an offset into the copy then runs past
// the end of the line it is used to cut.
func foldIndex(line, want string) (at, n int) {
	if want == "" {
		return -1, 0
	}
	for i := range line {
		if n := foldPrefix(line[i:], want); n > 0 {
			return i, n
		}
	}
	return -1, 0
}

// foldPrefix returns how many bytes of s match want ignoring case, and
// zero when it does not match.
func foldPrefix(s, want string) int {
	i, j := 0, 0
	for j < len(want) {
		if i >= len(s) {
			return 0
		}
		a, na := utf8.DecodeRuneInString(s[i:])
		b, nb := utf8.DecodeRuneInString(want[j:])
		if unicode.ToLower(a) != unicode.ToLower(b) {
			return 0
		}
		i, j = i+na, j+nb
	}
	return i
}
