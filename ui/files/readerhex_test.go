package files

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
)

// The hex view lays the file out as bytes: an offset, the numbers, and
// the printable characters beside them.
func TestTheHexViewLaysTheFileOutAsBytes(t *testing.T) {
	r := aFileOf(t, 80, 10, "hello")

	r.Hex(true)
	g := drawReader(r, 80, 10)

	if !r.Hexed() {
		t.Fatal("the reader does not know it is showing bytes")
	}
	line := readerRow(g, 1)
	if !strings.HasPrefix(line, "00000000") {
		t.Errorf("the first line is %q, want it to start at offset zero", line)
	}
	// "hello" is 68 65 6c 6c 6f.
	for _, want := range []string{"68", "65", "6c", "6f"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line is %q, missing the byte %q", line, want)
		}
	}
	if !strings.Contains(line, "|hello|") {
		t.Errorf("the line is %q, want the characters beside the numbers", line)
	}
}

// The offsets are the offsets in the file, counting the newlines that
// split it into lines.
func TestTheOffsetsCountTheNewlines(t *testing.T) {
	// Two lines of four, so the second line of the dump starts at 16.
	r := aFileOf(t, 80, 10, "abcd", "efgh", "ijkl", "mnop")

	r.Hex(true)
	g := drawReader(r, 80, 10)

	// "abcd\nefgh\nijkl\nm" is the first sixteen bytes.
	first := readerRow(g, 1)
	if !strings.Contains(first, "|abcd.efgh.ijkl.m|") {
		t.Errorf("the first line is %q, want the newlines counted and shown as dots", first)
	}
	second := readerRow(g, 2)
	if !strings.HasPrefix(second, "00000010") {
		t.Errorf("the second line is %q, want it to start at offset sixteen", second)
	}
}

// A byte that is not an ordinary character is a full stop, so the
// characters line up one to a byte under the numbers.
func TestAByteThatIsNotACharacterIsAFullStop(t *testing.T) {
	r := aFileOf(t, 80, 10, "a\x00b\x7fc")

	r.Hex(true)
	g := drawReader(r, 80, 10)

	if got := readerRow(g, 1); !strings.Contains(got, "|a.b.c|") {
		t.Errorf("the line is %q, want the bytes that are not characters as full stops", got)
	}
}

// Turning the hex view off puts the lines back.
func TestTurningTheHexViewOffPutsTheLinesBack(t *testing.T) {
	r := aFileOf(t, 80, 10, "one", "two", "three")
	was := r.Lines()

	r.Hex(true)
	r.Hex(false)

	if r.Hexed() {
		t.Error("the reader still says it is showing bytes")
	}
	if got := r.Lines(); got != was {
		t.Errorf("it holds %d lines, want the %d it had", got, was)
	}
	if got := readerRow(drawReader(r, 80, 10), 1); !strings.HasPrefix(got, "one") {
		t.Errorf("the first line is %q, want the file's own first line", got)
	}
}

// The top line says which view this is.
func TestTheTopLineSaysItIsShowingBytes(t *testing.T) {
	r := aFileOf(t, 80, 10, "hello")

	r.Hex(true)

	if got := readerRow(drawReader(r, 80, 10), 0); !strings.Contains(got, "hex") {
		t.Errorf("the top row is %q, want it to say it is showing bytes", got)
	}
}

// Ctrl+H turns it on and off.
func TestCtrlHTurnsTheHexViewOnAndOff(t *testing.T) {
	r := aFileOf(t, 80, 10, "hello")

	hex := input.Event{Kind: input.KeyPress, Key: input.KeyH, Mods: input.ModCtrl}
	if _, err := r.HandleKey(hex); err != nil {
		t.Fatalf("hex: %v", err)
	}
	if !r.Hexed() {
		t.Error("Ctrl+H did not show the bytes")
	}
	if _, err := r.HandleKey(hex); err != nil {
		t.Fatalf("hex again: %v", err)
	}
	if r.Hexed() {
		t.Error("Ctrl+H again did not put the lines back")
	}
}

// An empty file has no bytes to show and says so rather than drawing a
// line of nothing.
func TestAnEmptyFileHasNoBytes(t *testing.T) {
	r := aFileOf(t, 80, 10)

	r.Hex(true)

	if got := r.Lines(); got != 0 {
		t.Errorf("an empty file laid out as bytes is %d lines", got)
	}
	if got := readerRow(drawReader(r, 80, 10), 0); !strings.Contains(got, "empty") {
		t.Errorf("the top row is %q, want it to say the file is empty", got)
	}
}

// A search works on what is shown, so a byte can be looked for in the
// hex view.
func TestSearchingWorksOnTheBytes(t *testing.T) {
	// Long enough that the byte looked for is off the first screenful.
	r := aFileOf(t, 80, 6, strings.Repeat("a", 400)+"zz")

	r.Hex(true)
	typed(t, r, "/7a")
	press(t, r, input.KeyEnter)

	if got := r.Top(); got == 0 {
		t.Error("looking for a byte further down the file did not move")
	}
}
