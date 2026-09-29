package view

import (
	"strings"
	"unicode"

	"github.com/marrasen/gunim/widget"
)

// Access keys: Alt and a menu's letter opens it, and a letter then picks
// a line in it, where the keyboard is not in a terminal, which takes
// Alt and a letter itself. gunim reads them from a & before the letter.

// menuKeys are the menus' letters: each menu's first letter, and where
// two menus start alike, the one that reads best after it.
var menuKeys = map[string]rune{
	"File": 'f', "Edit": 'e', "View": 'v', "Pane": 'p', "Machine": 'm', "Servers": 's',
	"Share": 'a', "Secrets": 'c', "Options": 'o', "Font": 'n', "Help": 'h',
}

// menuAt is the place of the menu titled title on the bar, or -1.
func menuAt(title string) int {
	for i, m := range menus {
		if m.title == title {
			return i
		}
	}
	return -1
}

// withAccessKeys marks the access keys in a bar menu: its title's from
// menuKeys, and on each line the first letter of a word that no line
// before it has taken, or else any letter free. The lines in first go
// before the rest, so lines always there keep their letters whatever
// is listed with them, such as the saved servers. A caption is no line
// to pick and takes none.
func withAccessKeys(m widget.BarMenu, first ...int) widget.BarMenu {
	m.Title = markKey(m.Title, map[rune]bool{}, menuKeys[m.Title])
	taken := map[rune]bool{}
	items := make([]string, len(m.Items))
	done := make([]bool, len(m.Items))
	mark := func(i int) {
		if i < 0 || i >= len(m.Items) || done[i] {
			return
		}
		done[i] = true
		if isCaption(m, i) {
			items[i] = escapeAmp(m.Items[i])
			return
		}
		items[i] = markKey(m.Items[i], taken, 0)
	}
	for _, i := range first {
		mark(i)
	}
	for i := range m.Items {
		mark(i)
	}
	m.Items = items
	return m
}

// isCaption reports whether line i of m is a caption.
func isCaption(m widget.BarMenu, i int) bool {
	for _, c := range m.Captions {
		if c == i {
			return true
		}
	}
	return false
}

// markKey puts a & before the letter of s that is its access key, and
// notes the letter in taken: want when s has it, else the first letter
// of a word, else any letter, not taken already. With none free, s has
// no access key. A & in s is doubled, so it shows.
func markKey(s string, taken map[rune]bool, want rune) string {
	rs := []rune(s)
	at := -1
	free := func(i int) bool {
		return unicode.IsLetter(rs[i]) && !taken[unicode.ToLower(rs[i])]
	}
	if want != 0 {
		for i, r := range rs {
			if unicode.ToLower(r) == want && free(i) {
				at = i
				break
			}
		}
	}
	for i := range rs {
		if at >= 0 {
			break
		}
		if (i == 0 || !unicode.IsLetter(rs[i-1])) && free(i) {
			at = i
		}
	}
	for i := range rs {
		if at >= 0 {
			break
		}
		if free(i) {
			at = i
		}
	}
	var b strings.Builder
	for i, r := range rs {
		if i == at {
			b.WriteByte('&')
			taken[unicode.ToLower(r)] = true
		}
		if r == '&' {
			b.WriteByte('&')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeAmp doubles each & in s, so it shows rather than marks a key.
func escapeAmp(s string) string { return strings.ReplaceAll(s, "&", "&&") }

// shownText is a menu line as it is drawn, without the & that marks its
// access key.
func shownText(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '&' {
			if i+1 < len(rs) && rs[i+1] == '&' {
				i++
			} else {
				continue
			}
		}
		b.WriteRune(rs[i])
	}
	return b.String()
}
