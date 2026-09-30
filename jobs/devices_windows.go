//go:build windows

package jobs

import (
	"errors"

	"golang.org/x/sys/windows"
)

// acrossDevices reports whether a rename failed as its two ends are on
// two volumes.
func acrossDevices(err error) bool { return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) }
