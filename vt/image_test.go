package vt

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// aPNG is a picture of a given size, base64'd the way a program
// sends one.
func aPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

// sendImage writes an inline picture the way iTerm2's sequence does.
func sendImage(t *testing.T, term *Terminal, args, body string) {
	t.Helper()
	term.Write([]byte("\x1b]1337;File=" + args + ":" + body + "\x07"))
}

// A program puts a picture in the output and the pane holds it, on
// the line the cursor was on.
func TestAnInlinePictureIsHeld(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	sendImage(t, term, "inline=1;width=10;height=4", aPNG(t, 80, 64))

	got := term.Images()
	if len(got) != 1 {
		t.Fatalf("the pane holds %d pictures, want one", len(got))
	}
	if got[0].Cols != 10 || got[0].Rows != 4 {
		t.Errorf("it was given %dx%d cells, want 10x4", got[0].Cols, got[0].Rows)
	}
	if got[0].Img == nil {
		t.Error("the picture has no pixels")
	}
}

// The cursor moves past the picture, so what the program prints next
// lands under it rather than on top.
func TestTheCursorMovesPastAPicture(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	sendImage(t, term, "inline=1;width=10;height=4", aPNG(t, 80, 64))
	term.Write([]byte("after"))

	g := renderOf(t, term)
	var row strings.Builder
	for x := range 10 {
		if c := g.At(x, 4); c.Rune != 0 {
			row.WriteRune(c.Rune)
		}
	}
	if got := row.String(); !strings.HasPrefix(got, "after") {
		t.Errorf("row 4 is %q, want the text under the picture", got)
	}
}

// A picture says where it is in lines that keep their meaning as the
// screen scrolls.
func TestAPictureKeepsItsPlaceAsTheScreenScrolls(t *testing.T) {
	// Tall enough that the lines below move the picture up without
	// pushing it off: what is tested is that it moves.
	term := New(40, 12, DefaultPalette(), 100, Callbacks{})
	sendImage(t, term, "inline=1;width=4;height=6", aPNG(t, 32, 96))

	placed := term.Placed()
	if len(placed) != 1 {
		t.Fatalf("%d placed, want one", len(placed))
	}
	was := placed[0].Top

	// Filled past the bottom, so the screen scrolls rather than the
	// lines landing in the room that is left.
	for range 10 {
		term.Write([]byte("filler\r\n"))
	}

	placed = term.Placed()
	if len(placed) != 1 {
		t.Fatalf("%d placed after scrolling, want one", len(placed))
	}
	if placed[0].Top >= was {
		t.Errorf("it is at row %d after scrolling, was %d: it should have moved up",
			placed[0].Top, was)
	}
}

// A picture scrolled off the top is not placed, and one scrolled back
// to is placed again.
func TestAPictureScrolledOffIsNotPlaced(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 100, Callbacks{})
	sendImage(t, term, "inline=1;width=4;height=2", aPNG(t, 32, 32))
	for range 20 {
		term.Write([]byte("a line\r\n"))
	}

	if got := term.Placed(); len(got) != 0 {
		t.Fatalf("%d placed while scrolled out of sight, want none", len(got))
	}

	term.Screen().ScrollView(20)
	if got := term.Placed(); len(got) != 1 {
		t.Errorf("%d placed after scrolling back to it, want one", len(got))
	}
}

// A picture whose line has fallen out of history is forgotten, so a
// pane does not hold pixels nothing can reach.
func TestAPictureThatFellOutOfHistoryIsForgotten(t *testing.T) {
	term := New(40, 5, DefaultPalette(), 8, Callbacks{})
	sendImage(t, term, "inline=1;width=4;height=2", aPNG(t, 32, 32))
	if len(term.Images()) != 1 {
		t.Fatal("the picture was not held")
	}

	// Past the batch the scrollback trims in, not just past the
	// limit it trims to: until then the lines are still there and
	// the picture is still reachable.
	for range 400 {
		term.Write([]byte("a line\r\n"))
	}
	term.Placed()

	if got := term.Images(); len(got) != 0 {
		t.Errorf("the pane still holds %d pictures nothing can scroll back to", len(got))
	}
}

