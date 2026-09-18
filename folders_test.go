package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// savedWith puts a server in the list with folders on it.
func savedWith(t *testing.T, a *testApp, name string, folders ...string) {
	t.Helper()
	// Under its own name when it is there already, which is what saving
	// changes to a server does.
	under := ""
	if _, have := a.book.Lookup(name); have {
		under = name
	}
	if err := a.book.Put(remote.Host{
		Name: name, Address: name + ".example", Folders: folders,
	}, under); err != nil {
		t.Fatalf("save %q: %v", name, err)
	}
	a.refreshServers()
}

// plusLines are the lines the plus on a machine's row offers, each as
// its title and the command it runs.
func plusLines(t *testing.T, a *testApp, host string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, item := range clickPlus(t, a, host).Items() {
		out[item.Title] = item.Command
	}
	return out
}

// plusTitles are those lines' titles alone.
func plusTitles(t *testing.T, a *testApp, host string) []string {
	t.Helper()
	var out []string
	for _, item := range clickPlus(t, a, host).Items() {
		out = append(out, item.Title)
	}
	return out
}

// A folder saved on a server is where its file browser opens.
func TestOneFolderIsWhereFilesOpens(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	savedWith(t, a, "margit", "/var/log")

	if got := a.oneFolderOn("margit"); got != "/var/log" {
		t.Errorf("Files on margit opens at %q, want the one folder saved on it", got)
	}
}

// With several folders no one of them is the answer, so Files opens at
// home and each folder gets a line of its own.
func TestSeveralFoldersEachGetALine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	savedWith(t, a, "margit", "/var/log", "/srv/app")

	if got := a.oneFolderOn("margit"); got != "" {
		t.Errorf("Files on margit opens at %q, and it has two folders", got)
	}
	got := plusLines(t, a, "margit")
	// Each line runs the command for its own folder, so two lines that
	// read differently cannot do the same thing.
	for i, folder := range []string{"/var/log", "/srv/app"} {
		title := "Files in " + folder
		if got[title] != folderCommandID("margit", i) {
			t.Errorf("the line %q runs %q, want the command for folder %d", title, got[title], i+1)
		}
	}
	// And the plain line stays, because it is the only way from here to
	// wherever the machine calls home.
	if got["Files"] != "conn.files" {
		t.Errorf("the plus offers %v, want the plain Files line as well", got)
	}
}

// A machine with one folder keeps the one Files line: that line already
// opens at the folder.
func TestOneFolderKeepsTheOneFilesLine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	savedWith(t, a, "margit", "/var/log")

	got := plusTitles(t, a, "margit")
	if !slices.Contains(got, "Files") {
		t.Errorf("the plus offers %v, want the one Files line", got)
	}
	if slices.Contains(got, "Files in /var/log") {
		t.Errorf("the plus offers a line per folder for a machine with one: %v", got)
	}
}

// The palette can be searched by the folder as well as by the machine.
func TestThePaletteOffersALinePerFolder(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	savedWith(t, a, "margit", "/var/log", "/srv/app")

	var titles []string
	for i := range 2 {
		cmd, ok := a.root.Commands.Lookup(folderCommandID("margit", i))
		if !ok {
			t.Fatalf("no command for folder %d", i+1)
		}
		titles = append(titles, cmd.Title)
	}
	want := []string{"Browse /var/log on margit", "Browse /srv/app on margit"}
	if !slices.Equal(titles, want) {
		t.Errorf("the palette offers %v, want %v", titles, want)
	}
}

// Editing a server's folders changes the lines at once, rather than at
// whatever unrelated moment the server list next changes.
func TestChangingTheFoldersChangesTheLines(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	savedWith(t, a, "margit", "/var/log")

	savedWith(t, a, "margit", "/var/log", "/srv/app")

	if _, ok := a.root.Commands.Lookup(folderCommandID("margit", 1)); !ok {
		t.Error("the second folder has no command after the server was edited")
	}
}

