package vt

import (
	"bytes"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// An image larger than a kilobyte arrives whole. The parser keeps
// only a kilobyte of an OSC payload, which is smaller than any real
// image, so these sequences are read before it sees them.
func TestABigImageArrivesWhole(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	sendImage(t, term, "inline=1;width=10;height=4", aBigPNG(t))

	got := term.Images()
	if len(got) != 1 {
		t.Fatalf("%d images, want one", len(got))
	}
	if b := got[0].Img.Bounds(); b.Dx() != 700 || b.Dy() != 700 {
		t.Errorf("the image is %dx%d pixels, want 700x700", b.Dx(), b.Dy())
	}
	if len(got[0].Raw) < 1024 {
		t.Errorf("only %d bytes of it were kept", len(got[0].Raw))
	}
}

// An image written in small pieces arrives, because a program writes
// whatever it writes and the pieces land where the pipe puts them.
func TestAnImageWrittenInPiecesArrives(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})
	whole := []byte("before\x1b]1337;File=inline=1;width=4;height=2:" + aPNG(t, 64, 64) + "\x07after")

	for at := 0; at < len(whole); at += 3 {
		feed(t, term, string(whole[at:min(at+3, len(whole))]))
	}

	if got := term.Images(); len(got) != 1 {
		t.Fatalf("%d images, want one", len(got))
	}
	if got := rowText(renderOf(t, term), 0); got != "before" {
		t.Errorf("row 0 reads %q, want the text before the image", got)
	}
	if got := rowText(renderOf(t, term), 2); got != "after" {
		t.Errorf("row 2 reads %q, want the text after the image", got)
	}
}

// An image ended with a string terminator rather than a bell arrives
// too. Both spellings are in use.
func TestAnImageEndedWithAStringTerminatorArrives(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	feed(t, term, "\x1b]1337;File=inline=1;width=4;height=2:"+aPNG(t, 64, 64)+"\x1b\\next")

	if got := term.Images(); len(got) != 1 {
		t.Fatalf("%d images, want one", len(got))
	}
	if got := rowText(renderOf(t, term), 2); got != "next" {
		t.Errorf("row 2 reads %q, want the text after the image", got)
	}
}

// An image sequence the program gave up on does not swallow what
// comes after it.
func TestAnAbandonedImageDoesNotSwallowTheRest(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	feed(t, term, "\x1b]1337;File=inline=1:abc\x1b[31mred")

	if got := term.Images(); len(got) != 0 {
		t.Errorf("%d images from a sequence that never ended", len(got))
	}
	if got := rowText(renderOf(t, term), 0); got != "red" {
		t.Errorf("row 0 reads %q, want the text after the escape", got)
	}
}

// A payload past the largest image allowed is thrown away, and it
// does not print as text either.
func TestAPayloadTooBigIsThrownAway(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	feed(t, term, "\x1b]1337;File=inline=1:")
	for range (mostLongOSC / 4096) + 2 {
		feed(t, term, strings.Repeat("A", 4096))
	}
	feed(t, term, "\x07done")

	if got := term.Images(); len(got) != 0 {
		t.Errorf("%d images from a payload nobody could hold", len(got))
	}
	if got := rowText(renderOf(t, term), 0); got != "done" {
		t.Errorf("row 0 reads %q, want only the text after the sequence", got)
	}
}

// Everything that is not an image still reaches the parser, whether
// it looks like the start of one or not.
func TestEverythingElseStillReachesTheParser(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	feed(t, term, "\x1b]7;file://here/tmp\x07")
	feed(t, term, "\x1b]1339;not an image\x07")
	feed(t, term, "\x1b]2;a title\x07")
	feed(t, term, "\x1b\x1b]133;A\x07plain")

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

// However a stream is cut into writes, the filter hands on the same
// bytes and the same images as it does a byte at a time.
func TestLongOSCIsTheSameHoweverTheWritesAreCut(t *testing.T) {
	run := func(in []byte, cuts []int) (passed []byte, took []string) {
		var s longOSC
		pass := func(b []byte) { passed = append(passed, b...) }
		take := func(num, body []byte) { took = append(took, string(num)+"="+string(body)) }
		from := 0
		for _, to := range append(cuts, len(in)) {
			s.feed(in[from:to], pass, take)
			from = to
		}
		return passed, took
	}
	rng := rand.New(rand.NewSource(1))
	parts := []string{"\x1b]1337;File=:aGk=\x07", "\x1b]52;c;aGk=\x1b\\", "\x1b]1338;x\x1b[", "\x1b]0;t\x07",
		"\x1b[31m", "\x1b\x1b]52;c;", "text ", "\x1b", "]", "\x07", "\x18", "\x1b\\", "▀"}
	for i := 0; i < 3000; i++ {
		var b []byte
		for range rng.Intn(10) {
			b = append(b, parts[rng.Intn(len(parts))]...)
		}
		var cuts []int
		for k := 0; k <= len(b); k++ {
			if rng.Intn(4) == 0 {
				cuts = append(cuts, k)
			}
		}
		var each []int
		for k := 1; k < len(b); k++ {
			each = append(each, k)
		}
		wantP, wantT := run(b, each)
		gotP, gotT := run(b, cuts)
		if !bytes.Equal(gotP, wantP) || !slices.Equal(gotT, wantT) {
			t.Fatalf("%q cut at %v: passed %q took %q; byte by byte %q %q", b, cuts, gotP, gotT, wantP, wantT)
		}
	}
}
