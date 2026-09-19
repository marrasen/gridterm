package files

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/marrasen/gridterm/vfs"
)

// A file another program is holding open with no sharing cannot be read,
// and that comes back as an error rather than as an empty file.
//
// Windows lets a writer refuse everyone else, so tailing a log can fail
// to open at all. A reader showing nothing would look like an empty file.
func TestAFileHeldOpenWithNoSharingIsAnError(t *testing.T) {
	at := filepath.Join(t.TempDir(), "held.log")
	if err := os.WriteFile(at, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}
	name, err := windows.UTF16PtrFromString(at)
	if err != nil {
		t.Fatalf("name it: %v", err)
	}
	// No sharing at all, which is what a program writing a log without
	// FILE_SHARE_READ does.
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("hold it open: %v", err)
	}
	defer func() {
		if err := windows.CloseHandle(h); err != nil {
			t.Errorf("let it go: %v", err)
		}
	}()

	lines, _, err := ReadFile(vfs.NewLocal(), at)

	if err == nil {
		t.Fatalf("a file nobody may open read as %d lines", len(lines))
	}
	if lines != nil {
		t.Errorf("it gave %d lines along with the error", len(lines))
	}
}
