package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"

	"github.com/marrasen/gunim/filemanager"
)

// The folders saved for This Computer and for each server become
// favourites, once, and are taken off the servers. Moved, they stay
// moved: a folder saved after that is not moved again.
func TestSavedFoldersBecomeFavouritesOnce(t *testing.T) {
	a, _ := agentApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"localFolders":["/here","/there"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a.settings = s
	book, err := remote.LoadBook(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "web", Address: "web.example", Folders: []string{"/srv", "/var/log"}}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	web := book.Hosts()[0].ID
	pinned := fileManagerPrefs()
	if err := os.MkdirAll(filepath.Dir(pinned), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinned, []byte(`{"Favourites":["/pinned","/here"],"FavNames":{"/pinned":"Pins"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a.moveFolders()
	want := []Favourite{{Path: "/here"}, {Path: "/there"}, {Path: "/pinned", Name: "Pins"},
		{Machine: machines.ID(web), Path: "/srv"}, {Machine: machines.ID(web), Path: "/var/log"}}
	if !slices.Equal(a.st.Favourites, want) {
		t.Fatalf("the favourites are %+v", a.st.Favourites)
	}
	if h := book.Hosts()[0]; len(h.Folders) != 0 {
		t.Fatalf("the server keeps its folders %q", h.Folders)
	}
	if got := a.savedFolders(machines.ID(web)); !slices.Equal(got, []string{"/srv", "/var/log"}) {
		t.Fatalf("the server's files open among %q", got)
	}
	if got := a.savedFolders(""); len(got) != 0 {
		t.Fatalf("this computer's files open among %q", got)
	}

	h := book.Hosts()[0]
	h.Folders = []string{"/new"}
	if err := book.Put(h, h.Name); err != nil {
		t.Fatal(err)
	}
	a.moveFolders()
	if len(a.st.Favourites) != len(want) {
		t.Fatalf("moved again, the favourites are %+v", a.st.Favourites)
	}

	// A server removed takes its favourites with it.
	a.forgetFavourites(machines.ID(web))
	if !slices.Equal(a.st.Favourites, want[:3]) {
		t.Fatalf("with the server gone, the favourites are %+v", a.st.Favourites)
	}
}

// The file manager keeps its favourites, on every machine, in kakel's
// settings, and names the machine of one elsewhere.
func TestTheFileManagerKeepsFavouritesInTheSettings(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "web", Address: "web.example"}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	a.st.Saved = book.Hosts()
	web := a.st.Saved[0].ID
	store, ok := a.favStore().(filemanager.AnyFSFavourites)
	if !ok || store != a.favStore() {
		t.Fatal("the windows don't share one store of favourites on any machine")
	}
	favs := []filemanager.Favourite{{FS: serverFS + web, Path: "/srv", Name: "Site"}, {Path: "/home/me"}}
	if err := store.Save(favs); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-a.events:
		f()
	case <-t.Context().Done():
	}
	if want := []Favourite{{Machine: machines.ID(web), Path: "/srv", Name: "Site"}, {Path: "/home/me"}}; !slices.Equal(a.st.Favourites, want) {
		t.Fatalf("kakel's favourites are %+v", a.st.Favourites)
	}
	got, err := store.Load()
	if err != nil || !slices.Equal(got, favs) {
		t.Fatalf("loaded, the favourites are %+v, %v", got, err)
	}
	a.notePlaces()
	if w := store.Where(serverFS + web); w != "web" {
		t.Fatalf("the server's favourite is on %q", w)
	}
	if w := store.Where(""); w != "This computer" {
		t.Fatalf("this computer's favourite is on %q", w)
	}

	// Settings that can't be read are not no favourites.
	if err := os.WriteFile(a.settings.Path(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = a.settings.PutFilesInWindow(true)
	if got, err := store.Load(); err == nil {
		t.Fatalf("with the settings unreadable, the favourites load as %+v", got)
	}
	a.showFavourites()
	if len(a.st.Favourites) != 2 {
		t.Fatalf("with the settings unreadable, kakel shows the favourites %+v", a.st.Favourites)
	}
}
