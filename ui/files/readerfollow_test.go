package files

import (
	"errors"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/ui"
)

// A pane made shorter keeps a file that is being followed on its end.
//
// The end of the file moves away from where the reader is sitting when
// the pane loses rows, so without this the header goes on saying
// "(following)" while the new lines never arrive.
func TestAShorterPaneKeepsTheTail(t *testing.T) {
	r := aFileOf(t, 40, 20, numbered(100)...)
	r.Follow(true)
	if !r.AtEnd() {
		t.Fatal("the reader did not start at the end of the file")
	}

	r.Layout(ui.Size{Cols: 40, Rows: 10})

	if !r.AtEnd() {
		t.Errorf("the pane got shorter and the reader is on line %d, want the end", r.Top()+1)
	}
	// And the next answer stays there.
	r.Read = func(then func([]string, bool, error)) { then(numbered(140), false, nil) }
	r.Open()
	if !r.AtEnd() {
		t.Errorf("the file grew and the reader is on line %d, want the end", r.Top()+1)
	}
}

// A pane made shorter leaves a reader who scrolled back where they put
// themselves: following is about the end moving, not about taking the
// pane away from whoever is reading it.
func TestAShorterPaneLeavesAScrolledReaderAlone(t *testing.T) {
	r := aFileOf(t, 40, 20, numbered(100)...)
	r.Follow(true)
	r.Home()

	r.Layout(ui.Size{Cols: 40, Rows: 10})

	if got := r.Top(); got != 0 {
		t.Errorf("the reader moved to line %d, want the first, where they were", got+1)
	}
}

// A file read again shorter pulls the reader back inside it, across as
// well as down: a pane scrolled past the end of every line it now has
// would show nothing at all.
func TestAShorterFilePullsTheReaderBack(t *testing.T) {
	long := "x" + strings.Repeat("y", 200)
	r := aFileOf(t, 40, 10, long, long)
	r.Sideways(200)
	if r.Left() == 0 {
		t.Fatal("the reader would not scroll across a line wider than the pane")
	}

	r.Read = func(then func([]string, bool, error)) { then([]string{"short"}, false, nil) }
	r.Open()

	if got := r.Left(); got != 0 {
		t.Errorf("the reader is %d columns across a file whose longest line is 5", got)
	}
	g := drawReader(r, 40, 10)
	if got := readerRow(g, 1); got != "short" {
		t.Errorf("the line reads %q, want the one line the file has", got)
	}
}

// Failed puts a reason on the pane in place of the file, for something
// that went wrong away from a read.
func TestFailedShowsTheReason(t *testing.T) {
	r := aFileOf(t, 40, 10, "one", "two")

	r.Failed(errors.New("the machine has gone"))
	g := drawReader(r, 40, 10)

	if got := readerRow(g, 1); !strings.Contains(got, "the machine has gone") {
		t.Errorf("the second row is %q, want the reason", got)
	}
}

// Open says whether a read went out, so a caller that wrote down what it
// was about to read knows when it did not.
func TestOpenSaysWhetherAReadWentOut(t *testing.T) {
	r := aFileOf(t, 40, 10, "one")
	var answer func([]string, bool, error)
	r.Read = func(then func([]string, bool, error)) { answer = then }

	if !r.Open() {
		t.Fatal("the first read did not go out")
	}
	if r.Open() {
		t.Error("a second read went out while the first was still running")
	}
	answer([]string{"two"}, false, nil)
	if !r.Open() {
		t.Error("no read went out once the first had answered")
	}
}
