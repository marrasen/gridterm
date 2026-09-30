//go:build !windows

package jobs

import (
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/marrasen/kakel/vfs"
)

// mounted is one filesystem where a rename of the file moved fails as
// if the two ends were two devices, as across a mount point.
type mounted struct{ vfs.FS }

func (m mounted) Rename(from, to string) error {
	if strings.HasSuffix(from, "one.txt") {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	return m.FS.Rename(from, to)
}

// A move whose rename fails across devices copies and deletes instead.
func TestAMoveAcrossDevicesCopiesInstead(t *testing.T) {
	from := local(t)
	write(t, from.real, "one.txt", "the body")
	into := t.TempDir()
	fs := mounted{from.fs}
	j := New(1).Start(t.Context(), Op{Kind: Move, From: fs, At: from.at, Names: []string{"one.txt"}, To: fs, Into: into}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, into, "one.txt"); got != "the body" {
		t.Fatalf("what was moved holds %q", got)
	}
	gone(t, from.real, "one.txt")
	if p := j.Progress(); p.Files != 1 {
		t.Fatalf("the job counts %d files", p.Files)
	}
}
