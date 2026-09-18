package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/vfs"
)

// pictureFS is a filesystem a test can write a picture onto and read
// back what it was given.
type pictureFS struct {
	vfs.FS

	home    string
	sep     byte
	made    []string
	written map[string][]byte

	homeErr   error
	statErr   error
	mkdirErr  error
	createErr error
	writeErr  error
}

func newPictureFS() *pictureFS {
	return &pictureFS{home: "/home/marcus", sep: '/', written: map[string][]byte{}}
}

func (f *pictureFS) Sep() byte { return f.sep }

func (f *pictureFS) Home() (string, error) {
	if f.homeErr != nil {
		return "", f.homeErr
	}
	return f.home, nil
}

func (f *pictureFS) Stat(path string) (vfs.Entry, error) {
	if f.statErr != nil {
		return vfs.Entry{}, f.statErr
	}
	return vfs.Entry{}, errors.New("no such thing")
}

func (f *pictureFS) Mkdir(path string, _ fs.FileMode) error {
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	f.made = append(f.made, path)
	return nil
}

func (f *pictureFS) Create(path string, _ fs.FileMode) (io.WriteCloser, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &pictureFile{fs: f, path: path}, nil
}

// pictureFile collects what was written and records it when it closes.
type pictureFile struct {
	fs   *pictureFS
	path string
	buf  bytes.Buffer
}

func (w *pictureFile) Write(b []byte) (int, error) {
	if w.fs.writeErr != nil {
		return 0, w.fs.writeErr
	}
	return w.buf.Write(b)
}

func (w *pictureFile) Close() error {
	w.fs.written[w.path] = append([]byte(nil), w.buf.Bytes()...)
	return nil
}

var pastedAt = time.Date(2026, 9, 19, 14, 5, 6, 0, time.UTC)

// A picture is written under the home directory of whoever the
// connection logs in as, in a directory of its own.
func TestAPictureIsWrittenUnderHomeOnTheMachine(t *testing.T) {
	f := newPictureFS()

	path, err := putPictureOn(f, []byte("a picture"), pastedAt)

	if err != nil {
		t.Fatalf("write it: %v", err)
	}
	if want := "/home/marcus/" + pastedDir; !strings.HasPrefix(path, want+"/") {
		t.Errorf("it wrote %s, want it under %s", path, want)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("it wrote %s, want a name a program can tell is a picture", path)
	}
	if got := string(f.written[path]); got != "a picture" {
		t.Errorf("the file holds %q, want the picture", got)
	}
	if len(f.made) != 1 || f.made[0] != "/home/marcus/"+pastedDir {
		t.Errorf("it made %v, want the one directory", f.made)
	}
}

// A machine that spells its paths with backslashes gets backslashes,
// and a home directory that already ends in one does not get two.
func TestThePathIsSpeltTheMachinesOwnWay(t *testing.T) {
	f := newPictureFS()
	f.home, f.sep = `C:\Users\marcus\`, '\\'

	path, err := putPictureOn(f, []byte("a picture"), pastedAt)

	if err != nil {
		t.Fatalf("write it: %v", err)
	}
	if want := `C:\Users\marcus\` + pastedDir + `\`; !strings.HasPrefix(path, want) {
		t.Errorf("it wrote %s, want it under %s", path, want)
	}
	if strings.Contains(path, `\\`) {
		t.Errorf("it wrote %s, which has a doubled separator in it", path)
	}
}

// A directory that is already there is left alone rather than made
// again, because making one that exists is an error on most machines.
func TestADirectoryAlreadyThereIsNotMadeAgain(t *testing.T) {
	f := newPictureFS()
	// Making one that is already there is an error on most machines, so
	// this fails the test if it is tried.
	f.mkdirErr = errors.New("it is already there")

	if _, err := putPictureOn(&statOK{pictureFS: f}, []byte("a picture"), pastedAt); err != nil {
		t.Fatalf("write it: %v", err)
	}
	if len(f.made) != 0 {
		t.Errorf("it made %v, and the directory was already there", f.made)
	}
}

// statOK is a pictureFS whose Stat answers, which is how a filesystem
// says a directory is already there.
type statOK struct{ *pictureFS }

func (statOK) Stat(string) (vfs.Entry, error) { return vfs.Entry{}, nil }

// Every way the write can go wrong says which step it was, because "it
// did not work" leaves the user nowhere to look.
func TestWhatWentWrongWritingAPictureSaysWhichStep(t *testing.T) {
	for what, setup := range map[string]func(*pictureFS){
		"find somewhere":  func(f *pictureFS) { f.homeErr = errors.New("no home") },
		"make somewhere":  func(f *pictureFS) { f.mkdirErr = errors.New("read only") },
		"write the pict":  func(f *pictureFS) { f.createErr = errors.New("no room") },
		"write the bytes": func(f *pictureFS) { f.writeErr = errors.New("the disk went") },
	} {
		f := newPictureFS()
		setup(f)

		_, err := putPictureOn(f, []byte("a picture"), pastedAt)

		if err == nil {
			t.Errorf("%s: it wrote the picture anyway", what)
			continue
		}
		if !strings.Contains(err.Error(), "picture") && !strings.Contains(err.Error(), "somewhere") {
			t.Errorf("%s: it says %q, which does not say what it was doing", what, err)
		}
	}
}
