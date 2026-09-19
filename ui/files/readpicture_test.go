package files

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/vfs"
)

// pngOf writes a PNG of the given size and returns its path.
func pngOf(t *testing.T, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encode it: %v", err)
	}
	at := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(at, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}
	return at
}

// The names a picture goes by are known, and other files are not
// pictures.
func TestIsPicture(t *testing.T) {
	for name, want := range map[string]bool{
		"a.png":   true,
		"A.PNG":   true,
		"a.jpeg":  true,
		"a.webp":  true,
		"a.tiff":  true,
		"a.go":    false,
		"a":       false,
		"a.png.z": false,
	} {
		if got := IsPicture(name); got != want {
			t.Errorf("IsPicture(%q) = %v, want %v", name, got, want)
		}
	}
}

// A picture reads back at its own size, and says what kind it is.
func TestReadPictureGivesThePicture(t *testing.T) {
	img, kind, err := ReadPicture(vfs.NewLocal(), pngOf(t, "a.png", 12, 7))

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if kind != "png" {
		t.Errorf("it read a %q, want a png", kind)
	}
	if got := img.Bounds(); got.Dx() != 12 || got.Dy() != 7 {
		t.Errorf("the picture is %v, want 12 by 7", got)
	}
}

// A file that is not there is an error rather than an empty picture.
func TestAPictureThatIsNotThereIsAnError(t *testing.T) {
	_, _, err := ReadPicture(vfs.NewLocal(), filepath.Join(t.TempDir(), "nope.png"))

	if err == nil {
		t.Fatal("a picture that is not there read as one that is")
	}
}

// A file that is not a picture says so rather than coming back blank.
func TestAFileThatIsNotAPictureSaysSo(t *testing.T) {
	at := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(at, []byte("this is not a png"), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}

	_, _, err := ReadPicture(vfs.NewLocal(), at)

	if err == nil {
		t.Fatal("a file of words read as a picture")
	}
	if !strings.Contains(err.Error(), "picture") {
		t.Errorf("it said %q, want it to say what it could not do", err)
	}
}

// A picture with more pixels than the reader shows is refused, before
// the pixels are asked for: the file itself is small.
func TestAPictureOfTooManyPixelsIsRefused(t *testing.T) {
	// A blank PNG compresses to almost nothing, so this is a small file
	// holding a very large picture.
	side := 1
	for side*side <= MostPicturePixels {
		side *= 2
	}

	_, _, err := ReadPicture(vfs.NewLocal(), pngOf(t, "big.png", side, side))

	if err == nil {
		t.Fatalf("a picture of %d by %d was decoded", side, side)
	}
	if !strings.Contains(err.Error(), "more than this shows") {
		t.Errorf("it said %q, want it to say the picture is too big", err)
	}
}