// Only an inline picture is taken. The same sequence asks a terminal
// to save a file, which is not something a pane should make this
// window do.
func TestOnlyAnInlinePictureIsTaken(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	sendImage(t, term, "name=cGF5bG9hZA==;size=99", aPNG(t, 16, 16))

	if got := term.Images(); len(got) != 0 {
		t.Errorf("a file transfer was taken as %d pictures", len(got))
	}
}

// Something that is not a picture is not one, however it is labelled.
func TestSomethingThatIsNotAPictureIsNotTaken(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})

	sendImage(t, term, "inline=1", base64.StdEncoding.EncodeToString([]byte("not a picture")))
	sendImage(t, term, "inline=1", "!!!not base64!!!")
	sendImage(t, term, "inline=1", "")

	if got := term.Images(); len(got) != 0 {
		t.Errorf("%d pictures were taken from things that are not pictures", len(got))
	}
}

// Past the cap the oldest goes, so a program sending picture after
// picture does not hold every one of them for ever.
func TestPastTheCapTheOldestPictureGoes(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 10000, Callbacks{})

	for range MostImages + 10 {
		sendImage(t, term, "inline=1;width=2;height=1", aPNG(t, 16, 16))
	}

	if got := len(term.Images()); got != MostImages {
		t.Errorf("the pane holds %d pictures, want the cap of %d", got, MostImages)
	}
}

// A size in cells, in pixels and as a share of the screen are all
// read, and nothing said means as big as the picture is.
func TestTheSizesAProgramCanAskFor(t *testing.T) {
	for _, tc := range []struct {
		args       string
		cols, rows int
	}{
		{"inline=1;width=10;height=4", 10, 4},
		{"inline=1;width=50%;height=50%", 20, 5},
		{"inline=1;width=16px;height=32px", 2, 2},
		{"inline=1", 8, 4},
		{"inline=1;width=auto;height=auto", 8, 4},
	} {
		term := New(40, 10, DefaultPalette(), 100, Callbacks{})
		sendImage(t, term, tc.args, aPNG(t, 64, 64))

		got := term.Images()
		if len(got) != 1 {
			t.Errorf("%q: %d pictures", tc.args, len(got))
			continue
		}
		if got[0].Cols != tc.cols || got[0].Rows != tc.rows {
			t.Errorf("%q gave %dx%d cells, want %dx%d",
				tc.args, got[0].Cols, got[0].Rows, tc.cols, tc.rows)
		}
	}
}

// A picture never asks for more cells than the screen has.
func TestAPictureIsHeldInsideTheScreen(t *testing.T) {
	term := New(10, 4, DefaultPalette(), 100, Callbacks{})

	sendImage(t, term, "inline=1;width=500;height=500", aPNG(t, 64, 64))

	got := term.Images()
	if len(got) != 1 {
		t.Fatalf("%d pictures", len(got))
	}
	if got[0].Cols > 10 || got[0].Rows > 4 {
		t.Errorf("it was given %dx%d cells on a 10x4 screen", got[0].Cols, got[0].Rows)
	}
}

// A full-screen program has its own picture of the world, and the
// lines these sit on are not on it.
func TestTheAlternateScreenPlacesNoPictures(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})
	sendImage(t, term, "inline=1;width=4;height=2", aPNG(t, 32, 32))

	term.Write([]byte("\x1b[?1049h"))

	if got := term.Placed(); len(got) != 0 {
		t.Errorf("%d placed on the alternate screen, want none", len(got))
	}
}

// A reset forgets them, the same as it forgets the title.
func TestAResetForgetsThePictures(t *testing.T) {
	term := New(40, 10, DefaultPalette(), 100, Callbacks{})
	sendImage(t, term, "inline=1;width=4;height=2", aPNG(t, 32, 32))

	term.Write([]byte("\x1bc"))

	if got := term.Images(); len(got) != 0 {
		t.Errorf("%d pictures survived a reset", len(got))
	}
}
