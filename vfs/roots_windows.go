//go:build windows

package vfs

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// localRoots is one root per drive, which is the only way to reach
// another drive: going up from C: leads nowhere, because there is
// nothing above it.
//
// Asked of the machine as a bitmap rather than worked out by looking at
// twenty-six paths. A probe cannot tell a drive that is not there from
// one that is there and would not answer, and this runs on the goroutine
// that draws, where twenty-six of them is twenty-six chances to wait on
// an empty card reader.
func localRoots() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		// Nothing was read, so nothing can be said about what is there.
		// The root is where a path starts whatever the machine has.
		return nil
	}
	var out []string
	for letter := 0; letter < 26; letter++ {
		if mask&(1<<uint(letter)) != 0 {
			out = append(out, string(rune('A'+letter))+`:`+string(filepath.Separator))
		}
	}
	return out
}
