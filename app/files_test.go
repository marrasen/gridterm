package app

import (
	"archive/zip"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/settings"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/vfs"
)

func TestAReaderIsToldHowItsSaveWent(t *testing.T) {
	home := testhome.New(t)
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.setReader("p1", Reader{Path: "/x/notes.txt", Seq: 1})

	a.handle(SaveLines{Pane: "p1", Path: "~/kept.txt", Lines: []string{"one", "two"}})
	if got, err := os.ReadFile(filepath.Join(home, "kept.txt")); err != nil || string(got) != "one\ntwo\n" {
		t.Fatalf("saved %q, %v", got, err)
	}
	if r := a.st.Readers["p1"]; r.Saves != 1 || r.SaveErr != "" {
		t.Fatalf("saved, the reader reads %+v", r)
	}

	// Saved again under the same name, the file there is left alone.
	a.handle(SaveLines{Pane: "p1", Path: "~/kept.txt", Lines: []string{"three"}})
	if got, _ := os.ReadFile(filepath.Join(home, "kept.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("saved over a file that was there: it holds %q", got)
	}
	if r := a.st.Readers["p1"]; r.Saves != 2 || !strings.HasPrefix(r.SaveErr, "already there") {
		t.Fatalf("saved over a file that was there, the reader reads %+v", r)
	}
	// The name offered next is one nothing has, so saving again saves.
	if r := a.st.Readers["p1"]; r.SaveAs != "~/kept 2.txt" {
		t.Fatalf("after the refusal, the name offered is %q", r.SaveAs)
	}

	a.handle(SaveLines{Pane: "p1", Path: filepath.Join(home, "missing", "kept.txt"), Lines: []string{"one"}})
	if r := a.st.Readers["p1"]; r.Saves != 3 || r.SaveErr == "" {
		t.Fatalf("saved into a missing folder, the reader reads %+v", r)
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("the reader says how its save went, and notices say it again: %+v", a.st.Notices)
	}
}

// A file pane whose connection dropped opens its files again on the
// next thing asked of it, connecting again first.
func TestAFilePaneReconnectsOnItsNextAction(t *testing.T) {
	testhome.New(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Chdir(t.TempDir())
	s := sshtest.New(t)
	host, port := s.Host()
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		for _, id := range a.machines.Connected() {
			c := a.machines.Get(id).Conn
			_ = c.Close()
		}
	})
	answering := func() {
		for _, q := range a.st.Asks {
			ans := AskAnswered{ID: q.ID, Yes: true}
			if len(q.Prompts) > 0 {
				ans.Answers = []string{sshtest.Password}
			}
			a.handle(ans)
		}
	}
	a.handle(ConnectTo{Target: "tester@" + net.JoinHostPort(host, strconv.Itoa(port))})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	machine := a.st.Panes[0].Machine
	a.handle(OpenFiles{})
	waitFor(t, a, "the files", func() bool { return len(a.st.Panes) == 2 && a.st.Browsers[a.st.Panes[1].ID].Seq > 0 })
	files := a.st.Panes[1].ID
	seq := a.st.Browsers[files].Seq

	_ = a.machines.Get(machine).Conn.Close()
	waitFor(t, a, "the connection to go", func() bool { return a.machines.Get(machine).Conn == nil && a.machines.Get(machine).Files == nil })
	a.handle(Browse{Pane: files, Path: a.st.Browsers[files].Path})
	waitFor(t, a, "the folder read again", func() bool { answering(); return a.st.Browsers[files].Seq > seq })
	if a.machines.Get(machine).Conn == nil {
		t.Fatal("the folder was read with no connection")
	}
}

