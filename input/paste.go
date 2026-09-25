package input

import "strings"

// Paste markers for DECSET 2004. A program that turns bracketed paste on
// is telling the terminal it can distinguish pasted text from typing, so
// it will not act on a newline in the middle of a paste.
const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

// EncodePaste appends pasted text as the bytes to send to the program.
//
// Two things happen to the text on the way. Newlines become carriage
// returns, because that is what the Enter key sends and what a shell's
// line editor expects. And control characters are dropped, because
// pasted text is data: an escape sequence hidden in it would otherwise
// be obeyed, which is how a copied web page can run a command.
//
// When bracketed is false the text still goes through, since that is
// what the user asked for, but a program that has not enabled bracketed
// paste will act on every newline in it.
func EncodePaste(text string, bracketed bool, dst []byte) []byte {
	if text == "" {
		return dst
	}
	if bracketed {
		dst = append(dst, pasteStart...)
	}
	dst = appendSafe(dst, text)
	if bracketed {
		dst = append(dst, pasteEnd...)
	}
	return dst
}

// appendSafe writes text with newlines normalised and controls removed.
func appendSafe(dst []byte, text string) []byte {
	var sb strings.Builder
	sb.Grow(len(text))
	var prev rune
	for _, r := range text {
		was := prev
		prev = r
		switch {
		case r == '\n' || r == '\r':
			// A CRLF pair must not become two returns, or a paste runs
			// every line twice. Only a return in the text pairs with
			// the newline after it: two newlines are a blank line.
			if r == '\n' && was == '\r' {
				continue
			}
			sb.WriteByte('\r')
		case r == '\t':
			sb.WriteByte('\t')
		case r < 0x20 || r == 0x7f:
			// Dropped: an escape sequence in pasted data would be obeyed.
		default:
			sb.WriteRune(r)
		}
	}
	return append(dst, sb.String()...)
}
