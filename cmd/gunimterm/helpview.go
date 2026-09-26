package main

import (
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
)

// The window's help: every command with its shortcut and its name in
// the shortcuts file, the keys of the file pane and the reader, what
// this build is, and where its files are.

// fixedKeys are the keys of the file pane and the reader, which the
// shortcuts file leaves as they are.
var fixedKeys = []struct{ what, keys string }{
	{"Files: go up a folder", "Backspace"},
	{"Files: copy the marked or the one under the cursor", "F5, Ctrl+C"},
	{"Files: cut them", "F6, Ctrl+X"},
	{"Files: paste here", "F7, Ctrl+V"},
	{"Files: delete, after asking", "F8, Delete"},
	{"Files: rename", "F2"},
	{"Files: make a folder", "F9"},
	{"Files: mark", "Space"},
	{"Files: find by name", "type the name"},
	{"Reader: find", "/, Ctrl+F"},
	{"Reader: go to a line", ":"},
	{"Reader: next and previous match", "Enter, Shift+Enter"},
}

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
// its name in the shortcuts file. Typing finds one by its title.
type helpPane struct {
	table *widget.Table
	rows  map[widget.Key][3]string
}

func newHelpPane(w *window) *helpPane {
	p := &helpPane{rows: map[widget.Key][3]string{}}
	p.table = widget.NewTable(
		widget.TableColumn{Title: "Command"},
		widget.TableColumn{Title: "Shortcut", Width: 190},
		widget.TableColumn{Title: "In the shortcuts file", Width: 220},
	)
	p.table.Row = func(k widget.Key) widget.TableRow {
		r := p.rows[k]
		return widget.TableRow{Cells: r[:], Faint: r[2] == ""}
	}
	p.table.OnSort = func(int, bool, *gunim.UI) {}
	p.fill(w)
	return p
}

// fill takes the rows from the window's commands and keys, as they are
// now.
func (p *helpPane) fill(w *window) {
	clear(p.rows)
	for i, c := range everyCommand() {
		chord := ""
		if ch, ok := w.keys.ChordFor(c[0]); ok {
			chord = chordLabel(ch)
		}
		p.rows[widget.Key(fmt.Sprintf("c%03d", i))] = [3]string{c[1], chord, c[0]}
	}
	for i, f := range fixedKeys {
		p.rows[widget.Key(fmt.Sprintf("f%03d", i))] = [3]string{f.what, f.keys, ""}
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
// keys gridterm comes with. A file naming a command there is none of
// changes nothing, and says which.
func (w *window) applyShortcuts(changes []keys.Change, u *gunim.UI) {
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
		return
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
	d.Accept, d.Dismiss = DialogClosed{}, DialogClosed{}
	w.openDialog(d, u)
}
