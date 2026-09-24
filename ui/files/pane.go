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

// errorRow is the row of the head a failed read is reported on, and
// unreadable what it says. The reason itself is too long for a row, so
// the row offers it instead and OnError shows it.
// stillReading is the row a pane with nothing to show yet carries while
// its first listing is on the way, so a slow machine does not look like
// an empty directory.
const stillReading = "reading…"

const (
	errorRow       = 2
	unreadable     = "could not be read"
	unreadableHint = unreadable + " — click to see why"
)

// Style colours a pane.
type Style struct {
	// FG and BG are an ordinary row, and the whole pane's background.
	FG, BG color.RGBA

	// SelectedFG and SelectedBG mark the row the keys act on.
	SelectedFG, SelectedBG color.RGBA

	// HeaderFG is the line at the top naming the machine, and PathFG the
	// one under it saying which directory is being shown.
	HeaderFG color.RGBA
	PathFG   color.RGBA

	// DirFG is a directory, LinkFG a symbolic link, and MarkedFG a name
	// the user has picked out. A reader borrows the last two: LinkFG for
	// a string, and MarkedFG for a heading, a bullet and a search match.
	DirFG, LinkFG, MarkedFG color.RGBA

	// ClipFG is a name waiting to be pasted somewhere.
	ClipFG color.RGBA

	// KeyFG is a key on the bar along the bottom, and OffBG the ground
	// behind one with nothing wired to it. A key that does nothing here
	// is still worth reading, so OffBG sits between the bar's own ground
	// and the one a working key is marked out on.
	KeyFG color.RGBA
	OffBG color.RGBA

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

	// OnGoTo is called when the user asks to go somewhere by name. A
	// nil one leaves the key alone: asking where to go needs a dialog,
	// and this package has none.
	OnGoTo func()

	// OnError is called with the directory that could not be read and the
	// whole of the failure: once when the pane that has the keys fails, or
	// when a pane whose read failed gains them, and again whenever the row
	// reporting it is clicked. A nil one takes the offer off that row:
	// showing an error needs a dialog, and this package has none.
	OnError func(path string, err error)

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
	// is why the last read failed. errAt is the directory that read was
	// about, which is not where the pane is when a move failed. told says
	// the reason has been shown already, so one failure produces one
	// dialog.
	entries []vfs.Entry
	err     error
	errAt   string
	told    bool

	// going is the directory a move is waiting on, and waiting is who
	// asked for it.
	going   string
	waiting func(error)

	// reading counts the reads on their way back, so the pane can say it
	// is waiting, and asked is which read is the one being waited for:
	// an answer to an older one is about a directory the user has left.
	reading int
	asked   int

	// marked are the names the user has picked out, by name in this
	// directory. Moving to another directory forgets them: they were
	// about what was in front of them.
	marked map[string]bool

	// finding is what has been typed to jump to a name, and findingAt
	// when the last letter of it arrived. A pause between letters
	// starts a new name rather than adding to the old one.
	finding   string
	findingAt time.Time

	// clock says what time it is, so a test can decide when a pause has
	// gone by rather than waiting for one.
	clock func() time.Time

	// clipped are the names waiting to be pasted, and clippedAt the
	// directory they were picked out in. The pane may be somewhere else
	// by now, and a name means nothing outside the directory it was
	// read in.
	clipped   map[string]bool
	clippedAt string

	// land is the name to put the bar on once the listing arrives, for
	// coming back up out of a directory. The rows are not there yet when
	// the move is asked for.
	land string

	list *ui.List
	size ui.Size
}

// New returns a pane showing a filesystem. It has nothing in it until
// Open is called.
func New(f vfs.FS) *Pane {
	p := &Pane{fs: f, marked: map[string]bool{}, clock: time.Now}
	p.list = ui.NewList()
	p.list.OnActivate = func(row ui.ListRow) error { return p.activate(row) }
	// A click points at a name and a double click opens it, the way
	// every other file manager does: opening on one click took the user
	// into a directory they had only meant to pick out.
	p.list.DoubleClick = true
	p.list.Clock = func() time.Time { return p.clock() }
	return p
}

