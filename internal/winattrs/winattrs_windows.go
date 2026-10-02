package winattrs

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func init() { Lookup, Space = lookup, space }

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

// space reads how much room the volume holding the SFTP path p has, as
// Windows says: free to the user, and in all. The top of the drives, /,
// is on no volume.
func space(home, p string) (free, total uint64, ok bool) {
	full := filepath.FromSlash(Resolve(filepath.ToSlash(home), p))
	if len(full) < 2 || full[1] != ':' {
		return 0, 0, false
	}
	name, err := windows.UTF16PtrFromString(full)
	if err != nil {
		return 0, 0, false
	}
	var avail, all, allFree uint64
	if err := windows.GetDiskFreeSpaceEx(name, &avail, &all, &allFree); err != nil {
		return 0, 0, false
	}
	return avail, all, true
}
