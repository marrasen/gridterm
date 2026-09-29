package files

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/vfs"
)

// anImageFile is a reader on an image, laid out and read.
func anImageFile(t *testing.T, w, h int, cols, rows int) *Reader {
	t.Helper()
	r := NewReader("shot.png", "/tmp/shot.png")
	r.Style = readerStyle()
	r.ReadPic = func(then func(Pic, error)) {
		then(Pic{
			Img:  image.NewRGBA(image.Rect(0, 0, w, h)),
			Kind: "png",
			Was:  image.Pt(w, h),
		}, nil)
	}
	r.Layout(ui.Size{Cols: cols, Rows: rows})
	r.Open()
	return r
}

// A file named like an image is shown as one, before it has been read.
func TestAnImageFileIsShownAsAnImage(t *testing.T) {
	r := NewReader("shot.png", "/tmp/shot.png")

	if !r.ShowsAnImage() {
		t.Error("a .png is not shown as an image")
	}
	if NewReader("notes.txt", "/tmp/notes.txt").ShowsAnImage() {
		t.Error("a .txt is shown as an image")
	}
}

// The image is handed on once it has been read, and the top line says
// how big it is.
func TestAnImageIsHandedOnWithItsSize(t *testing.T) {
	r := anImageFile(t, 640, 480, 40, 10)

	if r.Image() == nil {
		t.Fatal("the reader has no image after its read")
	}
	g := drawReader(r, 40, 10)
	if got := readerRow(g, 0); !strings.Contains(got, "640×480 png") {
		t.Errorf("the top row is %q, want it to say how big the image is", got)
	}
}

// The room for the image is the pane less the name and the bar, so a
// window drawing it does not cover either.
func TestTheImageRoomLeavesTheNameAndTheBar(t *testing.T) {
	r := anImageFile(t, 64, 64, 40, 10)

	got := r.ImageRoom()

	want := ui.Rect{X: 0, Y: 1, Cols: 40, Rows: 8}
	if got != want {
		t.Errorf("the image goes in %v, want %v", got, want)
	}
	// A pane with no room for it has none.
	r.Layout(ui.Size{Cols: 40, Rows: 2})
	if got := r.ImageRoom(); got != (ui.Rect{}) {
		t.Errorf("a pane of two rows offers %v for an image, want nothing", got)
	}
}

// A read that failed drops the image: a pane showing an old image
// under a fresh error says the file is fine when it is not.
func TestAFailedReadDropsTheImage(t *testing.T) {
	r := anImageFile(t, 64, 64, 40, 10)
	r.ReadPic = func(then func(Pic, error)) {
		then(Pic{}, errors.New("the file has gone"))
	}

	r.Open()

	if r.Image() != nil {
		t.Error("the reader kept its image through a failed read")
	}
	g := drawReader(r, 40, 10)
	if got := readerRow(g, 1); !strings.Contains(got, "the file has gone") {
		t.Errorf("the second row is %q, want the reason", got)
	}
}

// The bar offers what there is to do with an image, and no more: there
// is nothing to search and nowhere to scroll.
func TestTheImageBarOffersWhatThereIsToDo(t *testing.T) {
	r := anImageFile(t, 64, 64, 60, 10)

	g := drawReader(r, 60, 10)

	bar := readerRow(g, 9)
	for _, want := range []string{"Reload", "Close"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the bar reads %q, want it to offer %q", bar, want)
		}
	}
	for _, gone := range []string{"Find", "Hex", "Follow"} {
		if strings.Contains(bar, gone) {
			t.Errorf("the bar reads %q, want %q left off an image", bar, gone)
		}
	}
}

