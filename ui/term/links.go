package term

import "strings"

// linkSchemes are the starts of an address worth finding in ordinary
// output, and the ones a browser is the right answer for.
//
// The same set the emulator takes from OSC 8 and the window opens. A
// scheme found here is one a user clicks, so it is the same question.
var linkSchemes = []string{"https://", "http://", "ftps://", "ftp://", "mailto:"}

// findLink finds the address under a column of a row of text.
//
// The row is one rune per column, so a column and an index are the
// same thing and the answer can be given in columns. It reports the
// address and the columns it runs between, the second one past the
// end, and false when that column is not in one.
//
// This is a guess: a program that means a link says so with OSC 8,
// and everything else is text that happens to look like an address.
// So it is deliberately narrow. Nothing without a scheme is found,
// because "example.com" in a sentence is a word.
func findLink(row []rune, at int) (link string, from, to int, ok bool) {
	if at < 0 || at >= len(row) {
		return "", 0, 0, false
	}
	for _, scheme := range linkSchemes {
		start := 0
		for {
			i := indexFrom(row, scheme, start)
			if i < 0 {
				break
			}
			end := endOfLink(row, i+len(scheme))
			if end > i+len(scheme) && at >= i && at < end {
				return string(row[i:end]), i, end, true
			}
			start = i + 1
		}
	}
	return "", 0, 0, false
}

// indexFrom is where a string next appears in a row of runes, from a
// column on, and -1 when it does not.
func indexFrom(row []rune, want string, from int) int {
	w := []rune(want)
	for i := max(from, 0); i+len(w) <= len(row); i++ {
		if runesFold(row[i:i+len(w)], w) {
			return i
		}
	}
	return -1
}

// runesFold reports whether two runs of runes are the same ignoring
// case, which is how a scheme is written: HTTPS:// is one too.
func runesFold(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

// lower is the lower case of a letter, for the ASCII a scheme is
// written in.
func lower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// endOfLink is the column one past the end of an address that starts
// at from.
//
// It stops at the first character an address cannot hold, and then
// gives back the punctuation a sentence put after it: a full stop at
// the end of "see https://example.com." belongs to the sentence. A
// closing bracket is kept only when the address opened one, which is
// what makes a Wikipedia address work inside brackets.
func endOfLink(row []rune, from int) int {
	end := from
	for end < len(row) && inLink(row[end]) {
		end++
	}
	for end > from {
		last := row[end-1]
		if open, closing, bracket := opener(last); bracket {
			// A bracket the address itself opened is part of it, which
			// is what makes a Wikipedia address work, and what keeps
			// the brackets around an IPv6 host. One it did not open
			// belongs to whatever was written around it.
			if countRune(row[from:end], open) >= countRune(row[from:end], closing) {
				break
			}
			end--
			continue
		}
		if !strings.ContainsRune(`.,;:!?'"`+"`", last) {
			break
		}
		end--
	}
	return end
}

// opener is the bracket that opens a closing one, and whether the
// character is a closing bracket at all.
func opener(r rune) (open, closing rune, ok bool) {
	switch r {
	case ')':
		return '(', ')', true
	case ']':
		return '[', ']', true
	case '}':
		return '{', '}', true
	}
	return 0, 0, false
}

// countRune is how many times a rune appears in a run of them.
func countRune(row []rune, want rune) int {
	n := 0
	for _, r := range row {
		if r == want {
			n++
		}
	}
	return n
}

// inLink reports whether a character can be part of an address.
//
// Anything printable that is not a space and not a quote or a
// bracket-like thing a sentence would put around one. Deliberately
// generous inside the address and strict at its edges, because
// endOfLink gives the edges back.
func inLink(r rune) bool {
	switch {
	case r <= ' ', r == 0x7f:
		return false
	case r == '"', r == '\'', r == '`':
		return false
	case r == '<', r == '>':
		// Never in an address, and the classic way to write one inside
		// a sentence.
		return false
	}
	return true
}
