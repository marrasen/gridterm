//go:build !windows

package main

import (
	"image"
	"runtime"
)

// clipboardImage returns the picture on the clipboard.
//
// Only Windows is wired up. Everywhere else it reports that there is
// none, so the command says the clipboard holds no picture rather than
// failing in a way that reads like a fault.
func clipboardImage() (image.Image, bool, error) {
	_ = runtime.GOOS
	return nil, false, nil
}
