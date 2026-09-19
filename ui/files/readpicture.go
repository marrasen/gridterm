package files

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"path"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/marrasen/gridterm/vfs"
)

// MostPictureBytes is the largest picture file a reader will read, and
// MostPicturePixels the most pixels it will decode one into.
//
// Both are needed: a small file can hold a very large picture, and the
// decoded pixels cost four bytes each.
const (
	MostPictureBytes  = 64 << 20
	MostPicturePixels = 32 << 20
)

// Pic is a picture a reader holds.
type Pic struct {
	// Img is what is drawn, and Kind what sort of file it came from.
	Img  image.Image
	Kind string

	// Was is how big the picture is in the file, which is not how big
	// Img is when the picture had to be shrunk to fit a texture.
	Was image.Point
}

// IsPicture reports whether a file is one a reader can show as a
// picture, by what it is called.
func IsPicture(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tif", ".tiff", ".webp":
		return true
	}
	return false
}

// ReadPicture reads a picture file and decodes it, shrinking one bigger
// than side either way so that it fits on a texture.
//
// The caller runs it somewhere that is not the goroutine that draws, so
// the shrinking happens there too.
func ReadPicture(f vfs.FS, at string, side int) (pic Pic, err error) {
	rc, err := f.Open(at)
	if err != nil {
		return Pic{}, err
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()

	// The whole file, because a decoder reads it twice: once for its
	// size and once for its pixels, and a file on another machine cannot
	// be wound back.
	raw, err := io.ReadAll(io.LimitReader(rc, MostPictureBytes+1))
	if err != nil {
		return Pic{}, err
	}
	if len(raw) > MostPictureBytes {
		return Pic{}, fmt.Errorf("the file is over %d bytes, which is more picture than this shows", MostPictureBytes)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Pic{}, fmt.Errorf("work out what kind of picture this is: %w", err)
	}
	if n := int64(cfg.Width) * int64(cfg.Height); n > MostPicturePixels {
		return Pic{}, fmt.Errorf("the picture is %d by %d, which is more than this shows", cfg.Width, cfg.Height)
	}

	img, kind, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Pic{}, fmt.Errorf("decode the picture: %w", err)
	}
	b := img.Bounds()
	return Pic{Img: fitPicture(img, side), Kind: kind, Was: image.Pt(b.Dx(), b.Dy())}, nil
}

// fitPicture shrinks a picture bigger than side either way, keeping its
// shape, and leaves a smaller one alone.
//
// A texture has a largest size, and asking for one past it is not an
// error the window can catch: it brings the window down.
func fitPicture(img image.Image, side int) image.Image {
	b := img.Bounds()
	if side <= 0 || (b.Dx() <= side && b.Dy() <= side) {
		return img
	}
	scale := min(float64(side)/float64(b.Dx()), float64(side)/float64(b.Dy()))
	w := max(int(float64(b.Dx())*scale), 1)
	h := max(int(float64(b.Dy())*scale), 1)

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}
