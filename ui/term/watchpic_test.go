package term

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"testing"
)

// anInlinePicture is the sequence a program sends to put a picture in
// its output, at the size in cells asked for.
func anInlinePicture(t *testing.T, cols, rows int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 320, 160))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return "\x1b]1337;File=inline=1;width=" + strconv.Itoa(cols) +
		";height=" + strconv.Itoa(rows) + ":" +
		base64.StdEncoding.EncodeToString(b.Bytes()) + "\x07"
}

// A picture in one window's pane reaches a window watching that pane,
// with the screen it sits on.
func TestAPictureReachesAWatcher(t *testing.T) {
	here, f := newTestTerm(t, 40, 10, Config{})
	f.feed(t, here, "output above\r\n")
	f.feed(t, here, anInlinePicture(t, 10, 4))
	if len(here.Pictures()) != 1 {
		t.Fatal("the pane did not take the picture")
	}

	w := &screenWatcher{}
	stop, err := here.Watch(w)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	// The screen the watcher was sent, read by the same emulator the
	// watching window would read it with.
	there, g := newTestTerm(t, 40, 10, Config{})
	g.feed(t, there, w.text())

	got := there.Pictures()
	if len(got) != 1 {
		t.Fatalf("the watching window has %d pictures, want one", len(got))
	}
	want := here.Pictures()[0]
	if got[0].Top != want.Top || got[0].Cols != want.Cols || got[0].Rows != want.Rows {
		t.Errorf("it landed at row %d as %dx%d cells, want row %d as %dx%d",
			got[0].Top, got[0].Cols, got[0].Rows, want.Top, want.Cols, want.Rows)
	}
	if got[0].Img == nil {
		t.Fatal("the pixels did not come")
	}
	if at, was := got[0].Img.At(3, 3), want.Img.At(3, 3); at != was {
		t.Errorf("a pixel came out as %v, want %v", color.RGBAModel.Convert(at), color.RGBAModel.Convert(was))
	}
	if got := rowText(draw(there, 40, 10), 0); got != "output above" {
		t.Errorf("row 0 reads %q, want the text above the picture", got)
	}
}

// A picture a program sends while somebody is watching reaches them
// as it happens, because the sequence carrying it is part of what the
// program said and that is what a watcher is sent.
func TestAPictureSentWhileWatchingArrivesAsItHappens(t *testing.T) {
	here, f := newTestTerm(t, 40, 10, Config{})
	w := &screenWatcher{}
	stop, err := here.Watch(w)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	f.feed(t, here, anInlinePicture(t, 10, 4))
	f.feed(t, here, "under the picture")

	there, g := newTestTerm(t, 40, 10, Config{})
	g.feed(t, there, w.text())

	if got := there.Pictures(); len(got) != 1 {
		t.Fatalf("the watching window has %d pictures, want one", len(got))
	}
	if got := rowText(draw(there, 40, 10), 4); got != "under the picture" {
		t.Errorf("row 4 reads %q, want the text under the picture", got)
	}
}
