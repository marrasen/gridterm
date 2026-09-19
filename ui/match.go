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

	// scoreRun is for the whole query landing in one unbroken run, and
	// scoreWholeWord for that run being a word of the title on its own.
	//
	// Somebody who types a word wants the thing named after it. Looking
	// for "WSL" used to put "Write a starting keyboard shortcuts file"
	// above "New pane on Ubuntu (WSL)", because scattered letters across
	// three words scored more word starts than three letters together.
	scoreRun       = 20
	scoreWholeWord = 40
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

// noPlace marks a letter that cannot go in a place, far enough below any
// real score that nothing climbs back out of it.
const noPlace = -(1 << 31)

// matchTitle finds the query in a title as a subsequence, ignoring case,
// and scores how good a match it is.
//
// The best placement rather than the leftmost one. "wsl" is in "New pane
// on Ubuntu (WSL)" twice over: the w of "New" with the s and l of "WSL",
// and the three letters of "WSL" together. The second is the one the
// user meant, and a matcher that took the first letter it saw could
// never find it -- so it both ranked the title too low and marked the
// wrong letters on it.
//
// One table of the best score for each letter of the query in each place
// in the title, filled left to right, and then the trail walked back for
// where the letters went.
func matchTitle(query, title string) (at []int, score int, ok bool) {
	runes := []rune(title)
	want := []rune(strings.ToLower(query))
	if len(want) == 0 {
		return nil, 0, true
	}
	if len(want) > len(runes) {
		return nil, 0, false
	}
	places, from := placeQuery(runes, want)

	// Where the last letter of the query went best.
	end, endScore := -1, noPlace
	for i, got := range places[len(want)-1] {
		if got > endScore {
			end, endScore = i, got
		}
	}
	if end < 0 {
		return nil, 0, false
	}
	at = make([]int, len(want))
	for j := len(want) - 1; j >= 0; j-- {
		at[j] = end
		end = from[j][end]
	}
	return at, endScore + runBonus(runes, at), true
}

// placeQuery scores every place each letter of the query could go, and
// records where the letter before it went.
//
// places[j][i] is the best a placement of the first j+1 letters can
// score with letter j on the rune at i, and noPlace when that letter
// cannot go there at all.
func placeQuery(runes, want []rune) (places, from [][]int) {
	places = make([][]int, len(want))
	from = make([][]int, len(want))
	for j := range want {
		places[j] = make([]int, len(runes))
		from[j] = make([]int, len(runes))
		for i := range runes {
			places[j][i], from[j][i] = noPlace, -1
		}
	}
	for j, letter := range want {
		// The best place the letter before this one could have gone,
		// among the runes already passed. Kept as the row is walked, so
		// a letter never lands on or before the one it follows.
		wasBest, wasAt := noPlace, -1
		for i, r := range runes {
			if j > 0 && i > 0 && places[j-1][i-1] > wasBest {
				wasBest, wasAt = places[j-1][i-1], i-1
			}
			if unicode.ToLower(r) != letter {
				continue
			}
			here := placeScore(runes, i)
			if j == 0 {
				places[j][i] = here
				continue
			}
			if wasAt >= 0 {
				places[j][i], from[j][i] = wasBest+here, wasAt
			}
			// The letter straight after the one before it is worth more
			// than the same letter further along.
			if i > 0 && places[j-1][i-1] > noPlace {
				if run := places[j-1][i-1] + here + scoreConsecutive; run > places[j][i] {
					places[j][i], from[j][i] = run, i-1
				}
			}
		}
	}
	return places, from
}

// placeScore is what one letter landing at i is worth on its own.
func placeScore(runes []rune, i int) int {
	score := 0
	if wordStart(runes, i) {
		score += scoreWordStart
	}
	if i == 0 {
		score += scoreLeading
	}
	return score
}

// runBonus is what a placement is worth for holding together: the whole
// query in one run, and that run standing as a word of its own.
func runBonus(runes []rune, at []int) int {
	for i := 1; i < len(at); i++ {
		if at[i] != at[i-1]+1 {
			return 0
		}
	}
	bonus := scoreRun
	if wordStart(runes, at[0]) && wordEnd(runes, at[len(at)-1]) {
		bonus += scoreWholeWord
	}
	return bonus
}

// wordStart reports whether the rune at i begins a word: the first one,
// one after a separator, or a capital in the middle of a word.
func wordStart(runes []rune, i int) bool {
	if i == 0 {
		return true
	}
	prev := runes[i-1]
	if isBreak(prev) {
		return true
	}
	return unicode.IsUpper(runes[i]) && !unicode.IsUpper(prev)
}

// wordEnd reports whether the rune at i ends a word: the last one, one
// before a separator, or the last capital of a run of them.
func wordEnd(runes []rune, i int) bool {
	if i == len(runes)-1 {
		return true
	}
	next := runes[i+1]
	if isBreak(next) {
		return true
	}
	return unicode.IsUpper(runes[i]) && !unicode.IsUpper(next)
}

// isBreak reports whether a rune stands between two words.
func isBreak(r rune) bool {
	if unicode.IsSpace(r) || unicode.IsPunct(r) {
		return true
	}
	return r == '.' || r == '-' || r == '_' || r == '/'
}
