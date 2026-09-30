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

// A move whose rename skips one name and then fails across devices
// moves the rest, copied and taken away: the skip is the renames', not
// the copy's.
func TestAMoveAcrossDevicesAfterASkipStillMoves(t *testing.T) {
	from := local(t)
	write(t, from.real, "a.txt", "mine")
	write(t, from.real, "one.txt", "the body")
	into := t.TempDir()
	write(t, into, "a.txt", "theirs")
	asked := make(chan Conflict, 4)
	fs := mounted{from.fs}
	j := New(1).Start(t.Context(), Op{Kind: Move, From: fs, At: from.at, Names: []string{"a.txt", "one.txt"}, To: fs, Into: into},
		Options{Ask: askWith(asked, Choice{What: Skip})})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, into, "one.txt"); got != "the body" {
		t.Fatalf("what was moved holds %q", got)
	}
	gone(t, from.real, "one.txt")
	if got := read(t, from.real, "a.txt"); got != "mine" {
		t.Fatalf("the name skipped holds %q where it was", got)
	}
}
