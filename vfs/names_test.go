package vfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestANameIsNoPath(t *testing.T) {
	f := NewLocal()
	for name, ok := range map[string]bool{"notes.txt": true, ".hidden": true, "": false, ".": false, "..": false, "a/b": false} {
		if err := PlainName(f, name); (err == nil) != ok {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestANameAlreadyInTheFolderIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewLocal()
	if err := NameFree(f, dir, "notes.txt"); err == nil {
		t.Fatal("a name that is there was free")
	}
	if err := NameFree(f, dir, "other.txt"); err != nil {
		t.Fatalf("a name that is not there: %v", err)
	}
}
