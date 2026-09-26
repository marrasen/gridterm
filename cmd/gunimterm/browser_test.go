package main

import (
	"io/fs"
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/gridterm/vfs"
)

func TestAFilePaneShowsLinksAndWhatWaitsToBePasted(t *testing.T) {
	win, _, publish := windowStage(t)
	panes := []Pane{{ID: "p1", Title: "a", Kind: kindFiles}, {ID: "p2", Title: "b", Kind: kindFiles}}
	entries := []vfs.Entry{
		{Name: "notes.txt", Size: 10},
		{Name: "latest", Mode: fs.ModeSymlink, Link: "/srv/www/v2"},
	}
	st := State{Panes: panes, Stage: &Box{Pane: "p1"}, Focus: "p1", Sidebar: true, SidebarWidth: 220,
		Browsers: map[string]Browser{"p1": {Path: "/srv", Entries: entries, Seq: 1}, "p2": {Path: "/", Seq: 1}},
		FileClip: FileClip{Key: "", At: "/srv", Names: []string{"notes.txt"}}}
	publish(st)
	b := win.browsers["p1"]
	if row := b.row("latest"); !row.Accent || row.Cells[1] != "→ /srv/www/v2" {
		t.Fatalf("a link shows as %+v", row)
	}
	if row := b.row("notes.txt"); row.Cells[0] != "·notes.txt" {
		t.Fatalf("a file waiting to be pasted shows as %+v", row)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastUI.Focus(b.table)
	press := func(k gi.Key, mods gi.Mods) {
		lastWindow.Input(gi.KeyPress{Key: k, Mods: mods, Time: time.Now()})
		lastWindow.Frame(time.Second / 60)
	}
	press(gi.KeyTab, 0)
	if in, ok := nextIntent(t).(FocusPane); !ok || in.Pane != "p2" {
		t.Fatalf("Tab sent %#v", in)
	}
	press(gi.KeyEscape, 0)
	if in := nextIntent(t); in != (DropFileClip{}) {
		t.Fatalf("Escape sent %#v", in)
	}
	press(gi.KeyD, gi.ModControl)
	if in, ok := nextIntent(t).(ClosePane); !ok || in.Pane != "p1" {
		t.Fatalf("Ctrl+D sent %#v", in)
	}
}

func TestTheTopOfAFilesystemHasNothingAboveIt(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Panes: []Pane{{ID: "p1", Title: "/", Kind: kindFiles}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/", Entries: []vfs.Entry{{Name: "etc", Mode: fs.ModeDir}}, Seq: 1, Top: true}}}
	publish(st)
	if k, _ := win.browsers["p1"].table.Cursor(); k != "etc" {
		t.Fatalf("at the top, the list starts at %q", k)
	}
}

func TestAPathIsCutWhereItsLastNameStarts(t *testing.T) {
	for _, c := range []struct{ sep, text, dir, leaf string }{
		{"/", "/home/rd", "/home", "rd"},
		{"/", "/ho", "/", "ho"},
		{`\`, `C:\Us`, `C:\`, "Us"},
		{`\`, "C:/Users/ma", `C:\Users`, "ma"},
	} {
		dir, leaf, ok := splitLeaf(c.sep, c.text)
		if !ok || dir != c.dir || leaf != c.leaf {
			t.Errorf("%q cuts to %q and %q, want %q and %q", c.text, dir, leaf, c.dir, c.leaf)
		}
	}
	if got := restOf(false, []string{"src", "srv", "sbin"}, "sr"); got != "" {
		t.Errorf("src and srv share nothing after sr, and got %q", got)
	}
	if got := restOf(false, []string{"projects", "project-x"}, "pro"); got != "ject" {
		t.Errorf("got %q, want the part both share", got)
	}
	if got := restOf(true, []string{"Users"}, "us"); got != "ers" {
		t.Errorf("paying case no mind, got %q", got)
	}
}

func TestGoToCompletesAFoldersName(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Panes: []Pane{{ID: "p1", Title: "/", Kind: kindFiles}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/", Seq: 1, Sep: "/", Roots: []string{"/"}}}}
	publish(st)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	b := win.browsers["p1"]
	b.askGoTo(lastUI)
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	b.goTo.SetText("/home/")
	lastWindow.Input(gi.TextInput{Text: "rd"})
	lastWindow.Frame(time.Second / 60)
	for {
		if in, ok := nextIntent(t).(ListFolders); ok {
			if in.Dir != "/home" {
				t.Fatalf("asked for the folders in %q", in.Dir)
			}
			break
		}
	}
	br := st.Browsers["p1"]
	br.Listed = Listed{Dir: "/home", Folders: []string{"rdp"}}
	st.Browsers = map[string]Browser{"p1": br}
	publish(st)
	if b.goTo.Ghost != "p" {
		t.Fatalf("the suggestion is %q, want the rest of rdp", b.goTo.Ghost)
	}
}

func TestTheKeyBarPressesItsKeys(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Panes: []Pane{{ID: "p1", Title: "/", Kind: kindFiles}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/srv", Entries: []vfs.Entry{{Name: "a.txt"}}, Seq: 1}}}
	publish(st)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	bar := win.browsers["p1"].keys
	at, _ := lastUI.Bounds(bar)
	click := func(name string) {
		t.Helper()
		for i, k := range bar.keys {
			if k.name == name {
				p := at.Min.Add(bar.boxes[i].Center())
				lastWindow.Input(gi.PointerDown{Pos: p, Button: gi.ButtonPrimary, Clicks: 1})
				lastWindow.Input(gi.PointerUp{Pos: p, Button: gi.ButtonPrimary})
				lastWindow.Frame(time.Second / 60)
				return
			}
		}
		t.Fatalf("no key %s", name)
	}
	click("F7 Paste")
	if n := len(lastWindow.Client().Intents()); n != 0 {
		t.Fatalf("with nothing to paste, Paste sent %d intents", n)
	}
	click("^D Close")
	if in, ok := nextIntent(t).(ClosePane); !ok || in.Pane != "p1" {
		t.Fatalf("Close sent %#v", in)
	}
}

func TestAFolderThatCannotBeReadSaysSoAndWhy(t *testing.T) {
	win, _, publish := windowStage(t)
	st := State{Panes: []Pane{{ID: "p1", Title: "/", Kind: kindFiles}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/root"}}}
	publish(st)
	b := win.browsers["p1"]
	if b.path.Text != "Reading /root…" {
		t.Fatalf("before the first read, the path says %q", b.path.Text)
	}
	st.Browsers = map[string]Browser{"p1": {Path: "/root", Err: "open /root: permission denied"}}
	publish(st)
	if b.problem.label.Text == "" {
		t.Fatal("a folder that could not be read says nothing")
	}
	at, _ := lastUI.Bounds(b.problem)
	lastWindow.Input(gi.PointerDown{Pos: at.Center(), Button: gi.ButtonPrimary, Clicks: 1})
	lastWindow.Input(gi.PointerUp{Pos: at.Center(), Button: gi.ButtonPrimary})
	lastWindow.Frame(time.Second / 60)
	if win.dialog == nil {
		t.Fatal("a click on the line did not say why")
	}
}

func TestAWSLDistributionsFilesAreOfferedHere(t *testing.T) {
	win, _, publish := windowStage(t)
	root := `\\wsl.localhost\Ubuntu`
	publish(State{Shells: []ShellChoice{{ID: "cmd", Title: "Command Prompt"}, {ID: "wsl:Ubuntu", Title: "Ubuntu", Folder: root}}})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	if !win.run("conn.files..1", lastUI) {
		t.Fatal("the first folder here was not taken")
	}
	if in := nextIntent(t); in != (FilesOn{Machine: "", Path: root}) {
		t.Fatalf("it sent %#v", in)
	}
}
