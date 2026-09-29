package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/vfs"
)

// readerApp is a program side with a file on this machine open in a
// reader, and the file's path and the reader's pane.
func readerApp(t *testing.T, follow bool) (a *app, file, id string) {
	t.Helper()
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a = newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	file = filepath.Join(t.TempDir(), "log.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id = a.readOn("", vfs.NewLocal(), file, follow, 0, placement{})
	waitFor(t, a, "the first read", func() bool { return a.st.Readers[id].Seq > 0 })
	return a, file, id
}

// lines is what reader id shows.
func lines(a *app, id string) []string { return a.st.Readers[id].Lines }

// titleOf is pane id's title.
func paneTitle(a *app, id string) string {
	for _, p := range a.st.Panes {
		if p.ID == id {
			return p.Title
		}
	}
	return ""
}

// A reader is there before its file has been read, for how far the
// read has got to show.
func TestAReaderIsThereBeforeItsFirstRead(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	file := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := a.readOn("", vfs.NewLocal(), file, false, 0, placement{})
	if r, ok := a.st.Readers[id]; !ok || r.Seq != 0 || r.Path != file {
		t.Fatalf("before the first read, the reader is %+v, %v", r, ok)
	}
}

// Following turned on in a reader opened without it reads the file
// again as it changes, and turned off, stops; the title says which.
func TestFollowingTurnsOnAndOffInAReader(t *testing.T) {
	a, file, id := readerApp(t, false)
	a.handle(FollowFile{Pane: id, On: true})
	if !strings.HasSuffix(paneTitle(a, id), followTitle) {
		t.Fatalf("following, the pane is called %q", paneTitle(a, id))
	}
	// Past the first look, which only notes how the file stands. The
	// time is a second's worth, so the next write is seen as a change
	// even by a clock that counts in seconds.
	pumpFor(a, followEvery+200*time.Millisecond)
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the file read again", func() bool { return slices.Contains(lines(a, id), "two") })

	a.handle(FollowFile{Pane: id, On: false})
	if strings.HasSuffix(paneTitle(a, id), followTitle) {
		t.Fatalf("not following, the pane is called %q", paneTitle(a, id))
	}
	waitFor(t, a, "the loop to stop", func() bool { return !a.following[id] })
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pumpFor(a, 2*followEvery)
	if slices.Contains(lines(a, id), "three") {
		t.Fatal("not following, the reader read the file again")
	}
}

// A followed file that goes says so in the reader, and is read again
// once it is back.
func TestAFollowedFileThatGoesSaysSo(t *testing.T) {
	a, file, id := readerApp(t, true)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the reader to say so", func() bool {
		return strings.HasPrefix(a.st.Readers[id].Err, "Couldn't look at the file")
	})
	if err := os.WriteFile(file, []byte("back\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the file read again", func() bool {
		r := a.st.Readers[id]
		return r.Err == "" && slices.Contains(r.Lines, "back")
	})
}

// A file named as a picture that is not one is read as lines once the
// reader asks for them.
func TestAPictureThatIsNotOneIsReadAsLines(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	file := filepath.Join(t.TempDir(), "not.png")
	if err := os.WriteFile(file, []byte("plain text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := a.readOn("", vfs.NewLocal(), file, false, 0, placement{})
	waitFor(t, a, "the first read", func() bool { return a.st.Readers[id].Seq > 0 })
	if a.st.Readers[id].Err == "" {
		t.Fatal("a picture that is not one was read without a complaint")
	}
	a.handle(ReadAgain{Pane: id, Text: true})
	waitFor(t, a, "the lines", func() bool { return slices.Contains(lines(a, id), "plain text") })
}

// A scrollback's reader says so once its pane has closed.
func TestAScrollbackSaysWhenItsPaneHasClosed(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.addPane(Pane{ID: "p1", Title: "Terminal 1"}, nil, placement{})
	a.addPane(Pane{ID: "p2", Title: "Scrollback of Terminal 1", Kind: kindReader}, nil, placement{})
	a.setReader("p2", Reader{Path: "Scrollback of Terminal 1", Lines: []string{"x"}, Seq: 1, Of: "p1"})
	a.remove("p1")
	if r := a.st.Readers["p2"]; r.Gone == "" {
		t.Fatalf("its pane closed, the scrollback reads %+v", r)
	}
}

// pumpFor runs the program's events for d.
func pumpFor(a *app, d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case f := <-a.events:
			f()
		case <-deadline:
			return
		}
	}
}

