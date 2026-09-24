package jobs

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/gridterm/vfs"
)

// aZip is the bytes of a zip holding one file.
func aZip(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.Create("inside.txt")
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	if _, err := f.Write([]byte("inside")); err != nil {
		t.Fatalf("zip: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip: %v", err)
	}
	return b.Bytes()
}

// withZips is a side as a browser pane reads it: its zips shown as
// directories to walk into.
func withZips(s side) side {
	s.fs = vfs.WithArchives(s.fs)
	return s
}

// A folder with a zip in it is copied with the zip in it, byte for byte.
// The copy used to walk into the zip, because a browser pane shows one
// as a directory, and copied what was inside it instead.
func TestCopyAFolderWithAZipInIt(t *testing.T) {
	bothWays(t, func(t *testing.T, from, to side) {
		from, to = withZips(from), withZips(to)
		body := aZip(t)
		write(t, from.real, "tree/one.txt", "one")
		write(t, from.real, "tree/pack.zip", string(body))

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"tree"},
			To: to.fs, Into: to.at,
		}, Options{})
		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}

		got, err := os.ReadFile(filepath.Join(to.real, "tree", "pack.zip"))
		if err != nil {
			t.Fatalf("the zip was not copied as a file: %v", err)
		}
		if !bytes.Equal(got, body) {
			t.Error("the zip that arrived is not the zip that was copied")
		}
		if p := j.Progress(); p.Files != 2 || p.FilesDone != 2 {
			t.Errorf("%d of %d files done, want the text file and the zip", p.FilesDone, p.Files)
		}
	})
}

// A zip copied on its own is a file too, and one copied over another
// replaces it.
func TestCopyAZipOverAZip(t *testing.T) {
	bothWays(t, func(t *testing.T, from, to side) {
		from, to = withZips(from), withZips(to)
		body := aZip(t)
		write(t, from.real, "pack.zip", string(body))
		write(t, to.real, "pack.zip", "an older zip")

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Copy, From: from.fs, At: from.at, Names: []string{"pack.zip"},
			To: to.fs, Into: to.at,
		}, Options{Ask: Always{What: Replace}})
		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}
		if got := read(t, to.real, "pack.zip"); got != string(body) {
			t.Error("the zip was not replaced")
		}
	})
}

// A folder with a zip in it is deleted, zip and all. Walking into the
// zip would have tried to take files out of it one by one, which an
// archive refuses.
func TestDeleteAFolderWithAZipInIt(t *testing.T) {
	bothWays(t, func(t *testing.T, from, _ side) {
		from = withZips(from)
		write(t, from.real, "tree/pack.zip", string(aZip(t)))

		q := New(1)
		j := q.Start(t.Context(), Op{
			Kind: Delete, From: from.fs, At: from.at, Names: []string{"tree"},
		}, Options{})
		if err := ends(t, j); err != nil {
			t.Fatalf("the job: %v", err)
		}
		gone(t, from.real, "tree")
	})
}

// A zip moved over another on one filesystem replaces it. A move there
// is a rename, and the zip in the way was taken for a directory with
// things in it, which a rename will not go over.
func TestMoveAZipOverAZip(t *testing.T) {
	from, to := withZips(local(t)), local(t)
	to.fs, to.at, to.real = from.fs, from.at+"/into", filepath.Join(from.real, "into")
	body := aZip(t)
	write(t, from.real, "pack.zip", string(body))
	write(t, to.real, "pack.zip", string(aZipOf(t, "old.txt")))

	q := New(1)
	j := q.Start(t.Context(), Op{
		Kind: Move, From: from.fs, At: from.at, Names: []string{"pack.zip"},
		To: to.fs, Into: to.at,
	}, Options{Ask: Always{What: Replace}})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if got := read(t, to.real, "pack.zip"); got != string(body) {
		t.Error("the zip was not replaced")
	}
	gone(t, from.real, "pack.zip")
}

// aZipOf is the bytes of a zip holding the names given.
func aZipOf(t *testing.T, names ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range names {
		if _, err := w.Create(name); err != nil {
			t.Fatalf("zip: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip: %v", err)
	}
	return b.Bytes()
}
