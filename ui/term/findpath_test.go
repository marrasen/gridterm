package term

import "testing"

// pathIn is what findPathText says about a column of a line.
func pathIn(line string, at int) (string, int, bool) {
	text, num, _, _, ok := findPathText([]rune(line), at)
	return text, num, ok
}

// A path in output is found as the run of characters it is written
// as. Whether it names anything is the disk's business, not this.
func TestAPathInOutputIsFound(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"see src/main.go for it", "src/main.go"},
		{"/home/marcus/notes.txt", "/home/marcus/notes.txt"},
		{`open C:\Users\marcus\notes.txt now`, `C:\Users\marcus\notes.txt`},
		{"./configure failed", "./configure"},
		{"../sibling/file", "../sibling/file"},
	} {
		got, _, ok := pathIn(tc.line, 8)
		if !ok {
			t.Errorf("%q: nothing found at column 8", tc.line)
			continue
		}
		if got != tc.want {
			t.Errorf("%q gave %q, want %q", tc.line, got, tc.want)
		}
	}
}

// A compiler names a place in a file as "file:line" or
// "file:line:col", and both come apart into a path and a line.
func TestALineReferenceComesApart(t *testing.T) {
	for _, tc := range []struct {
		line string
		want string
		at   int
	}{
		{"src/main.go:42: undefined", "src/main.go", 42},
		{"src/main.go:42:8: undefined", "src/main.go", 42},
		{"src/main.go: syntax error", "src/main.go", 0},
		{"src/main.go", "src/main.go", 0},
	} {
		got, num, ok := pathIn(tc.line, 4)
		if !ok {
			t.Errorf("%q: nothing found", tc.line)
			continue
		}
		if got != tc.want || num != tc.at {
			t.Errorf("%q gave %q at line %d, want %q at %d", tc.line, got, num, tc.want, tc.at)
		}
	}
}

// A Windows drive letter is not a line number, however much the
// colon after it looks like one.
func TestADriveLetterIsNotALineNumber(t *testing.T) {
	got, num, ok := pathIn(`C:\Users\marcus\notes.txt`, 4)

	if !ok {
		t.Fatal("nothing found")
	}
	if got != `C:\Users\marcus\notes.txt` {
		t.Errorf("it found %q, want the whole path", got)
	}
	if num != 0 {
		t.Errorf("it read line %d off a drive letter", num)
	}
}

// The punctuation a sentence puts after a path is the sentence's,
// the same as for an address.
func TestPunctuationAfterAPathIsLeftOut(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"it is in src/main.go.", "src/main.go"},
		{"see (src/main.go)", "src/main.go"},
		{"try src/main.go, then stop", "src/main.go"},
	} {
		got, _, ok := pathIn(tc.line, 12)
		if !ok {
			t.Errorf("%q: nothing found", tc.line)
			continue
		}
		if got != tc.want {
			t.Errorf("%q gave %q, want %q", tc.line, got, tc.want)
		}
	}
}

// A space is where a path stops, because a path with one in it
// cannot be told from two words.
func TestAPathStopsAtASpace(t *testing.T) {
	got, _, ok := pathIn("one/two three/four", 2)

	if !ok {
		t.Fatal("nothing found")
	}
	if got != "one/two" {
		t.Errorf("it found %q, want it to stop at the space", got)
	}
}

// A column on a space is in no path at all.
func TestAColumnOnASpaceIsInNoPath(t *testing.T) {
	if _, _, ok := pathIn("one/two three/four", 7); ok {
		t.Error("a space was taken as part of a path")
	}
}
