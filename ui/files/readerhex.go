package files

import (
	"fmt"
	"strings"
)

// hexRow is how many bytes one line of the hex view holds. Sixteen, the
// way every hex dump since the seventies has been laid out.
const hexRow = 16

// Hex sets whether the file is shown as bytes rather than as lines.
//
// The lines are built from the bytes the reader already holds, so
// turning it on costs one pass over the file and nothing after that.
func (r *Reader) Hex(on bool) {
	if on == r.hex {
		return
	}
	r.hex = on
	r.top, r.left = 0, 0
	r.stuck = false
	r.remake()
}

// Hexed reports whether the file is being shown as bytes.
func (r *Reader) Hexed() bool { return r.hex }

// remake builds what is shown from what was read.
func (r *Reader) remake() {
	// The lines are not the lines they were, so the match is not on the
	// line it was on either.
	r.found = -1
	if !r.hex {
		r.shown = r.lines
		r.wideOf = -1
		return
	}
	r.shown = hexDump(r.lines)
	r.wideOf = -1
}

// hexDump lays lines out as bytes: the offset, sixteen bytes, and the
// printable characters.
//
// The lines are joined back together with the newlines that split them,
// because the offsets have to be the offsets in the file. The last line
// gets none: a reader cannot tell a file that ended in a newline from
// one that did not, and guessing wrong would move every offset by one.
func hexDump(lines []string) []string {
	var body strings.Builder
	for i, line := range lines {
		if i > 0 {
			body.WriteByte('\n')
		}
		body.WriteString(line)
	}
	raw := body.String()

	out := make([]string, 0, len(raw)/hexRow+1)
	for at := 0; at < len(raw); at += hexRow {
		end := min(at+hexRow, len(raw))
		out = append(out, hexLine(at, raw[at:end]))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// hexLine is one line of the dump.
func hexLine(at int, chunk string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%08x  ", at)
	for i := 0; i < hexRow; i++ {
		if i == hexRow/2 {
			// A gap down the middle, so a byte can be counted to by
			// eye rather than one at a time.
			b.WriteByte(' ')
		}
		if i < len(chunk) {
			fmt.Fprintf(&b, "%02x ", chunk[i])
			continue
		}
		b.WriteString("   ")
	}
	b.WriteString(" |")
	for i := 0; i < len(chunk); i++ {
		b.WriteRune(printable(rune(chunk[i])))
	}
	b.WriteByte('|')
	return b.String()
}

// printable is what a byte is shown as beside the numbers: itself when
// it is an ordinary character, and a full stop when it is not.
//
// One column per byte, so the characters line up under the numbers. A
// byte that is part of a longer character is a full stop like any other
// byte that is not one on its own.
func printable(r rune) rune {
	if r < ' ' || r >= 0x7f {
		// Below a space is a control character, and 0x7f is delete,
		// which is one too. Above that is a byte of a longer character,
		// and a piece of one is not a character either.
		return '.'
	}
	return r
}