// Keys an image has no use for are swallowed all the same, so a letter
// typed into one does not reach the shell behind it.
func TestAnImageSwallowsTheKeysItHasNoUseFor(t *testing.T) {
	r := anImageFile(t, 64, 64, 40, 10)
	reads := 0
	r.ReadPic = func(then func(Pic, error)) { reads++ }

	typed(t, r, "/n:")
	press(t, r, input.KeyDown)
	press(t, r, input.KeyPageDown)

	if _, _, on := r.Asking(); on {
		t.Error("an image put a search prompt up")
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

// Closing an image works the same way closing a file does.
func TestAnImageCloses(t *testing.T) {
	r := anImageFile(t, 64, 64, 40, 10)
	closed := 0
	r.OnClose = func() { closed++ }

	press(t, r, input.KeyQ)

	if closed != 1 {
		t.Errorf("q closed it %d times, want once", closed)
	}
}

// Ctrl+H on an image shows the file as lines instead, which is the way
// out of a file named .png that is not one.
func TestCtrlHOnAnImageShowsItAsLines(t *testing.T) {
	r := anImageFile(t, 64, 64, 60, 10)
	r.Read = func(then func([]string, bool, error)) { then([]string{"one", "two"}, false, nil) }

	if _, err := r.HandleKey(input.Event{
		Kind: input.KeyPress, Key: input.KeyH, Mods: input.ModCtrl,
	}); err != nil {
		t.Fatalf("Ctrl+H: %v", err)
	}

	if r.ShowsAnImage() {
		t.Fatal("it is still showing the file as an image")
	}
	if r.Image() != nil {
		t.Error("it kept the image")
	}
	if got := r.Lines(); got != 2 {
		t.Errorf("it holds %d lines, want the two the file has", got)
	}
	// And the bar is a file's bar now.
	g := drawReader(r, 60, 10)
	if got := readerRow(g, 9); !strings.Contains(got, "Find") {
		t.Errorf("the bar reads %q, want a file's keys", got)
	}
}

// Tailing an image does nothing: an image is not appended to, and a
// pane stuck saying "(following)" would ask a machine at the far end
// about the file for ever.
func TestAnImageIsNotFollowed(t *testing.T) {
	r := anImageFile(t, 64, 64, 60, 10)

	r.Follow(true)

	if r.Following() {
		t.Error("an image is being followed")
	}
	g := drawReader(r, 60, 10)
	if got := readerRow(g, 0); strings.Contains(got, "following") {
		t.Errorf("the top row reads %q", got)
	}
}

// aNoisyPNG writes a PNG that does not compress away, so a read of it
// comes back in more than one piece.
func aNoisyPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for y := range h {
		for x := range w {
			// A cheap spread of colours. Anything regular enough to
			// compress would leave a file too small to read twice.
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.RGBA{
				R: uint8(seed >> 24), G: uint8(seed >> 16), B: uint8(seed >> 8), A: 0xff,
			})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encode it: %v", err)
	}
	at := filepath.Join(t.TempDir(), "noisy.png")
	if err := os.WriteFile(at, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}
	return at
}

// An image counts up as it arrives, the same as a file of lines. A
// image may be tens of megabytes, and over a tunnelled link at fifty
// kilobytes a second that is a wait worth measuring.
func TestAnImageCountsUpAsItArrives(t *testing.T) {
	at := aNoisyPNG(t, 400, 400)
	listed, err := os.Stat(at)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	var seen []int64
	pic, err := ReadImageWatched(vfs.NewLocal(), at, 4096, func(read int64) {
		seen = append(seen, read)
	})

	if err != nil {
		t.Fatalf("read the image: %v", err)
	}
	if pic.Img == nil {
		t.Fatal("no image came back")
	}
	if len(seen) < 2 {
		t.Fatalf("the watcher was told %d times for a %d byte file,"+
			" want it told as the read went", len(seen), listed.Size())
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("the count went %d then %d, want it only to grow", seen[i-1], seen[i])
		}
	}
	if got, want := seen[len(seen)-1], listed.Size(); got != want {
		t.Errorf("the last count was %d, want the whole file at %d", got, want)
	}
}

// An image nobody is watching still reads, which is what ReadImage
// is.
func TestAnImageNobodyIsWatchingStillReads(t *testing.T) {
	pic, err := ReadImage(vfs.NewLocal(), pngOf(t, "small.png", 8, 8), 4096)

	if err != nil {
		t.Fatalf("read the image: %v", err)
	}
	if pic.Img == nil {
		t.Error("no image came back")
	}
}

// What the pane says while an image is arriving.
func TestAnImageSaysHowFarOfHowMuchHasArrived(t *testing.T) {
	r := NewReader("shot.png", "/tmp/shot.png")
	r.Style = readerStyle()
	r.ReadPic = func(then func(Pic, error)) {}
	r.Expect = 4_400_000
	r.Layout(ui.Size{Cols: 44, Rows: 8})
	r.Open()

	r.ReadSoFar(2_000_000)

	if got := readerRow(drawReader(r, 44, 8), 0); !strings.Contains(got, "1.9 of 4.2 MB") {
		t.Errorf("the top row is %q, want it to say how far of how much", got)
	}
}
