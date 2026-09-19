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

// IsPicture reports whether a file is one a reader can show as a
// picture, by what it is called.
func IsPicture(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tif", ".tiff", ".webp":
		return true
	}
	return false
}

// ReadPicture reads a picture file and decodes it, giving the picture
// and what kind of file it was.
//
// The caller runs it somewhere that is not the goroutine that draws.
func ReadPicture(f vfs.FS, at string) (img image.Image, kind string, err error) {
	rc, err := f.Open(at)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()

	// The whole file, because a decoder reads it twice: once for its
	// size and once for its pixels, and a file on another machine cannot
	// be wound back.
	raw, err := io.ReadAll(io.LimitReader(rc, MostPictureBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > MostPictureBytes {
		return nil, "", fmt.Errorf("the file is over %d bytes, which is more picture than this shows", MostPictureBytes)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("work out what kind of picture this is: %w", err)
	}
	if n := int64(cfg.Width) * int64(cfg.Height); n > MostPicturePixels {
		return nil, "", fmt.Errorf("the picture is %d by %d, which is more than this shows", cfg.Width, cfg.Height)
	}

	img, kind, err = image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("decode the picture: %w", err)
	}
	return img, kind, nil
}
