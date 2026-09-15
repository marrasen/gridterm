package grid

import "testing"

// A cut at a width keeps what fits and hands back the rest, so nothing
// is lost between the two halves.
func TestCuttingKeepsEveryCharacter(t *testing.T) {
	for _, s := range []string{"hello", "日本語のテキスト", "éclair", "", "🇸🇪flag"} {
		for cols := -1; cols <= StringWidth(s)+2; cols++ {
			head, rest := Cut(s, cols)
			if head+rest != s {
				t.Errorf("Cut(%q, %d) = %q + %q, which is not the string", s, cols, head, rest)
			}
			if w := StringWidth(head); cols > 0 && w > cols {
				t.Errorf("Cut(%q, %d) kept %q, which is %d wide", s, cols, head, w)
			}
		}
	}
}

// A double-width character is never cut in half: the whole of it goes to
// one side or the other.
func TestCuttingDoesNotHalveAWideCharacter(t *testing.T) {
	const s = "日本語"

	head, rest := Cut(s, 3)
	if head != "日" || rest != "本語" {
		t.Fatalf("Cut(%q, 3) = %q + %q, want the second character whole in the rest", s, head, rest)
	}
}

// A combining mark stays in the cell of the character it belongs to, so
// a cut never leaves a mark with nothing to sit on.
func TestCuttingKeepsACombiningMarkWithItsCharacter(t *testing.T) {
	// "e" with a combining acute, then "f". Two cells, three runes.
	const s = "éf"
	if w := StringWidth(s); w != 2 {
		t.Fatalf("%q is %d cells wide, want 2", s, w)
	}

	head, rest := Cut(s, 1)
	if head != "é" || rest != "f" {
		t.Fatalf("Cut(%q, 1) = %q + %q, want the mark kept with its letter", s, head, rest)
	}
}

// Trimming to a width says nothing about having cut, because a label on
// a narrow bar is a reminder and "Del" reminds where "…" does not.
func TestTrimCutsSilently(t *testing.T) {
	cases := []struct {
		in   string
		cols int
		want string
	}{
		{"Rename", 3, "Ren"},
		{"Rename", 6, "Rename"},
		{"Rename", 99, "Rename"},
		{"Rename", 0, ""},
		{"Rename", -4, ""},
		{"日本語", 4, "日本"},
		{"日本語", 3, "日"},
		{"éclair", 2, "éc"},
	}
	for _, c := range cases {
		if got := Trim(c.in, c.cols); got != c.want {
			t.Errorf("Trim(%q, %d) = %q, want %q", c.in, c.cols, got, c.want)
		}
	}
}

// Trimming the tail marks the cut, and what is left still fits.
func TestTrimTailMarksTheCutAndFits(t *testing.T) {
	cases := []struct {
		in   string
		cols int
		want string
	}{
		{"Rename", 4, "Ren…"},
		{"Rename", 6, "Rename"},
		{"Rename", 0, ""},
		// Three cells: the ellipsis takes one, which leaves room for one
		// double-width character and not two.
		{"日本語", 3, "日…"},
		{"éclair", 3, "éc…"},
	}
	for _, c := range cases {
		got := TrimTail(c.in, c.cols)
		if got != c.want {
			t.Errorf("TrimTail(%q, %d) = %q, want %q", c.in, c.cols, got, c.want)
		}
		if w := StringWidth(got); c.cols > 0 && w > c.cols {
			t.Errorf("TrimTail(%q, %d) = %q, which is %d wide", c.in, c.cols, got, w)
		}
	}
}

// Trimming the head keeps the end, which for a path is the part that
// says which directory this is.
func TestTrimHeadKeepsTheEnd(t *testing.T) {
	cases := []struct {
		in   string
		cols int
		want string
	}{
		{"/home/marcus/work", 6, "…/work"},
		{"/home", 5, "/home"},
		{"/home", 0, ""},
		{"日本語", 3, "…語"},
		{"abé", 2, "…é"},
	}
	for _, c := range cases {
		got := TrimHead(c.in, c.cols)
		if got != c.want {
			t.Errorf("TrimHead(%q, %d) = %q, want %q", c.in, c.cols, got, c.want)
		}
		if w := StringWidth(got); c.cols > 0 && w > c.cols {
			t.Errorf("TrimHead(%q, %d) = %q, which is %d wide", c.in, c.cols, got, w)
		}
	}
}
