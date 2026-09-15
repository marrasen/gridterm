package ui

import "strings"

// cleanText makes a message fit to draw: a tab becomes a space and every
// other control character goes, because a far end can put anything in an
// error. Line breaks stay, so a message written as several lines keeps
// them.
func cleanText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteByte(' ')
		case r < ' ', r == 0x7f, r >= 0x80 && r <= 0x9f:
			// Dropped, the carriage return of a CRLF among them, which
			// leaves the line break behind it.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
