package ui

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/marrasen/gridterm/grid"
)

// Match is one command a query found, and where in its title it matched.
type Match struct {
	Command Command

	// At holds the index of every title rune the query matched, so the
	// matched letters can be picked out when the title is drawn.
	At []int

	score int
}

// Scores for what makes one match better than another. A query is
// usually the first letters of words, or the start of one word, so both
// are worth more than a letter found somewhere in the middle.
//
// There is no score for matching at all: a match uses every letter of
// the query or it is not a match, so a flat per-letter score would be
// the same constant everywhere and could never change an order.
const (
	scoreConsecutive = 8 // the letter after the one before it
	scoreWordStart   = 9 // the first letter of a word
	scoreLeading     = 4 // at the very start of the title
)

// MatchCommands returns the commands whose titles contain the query as a
// subsequence, best first.
//
// An empty query matches everything, which is what makes a palette open
// showing the whole list rather than nothing.
func MatchCommands(cmds []Command, query string) []Match {
	out := make([]Match, 0, len(cmds))
	for _, cmd := range cmds {
		at, score, ok := matchTitle(query, cmd.Title)
		if !ok {
			// Another word it answers to. Nothing is marked on the
			// title, because the match is not in it.
			if !answersTo(query, cmd.AlsoFind) {
				continue
			}
			at, score = nil, worstMatch
		}
		out = append(out, Match{Command: cmd, At: at, score: score})
	}
	slices.SortStableFunc(out, func(a, b Match) int {
		if n := cmp.Compare(b.score, a.score); n != 0 {
			return n
		}
		// A shorter title with the same score is the more likely answer:
		// the query is a larger part of it.
		if n := cmp.Compare(grid.StringWidth(a.Command.Title),
			grid.StringWidth(b.Command.Title)); n != 0 {
			return n
		}
		return cmp.Compare(a.Command.ID, b.Command.ID)
	})
	return out
}

// worstMatch is the score a command found by one of its other words
// gets, so it sorts below everything found by its title.
const worstMatch = -1 << 30

// answersTo reports whether a query finds one of a command's other
// words.
func answersTo(query string, also []string) bool {
	for _, word := range also {
		if _, _, ok := matchTitle(query, word); ok {
			return true
		}
	}
	return false
}

// matchTitle finds the query in a title as a subsequence, ignoring case,
// and scores how good a match it is.
func matchTitle(query, title string) (at []int, score int, ok bool) {
	runes := []rune(title)
	want := []rune(strings.ToLower(query))
	if len(want) == 0 {
		return nil, 0, true
	}

	last := -2
	next := 0
	for i, r := range runes {
		if next >= len(want) {
			break
		}
		if unicode.ToLower(r) != want[next] {
			continue
		}
		at = append(at, i)
		switch {
		case i == last+1:
			score += scoreConsecutive
		case wordStart(runes, i):
			score += scoreWordStart
		}
		if i == 0 {
			score += scoreLeading
		}
		last = i
		next++
	}
	if next < len(want) {
		return nil, 0, false
	}
	return at, score, true
}

// wordStart reports whether the rune at i begins a word: the first one,
// one after a separator, or a capital in the middle of a word.
func wordStart(runes []rune, i int) bool {
	if i == 0 {
		return true
	}
	prev := runes[i-1]
	if unicode.IsSpace(prev) || prev == '.' || prev == '-' || prev == '_' || prev == '/' {
		return true
	}
	return unicode.IsUpper(runes[i]) && !unicode.IsUpper(prev)
}
