package winattrs

import (
	"os"
	"path/filepath"
	"syscall"
)

func init() { Lookup = lookup }

// lookup reads the attributes of the item at the SFTP path p, as os
// reads them, long paths included, and without reading the item, which
// for a file kept online would download it.
func lookup(home, p string, follow bool) (uint32, bool) {
	full := filepath.FromSlash(Resolve(filepath.ToSlash(home), p))
	stat := os.Lstat
	if follow {
		stat = os.Stat
	}
	info, err := stat(full)
	if err != nil {
		return 0, false
	}
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return 0, false
	}
	return d.FileAttributes, true
}
