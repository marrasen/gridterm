package clip

import (
	"bytes"
	"fmt"
	"image/png"
)

// SetPNG puts an image, given as the bytes of a PNG, on the clipboard.
// It is how a window takes an image another window pasted, so a
// program running here can be handed it.
//
// The bytes are decoded before anything is put on the clipboard: a
// client that sent something that is not an image must not empty the
// clipboard of whoever is sitting here.
func SetPNG(raw []byte) error {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("that is not an image this window can read: %w", err)
	}
	return SetImage(img)
}
