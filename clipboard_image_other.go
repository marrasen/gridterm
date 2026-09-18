//go:build !windows

package main

import (
	"errors"
	"image"
	"runtime"
)

// clipboardHasText reports whether there is any text on the clipboard.
//
// Only Windows can say. Everywhere else the answer is yes, so the
// library that reads text is left to speak for itself.
func clipboardHasText() bool { return true }

// clipboardImage returns the picture on the clipboard.
//
// Only Windows is wired up. Everywhere else it reports that there is
// none, so the command says the clipboard holds no picture rather than
// failing in a way that reads like a fault.
func clipboardImage() (image.Image, bool, error) {
	_ = runtime.GOOS
	return nil, false, nil
}

// setClipboardImage puts a picture on the clipboard.
//
// Only Windows is wired up, so everywhere else this says so rather than
// reporting that it worked and leaving the clipboard untouched.
func setClipboardImage(image.Image) error {
	return errors.New("this gridterm cannot put a picture on the clipboard of " + runtime.GOOS)
}
