package grid

import "slices"

// Ellipsis marks where a string was cut.
const Ellipsis = "…"

// Cut splits a string at a width in cells: the part that fits, and the
// rest.
//
// It splits between grapheme clusters, never inside one. A grid draws a
// character and its combining marks in one cell, and cutting between
// them would leave half a character behind; a double-width character
// that would straddle the edge goes to the rest.
func Cut(s string, cols int) (head, rest string) {
	if cols <= 0 {
		return "", s
	}
	at, n := 0, 0
	for _, cluster := range Clusters(s) {
		w := StringWidth(cluster)
		if at+w > cols {
			return s[:n], s[n:]
		}
		at += w
		n += len(cluster)
	}
	return s, ""
}

// Trim cuts a string to a width in cells, with nothing to say it was
// cut.
//
// It is for a label that is a reminder rather than a sentence: on a
// narrow bar "Del" reminds where "…" does not.
func Trim(s string, cols int) string {
	if StringWidth(s) <= cols {
		return s
	}
	head, _ := Cut(s, cols)
	return head
}

// TrimTail cuts the end off a string too wide for the room it has,
// marking the cut with an ellipsis.
func TrimTail(s string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if StringWidth(s) <= cols {
		return s
	}
	head, _ := Cut(s, cols-StringWidth(Ellipsis))
	return head + Ellipsis
}

// TrimHead cuts the start off a string too wide for the room it has,
// marking the cut with an ellipsis in front of what is left.
//
// It is for a path, where the end is the part that says which directory
// this is.
func TrimHead(s string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if StringWidth(s) <= cols {
		return s
	}
	room := cols - StringWidth(Ellipsis)
	clusters := Clusters(s)
	at, n := 0, len(s)
	for _, cluster := range slices.Backward(clusters) {
		w := StringWidth(cluster)
		if at+w > room {
			break
		}
		at += w
		n -= len(cluster)
	}
	return Ellipsis + s[n:]
}
