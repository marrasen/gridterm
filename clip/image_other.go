//go:build !windows && !linux

package clip

import (
	"errors"
	"image"
	"runtime"
)

// HasText reports whether there is any text on the clipboard.
//
// Only Windows can say. Everywhere else the answer is yes, so the
// library that reads text is left to speak for itself.
func HasText() bool { return true }

// Image returns the image on the clipboard.
//
// Only Windows is wired up. Everywhere else it reports that there is
// none, so the command says the clipboard holds no image rather than
// failing in a way that reads like a fault.
func Image() (image.Image, bool, error) {
	_ = runtime.GOOS
	return nil, false, nil
}

// SetImage puts an image on the clipboard.
//
// Only Windows is wired up, so everywhere else this says so rather than
// reporting that it worked and leaving the clipboard untouched.
func SetImage(image.Image) error {
	return errors.New("this kakel cannot put an image on the clipboard of " + runtime.GOOS)
}
