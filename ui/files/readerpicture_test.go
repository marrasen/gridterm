package files

import (
	"errors"
	"image"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// aPictureFile is a reader on a picture, laid out and read.
func aPictureFile(t *testing.T, w, h int, cols, rows int) *Reader {
	t.Helper()
	r := NewReader("shot.png", "/tmp/shot.png")
	r.Style = readerStyle()
	r.ReadPic = func(then func(image.Image, string, error)) {
		then(image.NewRGBA(image.Rect(0, 0, w, h)), "png", nil)
	}
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Open()
	return r
}

// A file named like a picture is shown as one, before it has been read.
func TestAPictureFileIsShownAsAPicture(t *testing.T) {
	r := NewReader("shot.png", "/tmp/shot.png")

	if !r.ShowsAPicture() {
		t.Error("a .png is not shown as a picture")
	}
	if NewReader("notes.txt", "/tmp/notes.txt").ShowsAPicture() {
		t.Error("a .txt is shown as a picture")
	}
}

// The picture is handed on once it has been read, and the top line says
// how big it is.
func TestAPictureIsHandedOnWithItsSize(t *testing.T) {
	r := aPictureFile(t, 640, 480, 40, 10)

	if r.Picture() == nil {
		t.Fatal("the reader has no picture after its read")
	}
	g := drawReader(r, 40, 10)
	if got := readerRow(g, 0); !strings.Contains(got, "640×480 png") {
		t.Errorf("the top row is %q, want it to say how big the picture is", got)
	}
}

// The room for the picture is the pane less the name and the bar, so a
// window drawing it does not cover either.
func TestThePictureRoomLeavesTheNameAndTheBar(t *testing.T) {
	r := aPictureFile(t, 64, 64, 40, 10)

	got := r.PictureRoom()

	want := ui.Rect{X: 0, Y: 1, Cols: 40, Rows: 8}
	if got != want {
		t.Errorf("the picture goes in %v, want %v", got, want)
	}
	// A pane with no room for it has none.
	r.Layout(ui.Size{Cols: 40, Rows: 2})
	if got := r.PictureRoom(); got != (ui.Rect{}) {
		t.Errorf("a pane of two rows offers %v for a picture, want nothing", got)
	}
}

// A read that failed drops the picture: a pane showing an old picture
// under a fresh error says the file is fine when it is not.
func TestAFailedReadDropsThePicture(t *testing.T) {
	r := aPictureFile(t, 64, 64, 40, 10)
	r.ReadPic = func(then func(image.Image, string, error)) {
		then(nil, "", errors.New("the file has gone"))
	}

	r.Open()

	if r.Picture() != nil {
		t.Error("the reader kept its picture through a failed read")
	}
	g := drawReader(r, 40, 10)
	if got := readerRow(g, 1); !strings.Contains(got, "the file has gone") {
		t.Errorf("the second row is %q, want the reason", got)
	}
}

// The bar offers what there is to do with a picture, and no more: there
// is nothing to search and nowhere to scroll.
func TestThePictureBarOffersWhatThereIsToDo(t *testing.T) {
	r := aPictureFile(t, 64, 64, 60, 10)

	g := drawReader(r, 60, 10)

	bar := readerRow(g, 9)
	for _, want := range []string{"Reread", "Close"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the bar reads %q, want it to offer %q", bar, want)
		}
	}
	for _, gone := range []string{"Find", "Hex", "Follow"} {
		if strings.Contains(bar, gone) {
			t.Errorf("the bar reads %q, want %q left off a picture", bar, gone)
		}
	}
}

// Keys a picture has no use for are swallowed all the same, so a letter
// typed into one does not reach the shell behind it.
func TestAPictureSwallowsTheKeysItHasNoUseFor(t *testing.T) {
	r := aPictureFile(t, 64, 64, 40, 10)
	reads := 0
	r.ReadPic = func(then func(image.Image, string, error)) { reads++ }

	typed(t, r, "/n:")
	press(t, r, input.KeyDown)
	press(t, r, input.KeyPageDown)

	if _, _, on := r.Asking(); on {
		t.Error("a picture put a search prompt up")
	}
	if reads != 0 {
		t.Errorf("those keys set off %d reads", reads)
	}
	// And Ctrl+R still rereads it.
	if _, err := r.HandleKey(input.Event{
		Kind: input.KeyPress, Key: input.KeyR, Mods: input.ModCtrl,
	}); err != nil {
		t.Fatalf("Ctrl+R: %v", err)
	}
	if reads != 1 {
		t.Errorf("Ctrl+R set off %d reads, want one", reads)
	}
}

// Closing a picture works the same way closing a file does.
func TestAPictureCloses(t *testing.T) {
	r := aPictureFile(t, 64, 64, 40, 10)
	closed := 0
	r.OnClose = func() { closed++ }

	press(t, r, input.KeyQ)

	if closed != 1 {
		t.Errorf("q closed it %d times, want once", closed)
	}
}
