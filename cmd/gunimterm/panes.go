package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// The window's side of file panes and readers.

// up is the key of a file pane's row for the folder above.
const up widget.Key = ".."

// browser is a file pane: the folder's path over a table of what is in
// it, folders first.
type browser struct {
	w     *window
	id    string
	path  *widget.Label
	table *widget.Table
	col   *widget.Flex
	st    Browser
	shown int
	// byName finds an entry by its row's key; sortBy and descending are
	// the order the user asked for.
	byName     map[widget.Key]vfs.Entry
	sortBy     int
	descending bool
	// keys is the bar of keys at the foot, and problem the line under
	// the path saying a folder could not be read.
	keys    *keyBar
	problem *errLine
	// goTo is Go To's field while it is open, and asked the folder whose
	// names were last asked for, to complete from.
	goTo  *widget.TextField
	asked string
}

func newBrowser(w *window, id string) *browser {
	b := &browser{w: w, id: id, byName: map[widget.Key]vfs.Entry{}}
	b.path = widget.NewLabel("")
	b.path.Size, b.path.Color, b.path.MaxLines = smallText, faint, 1
	b.table = widget.NewTable(
		widget.TableColumn{Title: "Name"},
		widget.TableColumn{Title: "Size", Width: 90, End: true},
		widget.TableColumn{Title: "Modified", Width: 150},
	)
	b.table.Row = b.row
	b.table.OnActivate = func(k widget.Key, u *gunim.UI) {
		if k == up {
			u.Send(b.table, GoUp{Pane: b.id})
			return
		}
		u.Send(b.table, EnterEntry{Pane: b.id, Name: string(k)})
	}
	b.table.OnSort = func(col int, desc bool, u *gunim.UI) {
		b.sortBy, b.descending = col, desc
		b.list(u)
	}
	b.table.SetSorted(0, false)
	b.keys = b.newKeys()
	b.problem = &errLine{b: b, label: widget.NewLabel("")}
	b.problem.label.Size, b.problem.label.Color, b.problem.label.MaxLines = smallText, widget.ButtonDangerFill, 1
	b.col = widget.Column(widget.NewPad(b.path), b.problem, b.table, b.keys).Grow(b.table, 1)
	b.col.Cross, b.col.Gap = widget.CrossStretch, noGap
	return b
}

// Children implements [gunim.Composite].
func (b *browser) Children() []gunim.Node { return []gunim.Node{b.col} }

// Layout implements [gunim.Node].
func (b *browser) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (b *browser) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	kids.At(0).Paint(p)
}

// Handle implements [gunim.Handler]: gridterm's keys for files.
// Backspace goes up; F5 or Ctrl+C copies the marked names, or the one
// under the cursor, to the file clipboard, and F6 or Ctrl+X cuts them;
// F7 or Ctrl+V pastes here; F8 or Delete deletes, after asking; F2
// renames; F9 makes a folder.
func (b *browser) Handle(e gi.Event, u *gunim.UI) bool {
	k, ok := e.(gi.KeyPress)
	if !ok {
		return false
	}
	ctrl := k.Mods == gi.ModControl
	switch {
	case k.Key == gi.KeyBackspace && k.Mods == 0:
		u.Send(b, GoUp{Pane: b.id})
	case k.Key == gi.KeyF5 && k.Mods == 0, k.Key == gi.KeyC && ctrl:
		u.Send(b, ClipFiles{Pane: b.id, Names: b.picked()})
	case k.Key == gi.KeyF6 && k.Mods == 0, k.Key == gi.KeyX && ctrl:
		u.Send(b, ClipFiles{Pane: b.id, Names: b.picked(), Cut: true})
	case k.Key == gi.KeyF7 && k.Mods == 0, k.Key == gi.KeyV && ctrl:
		u.Send(b, PasteFiles{Pane: b.id})
	case k.Key == gi.KeyF8 && k.Mods == 0, k.Key == gi.KeyDelete && k.Mods == 0:
		b.confirmDelete(u)
	case k.Key == gi.KeyF2 && k.Mods == 0:
		b.askRename(u)
	case k.Key == gi.KeyF9 && k.Mods == 0:
		b.askFolder(u)
	case k.Key == gi.KeyF3 && k.Mods == 0, k.Key == gi.KeyF4 && k.Mods == 0:
		// A link is read through, wherever it goes; a folder is Enter's.
		if c, ok := b.table.Cursor(); ok && c != up && !(b.byName[c].IsDir() && !b.byName[c].IsLink()) {
			u.Send(b, ViewFile{Pane: b.id, Name: string(c), Follow: k.Key == gi.KeyF4})
		}
	case k.Key == gi.KeyG && ctrl:
		b.askGoTo(u)
	case k.Key == gi.KeyD && ctrl:
		u.Send(b, ClosePane{Pane: b.id})
	case k.Key == gi.KeyEscape && k.Mods == 0:
		// The table has had it first, for a name being found.
		u.Send(b, DropFileClip{})
	case k.Key == gi.KeyTab && (k.Mods == 0 || k.Mods == gi.ModShift):
		// To the next file pane, or the one before, as between
		// gridterm's two.
		if next := b.w.nextFilePane(b.id, k.Mods == gi.ModShift); next != "" {
			u.Send(b, FocusPane{Pane: next})
		}
	default:
		return false
	}
	b.table.ClearMarks()
	return true
}