// A reader on a server reads again after the connection went: through
// the files opened once it is back, connecting again for Ctrl+R.
func TestAReaderOnAServerReadsAgainAfterTheConnectionWent(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.openPath("srv", file, false, 0); err != nil {
		t.Fatal(err)
	}
	var id string
	waitFor(t, a, "the reader", func() bool {
		for rid, r := range a.st.Readers {
			if r.Path == file && slices.Contains(r.Lines, "one") {
				id = rid
				return true
			}
		}
		return false
	})
	_ = a.machines.Get("srv").Conn.Close() // as if the network went
	waitFor(t, a, "the connection to go", func() bool { return a.machines.Get("srv").Conn == nil && a.fsFor("srv") == nil })
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.handle(ReadAgain{Pane: id})
	waitFor(t, a, "the file read again", func() bool {
		answering()
		r := a.st.Readers[id]
		return r.Err == "" && slices.Contains(r.Lines, "two")
	})
}

// A followed file that cannot be looked at says so once, not once a
// look.
func TestAFileThatCannotBeLookedAtIsSaidOnce(t *testing.T) {
	a, file, id := readerApp(t, true)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the reader to say so", func() bool { return a.st.Readers[id].Err != "" })
	seq := a.st.Readers[id].Seq
	pumpFor(a, 3*followEvery)
	if got := a.st.Readers[id].Seq; got != seq {
		t.Fatalf("said again and again: the reads went from %d to %d", seq, got)
	}
}

// A reader with nothing to read again does not follow.
func TestAReaderWithNothingToFollowDoesNot(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	a.addPane(Pane{ID: "p1", Title: "Typing History", Kind: kindReader}, nil, placement{})
	a.setReader("p1", Reader{Path: "Typing History", Lines: []string{"x"}, Seq: 1})
	a.handle(FollowFile{Pane: "p1", On: true})
	if a.st.Readers["p1"].Follow || a.following["p1"] || strings.HasSuffix(paneTitle(a, "p1"), followTitle) {
		t.Fatal("a reader with nothing to read again follows")
	}
}

// Following turned off and straight back on reads the file again.
func TestFollowingBackOnReadsAgain(t *testing.T) {
	a, _, id := readerApp(t, true)
	seq := a.st.Readers[id].Seq
	a.handle(FollowFile{Pane: id, On: false})
	a.handle(FollowFile{Pane: id, On: true})
	waitFor(t, a, "the file read again", func() bool { return a.st.Readers[id].Seq > seq })
}

// A reader on another window whose connection went is never read by
// signing in at its address: the window is connected to again from the
// sidebar.
func TestAReaderOnAWindowThatWentDoesNotDial(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	a.addPane(Pane{ID: "p1", Title: "notes.txt", Kind: kindReader, Machine: "box:7777"}, nil, placement{})
	a.reads["p1"] = readSpec{machine: "box:7777", window: true, path: "/notes.txt", name: "notes.txt"}
	a.setReader("p1", Reader{Path: "/notes.txt", Name: "notes.txt", Lines: []string{"x"}, Seq: 1})
	a.handle(ReadAgain{Pane: "p1"})
	if len(a.machines.Dialing()) != 0 || len(a.machines.Connected()) != 0 {
		t.Fatalf("reading again dialled: %v", a.machines.Dialing())
	}
	if r := a.st.Readers["p1"]; !strings.Contains(r.Err, "Connect to it again") {
		t.Fatalf("the reader says %q", r.Err)
	}
}
