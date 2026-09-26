package main

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/internal/build"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui/files"
)

// The window's help: every command with its shortcut and its name in
// the shortcuts file, the keys of the file pane and the reader, what
// this build is, and where its files are.

// everyCommand is every command the window has, by id, with its title,
// the palette's words first and the menus' after.
func everyCommand() [][2]string {
	var out [][2]string
	seen := map[string]bool{}
	add := func(id, title string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, [2]string{id, strings.TrimSuffix(title, "…")})
	}
	for _, c := range commands {
		add(c.id, c.title)
	}
	for _, m := range menus {
		for _, it := range m.items {
			if !it.caption {
				add(it.id, m.title+": "+it.title)
			}
		}
	}
	return out
}

// helpPane lists every command in a table: its title, its shortcut and
// its name in the shortcuts file, under the menu it is on, as
// gridterm's help groups them. The keys no menu shows follow, then the
// file pane's and the reader's, from gridterm's own lists. Typing
// finds one by its title.
type helpPane struct {
	table *widget.Table
	rows  map[widget.Key][3]string
	// heads are the rows that head a group.
	heads map[widget.Key]bool
}

func newHelpPane(w *window) *helpPane {
	p := &helpPane{rows: map[widget.Key][3]string{}, heads: map[widget.Key]bool{}}
	p.table = widget.NewTable(
		widget.TableColumn{Title: "Command"},
		widget.TableColumn{Title: "Shortcut", Width: 190},
		widget.TableColumn{Title: "In the shortcuts file", Width: 220},
	)
	p.table.Row = func(k widget.Key) widget.TableRow {
		r := p.rows[k]
		if p.heads[k] {
			return widget.TableRow{Cells: r[:], Strong: true}
		}
		return widget.TableRow{Cells: r[:], Faint: r[2] == ""}
	}
	p.table.OnSort = func(int, bool, *gunim.UI) {}
	p.fill(w)
	return p
}

// helpSection is one group of the help: a heading, and a line per
// thing done, with its shortcut and its id when it has one.
type helpSection struct {
	title string
	lines [][3]string
}

// helpSections groups the commands as the menus do, the palette's
// words for each, then puts the commands no menu has under a heading of
// their own, then the file pane's and the reader's keys.
func helpSections(chord func(id string) string) []helpSection {
	titles := map[string]string{}
	for _, c := range commands {
		titles[c.id] = c.title
	}
	var out []helpSection
	listed := map[string]bool{}
	for _, m := range menus {
		s := helpSection{title: m.title}
		for _, it := range m.items {
			if it.caption || it.id == "" || listed[it.id] {
				continue
			}
			listed[it.id] = true
			title := cmp.Or(titles[it.id], it.title)
			s.lines = append(s.lines, [3]string{strings.TrimSuffix(title, "…"), chord(it.id), it.id})
		}
		if len(s.lines) > 0 {
			out = append(out, s)
		}
	}
	rest := helpSection{title: "Not on the menus"}
	for _, c := range everyCommand() {
		if !listed[c[0]] {
			rest.lines = append(rest.lines, [3]string{c[1], chord(c[0]), c[0]})
		}
	}
	if len(rest.lines) > 0 {
		out = append(out, rest)
	}
	browser := helpSection{title: "The file pane's keys, which the shortcuts file leaves as they are"}
	for _, k := range files.BrowserKeys() {
		browser.lines = append(browser.lines, [3]string{k.Title, k.Shown, ""})
	}
	browser.lines = append(browser.lines,
		[3]string{"Up a folder", "Backspace", ""},
		[3]string{"Mark", "Space", ""},
		[3]string{"Find by name", "type the name", ""},
	)
	reader := helpSection{title: "The reader's keys, which the shortcuts file leaves as they are"}
	for _, k := range files.ReaderKeys() {
		reader.lines = append(reader.lines, [3]string{k.Title, k.Shown, ""})
	}
	reader.lines = append(reader.lines,
		[3]string{"Pick text out", "drag, or Shift and a key that moves", ""},
		[3]string{"Pick out the whole file", "^A", ""},
		[3]string{"Copy what is picked out", files.CopyKey().Shown, ""},
		[3]string{"Drop what is picked out", "Esc", ""},
	)
	return append(out, browser, reader)
}

// fill takes the rows from the window's commands and keys, as they are
// now.
func (p *helpPane) fill(w *window) {
	clear(p.rows)
	clear(p.heads)
	chord := func(id string) string {
		if ch, ok := w.keys.ChordFor(id); ok {
			return chordLabel(ch)
		}
		return ""
	}
	n := 0
	add := func(r [3]string) widget.Key {
		k := widget.Key(fmt.Sprintf("%04d", n))
		n++
		p.rows[k] = r
		return k
	}
	for _, s := range helpSections(chord) {
		p.heads[add([3]string{s.title, "", ""})] = true
		for _, l := range s.lines {
			add(l)
		}
	}
}

