package vt

import (
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// screenOf is the escape sequences that would draw a terminal's live
// screen, the way a window hands one to a window watching it.
func screenOf(t *testing.T, term *Terminal, cols, rows int) string {
	t.Helper()
	pal := DefaultPalette()
	g := grid.New(cols, rows, pal.FG, pal.BG)
	term.RenderLive(g)
	full := term.Screenful()
	if full.Alt {
		under := grid.New(cols, rows, pal.FG, pal.BG)
		if term.RenderUnder(under) {
			full.Under = under
		}
	}
	full.Images = term.LivePlaced()
	return Repaint(g, full)
}

// A picture a program put in a pane reaches a window watching it, on
// the row it is on and at the size it was given.
func TestAPictureReachesAWatchingWindow(t *testing.T) {
	here := New(40, 10, DefaultPalette(), 100, Callbacks{})
	here.Write([]byte("before\r\n"))
	sendImage(t, here, "inline=1;width=10;height=4", aPNG(t, 80, 64))
	want := here.LivePlaced()
	if len(want) != 1 {
		t.Fatalf("%d pictures on the screen that is sent", len(want))
	}

	there := New(40, 10, DefaultPalette(), 100, Callbacks{})
	there.Write([]byte(screenOf(t, here, 40, 10)))

	got := there.Placed()
	if len(got) != 1 {
		t.Fatalf("the watching window has %d pictures, want one", len(got))
	}
	if got[0].Top != want[0].Top || got[0].Col != want[0].Col {
		t.Errorf("it landed at row %d column %d, want row %d column %d",
			got[0].Top, got[0].Col, want[0].Top, want[0].Col)
	}
	if got[0].Cols != want[0].Cols || got[0].Rows != want[0].Rows {
		t.Errorf("it was given %dx%d cells, want %dx%d",
			got[0].Cols, got[0].Rows, want[0].Cols, want[0].Rows)
	}
	if got[0].Img == nil || got[0].Img.Bounds() != want[0].Img.Bounds() {
		t.Error("the pixels did not come")
	}
}

// The text around a picture still lands where it should. The picture
// is placed by row, so it does not push the screen about the way the
// sequence a program sends does.
func TestAPictureOnTheWireDoesNotMoveTheText(t *testing.T) {
	here := New(40, 10, DefaultPalette(), 100, Callbacks{})
	sendImage(t, here, "inline=1;width=10;height=4", aPNG(t, 80, 64))
	here.Write([]byte("under the picture"))

	there := New(40, 10, DefaultPalette(), 100, Callbacks{})
	there.Write([]byte(screenOf(t, here, 40, 10)))

	theirs, ours := liveGrid(t, there, 40, 10), liveGrid(t, here, 40, 10)
	if got, want := rowText(theirs, 4), rowText(ours, 4); got != want {
		t.Errorf("row 4 reads %q, want %q", got, want)
	}
	if cur, was := theirs.Cursor(), ours.Cursor(); cur.X != was.X || cur.Y != was.Y {
		t.Errorf("the cursor is at %d,%d, want %d,%d", cur.X, cur.Y, was.X, was.Y)
	}
}

// liveGrid draws a terminal's live screen.
func liveGrid(t *testing.T, term *Terminal, cols, rows int) *grid.Grid {
	t.Helper()
	pal := DefaultPalette()
	g := grid.New(cols, rows, pal.FG, pal.BG)
	term.RenderLive(g)
	return g
}

// A screen sent again takes away the pictures the one before it left,
// so a picture does not sit on a line that now says something else.
func TestANewScreenForgetsTheOldPictures(t *testing.T) {
	here := New(40, 10, DefaultPalette(), 100, Callbacks{})
	sendImage(t, here, "inline=1;width=10;height=4", aPNG(t, 80, 64))

	there := New(40, 10, DefaultPalette(), 100, Callbacks{})
	there.Write([]byte(screenOf(t, here, 40, 10)))
	if len(there.Placed()) != 1 {
		t.Fatal("the picture did not arrive in the first place")
	}

	// The same pane, with the picture gone and only text on it.
	plain := New(40, 10, DefaultPalette(), 100, Callbacks{})
	plain.Write([]byte("just text\r\n"))
	there.Write([]byte(screenOf(t, plain, 40, 10)))

	if got := there.Images(); len(got) != 0 {
		t.Errorf("%d pictures from the screen before are still held", len(got))
	}
}

// A picture on the ordinary screen travels while a full-screen program
// is covering it, so quitting that program leaves it behind.
func TestAPictureTravelsUnderAFullScreenProgram(t *testing.T) {
	here := New(40, 10, DefaultPalette(), 100, Callbacks{})
	sendImage(t, here, "inline=1;width=10;height=4", aPNG(t, 80, 64))
	here.Write([]byte("\x1b[?1049h"))

	there := New(40, 10, DefaultPalette(), 100, Callbacks{})
	there.Write([]byte(screenOf(t, here, 40, 10)))

	if got := there.Placed(); len(got) != 0 {
		t.Errorf("%d pictures placed on the alternate screen", len(got))
	}
	there.Write([]byte("\x1b[?1049l"))
	if got := there.Placed(); len(got) != 1 {
		t.Errorf("%d pictures once the full-screen program quit, want one", len(got))
	}
}

// Past the budget the pictures are left out, so a screenful of large
// ones is not a repaint of a gigabyte.
func TestPicturesPastTheBudgetAreLeftOut(t *testing.T) {
	here := New(40, 10, DefaultPalette(), 100, Callbacks{})
	// Each of these is well over a megabyte of PNG, so the budget runs
	// out partway through the screen.
	for range 6 {
		sendImage(t, here, "inline=1;width=2;height=1", aBigPNG(t))
	}
	sent := 0
	for _, at := range here.LivePlaced() {
		sent += len(at.Raw)
	}
	if sent <= WirePicBudget {
		t.Fatalf("the pictures are %d bytes, which is inside the budget of %d", sent, WirePicBudget)
	}

	there := New(40, 10, DefaultPalette(), 100, Callbacks{})
	screen := screenOf(t, here, 40, 10)
	there.Write([]byte(screen))

	got := len(there.Placed())
	if got == 0 {
		t.Error("no pictures came at all")
	}
	if got == len(here.LivePlaced()) {
		t.Errorf("all %d pictures came, want the budget to stop some", got)
	}
}

// A place nobody could honour is dropped rather than guessed at.
func TestAPlacementThatMakesNoSenseIsDropped(t *testing.T) {
	body := aPNG(t, 32, 32)
	for _, args := range []string{
		"place",
		"place;0;0;0;2;" + body,
		"place;0;0;2;0;" + body,
		"place;0;-1;2;2;" + body,
		"place;-5;0;2;2;" + body,
		"place;x;0;2;2;" + body,
		"place;0;0;2;2;!!!not base64!!!",
		"nonsense",
	} {
		term := New(40, 10, DefaultPalette(), 100, Callbacks{})
		term.Write([]byte("\x1b]1338;" + args + "\x07"))
		if got := term.Images(); len(got) != 0 {
			t.Errorf("%q was taken as %d pictures", args, len(got))
		}
	}
}
