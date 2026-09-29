package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/conf"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/internal/update"
	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/themes"
)

func TestTheThemeFileIsWrittenAndReadAgain(t *testing.T) {
	a, _ := agentApp(t)
	a.themes = look.Load()
	a.pickTheme(a.themes[0].Name)
	registered := 0
	a.registerThemes = func(all []look.Themed) { registered = len(all) }
	a.handle(WriteThemeFile{})
	dir, _ := settings.Dir()
	read, err := themes.Load(themes.Path(dir))
	if err != nil || len(read) == 0 {
		t.Fatalf("written, the file reads %d themes, %v", len(read), err)
	}
	a.handle(ReloadThemes{})
	if registered == 0 || len(a.st.Themes) == 0 || a.st.Contents == nil {
		t.Fatalf("read again, %d themes were registered, the state has %v", registered, a.st.Themes)
	}
}

func TestANewerReleaseIsOffered(t *testing.T) {
	a, _ := agentApp(t)
	was, wasVersion := latestRelease, thisVersion
	latestRelease = func(context.Context) (update.Release, error) {
		return update.Release{Version: "v99.0.0", Page: "https://example.com/release"}, nil
	}
	t.Cleanup(func() { latestRelease, thisVersion = was, wasVersion })
	for _, c := range []struct{ have, title string }{
		{"v1.0.0", "Update available"},
		{"v1.0.0-3-gabcdef-dirty", "Newest release"},
	} {
		thisVersion = func() string { return c.have }
		a.handle(CheckUpdates{})
		waitFor(t, a, "the offer", func() bool { return len(a.st.Asks) > 0 })
		q := a.st.Asks[0]
		if q.Title != c.title || !strings.Contains(q.Text, "v99.0.0") || !strings.Contains(q.Text, c.have) || q.Yes != "Open the Page" || !q.Careful {
			t.Fatalf("to %s, the offer is %+v", c.have, q)
		}
		a.handle(AskAnswered{ID: q.ID})
		waitFor(t, a, "the offer to close", func() bool { return len(a.st.Asks) == 0 })
	}
}

func TestASecondUpdateCheckWaitsForTheFirst(t *testing.T) {
	a, _ := agentApp(t)
	was := latestRelease
	var asked atomic.Int32
	answer := make(chan struct{})
	latestRelease = func(context.Context) (update.Release, error) {
		asked.Add(1)
		<-answer
		return update.Release{Version: "v0.0.1"}, nil
	}
	t.Cleanup(func() { latestRelease = was })
	a.handle(CheckUpdates{})
	a.handle(CheckUpdates{})
	close(answer)
	waitFor(t, a, "the answer", func() bool { return !a.checking })
	if n := asked.Load(); n != 1 {
		t.Fatalf("pressed twice, it asked %d times", n)
	}
}

func TestClosingTheWindowAsksWhileAnythingIsOpen(t *testing.T) {
	a, _ := agentApp(t)
	a.handle(Exit{})
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	q := a.st.Asks[0]
	if q.Text != "Still open: 1 pane and an agent share." || !q.Danger {
		t.Fatalf("asked %+v", q)
	}
	// A second close while it asks asks nothing more.
	a.handle(Exit{})
	for f := range len(a.events) {
		_ = f
		(<-a.events)()
	}
	if len(a.st.Asks) != 1 {
		t.Fatalf("closed twice, it asks %d questions", len(a.st.Asks))
	}
	a.handle(AskAnswered{ID: q.ID})
	waitFor(t, a, "the no", func() bool { return !a.leaving })
	if len(a.st.Panes) != 1 {
		t.Fatalf("told no, the window closed %d panes", 1-len(a.st.Panes))
	}
	a.handle(Exit{})
	waitFor(t, a, "the question again", func() bool { return len(a.st.Asks) == 1 })
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true})
	// The window leaves with what it shows; its panes close once it
	// has gone.
	waitFor(t, a, "the window to leave", func() bool { return a.gone })
	if len(a.st.Panes) != 1 {
		t.Fatalf("leaving, the window shows %d panes, want the one it had", len(a.st.Panes))
	}
}

