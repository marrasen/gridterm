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
	// Then a listening address written without one, which is what a
	// development server prints.
	return findService(row, at)
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

// findPathText is the run of characters under a column that could
// name a file or a directory, with a line reference after it read off
// and given back separately.
//
// Deliberately generous: whether it names anything is settled by
// looking on the disk, not by guessing here, so a run that is not a
// path simply is not one. That is what makes this safer than finding
// an address, where nothing can check.
func findPathText(row []rune, at int) (text string, line, from, to int, ok bool) {
	if at < 0 || at >= len(row) || !inPath(row[at]) {
		return "", 0, 0, 0, false
	}
	from = at
	for from > 0 && inPath(row[from-1]) {
		from--
	}
	// A bracket a sentence opened in front of the path is not part of
	// it. The end is trimmed by endOfLink, and this is the same job
	// at the other end: an address does not need it, because the scan
	// for one starts at its scheme.
	for from < at && startsNothing(row[from]) {
		from++
	}
	to = endOfLink(row, from)
	if to <= from {
		return "", 0, 0, 0, false
	}
	text = string(row[from:to])
	text, line = splitLineRef(text)
	if text == "" {
		return "", 0, 0, 0, false
	}
	return text, line, from, from + len([]rune(text)), true
}

// splitLineRef takes a "file:42" or "file:42:8" apart, which is how
// every compiler and linter names a place in a file.
//
// The column is read off and thrown away: a viewer goes to a line.
func splitLineRef(text string) (path string, line int) {
	path = text
	for range 2 {
		head, tail, found := cutLast(path, ':')
		if !found || tail == "" || !allDigits(tail) {
			break
		}
		// A Windows drive letter is not a line number: "C:" has one
		// character in front of the colon and nothing useful after.
		if len(head) <= 1 {
			break
		}
		// The last one taken off is the line: "file:42:8" is a line
		// and a column, and the column comes off first.
		line = atoi(tail)
		path = head
	}
	return path, line
}

// cutLast splits a string at the last instance of a character.
func cutLast(s string, sep rune) (head, tail string, found bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if rune(s[i]) == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// allDigits reports whether a string is one or more digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// atoi reads a small count, and answers zero for anything it will not
// take: a line number past this is not one worth going to.
func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
		if n > 1<<24 {
			return 0
		}
	}
	return n
}

// startsNothing reports whether a character is one a path never
// starts with, whatever a sentence put it there for.
func startsNothing(r rune) bool {
	return strings.ContainsRune(`([{)]},;:!?`, r)
}

// inPath reports whether a character can be part of a path.
//
// Not a space, because a path with one in it cannot be told from two
// words, and not the quotes and brackets that surround one in prose.
// Everything else is allowed and the disk decides.
func inPath(r rune) bool {
	switch {
	case r <= ' ', r == 0x7f:
		return false
	case r == '"', r == '\'', r == '`':
		return false
	case r == '<', r == '>', r == '|', r == '*', r == '?':
		return false
	}
	return true
}

// loopbackNames are the hosts a program prints when it is listening
// on the machine it runs on.
//
// 0.0.0.0 and :: mean every address rather than the loopback one, and
// a program that prints one is still saying "reach me on this
// machine, on this port", which is what the link is for.
var loopbackNames = []string{"localhost", "127.0.0.1", "0.0.0.0", "[::1]", "[::]"}

// findService finds a listening address written without a scheme,
// such as the "localhost:5173" a development server prints.
//
// Narrow on purpose. Only a host that means the machine the program is
// running on, only with a port after it, and the address a browser
// would use comes back with a scheme in front so that everything
// downstream sees an ordinary link.
func findService(row []rune, at int) (link string, from, to int, ok bool) {
	if at < 0 || at >= len(row) {
		return "", 0, 0, false
	}
	for _, name := range loopbackNames {
		start := 0
		for {
			i := indexFrom(row, name, start)
			if i < 0 {
				break
			}
			start = i + 1
			// A host is a word of its own: "mylocalhost:80" is not one.
			if i > 0 && inPath(row[i-1]) {
				continue
			}
			end, good := endOfService(row, i+len([]rune(name)))
			if !good || at < i || at >= end {
				continue
			}
			return "http://" + string(row[i:end]), i, end, true
		}
	}
	return "", 0, 0, false
}

// endOfService is where a listening address ends, given where its host
// ends, and whether what follows the host is a port at all.
//
// The port, and then whatever path follows it: a server prints
// "localhost:5173/app" as readily as "localhost:5173".
func endOfService(row []rune, after int) (int, bool) {
	if after >= len(row) || row[after] != ':' {
		return 0, false
	}
	end := after + 1
	for end < len(row) && row[end] >= '0' && row[end] <= '9' {
		end++
	}
	if end == after+1 || end-after > 6 {
		// No port, or more digits than a port has.
		return 0, false
	}
	if end < len(row) && row[end] == '/' {
		return endOfLink(row, end), true
	}
	return end, true
}