// show brings the rows up to date.
func (p *helpPane) show(w *window, u *gunim.UI) {
	p.fill(w)
	keys := make([]widget.Key, 0, len(p.rows))
	for k := range p.rows {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	p.table.SetKeys(keys, u)
}

// Children implements [gunim.Composite].
func (p *helpPane) Children() []gunim.Node { return []gunim.Node{p.table} }

// Layout implements [gunim.Node].
func (p *helpPane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (p *helpPane) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	kids.At(0).Paint(pt)
}

// applyShortcuts takes on the changes the shortcuts file makes to the
// keys gridterm comes with, and reports whether it did. A file naming a
// command there is none of changes nothing, and says which.
func (w *window) applyShortcuts(changes []keys.Change, u *gunim.UI) bool {
	known := map[string]bool{}
	for _, c := range everyCommand() {
		known[c[0]] = true
	}
	for _, b := range shortcuts().Bindings() {
		known[b.ID] = true
	}
	for alias := range aliases {
		known[alias] = true
	}
	next := shortcuts()
	var unknown []string
	for _, c := range changes {
		// An id that has been renamed is followed to its new name, so a
		// file written before the rename goes on working.
		if to, moved := keys.Renamed[c.Command]; moved {
			c.Command = to
		}
		switch {
		case c.Command == "":
			next.Unbind(c.Chord)
		case !known[c.Command] && !isItem(c.Command):
			unknown = append(unknown, c.Written+" runs "+c.Command)
		default:
			id := c.Command
			if to, ok := aliases[id]; ok {
				id = to
			}
			if err := next.Bind(c.Chord, id); err != nil {
				unknown = append(unknown, c.Written+": "+err.Error())
			}
		}
	}
	if len(unknown) > 0 {
		w.toasts.Show(widget.Toast{Title: "The shortcuts file names commands this window lacks", Body: strings.Join(unknown, "; ") + ". None of it was used; Shortcuts and Commands lists every command's name."}, u)
		return false
	}
	w.keys.Become(next)
	// The menus and the palette say the new chords.
	for m := range menus {
		for i, it := range menus[m].items {
			if it.caption || i >= len(w.bar.Menus[m].Hints) {
				continue
			}
			w.bar.Menus[m].Hints[i] = ""
			if ch, ok := w.keys.ChordFor(it.id); ok {
				w.bar.Menus[m].Hints[i] = chordLabel(ch)
			}
		}
	}
	w.servers(w.saved)
	if w.help != nil {
		w.help.show(w, u)
	}
	return true
}

// aboutDialog says what this build is.
func (w *window) aboutDialog(u *gunim.UI) {
	d := widget.NewDialog("About gridterm")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("A GPU-drawn terminal emulator, on gunim.")).
		Add("Version", widget.NewLabel(build.Version()))
	d.SetButtons("Close", "")
	d.AddAction("Check for Updates", func(u *gunim.UI) { u.Send(w, CheckUpdates{}) })
	d.Accept, d.Dismiss = DialogClosed{}, DialogClosed{}
	w.openDialog(d, u)
}

// fileLocationsDialog says where the window keeps its files.
func (w *window) fileLocationsDialog(u *gunim.UI) {
	dir, err := conf.Dir()
	if err != nil {
		w.toasts.Show(widget.Toast{Title: "Couldn't find where the files are", Body: err.Error()}, u)
		return
	}
	form := widget.NewForm()
	for _, f := range []struct{ what, name string }{
		{"Settings", settings.File}, {"Saved servers", remote.BookFile}, {"Themes", themes.File},
		{"Shortcuts", keys.File}, {"Authorized keys", serve.AuthFile}, {"Known windows", knownWindowsFile},
	} {
		form.Add(f.what, widget.NewLabel(filepath.Join(dir, f.name)))
	}
	if key, err := serve.HostKeyPath(); err == nil {
		form.Add("Serving key", widget.NewLabel(key))
	}
	form.Add("", widget.NewLabel("SSH keys and known_hosts stay in ~/.ssh."))
	if own, beside, err := conf.CarriesItsOwn(); err == nil && own {
		form.Add("", widget.NewLabel("Portable: the files are kept beside gridterm, in "+beside+". The serving key is in there too, and is only as private as that folder."))
	}
	d := widget.NewDialog("File Locations")
	d.Body = form
	d.SetButtons("Close", "")
	// A copy not yet carrying its own files is offered to, until the
	// folder is made: this window goes on reading where it opened
	// reading, and a second press would copy over the first.
	if own, beside, err := conf.CarriesItsOwn(); err == nil && !own {
		if made, err := conf.IsDir(beside); err == nil && !made {
			d.AddAction("Make Portable", func(u *gunim.UI) {
				d.Close(u)
				u.Send(w, MakePortable{})
			})
		}
	}
	d.Accept, d.Dismiss = DialogClosed{}, DialogClosed{}
	w.openDialog(d, u)
}
