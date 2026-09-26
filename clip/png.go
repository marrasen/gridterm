package clip

import (
	"bytes"
	"fmt"
	"image/png"
)

// SetPNG puts a picture, given as the bytes of a PNG, on the clipboard.
// It is how a window takes a picture another window pasted, so a
// program running here can be handed it.
//
// The bytes are decoded before anything is put on the clipboard: a
// client that sent something that is not a picture must not empty the
// clipboard of whoever is sitting here.
func SetPNG(raw []byte) error {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("that is not a picture this window can read: %w", err)
	}
	return SetImage(img)
}
