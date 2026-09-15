package glyph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
)

// A candidate that cannot be read is skipped and kept; one that reads
// and will not parse is skipped in silence.
//
// Skipping either way is the fallback Marcus approved: the next
// candidate may have the character, and the user is waiting for a frame.
// The difference is what is worth saying. A disk that failed is; a file
// that is not a face this program can use is not, and a stock install
// has several.
func TestAFallbackThatWillNotLoadIsSkipped(t *testing.T) {
	a, err := NewAtlas(Fonts{Regular: gomono.TTF}, 14, 96)
	if err != nil {
		t.Fatalf("NewAtlas: %v", err)
	}

	dir := t.TempDir()
	// A directory named like a font file: the shape a read failure takes
	// without an ACL to set.
	locked := filepath.Join(dir, "locked.ttf")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	junk := filepath.Join(dir, "junk.ttf")
	if err := os.WriteFile(junk, []byte("not a font"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	good := filepath.Join(dir, "gomono.ttf")
	if err := os.WriteFile(good, gomono.TTF, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	a.fallbackPaths = []string{locked, junk, good}

	if face := a.loadNextFallback(); face == nil {
		t.Fatal("the candidate after the bad ones was not loaded")
	}

	trouble := a.Trouble()
	if len(trouble) != 1 {
		t.Fatalf("it kept %d failures, want the one file it could not read: %v",
			len(trouble), trouble)
	}
	if !strings.Contains(trouble[0].Error(), "locked.ttf") {
		t.Errorf("it said %q, want the file it could not read named", trouble[0])
	}
	// And handed over once: a window polling this every frame would
	// otherwise put the same dialog up for ever.
	if got := a.Trouble(); len(got) != 0 {
		t.Errorf("%d failures are still waiting to be shown", len(got))
	}
}

// The search for a fallback walks the same way the font scan does, so a
// directory it cannot read is reported rather than quietly leaving the
// list shorter.
func TestTheFallbackSearchHandsBackWhatItCouldNotRead(t *testing.T) {
	dir := t.TempDir()
	name := fallbackNames()[0]
	at := filepath.Join(dir, name)
	if err := os.WriteFile(at, gomono.TTF, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	paths, failed := findFallbackFilesIn([]string{dir})

	if len(failed) != 0 {
		t.Fatalf("reading a directory that is there failed: %v", failed)
	}
	if len(paths) != 1 || paths[0] != at {
		t.Fatalf("it found %v, want %q", paths, at)
	}
}

// A failure the window has already shown is not handed out again.
//
// The font scan walks the same directories the fallback search does, so
// without this the same unreadable directory produces two dialogs: one
// when the scan finished and one on the first character that needed a
// fallback.
func TestAFailureAlreadyShownIsNotHandedOutTwice(t *testing.T) {
	a, err := NewAtlas(Fonts{Regular: gomono.TTF}, 14, 96)
	if err != nil {
		t.Fatalf("NewAtlas: %v", err)
	}

	dir := t.TempDir()
	locked := filepath.Join(dir, "locked.ttf")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// What it would say about that file, which is what the font scan
	// says about it too.
	a.fallbackPaths = []string{locked}
	a.loadNextFallback()
	said := a.Trouble()
	if len(said) != 1 {
		t.Fatalf("it kept %d failures, want the one file", len(said))
	}

	// A second atlas, told that one has been shown already.
	b, err := NewAtlas(Fonts{Regular: gomono.TTF}, 14, 96)
	if err != nil {
		t.Fatalf("NewAtlas: %v", err)
	}
	b.Told(said[0].Error())
	b.fallbackPaths = []string{locked}
	b.loadNextFallback()

	if got := b.Trouble(); len(got) != 0 {
		t.Fatalf("it handed out %v, want nothing it had been told about", got)
	}
}

// And a rebuild does not forget what has been shown: stepping through
// the font sizes must not put the same dialog up at every step.
func TestRebuildingTheAtlasRemembersWhatWasShown(t *testing.T) {
	a, err := NewAtlas(Fonts{Regular: gomono.TTF}, 14, 96)
	if err != nil {
		t.Fatalf("NewAtlas: %v", err)
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked.ttf")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	a.fallbackPaths = []string{locked}
	a.loadNextFallback()
	if got := a.Trouble(); len(got) != 1 {
		t.Fatalf("it kept %d failures the first time", len(got))
	}

	if err := a.SetSize(20); err != nil {
		t.Fatalf("SetSize: %v", err)
	}
	a.fallbackPaths = []string{locked}
	a.loadNextFallback()
	if got := a.Trouble(); len(got) != 0 {
		t.Fatalf("after a rebuild it handed out %v again", got)
	}
}
