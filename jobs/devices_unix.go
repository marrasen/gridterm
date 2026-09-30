//go:build !windows

package jobs

import (
	"errors"
	"syscall"
)

// acrossDevices reports whether a rename failed as its two ends are on
// two devices.
func acrossDevices(err error) bool { return errors.Is(err, syscall.EXDEV) }
