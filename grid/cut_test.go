package grid

import "testing"

// CutLeft drops the columns asked for and says how many it dropped.
func TestCutLeft(t *testing.T) {
	for what, tc := range map[string]struct {
		in   string
		n    int
		rest string
		cut  int
	}{
		"nothing":                {"abc", 0, "abc", 0},
		"one ascii column":       {"abc", 1, "bc", 1},
		"the whole line":         {"abc", 3, "", 3},
		"past the end":           {"abc", 9, "", 3},
		"a tab counts as a cell": {"\tabc", 1, "abc", 1},
		"a wide character":       {"日本", 2, "本", 2},
		"a wide one straddled":   {"日本", 1, "本", 2},
		"a combining mark stays with its character": {"e\u0301x", 1, "x", 1},
	} {
		rest, cut := CutLeft(tc.in, tc.n)
		if rest != tc.rest || cut != tc.cut {
			t.Errorf("%s: CutLeft(%q, %d) = %q, %d, want %q, %d",
				what, tc.in, tc.n, rest, cut, tc.rest, tc.cut)
		}
	}
}

// What is left plus what was cut is the whole line, so text cut here
// lines up with the same text written whole.
func TestCutLeftAddsUp(t *testing.T) {
	for _, line := range []string{"abc", "\tx := 1", "日本語 text", "e\u0301clair", "1\ufe0f\u20e3 one"} {
		for n := 0; n <= StringWidth(line)+2; n++ {
			rest, cut := CutLeft(line, n)
			if got := cut + StringWidth(rest); got != StringWidth(line) {
				t.Errorf("CutLeft(%q, %d) cut %d and left %d columns, want %d in all",
					line, n, cut, StringWidth(rest), StringWidth(line))
			}
			if cut < n && rest != "" {
				t.Errorf("CutLeft(%q, %d) stopped after %d columns", line, n, cut)
			}
		}
	}
}

// SnapToClusters moves a boundary inside a cluster to the end of it.
func TestSnapToClusters(t *testing.T) {
	// "1" then a variation selector and a keycap: one cluster of four
	// bytes, then "x".
	line := "1\ufe0f\u20e3x"
	ends := []int{1, len(line)}

	SnapToClusters(line, ends)

	if ends[0] != len(line)-1 {
		t.Errorf("the first end is %d, want %d: the keycap is one cluster", ends[0], len(line)-1)
	}
	if ends[1] != len(line) {
		t.Errorf("the last end moved to %d, want %d", ends[1], len(line))
	}
}

// An offset already on a boundary is left where it is, and one past the
// end is brought back to it.
func TestSnapToClustersLeavesGoodOffsetsAlone(t *testing.T) {
	line := "abc"
	ends := []int{0, 1, 3, 99}

	SnapToClusters(line, ends)

	for i, want := range []int{0, 1, 3, 3} {
		if ends[i] != want {
			t.Errorf("end %d is %d, want %d", i, ends[i], want)
		}
	}
}