func TestAnArchiveOpensAsAFolder(t *testing.T) {
	a, _ := agentApp(t)
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "bundle.zip"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("inside.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("from the zip"))
	// A zip inside it, which is a file there: only the outer one opens.
	if w, err = z.Create("inner.zip"); err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("PK"))
	if err := errors.Join(z.Close(), f.Close()); err != nil {
		t.Fatal(err)
	}
	if err := a.filesOn("", dir); err != nil {
		t.Fatal(err)
	}
	pane := a.st.Panes[len(a.st.Panes)-1].ID
	waitFor(t, a, "the folder", func() bool { return len(a.st.Browsers[pane].Entries) == 1 })
	a.handle(EnterEntry{Pane: pane, Name: "bundle.zip"})
	waitFor(t, a, "the archive's insides", func() bool {
		es := a.st.Browsers[pane].Entries
		return len(es) == 2 && slices.ContainsFunc(es, func(e vfs.Entry) bool { return e.Name == "inside.txt" })
	})
	// Enter on the zip inside reads it, rather than walking into an
	// empty folder.
	a.handle(EnterEntry{Pane: pane, Name: "inner.zip"})
	waitFor(t, a, "a reader of the inner zip", func() bool {
		return slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.Kind == KindReader })
	})
	if got := a.st.Browsers[pane].Path; strings.HasSuffix(got, "inner.zip") {
		t.Fatalf("Enter on the inner zip walked into it: %q", got)
	}
}

// A rename that changes only the letter case goes through, unless the
// folder holds another file with that very name, as a folder where case
// counts can: that one is not written over.
func TestARenameOfCaseAloneWritesOverNothing(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("readme", "lower")
	if err := a.openFilesOn("", vfs.NewLocal(), dir); err != nil {
		t.Fatal(err)
	}
	pane := a.st.Focus
	waitFor(t, a, "the listing", func() bool { return a.st.Browsers[pane].Seq > 0 })
	// Is case counted here? Where it is not, readme and README are one.
	write("README", "upper")
	if entries, _ := os.ReadDir(dir); len(entries) == 2 {
		a.handle(RenameFile{Pane: pane, From: "readme", To: "README"})
		waitFor(t, a, "the refusal", func() bool { return len(a.st.Notices) > 0 })
		if got, _ := os.ReadFile(filepath.Join(dir, "README")); string(got) != "upper" {
			t.Fatalf("renaming readme to README wrote over README: it holds %q", got)
		}
		if n := a.st.Notices[0]; !strings.Contains(n.Body, "already there") {
			t.Fatalf("the refusal says %+v", n)
		}
		if err := os.Remove(filepath.Join(dir, "README")); err != nil {
			t.Fatal(err)
		}
	}
	a.handle(RenameFile{Pane: pane, From: "readme", To: "Readme"})
	waitFor(t, a, "the rename", func() bool {
		entries, _ := os.ReadDir(dir)
		return len(entries) == 1 && entries[0].Name() == "Readme"
	})
}

// Files on a server with one favourite open at that folder, however
// they are asked for; with more than one, or none, at home.
func TestFilesOnAServerWithOneSavedFolderOpenThere(t *testing.T) {
	a, answering := dialApp(t)
	there := t.TempDir()
	h, _ := a.book.Lookup("srv")
	a.settings = mustSettings(t)
	if err := a.settings.PutFavourites([]settings.Favourite{{Machine: h.ID, Path: there}}); err != nil {
		t.Fatal(err)
	}
	a.showFavourites()
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	a.handle(OpenFiles{})
	waitFor(t, a, "the files", func() bool {
		return len(a.st.Panes) == 2 && a.st.Browsers[a.st.Panes[1].ID].Seq > 0
	})
	if got := a.st.Browsers[a.st.Panes[1].ID].Path; got != onServer(there) {
		t.Fatalf("the files opened at %q, want the saved folder %q", got, there)
	}
}

// onServer is a path of this machine as the test server's SFTP spells
// it: the same on Linux, and /C:/Users/... on Windows.
func onServer(path string) string {
	if runtime.GOOS == "windows" {
		return "/" + filepath.ToSlash(path)
	}
	return path
}