// newKeys is the bar of the pane's keys, as gridterm's file manager
// shows them, each lit while it does something here.
func (b *browser) newKeys() *keyBar {
	onRow := func() bool {
		c, ok := b.table.Cursor()
		return ok && c != up
	}
	onFile := func() bool {
		c, ok := b.table.Cursor()
		return ok && c != up && !(b.byName[c].IsDir() && !b.byName[c].IsLink())
	}
	somePicked := func() bool { return len(b.picked()) > 0 }
	waiting := func() bool { return len(b.w.fileClip.Names) > 0 }
	on := map[string]func() bool{
		"Rename": onRow, "View": onFile, "Tail": onFile,
		"Copy": somePicked, "Cut": somePicked, "Delete": somePicked, "Paste": waiting,
	}
	var keys []barKey
	// gridterm's own list, so the two bars say the same.
	for _, k := range files.BrowserKeys() {
		if press, ok := pressOf(k.Chord); ok {
			keys = append(keys, barKey{k.Shown + " " + k.Title, press, on[k.Title]})
		}
	}
	bar := newKeyBar(keys...)
	bar.pressed = func(k gi.KeyPress, u *gunim.UI) { b.Handle(k, u) }
	return bar
}

// picked returns the marked names, or the one under the cursor.
func (b *browser) picked() []string {
	var out []string
	for _, k := range b.table.Marked() {
		if k != up {
			out = append(out, string(k))
		}
	}
	if len(out) == 0 {
		if k, ok := b.table.Cursor(); ok && k != up {
			out = []string{string(k)}
		}
	}
	return out
}

func (b *browser) confirmDelete(u *gunim.UI) {
	names := b.picked()
	if len(names) == 0 {
		return
	}
	what := names[0]
	if len(names) > 1 {
		what = count(len(names), "item")
	}
	d := widget.NewDialog("Delete " + what + "?")
	d.Body = widget.NewLabel("From " + b.st.Path + ". This can't be undone.")
	d.SetButtons("Delete", "Cancel")
	d.Danger = true
	d.Accept = DeleteFiles{Pane: b.id, Names: names}
	d.Dismiss = DialogClosed{}
	b.w.openDialog(d, u)
}

