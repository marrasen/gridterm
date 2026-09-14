//go:build !windows

package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The list is written through a temporary file and a rename, not by
// truncating the file in place.
//
// A read-only file is the way to tell the two apart: a rename is
// governed by the directory, so it goes through, while opening the file
// for writing does not. Unix only, because Windows refuses the rename
// too and cannot separate them.
func TestBookWritesByRenamingRatherThanTruncating(t *testing.T) {
	b := newBook(t)
	if err := b.Put(margit(), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.Chmod(b.Path(), 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if err := b.Put(Host{Name: "second", Address: "second.example"}, ""); err != nil {
		t.Fatalf("Put over a read-only file: %v; the file is written in place", err)
	}
	raw, err := os.ReadFile(b.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "second") {
		t.Fatalf("the change did not land: %s", raw)
	}
}

// The list names machines somebody reaches, so it is theirs to read and
// nobody else's.
func TestBookIsWrittenForItsOwnerOnly(t *testing.T) {
	b := newBook(t)
	if err := b.Put(margit(), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	info, err := os.Stat(b.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("the file is %v, want nothing for anyone else", mode)
	}
	dir, err := os.Stat(filepath.Dir(b.Path()))
	if err != nil {
		t.Fatalf("stat the directory: %v", err)
	}
	// The directory is only ours to check when this made it.
	if dir.Mode().Perm()&0o077 != 0 && strings.Contains(dir.Name(), "gridterm") {
		t.Errorf("the directory is %v, want nothing for anyone else", dir.Mode().Perm())
	}
}
