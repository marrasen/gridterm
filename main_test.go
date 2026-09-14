package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFonts(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	regular := write("regular.ttf", "R")
	italic := write("italic.ttf", "I")

	t.Run("regular only", func(t *testing.T) {
		f, err := loadFonts(regular)
		if err != nil {
			t.Fatalf("loadFonts: %v", err)
		}
		if string(f.Regular) != "R" || f.Bold != nil || f.Italic != nil {
			t.Errorf("got %+v", f)
		}
	})

	t.Run("empty entry is skipped", func(t *testing.T) {
		f, err := loadFonts(regular + ",," + italic)
		if err != nil {
			t.Fatalf("loadFonts: %v", err)
		}
		if f.Bold != nil {
			t.Error("the empty bold entry loaded something")
		}
		if string(f.Italic) != "I" {
			t.Errorf("italic = %q, want %q", f.Italic, "I")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, err := loadFonts(filepath.Join(dir, "absent.ttf")); err == nil {
			t.Error("a missing file was accepted")
		}
	})

	t.Run("no regular", func(t *testing.T) {
		if _, err := loadFonts("," + italic); err == nil {
			t.Error("a list with no regular font was accepted")
		}
	})

	t.Run("too many files", func(t *testing.T) {
		if _, err := loadFonts(regular + ",a,b,c,d"); err == nil {
			t.Error("five files were accepted")
		}
	})
}