func TestMakePortableCopiesTheFilesBesideTheProgram(t *testing.T) {
	a, _ := agentApp(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	beside := conf.Beside(exe)
	t.Cleanup(func() { _ = os.RemoveAll(beside) })
	a.handle(MakePortable{})
	waitFor(t, a, "the list of what was done", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "Made Portable" || !strings.Contains(q.Text, "Created "+beside) || !strings.Contains(q.Text, "Restart kakel") {
		t.Fatalf("made portable, it says %+v", q)
	}
	if made, err := conf.IsDir(beside); !made || err != nil {
		t.Fatalf("the folder beside is made %v, %v", made, err)
	}
}

// Saving a server keeps the key files it names, to offer again.
func TestSavingAServerKeepsItsKeyFiles(t *testing.T) {
	a := fontApp(t)
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.book = book
	a.handle(SaveServer{Host: remote.Host{Name: "srv", Address: "srv.example", Identities: []string{"/k/id_ed25519"}}})
	if !slices.Equal(a.st.KeyFiles, []string{"/k/id_ed25519"}) {
		t.Fatalf("saved, the kept keys are %v", a.st.KeyFiles)
	}
}

func TestTheShortcutsFileMovesAKey(t *testing.T) {
	a, _ := agentApp(t)
	dir, err := settings.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keys.Path(dir), []byte(`{"version":1,"keys":{"ctrl+shift+J":"pane.close","ctrl+shift+W":"nothing"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.handle(ReloadShortcuts{})
	if len(a.st.Shortcuts) != 2 || a.st.ShortcutsRead == 0 {
		t.Fatalf("read, the changes are %+v; notices %+v", a.st.Shortcuts, a.st.Notices)
	}

	if !a.st.ShortcutsAgain {
		t.Fatal("read again, the state says it was read as the window opened")
	}

	if c := a.st.Shortcuts; c[0].Command == c[1].Command {
		t.Fatalf("read, the changes are %+v", c)
	}
}

// Saving a server again with its key untouched keeps nothing: the key
// is not moved to the front of the list for nothing. A new key is.
func TestSavingAServerWithItsKeyUntouchedKeepsNothing(t *testing.T) {
	a := fontApp(t)
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.book = book
	margit := remote.Host{Name: "margit", Address: "margit.example", Identities: []string{"/already/there"}}
	if err := book.Put(margit, ""); err != nil {
		t.Fatal(err)
	}
	a.handle(SaveServer{Host: margit, Under: "margit"})
	if len(a.st.KeyFiles) != 0 {
		t.Fatalf("saved untouched, the kept keys are %v", a.st.KeyFiles)
	}
	margit.Identities = []string{"/k/new_ed25519"}
	a.handle(SaveServer{Host: margit, Under: "margit"})
	if !slices.Equal(a.st.KeyFiles, []string{"/k/new_ed25519"}) {
		t.Fatalf("saved with a new key, the kept keys are %v", a.st.KeyFiles)
	}
}

// Pop Out on a pane that is not in a split says so.
func TestPopOutOnAPaneNotInASplitSaysSo(t *testing.T) {
	a, _ := agentApp(t)
	a.handle(PopOut{})
	if len(a.st.Notices) != 1 || a.st.Notices[0].Title != "Nothing to pop out" {
		t.Fatalf("the notices are %+v", a.st.Notices)
	}
}

// Where the window was as kakel exits is kept, and the next start
// opens it there.
func TestTheWindowOpensWhereItWasLeft(t *testing.T) {
	testhome.New(t)
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	path, err := settings.Path()
	if err != nil {
		t.Fatal(err)
	}
	set, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.cur.gw = w
	want, ok := w.Placement()
	if !ok {
		t.Fatal("the test window has no placement")
	}
	a.leave()
	got := Options{}.WindowPlace()
	if got == nil || *got != want {
		t.Fatalf("started again, the window opens at %+v, want %+v", got, want)
	}
}
