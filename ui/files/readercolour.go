package files

import (
	"path"
	"strings"

	"github.com/marrasen/gridterm/grid"
)

// run is a stretch of one line drawn in one colour, ending at end bytes
// into the line.
type run struct {
	end  int
	col  colour
	attr grid.Attr
}

// colour names one of the handful a reader draws with, rather than a
// colour itself: the reader turns it into one from its own style, so a
// file reads in the window's own scheme.
type colour uint8

const (
	// colourPlain is the reader's ordinary foreground.
	colourPlain colour = iota

	// colourNote is a comment, or anything else beside the point.
	colourNote

	// colourText is a string, a heading's text, or a quote.
	colourText

	// colourMark is punctuation that carries meaning: a heading's
	// hashes, a bullet, a number.
	colourMark
)

// colourer turns a line into runs. A nil one leaves the file plain.
type colourer func(line string, into []run) []run

// colourerFor picks how a file is drawn, by what it is called, and
// gives nil for a name it does not know.
func colourerFor(name string) colourer {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdown":
		return markdownRuns
	case ".json", ".jsonl", ".ndjson", ".geojson":
		return jsonRuns
	case ".go", ".c", ".h", ".cc", ".cpp", ".hpp", ".java", ".js", ".ts",
		".jsx", ".tsx", ".rs", ".swift", ".kt", ".cs", ".php", ".scala":
		return runsWith(slashComments)
	case ".py", ".rb", ".sh", ".bash", ".zsh", ".pl", ".yaml", ".yml",
		".toml", ".ini", ".conf", ".cfg", ".tf", ".r":
		return runsWith(hashComments)
	case ".sql", ".lua", ".hs", ".elm", ".ada":
		return runsWith(dashComments)
	}
	return nil
}

// commentStart is how a line comment starts, per family of languages.
type commentStart uint8

const (
	slashComments commentStart = iota
	hashComments
	dashComments
)

// begins reports whether a comment starts at i.
func (c commentStart) begins(line string, i int) bool {
	switch c {
	case slashComments:
		return strings.HasPrefix(line[i:], "//")
	case hashComments:
		return line[i] == '#'
	case dashComments:
		return strings.HasPrefix(line[i:], "--")
	}
	return false
}

// runsWith colours comments, strings and numbers in a family of
// languages that spells them the same way.
//
// One line at a time, so a string or a comment that runs over the end of
// a line is coloured to the end of that line and no further.
func runsWith(comments commentStart) colourer {
	return func(line string, into []run) []run {
		out := into[:0]
		at := 0
		for i := 0; i < len(line); {
			if comments.begins(line, i) {
				out = add(out, at, i, colourPlain, 0)
				return add(out, i, len(line), colourNote, 0)
			}
			if q := line[i]; q == '"' || q == '\'' || q == '`' {
				end, closed := closes(line, i, q)
				// An apostrophe is far commoner than a one-character
				// string, so a single quote counts only when the line
				// closes it.
				if q == '\'' && !closed {
					i++
					continue
				}
				out = add(out, at, i, colourPlain, 0)
				out = add(out, i, end, colourText, 0)
				i, at = end, end
				continue
			}
			if isDigit(line[i]) && (i == 0 || !wordish(line[i-1]) && !inRune(line[i-1])) {
				end := i
				for end < len(line) && wordish(line[end]) {
					end++
				}
				out = add(out, at, i, colourPlain, 0)
				out = add(out, i, end, colourMark, 0)
				i, at = end, end
				continue
			}
			i++
		}
		return add(out, at, len(line), colourPlain, 0)
	}
}

