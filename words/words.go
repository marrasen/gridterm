// Package words writes numbers and names the way kakel says them to
// people, the same in the window and in what the program reports.
package words

import (
	"fmt"
	"strconv"
	"strings"
)

// Count writes n things, with the plural where it takes one.
func Count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// ManyOf writes a number and the word for it, for a word whose plural
// is not an added s.
func ManyOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// Size writes a size in bytes the way a person reads it.
func Size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n), 0
	for v >= unit && i < 4 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGT"[i-1])
}

// UpperFirst capitalises the first letter of s.
func UpperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