func (b *browser) askRename(u *gunim.UI) {
	k, ok := b.table.Cursor()
	if !ok || k == up {
		return
	}
	name := widget.NewTextField()
	name.SetText(string(k))
	d := widget.NewDialog("Rename " + string(k))
	d.Body = widget.NewForm().Add("New name", name)
	d.SetButtons("Rename", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(name.Text()) == "" {
			return "It needs a name."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return RenameFile{Pane: b.id, From: string(k), To: strings.TrimSpace(name.Text())}
	}
	d.Dismiss = DialogClosed{}
	b.w.openDialog(d, u)
}

// askGoTo asks for a folder to show.
func (b *browser) askGoTo(u *gunim.UI) {
	path := widget.NewTextField()
	path.SetText(b.st.Path)
	// The rest of a folder's name, suggested as it is typed, from the
	// folder the text is in.
	path.OnEdit = func(text string, u *gunim.UI) { b.complete(text, u) }
	b.goTo = path
	form := widget.NewForm()
	if len(b.st.Roots) > 1 {
		// Where this filesystem starts, such as each drive, one pick
		// away rather than a letter to remember.
		places := widget.NewDropdown(append([]string{b.st.Path}, b.st.Roots...)...)
		places.OnPick(func(i int, u *gunim.UI) {
			if i > 0 {
				path.SetText(b.st.Roots[i-1])
				u.Invalidate()
			}
		})
		form.Add("Places", places)
	}
	form.Add("Folder", path)
	d := widget.NewDialog("Go to a folder")
	d.Body = form
	d.SetButtons("Go", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(path.Text()) == "" {
			return "Say which folder."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return GoTo{Pane: b.id, Path: path.Text()} }
	d.Dismiss = DialogClosed{}
	b.w.openDialog(d, u)
}

func (b *browser) askFolder(u *gunim.UI) {
	name := widget.NewTextField()
	d := widget.NewDialog("New folder in " + b.st.Path)
	d.Body = widget.NewForm().Add("Name", name)
	d.SetButtons("Make", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(name.Text()) == "" {
			return "It needs a name."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return MakeFolder{Pane: b.id, Name: strings.TrimSpace(name.Text())} }
	d.Dismiss = DialogClosed{}
	b.w.openDialog(d, u)
}

// show takes the program's state for the pane.
func (b *browser) show(st Browser, u *gunim.UI) {
	listed := st.Listed.Dir != b.st.Listed.Dir || !slices.Equal(st.Listed.Folders, b.st.Listed.Folders)
	b.st = st
	b.problem.label.SetText("")
	if st.Err != "" {
		b.problem.label.SetText("Couldn't read this folder. Click for why.")
	}
	if listed && b.goTo != nil {
		b.complete(b.goTo.Text(), u)
	}
	text := st.Path
	if st.Seq == 0 && st.Err == "" {
		// Nothing to show yet, and a read on its way.
		text = "Reading " + st.Path + "…"
	}
	moved := b.path.Text != text
	b.path.SetText(text)
	if st.Seq == b.shown {
		return
	}
	moved = moved || b.shown == 0
	b.shown = st.Seq
	b.list(u)
	// A new folder puts the cursor at the top, or on the name it came
	// from, and shows its rows in place rather than gliding them in
	// from where the last folder was scrolled to; the same folder
	// listed again keeps the cursor where it was.
	switch {
	case st.Land != "" && moved:
		b.table.JumpTo(widget.Key(st.Land), u)
	case st.Land != "":
		b.table.SetCursor(widget.Key(st.Land), u)
	case moved:
		b.table.JumpTo(up, u)
	}
}

// list lists the entries in the order asked for, folders first, under
// the row for the folder above.
func (b *browser) list(u *gunim.UI) {
	entries := slices.Clone(b.st.Entries)
	slices.SortStableFunc(entries, func(x, y vfs.Entry) int {
		if x.IsDir() != y.IsDir() {
			if x.IsDir() {
				return -1
			}
			return 1
		}
		n := 0
		switch b.sortBy {
		case 1:
			n = cmp.Compare(x.Size, y.Size)
		case 2:
			n = x.Mod.Compare(y.Mod)
		}
		if n == 0 {
			n = cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		}
		if b.descending {
			n = -n
		}
		return n
	})
	var keys []widget.Key
	if !b.st.Top {
		// Nowhere above the top of a filesystem, as in gridterm.
		keys = append(keys, up)
	}
	clear(b.byName)
	for _, e := range entries {
		k := widget.Key(e.Name)
		keys = append(keys, k)
		b.byName[k] = e
	}
	b.table.SetKeys(keys, u)
}

// row is what the table shows for an entry: folders strong, links in
// the accent colour with where they go, names starting with a dot
// faint, and one waiting to be pasted with a dot in front, as gridterm
// marks it.
func (b *browser) row(k widget.Key) widget.TableRow {
	if k == up {
		return widget.TableRow{Cells: []string{"..", "", ""}, Strong: true}
	}
	e := b.byName[k]
	size := ""
	switch {
	case e.IsLink() && e.Link != "":
		size = "→ " + e.Link
	case e.IsLink():
		size = "link"
	case !e.IsDir():
		size = humanSize(e.Size)
	}
	name := e.Name
	if b.clipped(e.Name) {
		name = "·" + name
	}
	when := ""
	if !e.Mod.IsZero() {
		when = e.Mod.Format("2006-01-02 15:04")
	}
	return widget.TableRow{Cells: []string{name, size, when}, Strong: e.IsDir() && !e.IsLink(), Faint: strings.HasPrefix(e.Name, "."), Accent: e.IsLink()}
}

// clipped reports whether a name here is waiting to be pasted.
func (b *browser) clipped(name string) bool {
	c := b.w.fileClip
	return c.At == b.st.Path && c.Key == b.w.filesKeyOf(b.id) && slices.Contains(c.Names, name)
}

// humanSize writes a size the way a person reads it.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n), 0
	for v >= unit && i < 4 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGT"[i-1])
}
