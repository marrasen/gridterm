// Package files is a two-pane file browser.
//
// One pane is one filesystem and one directory on it. Two of them side
// by side is the whole point: a copy goes from the pane with the keys to
// the other one, and neither pane knows nor cares whether it is this
// machine or one on the far end of a connection.
//
// Nothing here reads a directory on the goroutine that draws. A slow
// mount or a connection with a long way to go would stop the window, so
// a read is handed to another goroutine and the answer is handed back.
package files

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// Sort is what order the names are shown in.
type Sort uint8

const (
	// ByName is alphabetical, which is where a pane starts.
	ByName Sort = iota

	// BySize is largest first.
	BySize

	// ByTime is most recently changed first.
	ByTime
)

// String names a sort the way the pane shows it.
func (s Sort) String() string {
	switch s {
	case BySize:
		return "size"
	case ByTime:
		return "time"
	}
	return "name"
}

// up is the name of the row that goes to the directory above.
const up = ".."

// Style colours a pane.
type Style struct {
	// FG and BG are an ordinary row, and the whole pane's background.
	FG, BG color.RGBA

	// SelectedFG and SelectedBG mark the row the keys act on.
	SelectedFG, SelectedBG color.RGBA

	// HeaderFG is the line at the top saying where the pane is.
	HeaderFG color.RGBA

	// DirFG is a directory, LinkFG a symbolic link, and MarkedFG a name
	// the user has picked out.
	DirFG, LinkFG, MarkedFG color.RGBA

	// NoteFG is the size or the time at the end of a row.
	NoteFG color.RGBA

	// ErrorFG is the line that says why the directory could not be read.
	ErrorFG color.RGBA
}

// Pane is one side of the browser.
//
// It is a widget, and everything it shows comes from what it was last
// told: reading happens elsewhere, so a pane whose machine has gone
// still draws the listing it had and the reason it has no other.
type Pane struct {
	Style Style

	// OnOpen is called when the user opens something that is not a
	// directory. A nil one means Enter does nothing on a file.
	OnOpen func(vfs.Entry)

	// OnChange is called when the pane moves to another directory, so
	// whatever is watching can say where it is.
	OnChange func()

	// Read is how a listing is fetched. It runs the work somewhere else
	// and calls back with what it found, on the goroutine that draws.
	//
	// A nil one leaves the pane empty: there is nothing that can read
	// for it.
	Read func(f vfs.FS, path string, then func([]vfs.Entry, error))

	fs   vfs.FS
	at   string
	sort Sort

	// entries is what is in the directory, in the order shown, and err
	// is why the last read failed.
	entries []vfs.Entry
	err     error

	// reading counts the reads on their way back, so the pane can say it
	// is waiting, and asked is which read is the one being waited for:
	// an answer to an older one is about a directory the user has left.
	reading int
	asked   int

	// marked are the names the user has picked out, by name in this
	// directory. Moving to another directory forgets them: they were
	// about what was in front of them.
	marked map[string]bool

	list *ui.List
	size ui.Size
}

// New returns a pane showing a filesystem. It has nothing in it until
// Open is called.
func New(f vfs.FS) *Pane {
	p := &Pane{fs: f, marked: map[string]bool{}}
	p.list = ui.NewList()
	p.list.OnActivate = func(row ui.ListRow) error { return p.activate(row) }
	return p
}

// FS is the filesystem the pane is showing.
func (p *Pane) FS() vfs.FS { return p.fs }

// At is the directory it is showing.
func (p *Pane) At() string { return p.at }

// Err is why the last read failed, or nil.
func (p *Pane) Err() error { return p.err }

// Entries are the names it is showing, in the order it shows them.
func (p *Pane) Entries() []vfs.Entry { return p.entries }

// Busy reports whether a read is on its way back.
func (p *Pane) Busy() bool { return p.reading > 0 }

// Open moves the pane to a directory and reads it.
//
// The pane shows what it had until the answer comes back, so a slow read
// leaves the user looking at the last thing that worked rather than at
// nothing.
func (p *Pane) Open(path string) {
	if path == "" {
		return
	}
	p.at = path
	p.marked = map[string]bool{}
	if p.OnChange != nil {
		p.OnChange()
	}
	p.Reload()
}

