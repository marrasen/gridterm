package app

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"

	"github.com/marrasen/gunim/filemanager"
)

// Favourites are folders saved on any machine: this computer, a server,
// or one beyond a window. The file manager lists them all, a machine's
// menu and the palette list its own, and they take the place of the
// folders saved for a server and for This Computer before there were
// favourites, which are moved to them once.

// Favourite is a folder saved on Machine, empty for this computer. Name
// is the name the user gave it, empty for the folder's own.
type Favourite struct {
	Machine machines.ID
	Path    string
	Name    string
}

// Label is what a menu calls the favourite: its name, or its path.
func (f Favourite) Label() string {
	if n := strings.TrimSpace(f.Name); n != "" {
		return n
	}
	return f.Path
}

// showFavourites tells the windows the favourites, as kept.
func (a *app) showFavourites() {
	if a.settings == nil || a.settings.Err() != nil {
		// Unreadable, they are not none: what was shown stays.
		return
	}
	kept, _ := a.settings.Favourites()
	out := make([]Favourite, len(kept))
	for i, f := range kept {
		out[i] = Favourite{Machine: machines.ID(f.Machine), Path: f.Path, Name: f.Name}
	}
	a.st.Favourites = out
}

// moveFolders makes the folders saved for This Computer and for each
// server into favourites, once, and takes them off the servers. The
// folders pinned in a file manager window before kakel kept its
// favourites, in its own settings file, come too.
func (a *app) moveFolders() {
	if a.settings == nil || a.book == nil || a.settings.Err() != nil {
		return
	}
	if _, moved := a.settings.Favourites(); moved {
		return
	}
	from := pinnedBefore(fileManagerPrefs())
	hosts := a.book.Hosts()
	for _, h := range hosts {
		for _, path := range h.Folders {
			from = append(from, settings.Favourite{Machine: h.ID, Path: path})
		}
	}
	if err := a.settings.MoveFolders(from); err != nil {
		a.failed("Couldn't make the saved folders favourites", err.Error())
		return
	}
	for _, h := range hosts {
		if len(h.Folders) == 0 {
			continue
		}
		h.Folders = nil
		// One that won't save keeps its folders, unused: they are
		// favourites now.
		_ = a.book.Put(h, h.Name)
	}
	a.st.Saved = a.book.Hosts()
	a.showFavourites()
}

// pinnedBefore are the folders pinned in the file manager's settings
// file at path, as it kept them itself: none when it can't be read.
func pinnedBefore(path string) []settings.Favourite {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var prefs struct {
		Favourites []string
		FavNames   map[string]string
	}
	if json.Unmarshal(raw, &prefs) != nil {
		return nil
	}
	out := make([]settings.Favourite, 0, len(prefs.Favourites))
	for _, p := range prefs.Favourites {
		if p != "" {
			out = append(out, settings.Favourite{Path: p, Name: prefs.FavNames[p]})
		}
	}
	return out
}

// favouritesOn are the paths of machine's favourites.
func (a *app) favouritesOn(machine machines.ID) []string {
	var out []string
	for _, f := range a.st.Favourites {
		if f.Machine == machine {
			out = append(out, f.Path)
		}
	}
	return out
}

// forgetFavourites takes the favourites on machine away, as it was
// removed.
func (a *app) forgetFavourites(machine machines.ID) {
	if a.settings == nil {
		return
	}
	kept, _ := a.settings.Favourites()
	left := slices.DeleteFunc(slices.Clone(kept), func(f settings.Favourite) bool { return machines.ID(f.Machine).Of(machine) })
	if len(left) == len(kept) {
		return
	}
	if err := a.settings.PutFavourites(left); err != nil {
		a.failed("Couldn't forget the favourites on "+a.machines.Name(machine), err.Error())
		return
	}
	a.showFavourites()
	if a.files != nil && len(a.fileWins) > 0 {
		a.files.Refresh()
	}
}

// fmFavourites keeps the file manager's favourites in kakel's settings,
// on every machine, each by the ID of the machine's file system there.
// Its windows call it on their own goroutines; the settings lock.
type fmFavourites struct{ a *app }

// fsOf is the ID of machine's file system in the file manager.
func fsOf(machine machines.ID) string {
	if machine == machines.Local {
		return ""
	}
	return serverFS + string(machine)
}

// machineOfFS is the machine whose file system has ID fs.
func machineOfFS(fs string) machines.ID {
	return machines.ID(strings.TrimSuffix(strings.TrimPrefix(fs, serverFS), gone))
}

// Load implements [filemanager.FavouriteStore]. Settings that can't be
// read fail it, rather than read as no favourites, which the next pin
// would save over every one.
func (s *fmFavourites) Load() ([]filemanager.Favourite, error) {
	if err := s.a.settings.Err(); err != nil {
		return nil, err
	}
	kept, _ := s.a.settings.Favourites()
	out := make([]filemanager.Favourite, len(kept))
	for i, f := range kept {
		out[i] = filemanager.Favourite{FS: fsOf(machines.ID(f.Machine)), Path: f.Path, Name: f.Name}
	}
	return out, nil
}

// Save implements [filemanager.FavouriteStore], and tells kakel's
// windows.
func (s *fmFavourites) Save(favs []filemanager.Favourite) error {
	out := make([]settings.Favourite, len(favs))
	for i, f := range favs {
		out[i] = settings.Favourite{Machine: string(machineOfFS(f.FS)), Path: f.Path, Name: f.Name}
	}
	if err := s.a.settings.PutFavourites(out); err != nil {
		return err
	}
	go func() { s.a.events <- s.a.showFavourites }()
	return nil
}

// Where implements [filemanager.AnyFSFavourites].
func (s *fmFavourites) Where(fs string) string { return s.a.fsName(fs) }

// fsName names the machine whose file system has ID fs, as the file
// manager's titles and favourites say: by the names last noted, as it
// asks off the program's goroutine.
func (a *app) fsName(fs string) string {
	if fs == "" {
		return "This computer"
	}
	if names := a.fmNames.Load(); names != nil {
		if n, ok := (*names)[fs]; ok {
			return n
		}
	}
	return string(machineOfFS(fs))
}

// noteNames notes the machines' names, for fsName: the saved servers',
// and those of the machines with favourites or open in a file manager
// window. Changed, the windows read their titles and favourites again.
func (a *app) noteNames() {
	names := map[string]string{}
	for _, h := range a.st.Saved {
		names[fsOf(machines.ID(h.ID))] = h.Name
	}
	for _, f := range a.st.Favourites {
		if f.Machine != machines.Local {
			names[fsOf(f.Machine)] = a.machines.Name(f.Machine)
		}
	}
	for m := range a.fmFiles {
		names[fsOf(m)] = a.machines.Name(m)
	}
	old := a.fmNames.Load()
	if old != nil && maps.Equal(*old, names) {
		return
	}
	a.fmNames.Store(&names)
	if old != nil && a.files != nil && len(a.fileWins) > 0 {
		a.files.Refresh()
	}
}
