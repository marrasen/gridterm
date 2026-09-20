package term

import "testing"

// found is what findLink says about a column of a line of text.
func found(line string, at int) (string, int, int, bool) {
	return findLink([]rune(line), at)
}

// An address written out in ordinary output is found, and the columns
// it runs between are its own.
func TestABareAddressIsFound(t *testing.T) {
	line := "see https://example.com/a for more"

	got, from, to, ok := found(line, 10)

	if !ok {
		t.Fatal("the address was not found")
	}
	if want := "https://example.com/a"; got != want {
		t.Errorf("it found %q, want %q", got, want)
	}
	if line[from:to] != got {
		t.Errorf("it says columns %d to %d, which is %q", from, to, line[from:to])
	}
}

// A column outside the address is not in it, so hovering the word
// beside a link finds nothing.
func TestAColumnBesideAnAddressIsNotInIt(t *testing.T) {
	line := "see https://example.com/a for more"

	for _, at := range []int{0, 2, 3, 25, 30} {
		if _, _, _, ok := found(line, at); ok {
			t.Errorf("column %d was taken as part of the address", at)
		}
	}
}

// The punctuation a sentence puts after an address is the sentence's.
func TestPunctuationAfterAnAddressIsLeftOut(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"go to https://example.com.", "https://example.com"},
		{"go to https://example.com, then back", "https://example.com"},
		{"go to https://example.com!", "https://example.com"},
		{"see https://example.com/a?b=1;", "https://example.com/a?b=1"},
		{"(https://example.com)", "https://example.com"},
		{"<https://example.com>", "https://example.com"},
	} {
		got, _, _, ok := found(tc.line, 8)
		if !ok {
			t.Errorf("%q: nothing found", tc.line)
			continue
		}
		if got != tc.want {
			t.Errorf("%q gave %q, want %q", tc.line, got, tc.want)
		}
	}
}

// A closing bracket the address itself opened is part of it, which is
// what makes a Wikipedia address work.
func TestABracketTheAddressOpenedIsKept(t *testing.T) {
	line := "https://en.wikipedia.org/wiki/Terminal_(disambiguation)"

	got, _, _, ok := found(line, 10)

	if !ok {
		t.Fatal("nothing found")
	}
	if got != line {
		t.Errorf("it found %q, want the whole address", got)
	}
}

// Only a written-out scheme counts. A bare hostname in a sentence is
// a word, and guessing wrong sends a click somewhere nobody meant.
func TestSomethingWithNoSchemeIsNotAnAddress(t *testing.T) {
	for _, line := range []string{
		"visit example.com for more",
		"the file is at /home/marcus/notes.txt",
		"marcus@example.com wrote",
		"C:\\Users\\marcus",
	} {
		if got, _, _, ok := found(line, 8); ok {
			t.Errorf("%q was taken as the address %q", line, got)
		}
	}
}

// Only the schemes worth opening. A program in a pane may print
// anything, and these are the ones a browser is the answer for.
func TestOnlyTheSchemesWorthOpeningAreFound(t *testing.T) {
	for _, tc := range []struct {
		line string
		find bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"ftp://example.com/f", true},
		{"mailto:marcus@example.com", true},
		{"HTTPS://EXAMPLE.COM", true},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"data:text/html,x", false},
		{"ms-msdt:/id", false},
	} {
		_, _, _, ok := found(tc.line, 3)
		if ok != tc.find {
			t.Errorf("%q: found=%v, want %v", tc.line, ok, tc.find)
		}
	}
}

// Two addresses on one line are told apart by where the pointer is.
func TestTwoAddressesOnOneLineAreToldApart(t *testing.T) {
	line := "https://one.example and https://two.example"

	first, _, _, ok := found(line, 3)
	if !ok {
		t.Fatal("the first was not found")
	}
	second, _, _, ok := found(line, 30)
	if !ok {
		t.Fatal("the second was not found")
	}

	if first != "https://one.example" {
		t.Errorf("the first is %q", first)
	}
	if second != "https://two.example" {
		t.Errorf("the second is %q", second)
	}
}

// A scheme with nothing after it is not an address.
func TestASchemeWithNothingAfterItIsNotAnAddress(t *testing.T) {
	for _, line := range []string{"https://", "see https:// there", "mailto:"} {
		if got, _, _, ok := found(line, 5); ok {
			t.Errorf("%q was taken as %q", line, got)
		}
	}
}

// A column off either end of the line is in nothing.
func TestAColumnOffTheLineIsInNothing(t *testing.T) {
	line := "https://example.com"

	for _, at := range []int{-1, len(line), len(line) + 10} {
		if _, _, _, ok := found(line, at); ok {
			t.Errorf("column %d was taken as part of the address", at)
		}
	}
}
