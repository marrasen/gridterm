package serve

import (
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text a far end chose is shown in three places -- a pane, a dialog and
// the console gridterm was started from -- and all three act on escape
// sequences. One rule, in one place, so they cannot drift apart:
//
//   - a line feed is kept, because the text is meant to be read on lines
//   - a carriage return goes, because a pane draws it as a jump to the
//     start of the line and would let the next word overwrite this one
//   - every other control character goes, C0 and C1 alike, ESC among
//     them
//   - a tab becomes a space, which is what it looks like anyway
//   - a byte that is not part of a character becomes U+FFFD, so a far
//     end cannot pick what the terminal does with it
//
// Exported because the window and the packages under it all show what
// far ends say, and a second copy of this is a second rule.

// Plain returns s with everything a terminal would act on taken out.
func Plain(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if keep := plainRune(r); keep >= 0 {
			b.WriteRune(keep)
		}
		s = s[size:]
	}
	return b.String()
}

// plainRune is one character as it may be shown, or -1 for one that is
// dropped. DecodeRune has already turned a byte that begins no character
// into utf8.RuneError, which is the replacement character itself.
func plainRune(r rune) rune {
	switch {
	case r == '\n':
		return r
	case r == '\t':
		return ' '
	case unicode.IsControl(r):
		return -1
	}
	return r
}

// PlainWriter is Plain over a stream, for text arriving in pieces.
//
// What the far end says about a session it would not start goes into the
// pane's own stream, and a pane parses that stream as a terminal. Left
// alone, a window at the other end could move this one's cursor,
// overwrite the lines above what it wrote, recolour them, or set the
// window's title -- and what it wrote would then read as this window's
// own words.
type PlainWriter struct {
	// To is where the text that survives goes.
	To io.Writer

	// part is the start of a character whose remaining bytes have not
	// arrived. It is never more than three bytes: anything that cannot
	// begin a character is decoded and dropped straight away.
	part []byte
}

// Write passes on everything a terminal would print rather than act on.
func (p *PlainWriter) Write(b []byte) (int, error) {
	took := len(b)
	if len(p.part) > 0 {
		b = append(p.part, b...)
		p.part = nil
	}
	out := make([]byte, 0, len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 && !utf8.FullRune(b) {
			// The rest of this character has not arrived. It is held
			// back rather than replaced, or a character split across two
			// writes would come out as two question marks.
			p.part = append(p.part, b...)
			break
		}
		if keep := plainRune(r); keep >= 0 {
			out = utf8.AppendRune(out, keep)
		}
		b = b[size:]
	}
	if len(out) == 0 {
		return took, nil
	}
	if _, err := p.To.Write(out); err != nil {
		return 0, err
	}
	return took, nil
}
