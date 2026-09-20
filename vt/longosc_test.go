package vt

import (
	"strings"
	"testing"
)

// A picture larger than a kilobyte arrives whole. The parser keeps
// only a kilobyte of an OSC payload, which is smaller than any real
// picture, so these sequences are read before it sees them.
func TestABigPictureArrivesWhole(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	sendImage(t, term, "inline=1;width=10;height=4", aBigPNG(t))

	got := term.Images()
	if len(got) != 1 {
		t.Fatalf("%d pictures, want one", len(got))
	}
	if b := got[0].Img.Bounds(); b.Dx() != 700 || b.Dy() != 700 {
		t.Errorf("the picture is %dx%d pixels, want 700x700", b.Dx(), b.Dy())
	}
	if len(got[0].Raw) < 1024 {
		t.Errorf("only %d bytes of it were kept", len(got[0].Raw))
	}
}

// A picture written in small pieces arrives, because a program writes
// whatever it writes and the pieces land where the pipe puts them.
func TestAPictureWrittenInPiecesArrives(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})
	whole := []byte("before\x1b]1337;File=inline=1;width=4;height=2:" + aPNG(t, 64, 64) + "\x07after")

	for at := 0; at < len(whole); at += 3 {
		term.Write(whole[at:min(at+3, len(whole))])
	}

	if got := term.Images(); len(got) != 1 {
		t.Fatalf("%d pictures, want one", len(got))
	}
	if got := rowText(renderOf(t, term), 0); got != "before" {
		t.Errorf("row 0 reads %q, want the text before the picture", got)
	}
	if got := rowText(renderOf(t, term), 2); got != "after" {
		t.Errorf("row 2 reads %q, want the text after the picture", got)
	}
}

// A picture ended with a string terminator rather than a bell arrives
// too. Both spellings are in use.
func TestAPictureEndedWithAStringTerminatorArrives(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	term.Write([]byte("\x1b]1337;File=inline=1;width=4;height=2:" + aPNG(t, 64, 64) + "\x1b\\next"))

	if got := term.Images(); len(got) != 1 {
		t.Fatalf("%d pictures, want one", len(got))
	}
	if got := rowText(renderOf(t, term), 2); got != "next" {
		t.Errorf("row 2 reads %q, want the text after the picture", got)
	}
}

// A picture sequence the program gave up on does not swallow what
// comes after it.
func TestAnAbandonedPictureDoesNotSwallowTheRest(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	term.Write([]byte("\x1b]1337;File=inline=1:abc\x1b[31mred"))

	if got := term.Images(); len(got) != 0 {
		t.Errorf("%d pictures from a sequence that never ended", len(got))
	}
	if got := rowText(renderOf(t, term), 0); got != "red" {
		t.Errorf("row 0 reads %q, want the text after the escape", got)
	}
}

// A payload past the largest picture allowed is thrown away, and it
// does not print as text either.
func TestAPayloadTooBigIsThrownAway(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	term.Write([]byte("\x1b]1337;File=inline=1:"))
	for range (mostLongOSC / 4096) + 2 {
		term.Write([]byte(strings.Repeat("A", 4096)))
	}
	term.Write([]byte("\x07done"))

	if got := term.Images(); len(got) != 0 {
		t.Errorf("%d pictures from a payload nobody could hold", len(got))
	}
	if got := rowText(renderOf(t, term), 0); got != "done" {
		t.Errorf("row 0 reads %q, want only the text after the sequence", got)
	}
}

// Everything that is not a picture still reaches the parser, whether
// it looks like the start of one or not.
func TestEverythingElseStillReachesTheParser(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	term.Write([]byte("\x1b]7;file://here/tmp\x07"))
	term.Write([]byte("\x1b]1339;not a picture\x07"))
	term.Write([]byte("\x1b]2;a title\x07"))
	term.Write([]byte("\x1b\x1b]133;A\x07plain"))

	if dir, host := term.Dir(); dir != "/tmp" || host != "here" {
		t.Errorf("the working directory came out as %q on %q", dir, host)
	}
	if term.Title() != "a title" {
		t.Errorf("the title came out as %q", term.Title())
	}
	if got := rowText(renderOf(t, term), 0); got != "plain" {
		t.Errorf("row 0 reads %q, want the text", got)
	}
}