// closes finds the end of a quoted stretch, past any escaped quote. It
// reports whether the line closed it, and gives the end of the line when
// it did not.
func closes(line string, from int, q byte) (end int, closed bool) {
	for i := from + 1; i < len(line); i++ {
		if line[i] == '\\' && q != '`' {
			i++
			continue
		}
		if line[i] == q {
			return i + 1, true
		}
	}
	return len(line), false
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// inRune reports whether a byte is part of a character rather than a
// character on its own, so a digit after a letter outside ASCII reads as
// part of a name and not as a number.
func inRune(b byte) bool { return b >= 0x80 }

// wordish reports whether a byte belongs to a word, for telling a number
// on its own from the digits inside a name.
func wordish(b byte) bool {
	return isDigit(b) || b == '.' || b == '_' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// jsonRuns colours a line of JSON: a name in bold, a value's string in
// the colour a string takes, and a number or a keyword marked.
//
// One line at a time, the way the language colourers work, so a string
// that runs over the end of a line is coloured to the end of that line
// and no further.
func jsonRuns(line string, into []run) []run {
	out := into[:0]
	at := 0
	for i := 0; i < len(line); {
		if line[i] == '"' {
			end, closed := closes(line, i, '"')
			attr := grid.Attr(0)
			if closed && namesAValue(line, end) {
				attr = grid.AttrBold
			}
			out = add(out, at, i, colourPlain, 0)
			out = add(out, i, end, colourText, attr)
			i, at = end, end
			continue
		}
		if end := literal(line, i); end > i {
			out = add(out, at, i, colourPlain, 0)
			out = add(out, i, end, colourMark, 0)
			i, at = end, end
			continue
		}
		i++
	}
	return add(out, at, len(line), colourPlain, 0)
}

// namesAValue reports whether a colon follows the string that ended at
// end, which is what makes that string a name rather than a value.
func namesAValue(line string, end int) bool {
	for i := end; i < len(line); i++ {
		if line[i] == ':' {
			return true
		}
		if line[i] != ' ' && line[i] != '\t' {
			return false
		}
	}
	return false
}

// literal is how far a number, true, false or null runs from i, and i
// when none of them starts there.
func literal(line string, i int) int {
	if i > 0 && (wordish(line[i-1]) || inRune(line[i-1])) {
		return i
	}
	if end := number(line, i); end > i {
		return end
	}
	return keyword(line, i)
}

// number is how far a JSON number runs from i, and i when one does not
// start there.
func number(line string, i int) int {
	end := i
	if end < len(line) && line[end] == '-' {
		end++
	}
	if end >= len(line) || !isDigit(line[end]) {
		return i
	}
	for end < len(line) && (isDigit(line[end]) || line[end] == '.') {
		end++
	}
	if end < len(line) && (line[end] == 'e' || line[end] == 'E') {
		end++
		if end < len(line) && (line[end] == '+' || line[end] == '-') {
			end++
		}
		for end < len(line) && isDigit(line[end]) {
			end++
		}
	}
	return end
}

// keyword is how far true, false or null runs from i, and i when none of
// them starts there.
func keyword(line string, i int) int {
	for _, word := range []string{"true", "false", "null"} {
		if !strings.HasPrefix(line[i:], word) {
			continue
		}
		if end := i + len(word); end == len(line) || !wordish(line[end]) {
			return end
		}
	}
	return i
}

// markdownRuns colours a line of markdown.
//
// Line by line, which is what markdown mostly is: a heading, a bullet, a
// quote or a fence is decided by how the line starts.
func markdownRuns(line string, into []run) []run {
	out := into[:0]
	trimmed := strings.TrimLeft(line, " \t")
	indent := len(line) - len(trimmed)
	switch {
	case heading(trimmed) > 0:
		// The hashes marked, the words bold.
		n := heading(trimmed)
		out = add(out, 0, indent+n, colourMark, 0)
		return add(out, indent+n, len(line), colourText, grid.AttrBold)
	case strings.HasPrefix(trimmed, ">"):
		return add(out, 0, len(line), colourNote, grid.AttrItalic)
	case strings.HasPrefix(trimmed, "```"), strings.HasPrefix(trimmed, "~~~"):
		return add(out, 0, len(line), colourMark, 0)
	case isRule(trimmed):
		return add(out, 0, len(line), colourMark, 0)
	default:
		if n := bullet(trimmed); n > 0 {
			out = add(out, 0, indent+n, colourMark, 0)
			return inlineRuns(line, indent+n, out)
		}
	}
	return inlineRuns(line, 0, out)
}

// heading is how many bytes of a line are its heading hashes, and zero
// when it is not a heading. A hash with no space after it is a fragment
// or a colour, not a heading.
func heading(s string) int {
	n := len(s) - len(strings.TrimLeft(s, "#"))
	if n == 0 || n > 6 {
		return 0
	}
	if len(s) > n && s[n] != ' ' {
		return 0
	}
	return n
}

// isRule reports whether a line is a horizontal rule.
func isRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	return strings.Trim(s, string(c)+" ") == ""
}

// bullet is how many bytes of a line are its list marker, and zero when
// it is not a list.
func bullet(s string) int {
	if len(s) > 1 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' ' {
		return 2
	}
	// A numbered item: digits, then a full stop or a bracket, then a
	// space.
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i > 0 && i+1 < len(s) && (s[i] == '.' || s[i] == ')') && s[i+1] == ' ' {
		return i + 2
	}
	return 0
}

// inlineRuns colours what markdown marks inside a line: code spans in
// backticks, and emphasis in stars or underscores.
func inlineRuns(line string, from int, out []run) []run {
	at := from
	for i := from; i < len(line); {
		switch {
		case line[i] == '`':
			end, _ := closes(line, i, '`')
			out = add(out, at, i, colourPlain, 0)
			out = add(out, i, end, colourText, 0)
			i, at = end, end
			continue
		case strings.HasPrefix(line[i:], "**"), strings.HasPrefix(line[i:], "__"):
			if end := marked(line, i, line[i:i+2]); end > i && opens(line, i) {
				out = add(out, at, i, colourPlain, 0)
				out = add(out, i, end, colourPlain, grid.AttrBold)
				i, at = end, end
				continue
			}
		case line[i] == '*', line[i] == '_':
			if end := marked(line, i, line[i:i+1]); end > i && opens(line, i) {
				out = add(out, at, i, colourPlain, 0)
				out = add(out, i, end, colourPlain, grid.AttrItalic)
				i, at = end, end
				continue
			}
		}
		i++
	}
	return add(out, at, len(line), colourPlain, 0)
}

// opens reports whether a mark at i starts emphasis rather than sitting
// inside a word, which is what an underscore in snake_case does.
func opens(line string, i int) bool {
	return i == 0 || !wordish(line[i-1]) && !inRune(line[i-1])
}

// marked finds the end of a stretch between two of the same mark, and
// returns from when there is no closing one on this line.
func marked(line string, from int, mark string) int {
	rest := line[from+len(mark):]
	if i := strings.Index(rest, mark); i > 0 {
		return from + len(mark) + i + len(mark)
	}
	return from
}

// add puts a run on the end, leaving out the empty ones and joining one
// that carries on from the last.
func add(out []run, from, to int, col colour, attr grid.Attr) []run {
	if to <= from {
		return out
	}
	if n := len(out); n > 0 && out[n-1].col == col && out[n-1].attr == attr {
		out[n-1].end = to
		return out
	}
	return append(out, run{end: to, col: col, attr: attr})
}
