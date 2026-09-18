//go:build !windows

package main

import (
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
