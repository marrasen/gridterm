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