// The dialog shows the folders as one line and reads them back.
func TestTheServerDialogKeepsTheFolders(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	savedWith(t, a, "margit", "/var/log", "/srv/app")

	if err := a.openEditServer("margit"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal(t, a, "the edit dialog", byTitle[*ui.Form]("Edit margit"))
	if got := f.Field("Folders").Text(); got != "/var/log,/srv/app" {
		t.Fatalf("the field says %q", got)
	}
	retypeField(t, a, f, "Folders", " /srv/app , /etc/nginx ")
	pressButton(t, a, f, "Save")

	h, ok := a.book.Lookup("margit")
	if !ok {
		t.Fatal("the server is gone")
	}
	if want := []string{"/srv/app", "/etc/nginx"}; !slices.Equal(h.Folders, want) {
		t.Errorf("it saved %v, want %v with the spaces trimmed", h.Folders, want)
	}
}

// Saving with the field emptied takes the folders away.
func TestClearingTheFieldTakesTheFoldersAway(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	savedWith(t, a, "margit", "/var/log")

	if err := a.openEditServer("margit"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal(t, a, "the edit dialog", byTitle[*ui.Form]("Edit margit"))
	retypeField(t, a, f, "Folders", "")
	pressButton(t, a, f, "Save")

	h, ok := a.book.Lookup("margit")
	if !ok {
		t.Fatal("the server is gone")
	}
	if len(h.Folders) != 0 {
		t.Errorf("it kept %v", h.Folders)
	}
}

// A folder with nothing in it, and the same folder twice, are turned
// away: one names nothing and the other would offer two lines that do
// the same thing.
func TestABadFolderIsRefused(t *testing.T) {
	for _, c := range []struct {
		why     string
		folders []string
		says    string
	}{
		{"nothing in it", []string{"/var/log", ""}, "no path"},
		{"the same twice", []string{"/var/log", "/var/log"}, "twice"},
		{"a control character", []string{"/var/\x07log"}, "cannot be shown"},
	} {
		h := remote.Host{Name: "margit", Address: "margit.example", Folders: c.folders}
		err := h.Validate()
		if err == nil {
			t.Errorf("a folder %s was allowed", c.why)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("a folder %s says %q, want it to say %q", c.why, err, c.says)
		}
	}
}

// aFilePaneAt opens a file browser on this machine at a folder and gives
// back the pane once it has landed somewhere.
func aFilePaneAt(t *testing.T, a *testApp, at string) *files.Pane {
	t.Helper()
	if err := a.openFilesAt(conns.Local, at); err != nil {
		t.Fatalf("open files at %q: %v", at, err)
	}
	var p *files.Pane
	waitFor(t, a, "the pane to land somewhere", func() bool {
		if a.files == nil {
			return false
		}
		for _, have := range a.files.view.Panes() {
			if have.At() != "" {
				p = have
				return true
			}
		}
		return false
	})
	return p
}

// A pane opens at the folder it was given and reads it: the pane takes
// a path before the read comes back, so the listing is what says it
// worked.
func TestAPaneOpensAtTheFolderItWasGiven(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := aFilePaneAt(t, a, dir)
	waitFor(t, a, "the folder to be read", func() bool { return len(p.Entries()) > 0 })

	if got := p.At(); !samePath(got, dir) {
		t.Errorf("the pane opened at %q, want %q", got, dir)
	}
	if err := p.Err(); err != nil {
		t.Errorf("reading the folder: %v", err)
	}
	var names []string
	for _, e := range p.Entries() {
		names = append(names, e.Name)
	}
	if !slices.Contains(names, "marker.txt") {
		t.Errorf("the pane lists %v, want what is in the folder", names)
	}
}

// A folder that cannot be read is said so and the pane stays where it
// is, rather than being moved somewhere it was not asked for.
func TestAFolderThatWillNotOpenIsSaidSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	gone := filepath.Join(t.TempDir(), "no-such-folder")

	p := aFilePaneAt(t, a, gone)

	n := awaitModal(t, a, "the notice about the folder", func(got *ui.Notice) bool {
		return strings.Contains(got.Title, "no-such-folder")
	})
	if !n.Failure {
		t.Error("the notice does not read as something going wrong")
	}
	home, err := vfs.NewLocal().Home()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	// Watched for long enough that a read of this machine's own home
	// would have landed, which is what a fallback would have asked for.
	// A second pane reads one in a fraction of this.
	for range 60 {
		a.pump.run()
		time.Sleep(5 * time.Millisecond)
		if samePath(p.At(), home) {
			t.Fatal("the pane moved to home, which is not where it was asked to go")
		}
	}
	if len(p.Entries()) != 0 {
		t.Errorf("the pane lists %d things from a folder that would not open", len(p.Entries()))
	}
	if p.Err() == nil {
		t.Error("the pane says the folder opened")
	}
}

// samePath reports whether two paths name the same place, whichever way
// round the separators are written and whatever case they are in.
func samePath(a, b string) bool {
	clean := func(s string) string {
		return strings.ToLower(filepath.Clean(strings.ReplaceAll(s, "/", string(filepath.Separator))))
	}
	return clean(a) == clean(b)
}

// The line the plus offers opens the folder it names, on a machine the
// window really reaches.
func TestThePlusLineOpensTheFolderItNames(t *testing.T) {
	a, _ := aSavedMachine(t)
	one, two := filepath.ToSlash(t.TempDir()), filepath.ToSlash(t.TempDir())
	putFile(t, two, "marker.txt", "x")
	withFoldersOn(t, a, "margit", one, two)

	chooseMenuItem(t, clickPlus(t, a, "margit"), folderCommandID("margit", 1))

	p := filePaneThatReads(t, a, "margit")
	if got := p.At(); !samePath(got, two) {
		t.Errorf("the second line opened %q, want %q", got, two)
	}
}

// And "Files" on a machine with one folder opens there rather than at
// home, which is the only thing that ties oneFolderOn to a pane.
func TestTheFilesLineOpensAtTheOneFolder(t *testing.T) {
	a, _ := aSavedMachine(t)
	dir := filepath.ToSlash(t.TempDir())
	putFile(t, dir, "marker.txt", "x")
	withFoldersOn(t, a, "margit", dir)

	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.files")

	p := filePaneThatReads(t, a, "margit")
	if got := p.At(); !samePath(got, dir) {
		t.Errorf("Files opened %q, want the one folder saved", got)
	}
}

// The folder survives the dial: a machine nothing is connected to is
// reached first and the pane opens at the folder it was asked for.
func TestTheFolderSurvivesTheConnection(t *testing.T) {
	a, _ := aSavedMachine(t)
	dir := filepath.ToSlash(t.TempDir())
	putFile(t, dir, "marker.txt", "x")
	withFoldersOn(t, a, "margit", dir)
	if a.machines.named("margit") != nil {
		t.Fatal("something is connected already, so the dial proves nothing")
	}

	chooseMenuItem(t, clickPlus(t, a, "margit"), "conn.files")

	p := filePaneThatReads(t, a, "margit")
	if got := p.At(); !samePath(got, dir) {
		t.Errorf("the pane opened at %q after the dial, want %q", got, dir)
	}
}

// withFoldersOn puts folders on a machine already in the server list.
func withFoldersOn(t *testing.T, a *testApp, name string, folders ...string) {
	t.Helper()
	h, ok := a.book.Lookup(name)
	if !ok {
		t.Fatalf("there is no saved server called %q", name)
	}
	h.Folders = folders
	if err := a.book.Put(h, name); err != nil {
		t.Fatalf("save %q: %v", name, err)
	}
	a.refreshServers()
}

// filePaneThatReads is the file pane on a machine, once it has read
// somewhere: a pane takes a path before the read comes back, so the
// listing is what says it arrived.
func filePaneThatReads(t *testing.T, a *testApp, host string) *files.Pane {
	t.Helper()
	var p *files.Pane
	waitFor(t, a, "a file pane reading "+host, func() bool {
		p = filePaneOn(a, host)
		return p != nil && len(p.Entries()) > 0
	})
	return p
}

// The dialog turns away folders the server list would refuse, with the
// dialog still open and what was typed still in it.
func TestTheDialogRefusesTheSameFolderTwice(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	savedWith(t, a, "margit", "/var/log")

	if err := a.openEditServer("margit"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal(t, a, "the edit dialog", byTitle[*ui.Form]("Edit margit"))
	retypeField(t, a, f, "Folders", "/srv/app,/srv/app")
	pressButton(t, a, f, "Save")

	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a folder named twice")
	}
	if f.Error() == nil || !strings.Contains(f.Error().Error(), "twice") {
		t.Errorf("it said %v", f.Error())
	}
	h, _ := a.book.Lookup("margit")
	if !slices.Equal(h.Folders, []string{"/var/log"}) {
		t.Errorf("it saved %v anyway", h.Folders)
	}
}

// A server list holding a folder with nothing in it is turned away
// rather than read back with a folder that names nothing.
func TestAServerListWithABadFolderIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	raw := `{"version":1,"servers":[{"name":"margit","address":"m.example","folders":["/var/log",""]}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := remote.LoadBook(path)
	if err == nil {
		t.Fatal("a folder with nothing in it was read back")
	}
	if !strings.Contains(err.Error(), "no path") {
		t.Errorf("it said %q", err)
	}
}

// Folders go to the file and come back, which is the whole point of
// saving them on the server.
func TestFoldersSurviveTheServerList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	book, err := remote.LoadBook(path)
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	if err := book.Put(remote.Host{
		Name: "margit", Address: "m.example", Folders: []string{"/var/log", "/srv/app"},
	}, ""); err != nil {
		t.Fatalf("put: %v", err)
	}

	again, err := remote.LoadBook(path)
	if err != nil {
		t.Fatalf("read it again: %v", err)
	}
	h, ok := again.Lookup("margit")
	if !ok {
		t.Fatal("the server is not in the file")
	}
	if want := []string{"/var/log", "/srv/app"}; !slices.Equal(h.Folders, want) {
		t.Errorf("the file holds %v, want %v", h.Folders, want)
	}
}

// What Lookup gives back is the caller's own, so changing it cannot
// change the server list.
func TestLookupGivesBackItsOwnFolders(t *testing.T) {
	a := newTestApp(t, 80, 24)
	savedWith(t, a, "margit", "/var/log")

	h, _ := a.book.Lookup("margit")
	h.Folders[0] = "/etc/shadow"

	again, _ := a.book.Lookup("margit")
	if again.Folders[0] != "/var/log" {
		t.Errorf("the server list now says %q", again.Folders[0])
	}
}

// A folder moved from one machine to another rebuilds the lines. The
// list of folders reads the same both ways round, so the machine each
// one is on has to be part of what says the list changed.
func TestMovingAFolderBetweenMachinesRebuildsTheLines(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withMenubar(t, a)
	savedWith(t, a, "margit", "/var/log", "/srv/app")
	savedWith(t, a, "olga")

	// Both in one go, and the lines built once afterwards: a refresh in
	// between would see a list that had changed for another reason.
	putFolders(t, a, "margit", "/var/log")
	putFolders(t, a, "olga", "/srv/app")
	a.refreshServers()

	if _, ok := a.root.Commands.Lookup(folderCommandID("margit", 1)); ok {
		t.Error("margit still has a command for a folder it no longer holds")
	}
	if _, ok := a.root.Commands.Lookup(folderCommandID("olga", 0)); !ok {
		t.Error("olga has no command for the folder it was given")
	}
}

// putFolders puts folders on a saved server without rebuilding the
// lines.
func putFolders(t *testing.T, a *testApp, name string, folders ...string) {
	t.Helper()
	h, ok := a.book.Lookup(name)
	if !ok {
		t.Fatalf("there is no saved server called %q", name)
	}
	h.Folders = folders
	if err := a.book.Put(h, name); err != nil {
		t.Fatalf("save %q: %v", name, err)
	}
}

// A folder the one-line field cannot show is left as it was when the
// field is not touched, rather than being split in two by a save that
// was about something else.
func TestAFolderWithACommaSurvivesAnUntouchedSave(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	savedWith(t, a, "margit", "/srv/my,app")

	if err := a.openEditServer("margit"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal(t, a, "the edit dialog", byTitle[*ui.Form]("Edit margit"))
	pressButton(t, a, f, "Save")

	if a.root.Modal() == f {
		t.Fatalf("the save was refused although the field was not touched: %v", f.Error())
	}
	h, ok := a.book.Lookup("margit")
	if !ok {
		t.Fatal("the server is gone")
	}
	if want := []string{"/srv/my,app"}; !slices.Equal(h.Folders, want) {
		t.Errorf("it saved %v, want the folder it had", h.Folders)
	}
}

// And a save that does touch the field is refused, rather than writing
// back folders the field could never have shown.
func TestEditingAFolderWithACommaIsRefused(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	savedWith(t, a, "margit", "/srv/my,app")

	if err := a.openEditServer("margit"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	f := awaitModal(t, a, "the edit dialog", byTitle[*ui.Form]("Edit margit"))
	retypeField(t, a, f, "Folders", "/var/log")
	pressButton(t, a, f, "Save")

	if a.root.Modal() != f {
		t.Fatal("the dialog closed on folders it cannot show")
	}
	if f.Error() == nil || !strings.Contains(f.Error().Error(), "comma") {
		t.Errorf("it said %v", f.Error())
	}
	h, _ := a.book.Lookup("margit")
	if want := []string{"/srv/my,app"}; !slices.Equal(h.Folders, want) {
		t.Errorf("it saved %v anyway", h.Folders)
	}
}
