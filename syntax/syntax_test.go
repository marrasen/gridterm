package syntax

import "testing"

func TestAGoCommentIsANote(t *testing.T) {
	c := For("main.go")
	if c == nil {
		t.Fatal("no colourer for a .go file")
	}
	runs := c(`x := "hi" // why`, nil)
	last := runs[len(runs)-1]
	if last.Colour != Note || last.End != len(`x := "hi" // why`) {
		t.Fatalf("the line ends in %+v, want a note to its end", last)
	}
	found := false
	for _, r := range runs {
		if r.Colour == Text {
			found = true
		}
	}
	if !found {
		t.Fatalf("runs %+v hold no string", runs)
	}
}

func TestAFileOfNoKnownKindHasNoColourer(t *testing.T) {
	if For("notes.unknownkind") != nil {
		t.Fatal("a colourer for a kind of file no one knows")
	}
}