// dress passes the pane's colours to the list, which is what draws every
// row.
//
// Done here rather than once at the start, because Style is a field the
// caller sets after the pane is made. Writing the same colours again
// costs nothing.
func (p *Pane) dress() {
	p.list.Style = ui.ListStyle{
		FG:         p.Style.FG,
		BG:         p.Style.BG,
		SelectedFG: p.Style.SelectedFG,
		SelectedBG: p.Style.SelectedBG,
		HeaderFG:   p.Style.HeaderFG,
		NoteFG:     p.Style.NoteFG,
	}
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
func (p *Pane) Open(path string) { p.openAt(path, "", nil) }

// OpenThen moves the pane and says how the read went. then takes the
// place of OnError for that read.
func (p *Pane) OpenThen(path string, then func(error)) { p.openAt(path, "", then) }

// openAt asks for a directory, remembering a name to put the bar on once
// the listing arrives.
func (p *Pane) openAt(path, land string, then func(error)) {
	if path == "" {
		return
	}
	// A pane with nothing showing has nothing to keep, so it moves now.
	if p.at == "" {
		p.arrive(path)
	}
	p.going = path
	p.ask(path, land, then)
}

// Reload reads the directory again. A move that is still waiting for its
// listing is left alone: the pane is on its way somewhere else, and
// cutting in would answer whoever asked for the move with the wrong
// directory.
func (p *Pane) Reload() {
	if p.going != "" {
		return
	}
	p.ask(p.at, "", nil)
}

// ask asks for a listing and shows it when it arrives.
func (p *Pane) ask(path, land string, then func(error)) {
	if p.Read == nil || path == "" {
		return
	}
	p.reading++
	p.asked++
	if p.waitingForFirst() {
		p.rows()
	}
	// Which read this is. An answer that arrives after a later one has
	// been asked for is about a directory the user has left, whichever
	// order the two came back in.
	want := p.asked
	p.Read(p.fs, path, func(entries []vfs.Entry, err error) {
		p.reading--
		if want != p.asked {
			// Not the read being waited for, which will say what it
			// found when it comes back.
			return
		}
		// Carried by the read rather than by the pane, so an answer
		// lands on whoever asked for that read and nobody else.
		p.land, p.waiting = land, then
		p.show(path, entries, err)
	})
}

// show puts a listing in the pane.
//
// A read that failed leaves the names that were there, which is the
// fallback Marcus approved: the whole of the error goes to OnError, and
// the row that reports it brings it back.
func (p *Pane) show(path string, entries []vfs.Entry, err error) {
	was := p.head()
	p.err, p.told, p.going = err, false, ""
	if err != nil {
		p.errAt = path
	} else {
		p.entries = entries
		p.order()
		// After the listing, so anything OnChange reads is the new one.
		p.arrive(path)
	}
	// The rows are about to be built, so a name to land on can be
	// reached now and not before.
	defer func() {
		if p.land != "" {
			p.list.Select(p.land)
			p.land = ""
		}
	}()
	shorter := p.head() != was
	if shorter {
		// The line saying why takes a row from the listing. Without
		// telling the list, it works from a height it no longer has and
		// the bar walks off the bottom of what is on screen.
		p.Layout(p.size)
	}
	p.rows()
	if shorter {
		// And the bar is brought back into view: it is where the user
		// put it, and it has to still be somewhere they can see.
		p.list.Reveal()
	}
	// Whoever asked for the move hears instead of OnError, because they
	// are showing the reason themselves.
	if waiting := p.waiting; waiting != nil {
		p.waiting, p.told = nil, true
		waiting(err)
		return
	}
	// Straight away in the pane the user asked in; SetFocus does it for
	// a pane they are not looking at.
	if p.Focused() {
		p.tell()
	}
}

// arrive moves the pane to a directory a read has just come back from.
func (p *Pane) arrive(path string) {
	if p.at == path {
		return
	}
	p.at = path
	// The marks were about the old directory.
	p.marked = map[string]bool{}
	if p.OnChange != nil {
		p.OnChange()
	}
}

// tell offers the reason the last read failed, once per failure.
func (p *Pane) tell() {
	if p.err == nil || p.told || p.OnError == nil {
		return
	}
	p.told = true
	p.OnError(p.errAt, p.err)
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
	if p.waitingForFirst() {
		// A header, so the bar steps over it and a click does nothing.
		rows = append(rows, ui.ListRow{Text: " " + stillReading, Header: true})
	}
	p.list.SetRows(rows)
}

// waitingForFirst says the pane has nothing to show and a read is on the
// way. A pane with a listing keeps showing it while it moves: that is
// the last thing that worked, and the path line says where it is going.
func (p *Pane) waitingForFirst() bool {
	return p.Busy() && len(p.entries) == 0 && p.err == nil
}

// row is one name as a line.
func (p *Pane) row(e vfs.Entry) ui.ListRow {
	name := e.Name
	switch {
	case p.marked[e.Name]:
		// A mark is a character in front of the name rather than a
		// colour alone: a pane on a screen nobody can see the colours of
		// still has to say what is picked out.
		name = "*" + name
	case p.isClipped(e.Name):
		// And one waiting to be pasted says so the same way.
		name = "·" + name
	default:
		name = " " + name
	}
	row := ui.ListRow{Text: name, Key: e.Name, Note: p.note(e)}
	switch {
	case p.marked[e.Name]:
		row.FG = p.Style.MarkedFG
	case p.isClipped(e.Name):
		row.FG = p.Style.ClipFG
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

// sizeIn writes a byte count in the unit another one would be written
// in, and with the same decimals, so a pair reads as "1.9 of 4.2 MB"
// rather than as two counts the reader has to line up.
func sizeIn(n, of int64) string {
	const unit = 1024
	if of < unit {
		return fmt.Sprintf("%d", n)
	}
	div, exp := int64(unit), 0
	for of/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	if float64(of)/float64(div) < 10 {
		return fmt.Sprintf("%.1f", float64(n)/float64(div))
	}
	return fmt.Sprintf("%.0f", float64(n)/float64(div))
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

// SetClipped says which names in a directory are waiting to be pasted,
// so the pane can show them as spoken for.
//
// The directory comes with them: the pane may have moved on, and the
// same name in another directory is another file.
func (p *Pane) SetClipped(at string, names []string) {
	p.clipped = make(map[string]bool, len(names))
	p.clippedAt = at
	for _, name := range names {
		p.clipped[name] = true
	}
	p.rows()
}

// isClipped reports whether a name in the directory being shown is
// waiting to be pasted.
func (p *Pane) isClipped(name string) bool {
	return p.clippedAt != "" && p.clippedAt == p.at && p.clipped[name]
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
	// of a directory the user has just come out of. It is remembered
	// rather than selected: the rows of the directory above have not
	// been read yet.
	was := vfs.Base(p.fs, p.at)
	p.openAt(vfs.Dir(p.fs, p.at), was, nil)
}

// Size is the room the pane was last given.
func (p *Pane) Size() ui.Size { return p.size }

// The switcher draws a picture of a pane at the size it says it has, so
// a method here with the wrong shape would leave its tile empty.
var _ ui.Sized = (*Pane)(nil)

// Layout tells the pane how much room it has.
func (p *Pane) Layout(size ui.Size) {
	p.size = size
	// One row for where it is, and one for a failure when there is one.
	p.list.Layout(ui.Size{Cols: size.Cols, Rows: max(size.Rows-p.head(), 0)})
}

// head is how many rows the pane uses above the listing: the machine,
// the directory, and the row that reports a failed read when there is
// one.
func (p *Pane) head() int {
	if p.err != nil {
		return errorRow + 1
	}
	return errorRow
}

// Draw paints the pane.
func (p *Pane) Draw(v grid.View) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	p.dress()

	// Where it is going while it is going there, so a slow directory
	// says what is being waited for rather than only that something is.
	where := p.at
	if p.going != "" {
		where = p.going
	}
	if p.Busy() {
		where += " …"
	}
	// The machine above the directory: a pane says nothing about which
	// machine it is on otherwise, and with several panes open that is
	// the first thing to know about one.
	//
	// Written once, padded, rather than filled and written over: a cell
	// written twice in one frame is a cell that changed, and a window
	// that redraws a browser nobody is touching is what the whole
	// display is built to avoid.
	line(v, 0, cols, grid.TrimTail(p.fs.Name(), cols), p.Style.HeaderFG, p.Style.BG, grid.AttrBold)

	// The end of the path rather than the start: which directory this is
	// matters more than which disk it is on, and there is never room for
	// both.
	if rows > 1 {
		line(v, 1, cols, grid.TrimHead(where, cols), p.Style.PathFG, p.Style.BG, 0)
	}
	if p.err != nil && rows > errorRow {
		// The row says what happened, not why: a reason trimmed to the
		// width of a pane loses the part that matters.
		said := unreadable
		if p.OnError != nil {
			said = unreadableHint
		}
		line(v, errorRow, cols, grid.TrimTail(said, cols), p.Style.ErrorFG, p.Style.BG, 0)
	}
	if rows > p.head() {
		// The list fills what is under the head, and keeps its own copy
		// of what it drew, so an idle one writes nothing at all.
		p.list.Draw(v.Sub(0, p.head(), cols, rows-p.head()))
	}
}

// line writes one row and pads it, so every cell in the row is written
// exactly once.
func line(v grid.View, y, cols int, text string, fg, bg color.RGBA, attr grid.Attr) {
	at := v.SetString(0, y, text, fg, bg, attr)
	for x := at; x < cols; x++ {
		v.Set(x, y, grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
	}
}

// typeToFind moves the selection to the first name starting with what
// has been typed.
//
// What was typed stands until a pause, Escape, or a move of the
// selection by any other means: a run of letters is one search, and two
// searches a second apart are two.
func (p *Pane) typeToFind(r rune) {
	now := p.clock()
	if p.finding != "" && now.Sub(p.findingAt) > findPause {
		p.finding = ""
	}
	p.findingAt = now
	if found, ok := p.findFrom(p.finding + string(r)); ok {
		p.finding += string(r)
		p.list.Select(found)
		return
	}
	// Nothing starts with it. A letter that matches nothing is more
	// likely the start of another name than a typing mistake, so it is
	// tried on its own before it is thrown away.
	if found, ok := p.findFrom(string(r)); ok {
		p.finding = string(r)
		p.list.Select(found)
	}
}

// findLess takes the last letter back off what is being searched for.
func (p *Pane) findLess() {
	runes := []rune(p.finding)
	p.finding = string(runes[:len(runes)-1])
	p.findingAt = p.clock()
	if p.finding == "" {
		return
	}
	if found, ok := p.findFrom(p.finding); ok {
		p.list.Select(found)
	}
}

// clearFinding forgets what was being searched for.
func (p *Pane) clearFinding() { p.finding = "" }

// Finding is what has been typed to find a name, for the bar to show.
func (p *Pane) Finding() string { return p.finding }

// findFrom returns the key of the first entry whose name starts with
// what was typed, ignoring case the way a filename does on Windows.
func (p *Pane) findFrom(want string) (any, bool) {
	want = strings.ToLower(want)
	for _, e := range p.entries {
		if strings.HasPrefix(strings.ToLower(e.Name), want) {
			return e.Name, true
		}
	}
	return nil, false
}

// findPause is how long a name being typed stands before the next
// letter starts a new one.
const findPause = time.Second

// SetFocus passes the focus on to the listing, so the bar shows where
// the keys are going, and offers the reason a read failed while this
// pane did not have them.
func (p *Pane) SetFocus(on bool) {
	p.list.SetFocus(on)
	if on {
		p.tell()
	}
}

// Focused reports whether the pane has the keys.
func (p *Pane) Focused() bool { return p.list.Focused() }

// HandleKey takes the keys the pane knows and passes the rest on.
func (p *Pane) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind == input.Text && ev.NormalText && ev.Rune >= ' ' {
		// Typing moves to the name being typed, the way a file list
		// does everywhere else.
		p.typeToFind(ev.Rune)
		return true, nil
	}
	if ev.Mods != 0 {
		// Ctrl+Tab and the rest belong to whatever is around the pane.
		return p.list.HandleKey(ev)
	}
	if ev.Kind == input.KeyPress || ev.Kind == input.KeyRepeat {
		switch ev.Key {
		case input.KeyEscape:
			if p.finding != "" {
				p.clearFinding()
				return true, nil
			}
		case input.KeyBackspace:
			if p.finding != "" {
				// The typing comes apart before the pane goes up a
				// directory, so a mistyped letter is taken back rather
				// than losing the place entirely.
				p.findLess()
				return true, nil
			}
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
		if p.err != nil && ev.Row == errorRow && p.OnError != nil &&
			ev.Kind == input.MousePress && ev.Button == input.MouseLeft {
			p.OnError(p.errAt, p.err)
		}
		return true, nil
	}
	ev.Row -= head
	return p.list.HandleMouse(ev)
}

// FocusesFirst says a press that moves the keys to this pane moves the
// bar too. A click only points at a name, so there is nothing to lose
// by letting the first one do it, and a double click on a pane without
// the keys opens what it was aimed at.
func (p *Pane) FocusesFirst() bool { return false }