// Reload reads the directory again.
func (p *Pane) Reload() {
	if p.Read == nil || p.at == "" {
		return
	}
	p.reading++
	p.asked++
	// Which read this is. An answer that arrives after a later one has
	// been asked for is about a directory the user has left, whichever
	// order the two came back in.
	want := p.asked
	p.Read(p.fs, p.at, func(entries []vfs.Entry, err error) {
		p.reading--
		if want != p.asked {
			return
		}
		p.show(entries, err)
	})
}

// show puts a listing in the pane.
//
// A read that failed leaves the names that were there. The user is
// looking at a directory they can still act on, and replacing it with
// nothing would say the directory is empty.
func (p *Pane) show(entries []vfs.Entry, err error) {
	p.err = err
	if err == nil {
		p.entries = entries
		p.order()
	}
	p.rows()
}

// SetSort changes the order and redraws.
func (p *Pane) SetSort(s Sort) {
	p.sort = s
	p.order()
	p.rows()
}

// Sort is the order the pane is in.
func (p *Pane) Sort() Sort { return p.sort }

// order sorts the listing the way the pane is set to.
//
// Directories come first whatever the order, because that is what a
// browser is for: the way down is what the user is looking for.
func (p *Pane) order() {
	sort.SliceStable(p.entries, func(i, j int) bool {
		a, b := p.entries[i], p.entries[j]
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		switch p.sort {
		case BySize:
			if a.Size != b.Size {
				return a.Size > b.Size
			}
		case ByTime:
			if !a.Mod.Equal(b.Mod) {
				return a.Mod.After(b.Mod)
			}
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// rows rebuilds what the list shows.
func (p *Pane) rows() {
	rows := make([]ui.ListRow, 0, len(p.entries)+1)
	if !vfs.IsTop(p.fs, p.at) {
		rows = append(rows, ui.ListRow{Text: up, Key: up, FG: p.Style.DirFG})
	}
	for _, e := range p.entries {
		rows = append(rows, p.row(e))
	}
	p.list.SetRows(rows)
}

// row is one name as a line.
func (p *Pane) row(e vfs.Entry) ui.ListRow {
	name := e.Name
	if p.marked[e.Name] {
		// A mark is a character in front of the name rather than a
		// colour alone: a pane on a screen nobody can see the colours of
		// still has to say what is picked out.
		name = "*" + name
	} else {
		name = " " + name
	}
	row := ui.ListRow{Text: name, Key: e.Name, Note: p.note(e)}
	switch {
	case p.marked[e.Name]:
		row.FG = p.Style.MarkedFG
	case e.IsLink():
		row.FG = p.Style.LinkFG
	case e.IsDir():
		row.FG = p.Style.DirFG
	}
	return row
}

// note is what a row says at its end: how big, or when it changed.
func (p *Pane) note(e vfs.Entry) string {
	switch {
	case e.IsLink():
		if e.Link == "" {
			return "link"
		}
		return "→ " + e.Link
	case e.IsDir():
		return "dir"
	case p.sort == ByTime:
		return e.Mod.Format(time.DateOnly)
	}
	return size(e.Size)
}

// size writes a byte count the way a person reads one.
func size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := [...]string{"kB", "MB", "GB", "TB", "PB"}[exp]
	if value < 10 {
		return fmt.Sprintf("%.1f %s", value, suffix)
	}
	return fmt.Sprintf("%.0f %s", value, suffix)
}

// Selected is the name the keys would act on, and whether there is one.
func (p *Pane) Selected() (vfs.Entry, bool) {
	row, ok := p.list.Selected()
	if !ok {
		return vfs.Entry{}, false
	}
	name, _ := row.Key.(string)
	if name == up {
		return vfs.Entry{}, false
	}
	for _, e := range p.entries {
		if e.Name == name {
			return e, true
		}
	}
	return vfs.Entry{}, false
}

// Marked are the names the user has picked out, or the selected one when
// they have picked out nothing.
//
// That is what makes a key work without marking anything first: the
// thing under the bar is what it acts on.
func (p *Pane) Marked() []string {
	if len(p.marked) > 0 {
		out := make([]string, 0, len(p.marked))
		for _, e := range p.entries {
			if p.marked[e.Name] {
				out = append(out, e.Name)
			}
		}
		return out
	}
	if e, ok := p.Selected(); ok {
		return []string{e.Name}
	}
	return nil
}

// Mark picks a name out, or stops picking it out.
func (p *Pane) Mark(name string, on bool) {
	if on {
		p.marked[name] = true
	} else {
		delete(p.marked, name)
	}
	p.rows()
}

// ClearMarks stops picking anything out.
func (p *Pane) ClearMarks() {
	if len(p.marked) == 0 {
		return
	}
	p.marked = map[string]bool{}
	p.rows()
}

// activate opens what the bar is on: a directory is moved into, and
// anything else is handed on.
func (p *Pane) activate(row ui.ListRow) error {
	name, _ := row.Key.(string)
	if name == up {
		p.Up()
		return nil
	}
	e, ok := p.Selected()
	if !ok {
		return nil
	}
	if e.IsDir() && !e.IsLink() {
		p.Open(vfs.Join(p.fs, p.at, e.Name))
		return nil
	}
	if p.OnOpen != nil {
		p.OnOpen(e)
	}
	return nil
}

// Up moves to the directory above, if there is one.
func (p *Pane) Up() {
	if vfs.IsTop(p.fs, p.at) {
		return
	}
	// The name being left, so the bar lands on it rather than at the top
	// of a directory the user has just come out of.
	was := vfs.Base(p.fs, p.at)
	p.Open(vfs.Dir(p.fs, p.at))
	p.list.Select(was)
}

// Layout tells the pane how much room it has.
func (p *Pane) Layout(size ui.Size) {
	p.size = size
	// One row for where it is, and one for a failure when there is one.
	p.list.Layout(ui.Size{Cols: size.Cols, Rows: max(size.Rows-p.head(), 0)})
}

// head is how many rows the pane uses above the listing.
func (p *Pane) head() int {
	if p.err != nil {
		return 2
	}
	return 1
}

// Draw paints the pane.
func (p *Pane) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	v.Fill(grid.Cell{Rune: ' ', FG: p.Style.FG, BG: p.Style.BG, Width: 1})

	where := p.at
	if p.Busy() {
		where += " …"
	}
	// The end of the path rather than the start: which directory this is
	// matters more than which disk it is on, and there is never room for
	// both.
	v.SetString(0, 0, trimLeft(where, cols), p.Style.HeaderFG, p.Style.BG, 0)

	if p.err != nil && rows > 1 {
		v.SetString(0, 1, trimLeft(p.err.Error(), cols), p.Style.ErrorFG, p.Style.BG, 0)
	}
	if rows > p.head() {
		p.list.Draw(v.Sub(0, p.head(), cols, rows-p.head()))
	}
}

// trimLeft keeps the end of a string when it is too long, because that
// is the part that says which directory this is.
func trimLeft(s string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if grid.StringWidth(s) <= cols {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && grid.StringWidth("…"+string(runes)) > cols {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

// SetFocus passes the focus on to the listing, so the bar shows where
// the keys are going.
func (p *Pane) SetFocus(on bool) { p.list.SetFocus(on) }

// Focused reports whether the pane has the keys.
func (p *Pane) Focused() bool { return p.list.Focused() }

// HandleKey takes the keys the pane knows and passes the rest on.
func (p *Pane) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind == input.KeyPress || ev.Kind == input.KeyRepeat {
		switch ev.Key {
		case input.KeyBackspace:
			p.Up()
			return true, nil
		case input.KeySpace:
			// Mark and move on, so a run of names is picked out by
			// holding one key.
			if e, ok := p.Selected(); ok {
				p.Mark(e.Name, !p.marked[e.Name])
				p.list.Move(1)
			}
			return true, nil
		case input.KeyInsert:
			if e, ok := p.Selected(); ok {
				p.Mark(e.Name, !p.marked[e.Name])
				p.list.Move(1)
			}
			return true, nil
		}
	}
	return p.list.HandleKey(ev)
}

// HandleMouse passes the mouse to the listing, which is what knows where
// its rows are.
func (p *Pane) HandleMouse(ev input.MouseEvent) (bool, error) {
	head := p.head()
	if ev.Row < head {
		return true, nil
	}
	ev.Row -= head
	return p.list.HandleMouse(ev)
}
