package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/vfs"
)

// readerApp is a program side with a file on this machine open in a
// reader, and the file's path and the reader's pane.
func readerApp(t *testing.T, follow bool) (a *app, file, id string) {
	t.Helper()
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a = newApp(w.Client(), &shells{m: map[string]*shell{}})
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
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
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
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
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
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.addPane(Pane{ID: "p1", Title: "Terminal 1"}, nil, placement{})
	a.addPane(Pane{ID: "p2", Title: "Scrollback of Terminal 1", Kind: kindReader}, nil, placement{})
	a.setReader("p2", Reader{Path: "Scrollback of Terminal 1", Lines: []string{"x"}, Seq: 1, Of: "p1"})
	a.remove("p1")
	if r := a.st.Readers["p2"]; r.Gone == "" {
		t.Fatalf("its pane closed, the scrollback reads %+v", r)
	}
}

// The reader is made before the first read arrives, says how far it
// has got of the size listed, and asks for nothing more meanwhile.
func TestTheReaderShowsHowFarItsFirstReadHasGot(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Panes: []Pane{{ID: "p1", Title: "big.log", Kind: kindReader}}, Stage: &Box{Pane: "p1"}, Focus: "p1"}
	st.Readers = map[string]Reader{"p1": {Path: "/x/big.log", Name: "big.log", Expect: 4 << 20}}
	publish(st)
	st.Readers = map[string]Reader{"p1": {Path: "/x/big.log", Name: "big.log", Expect: 4 << 20, SoFar: 1 << 20}}
	publish(st)
	rd := win.readers["p1"]
	if rd == nil || rd.r == nil || !rd.r.Busy() || rd.r.SoFar() != 1<<20 {
		t.Fatal("before its first read arrived, the reader says nothing of it")
	}
	_, rows := rd.g.Size()
	found := false
	for y := range rows {
		if row := gridRow(rd, y); strings.Contains(row, "4") && strings.Contains(row, "MB") {
			found = true
		}
	}
	if !found {
		t.Fatal("the reader does not say the size it was listed as")
	}
	for len(lastWindow.Client().Intents()) > 0 {
		if in, ok := (<-lastWindow.Client().Intents()).Intent.(ReadAgain); ok {
			t.Fatalf("the reader asked for a read with one on its way: %#v", in)
		}
	}
}

// Ctrl+F in the reader tells the program to follow, and again to stop.
func TestCtrlFInTheReaderTellsTheProgram(t *testing.T) {
	_, _, publish := windowStage(t)
	st := State{Panes: []Pane{{ID: "p1", Title: "log.txt", Kind: kindReader}}, Stage: &Box{Pane: "p1"}, Focus: "p1"}
	st.Readers = map[string]Reader{"p1": {Path: "/x/log.txt", Name: "log.txt", Lines: []string{"a"}, Seq: 1}}
	publish(st)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	for _, want := range []bool{true, false} {
		lastWindow.Input(gi.KeyPress{Key: gi.KeyF, Mods: gi.ModControl})
		lastWindow.Frame(time.Second / 60)
		var got *FollowFile
		for got == nil {
			if in, ok := nextIntent(t).(FollowFile); ok {
				got = &in
			}
		}
		if got.Pane != "p1" || got.On != want {
			t.Fatalf("Ctrl+F sent %#v, want following %v", *got, want)
		}
	}
}

// A name the program offers to save under, after one was refused,
// reaches the reader.
func TestTheReaderTakesTheNameOfferedToSaveUnder(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Panes: []Pane{{ID: "p1", Title: "log.txt", Kind: kindReader}}, Stage: &Box{Pane: "p1"}, Focus: "p1"}
	st.Readers = map[string]Reader{"p1": {Path: "/x/log.txt", Name: "log.txt", Lines: []string{"a"}, Seq: 1, SaveAs: "~/log.txt"}}
	publish(st)
	st.Readers = map[string]Reader{"p1": {Path: "/x/log.txt", Name: "log.txt", Lines: []string{"a"}, Seq: 1, SaveAs: "~/log 2.txt"}}
	publish(st)
	if got := win.readers["p1"].r.SaveAs; got != "~/log 2.txt" {
		t.Fatalf("the reader offers to save as %q", got)
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
	_ = a.conns["srv"].Close() // as if the network went
	waitFor(t, a, "the connection to go", func() bool { return a.conns["srv"] == nil && a.fsFor("srv") == nil })
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
