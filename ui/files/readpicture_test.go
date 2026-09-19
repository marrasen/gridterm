package files

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
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
	pic, err := ReadPicture(vfs.NewLocal(), pngOf(t, "a.png", 12, 7), 4096)

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if pic.Kind != "png" {
		t.Errorf("it read a %q, want a png", pic.Kind)
	}
	if got := pic.Img.Bounds(); got.Dx() != 12 || got.Dy() != 7 {
		t.Errorf("the picture is %v, want 12 by 7", got)
	}
	if want := image.Pt(12, 7); pic.Was != want {
		t.Errorf("it says the picture is %v in the file, want %v", pic.Was, want)
	}
}

// A picture wider than a texture can be is shrunk to fit, keeping its
// shape, and still says how big it is in the file.
//
// A texture past the limit is not an error the window can catch: asking
// for one brings the window down.
func TestAPictureTooWideForATextureIsShrunk(t *testing.T) {
	pic, err := ReadPicture(vfs.NewLocal(), pngOf(t, "wide.png", 800, 200), 100)

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	got := pic.Img.Bounds()
	if got.Dx() > 100 || got.Dy() > 100 {
		t.Errorf("the picture came out %v, want it inside 100 by 100", got)
	}
	if got.Dx() != 100 || got.Dy() != 25 {
		t.Errorf("the picture came out %v, want 100 by 25: the same shape", got)
	}
	if want := image.Pt(800, 200); pic.Was != want {
		t.Errorf("it says the picture is %v in the file, want %v", pic.Was, want)
	}
}

// A picture that fits comes back at its own size.
func TestAPictureThatFitsKeepsItsSize(t *testing.T) {
	pic, err := ReadPicture(vfs.NewLocal(), pngOf(t, "small.png", 64, 64), 4096)

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if got := pic.Img.Bounds(); got.Dx() != 64 || got.Dy() != 64 {
		t.Errorf("the picture came out %v, want 64 by 64", got)
	}
}

// A file that is not there is an error rather than an empty picture.
func TestAPictureThatIsNotThereIsAnError(t *testing.T) {
	_, err := ReadPicture(vfs.NewLocal(), filepath.Join(t.TempDir(), "nope.png"), 4096)

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

	_, err := ReadPicture(vfs.NewLocal(), at, 4096)

	if err == nil {
		t.Fatal("a file of words read as a picture")
	}
	if !strings.Contains(err.Error(), "picture") {
		t.Errorf("it said %q, want it to say what it could not do", err)
	}
}

// A picture with more pixels than the reader shows is refused by its
// header, before any pixel is asked for.
//
// The test writes the header and nothing else, which is how it can name
// a picture of forty thousand square without building one.
func TestAPictureOfTooManyPixelsIsRefused(t *testing.T) {
	at := filepath.Join(t.TempDir(), "huge.png")
	if err := os.WriteFile(at, pngHeader(40000, 40000), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}

	_, err := ReadPicture(vfs.NewLocal(), at, 4096)

	if err == nil {
		t.Fatal("a picture of 40000 by 40000 was decoded")
	}
	if !strings.Contains(err.Error(), "more than this shows") {
		t.Errorf("it said %q, want it to say the picture is too big", err)
	}
}

// A picture of nought by nought is refused. A BMP that size decodes
// without complaint, and asking for a texture that size brings the
// window down.
func TestAPictureWithNoPixelsIsRefused(t *testing.T) {
	at := filepath.Join(t.TempDir(), "empty.bmp")
	if err := os.WriteFile(at, emptyBMP(), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}

	pic, err := ReadPicture(vfs.NewLocal(), at, 4096)

	if err == nil {
		t.Fatalf("a picture of nought by nought came back as %v", pic.Img.Bounds())
	}
	if !strings.Contains(err.Error(), "nothing to show") {
		t.Errorf("it said %q, want it to say there is nothing there", err)
	}
}

// pngHeader is a PNG signature and an IHDR chunk saying the picture is
// this big, and nothing after it. DecodeConfig reads no further.
func pngHeader(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	_ = binary.Write(&ihdr, binary.BigEndian, w)
	_ = binary.Write(&ihdr, binary.BigEndian, h)
	// Eight bits a channel, colour with alpha, and none of the optional
	// coding.
	ihdr.Write([]byte{8, 6, 0, 0, 0})

	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	_ = binary.Write(&out, binary.BigEndian, uint32(ihdr.Len()-4))
	out.Write(ihdr.Bytes())
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

// emptyBMP is a BMP header saying the picture is nought by nought, which
// the decoder takes without complaint.
func emptyBMP() []byte {
	out := make([]byte, 54)
	out[0], out[1] = 'B', 'M'
	binary.LittleEndian.PutUint32(out[2:], 54)
	binary.LittleEndian.PutUint32(out[10:], 54)
	binary.LittleEndian.PutUint32(out[14:], 40)
	// Width and height are left at nought, which is what this is for.
	binary.LittleEndian.PutUint16(out[26:], 1)
	binary.LittleEndian.PutUint16(out[28:], 24)
	return out
}
