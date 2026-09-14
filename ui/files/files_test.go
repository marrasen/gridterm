package files

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// styled colours a pane so every part of it can be told apart in a test.
func styled() Style {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{A: 255}
	return Style{
		FG: white, BG: black,
		SelectedFG: black, SelectedBG: white,
		HeaderFG: white, DirFG: white, LinkFG: white, MarkedFG: white,
		NoteFG: white, ErrorFG: white,
	}
}

// here makes a pane on a directory of this machine, reading straight
// away rather than on another goroutine: a test has nothing to wait for
// that way, and the pane cannot tell the difference.
func here(t *testing.T, at string) *Pane {
	t.Helper()
	p := New(vfs.NewLocal())
	p.Style = styled()
	p.Read = func(f vfs.FS, path string, then func([]vfs.Entry, error)) {
		then(f.ReadDir(path))
	}
	p.Layout(ui.Size{Cols: 40, Rows: 12})
	p.Open(at)
	return p
}

// alone is a pane with the keys, for a test about one pane rather than
// about a browser: inside a browser it is the split that hands the focus
// to one of the two.
func alone(t *testing.T, at string) *Pane {
	t.Helper()
	p := here(t, at)
	p.SetFocus(true)
	return p
}

// write puts a file on this machine.
func write(t *testing.T, at, name, body string) {
	t.Helper()
	path := filepath.Join(at, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// names is what a pane is showing, in the order it shows it.
func names(p *Pane) []string {
	out := make([]string, 0, len(p.Entries()))
	for _, e := range p.Entries() {
		out = append(out, e.Name)
	}
	return out
}

// press sends one key to a widget that takes keys.
func press(t *testing.T, w ui.KeyHandler, key input.Key) {
	t.Helper()
	if _, err := w.HandleKey(input.Event{Kind: input.KeyPress, Key: key}); err != nil {
		t.Fatalf("key %v: %v", key, err)
	}
}

// drawn reads a pane back as lines of text.
func drawn(p *Pane, cols, rows int) []string {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	p.Layout(ui.Size{Cols: cols, Rows: rows})
	p.Draw(g.View())
	out := make([]string, rows)
	for y := 0; y < rows; y++ {
		var b strings.Builder
		for x := 0; x < cols; x++ {
			c := g.At(x, y)
			if c.Rune == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(c.Rune)
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// A pane shows what is in a directory, directories first.
func TestPaneShowsADirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")
	write(t, dir, "sub/two.txt", "two")

	p := alone(t, dir)
	if err := p.Err(); err != nil {
		t.Fatalf("reading it: %v", err)
	}
	got := names(p)
	if len(got) != 2 || got[0] != "sub" || got[1] != "one.txt" {
		t.Fatalf("it shows %v, want the directory first", got)
	}
}

// Enter on a directory goes into it, and Backspace comes back out,
// landing on the directory that was left.
func TestPaneGoesInAndOut(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub/two.txt", "two")
	write(t, dir, "one.txt", "one")

	p := alone(t, dir)
	// The bar opens on the way up, which is the safe place for it: a key
	// that acts on what is under the bar must not act on a name the user
	// has not looked at yet.
	press(t, p, input.KeyDown)
	press(t, p, input.KeyEnter)
	if got := vfs.Base(p.FS(), p.At()); got != "sub" {
		t.Fatalf("it is in %q, want sub", p.At())
	}
	if got := names(p); len(got) != 1 || got[0] != "two.txt" {
		t.Fatalf("it shows %v", got)
	}

	press(t, p, input.KeyBackspace)
	if p.At() != dir {
		t.Fatalf("it is in %q, want back where it started", p.At())
	}
	// The bar is on the directory it came out of, not at the top.
	if e, ok := p.Selected(); !ok || e.Name != "sub" {
		t.Fatalf("the bar is on %v, want the directory it came out of", e.Name)
	}
}

// The top of a filesystem has nothing above it, and says so by not
// offering the way up.
func TestPaneAtTheTopHasNoWayUp(t *testing.T) {
	dir := t.TempDir()
	p := alone(t, dir)
	if p.list.Rows()[0].Text != up {
		t.Fatal("a directory with somewhere above it does not offer the way up")
	}

	top := dir
	for !vfs.IsTop(p.FS(), top) {
		top = vfs.Dir(p.FS(), top)
	}
	p.Open(top)
	for _, row := range p.list.Rows() {
		if row.Text == up {
			t.Fatalf("the top of the filesystem offers a way up: %q", p.At())
		}
	}
	press(t, p, input.KeyBackspace)
	if p.At() != top {
		t.Fatalf("it went up from the top, to %q", p.At())
	}
}

// Marking picks names out and moves on, so a run of them is one key held
// down.
func TestPaneMarks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		write(t, dir, name, name)
	}

	p := alone(t, dir)
	press(t, p, input.KeyDown)
	press(t, p, input.KeySpace)
	press(t, p, input.KeySpace)
	got := p.Marked()
	if len(got) != 2 || got[0] != "a.txt" || got[1] != "b.txt" {
		t.Fatalf("it picked out %v", got)
	}

	// With nothing picked out, the bar itself is what a key acts on.
	p.ClearMarks()
	if got := p.Marked(); len(got) != 1 || got[0] != "c.txt" {
		t.Fatalf("with nothing marked it offers %v, want the row under the bar", got)
	}
}

// Moving to another directory forgets what was picked out: the marks
// were about what was in front of the user.
func TestPaneForgetsMarksWhenItMoves(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")
	write(t, dir, "sub/two.txt", "two")

	p := alone(t, dir)
	p.Mark("one.txt", true)
	p.Open(filepath.Join(dir, "sub"))
	press(t, p, input.KeyDown)
	if got := p.Marked(); len(got) != 1 || got[0] != "two.txt" {
		t.Fatalf("after moving it offers %v", got)
	}
}

// A directory that cannot be read leaves the last listing on screen and
// says why, rather than showing an empty directory.
func TestPaneKeepsWhatItHadWhenAReadFails(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")

	p := alone(t, dir)
	if got := names(p); len(got) != 1 {
		t.Fatalf("it shows %v", got)
	}

	// The next read fails.
	want := errors.New("the machine went away")
	p.Read = func(vfs.FS, string, func([]vfs.Entry, error)) {}
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) {
		then(nil, want)
	}
	p.Reload()

	if !errors.Is(p.Err(), want) {
		t.Fatalf("it says %v", p.Err())
	}
	if got := names(p); len(got) != 1 || got[0] != "one.txt" {
		t.Fatalf("it shows %v, want what it had", got)
	}
	// And it says so on screen.
	lines := drawn(p, 40, 8)
	if !strings.Contains(strings.Join(lines, "\n"), "went away") {
		t.Fatalf("the pane does not say why:\n%s", strings.Join(lines, "\n"))
	}
}

// An answer to a read the user has moved on from is thrown away: a slow
// directory must not land on top of the one they are looking at now.
func TestPaneIgnoresAnAnswerItNoLongerWants(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")
	write(t, dir, "sub/two.txt", "two")

	p := New(vfs.NewLocal())
	p.Style = styled()
	var answer func([]vfs.Entry, error)
	p.Read = func(f vfs.FS, path string, then func([]vfs.Entry, error)) {
		entries, err := f.ReadDir(path)
		// Held rather than answered, so the test decides when.
		answer = func([]vfs.Entry, error) { then(entries, err) }
	}
	p.Layout(ui.Size{Cols: 40, Rows: 12})

	p.Open(dir)
	slow := answer
	p.Open(filepath.Join(dir, "sub"))
	quick := answer

	// The one asked for second lands first, and then the slow one.
	quick(nil, nil)
	slow(nil, nil)

	if got := names(p); len(got) != 1 || got[0] != "two.txt" {
		t.Fatalf("it shows %v, want the directory it is in", got)
	}
}

// The order can be changed, and directories stay first whatever it is.
func TestPaneSorts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "small.txt", "x")
	write(t, dir, "big.txt", strings.Repeat("x", 100))
	write(t, dir, "sub/two.txt", "two")

	p := alone(t, dir)
	p.SetSort(BySize)
	got := names(p)
	if got[0] != "sub" {
		t.Fatalf("by size it shows %v, want the directory first", got)
	}
	if got[1] != "big.txt" {
		t.Fatalf("by size it shows %v, want the largest first", got)
	}

	p.SetSort(ByName)
	got = names(p)
	if got[1] != "big.txt" || got[2] != "small.txt" {
		t.Fatalf("by name it shows %v", got)
	}
}

// What a pane draws says where it is and what is in it.
func TestPaneDraws(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "hello")
	write(t, dir, "sub/two.txt", "two")

	p := alone(t, dir)
	lines := drawn(p, 40, 8)
	if !strings.HasSuffix(lines[0], vfs.Base(p.FS(), dir)) {
		t.Errorf("the first line is %q, want where it is", lines[0])
	}
	whole := strings.Join(lines, "\n")
	for _, want := range []string{up, "sub", "one.txt", "dir", "5 B"} {
		if !strings.Contains(whole, want) {
			t.Errorf("the pane does not show %q:\n%s", want, whole)
		}
	}
}

// A pane too small to draw in draws nothing rather than falling over.
func TestPaneInNoRoom(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")
	p := alone(t, dir)
	for _, size := range []ui.Size{{Cols: 0, Rows: 0}, {Cols: 1, Rows: 1}, {Cols: 3, Rows: 2}} {
		p.Layout(size)
		g := grid.New(max(size.Cols, 1), max(size.Rows, 1), color.RGBA{}, color.RGBA{})
		p.Draw(g.View().Sub(0, 0, size.Cols, size.Rows))
	}
}

// two makes a browser over two directories of this machine.
func two(t *testing.T) (*Browser, string, string) {
	t.Helper()
	left, right := t.TempDir(), t.TempDir()
	b := NewBrowser(here(t, left), here(t, right), nil)
	b.Layout(ui.Size{Cols: 80, Rows: 12})
	b.SetFocus(true)
	return b, left, right
}

// Tab moves the keys to the other pane, which is what decides where a
// copy goes.
func TestBrowserSwapsPanes(t *testing.T) {
	b, left, right := two(t)
	if b.Here().At() != left {
		t.Fatalf("it starts in %q, want the left pane", b.Here().At())
	}
	if _, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyTab}); err != nil {
		t.Fatalf("tab: %v", err)
	}
	if b.Here().At() != right {
		t.Fatalf("after tab it is in %q, want the right pane", b.Here().At())
	}
	if b.There().At() != left {
		t.Fatalf("the other pane is %q", b.There().At())
	}
}

// A copy goes from the pane with the keys to the other one, and says
// what was picked out.
func TestBrowserAsksForWork(t *testing.T) {
	b, left, right := two(t)
	write(t, left, "one.txt", "one")
	write(t, left, "two.txt", "two")
	b.Here().Reload()

	var asked Work
	var what string
	b.OnCopy = func(w Work) { asked, what = w, "copy" }
	b.OnMove = func(w Work) { asked, what = w, "move" }
	b.OnDelete = func(w Work) { asked, what = w, "delete" }

	press(t, b, input.KeyDown)
	press(t, b, input.KeySpace)
	press(t, b, input.KeyF5)

	if what != "copy" {
		t.Fatalf("F5 asked for %q", what)
	}
	if asked.From.At() != left || asked.To.At() != right {
		t.Fatalf("it copies from %q to %q", asked.From.At(), asked.To.At())
	}
	if len(asked.Names) != 1 || asked.Names[0] != "one.txt" {
		t.Fatalf("it copies %v", asked.Names)
	}

	press(t, b, input.KeyF6)
	if what != "move" || asked.To == nil {
		t.Fatalf("F6 asked for %q with To=%v", what, asked.To)
	}
	press(t, b, input.KeyF8)
	if what != "delete" {
		t.Fatalf("F8 asked for %q", what)
	}
	if asked.To != nil {
		t.Error("deleting was given somewhere to put it")
	}
}

// With nothing picked out and the bar on the way up, there is nothing to
// act on and nothing is asked for.
func TestBrowserAsksForNothingWhenNothingIsPickedOut(t *testing.T) {
	b, left, _ := two(t)
	write(t, left, "one.txt", "one")
	b.Here().Reload()

	asked := false
	b.OnCopy = func(Work) { asked = true }
	// The bar is on the way up, which is not a name to copy.
	press(t, b, input.KeyF5)
	if asked {
		t.Fatal("it asked to copy the way up")
	}
}

// Making a directory is about the pane rather than about what is picked
// out in it.
func TestBrowserMkdirIsAboutThePane(t *testing.T) {
	b, left, _ := two(t)
	var asked Work
	got := false
	b.OnMkdir = func(w Work) { asked, got = w, true }

	press(t, b, input.KeyF7)
	if !got {
		t.Fatal("F7 asked for nothing")
	}
	if asked.From.At() != left {
		t.Fatalf("it would make one in %q", asked.From.At())
	}
	if len(asked.Names) != 0 {
		t.Errorf("it named %v", asked.Names)
	}
}

// A key nothing is wired to does nothing, rather than falling over.
func TestBrowserWithNothingWiredUp(t *testing.T) {
	b, left, _ := two(t)
	write(t, left, "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)
	for _, key := range []input.Key{input.KeyF2, input.KeyF5, input.KeyF6, input.KeyF7, input.KeyF8} {
		press(t, b, key)
	}
}

// Both panes on one machine is one filesystem, which is what makes a
// move a rename.
func TestBrowserKnowsWhenBothPanesAreOneFilesystem(t *testing.T) {
	b, _, _ := two(t)
	if !b.SameFS() {
		t.Fatal("two panes on this machine are not the same filesystem")
	}
}

// The colours a pane is given reach the rows, which is what the list
// draws. Without that the names and the bar are painted in nothing at
// all, and the user cannot see where the keys are pointing.
func TestPaneColoursItsRows(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "hello")

	p := alone(t, dir)
	press(t, p, input.KeyDown)

	g := grid.New(30, 6, color.RGBA{}, color.RGBA{})
	p.Layout(ui.Size{Cols: 30, Rows: 6})
	p.Draw(g.View())

	// The row under the bar, which is the one every key acts on.
	var painted bool
	for y := 0; y < 6; y++ {
		if strings.Contains(rowText(g, y, 30), "one.txt") {
			c := g.At(1, y)
			if c.FG.A == 0 && c.BG.A == 0 {
				t.Fatalf("the selected row is drawn in nothing: fg %v bg %v", c.FG, c.BG)
			}
			painted = true
		}
	}
	if !painted {
		t.Fatal("the row was not drawn at all")
	}
}

// rowText reads one row of a grid back as a string.
func rowText(g *grid.Grid, y, cols int) string {
	var b strings.Builder
	for x := 0; x < cols; x++ {
		c := g.At(x, y)
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// A pane nobody is touching writes nothing. Filling the view and then
// writing over it changes every cell twice a frame, and a window that
// redraws a browser nobody is touching is what the whole display is
// built to avoid.
func TestAnIdlePaneDirtiesNothing(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		write(t, dir, name, name)
	}

	p := alone(t, dir)
	g := grid.New(30, 8, color.RGBA{}, color.RGBA{})
	p.Layout(ui.Size{Cols: 30, Rows: 8})

	p.Draw(g.View())
	g.ClearDirty()
	for i := 0; i < 5; i++ {
		p.Draw(g.View())
	}
	if g.AnyDirty() {
		var rows []int
		for y := 0; y < 8; y++ {
			if g.RowDirty(y) {
				rows = append(rows, y)
			}
		}
		t.Fatalf("an idle pane dirtied rows %v", rows)
	}
}

// The line saying why a read failed takes a row from the listing, and
// the listing has to be told: otherwise the bar walks off the bottom of
// what is on screen.
func TestTheErrorLineDoesNotPushTheBarOffScreen(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 12; i++ {
		write(t, dir, string(rune('a'+i))+".txt", "x")
	}

	p := alone(t, dir)
	p.Layout(ui.Size{Cols: 30, Rows: 8})
	// To the bottom of the listing.
	press(t, p, input.KeyEnd)
	last, ok := p.Selected()
	if !ok {
		t.Fatal("nothing is selected")
	}

	// Now a read fails, which costs a row.
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) {
		then(nil, errors.New("boom"))
	}
	p.Reload()

	lines := drawn(p, 30, 8)
	if !strings.Contains(strings.Join(lines, "\n"), last.Name) {
		t.Fatalf("the selected %q is not on screen:\n%s", last.Name, strings.Join(lines, "\n"))
	}
}

// A key with a modifier belongs to whatever is around the pane: Ctrl and
// Shift chords are bound elsewhere in the window.
func TestPaneLeavesChordsAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub/one.txt", "one")
	p := alone(t, dir)
	was := p.At()

	for _, key := range []input.Key{input.KeyBackspace, input.KeySpace, input.KeyInsert} {
		took, err := p.HandleKey(input.Event{
			Kind: input.KeyPress, Key: key, Mods: input.ModCtrl,
		})
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		if took {
			t.Errorf("the pane swallowed a Ctrl chord: %v", key)
		}
	}
	if p.At() != was {
		t.Fatalf("a Ctrl chord moved the pane to %q", p.At())
	}
}

// A key nothing is wired to travels on, so whatever it is bound to
// elsewhere still works.
func TestBrowserLeavesKeysItDoesNothingWith(t *testing.T) {
	b, left, _ := two(t)
	write(t, left, "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)

	for _, key := range []input.Key{input.KeyF2, input.KeyF5, input.KeyF6, input.KeyF7, input.KeyF8} {
		took, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: key})
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		if took {
			t.Errorf("the browser swallowed %v with nothing wired to it", key)
		}
	}

	// And with nothing picked out, a key that acts on names is not taken
	// either.
	b.OnCopy = func(Work) {}
	b.Here().ClearMarks()
	// The bar back on the way up, which is not a name to copy.
	press(t, b, input.KeyHome)
	took, _ := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyF5})
	if took {
		t.Error("the browser swallowed F5 with nothing to copy")
	}
}

// Which side a copy comes from is the same answer whether or not the
// browser is being looked at.
func TestWhichSideHasTheKeysSurvivesLosingThem(t *testing.T) {
	b, left, right := two(t)
	if _, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyTab}); err != nil {
		t.Fatalf("tab: %v", err)
	}
	if b.Here().At() != right {
		t.Fatalf("after tab it is in %q", b.Here().At())
	}

	// The keys go somewhere else entirely, the way they do when another
	// pane is focused.
	b.SetFocus(false)
	if got := b.Here().At(); got != right {
		t.Fatalf("with the keys elsewhere it says %q, want the side that had them", got)
	}
	if got := b.There().At(); got != left {
		t.Fatalf("the other side is %q", got)
	}
}

// Renaming asks about one name, because there is one answer to "what
// should it be called".
func TestRenamingIsAboutOneName(t *testing.T) {
	b, left, _ := two(t)
	for _, name := range []string{"a.txt", "b.txt"} {
		write(t, left, name, name)
	}
	b.Here().Reload()

	var asked Work
	b.OnRename = func(w Work) { asked = w }
	press(t, b, input.KeyDown)
	press(t, b, input.KeySpace)
	press(t, b, input.KeySpace)
	press(t, b, input.KeyF2)

	if len(asked.Names) != 1 {
		t.Fatalf("it asked to rename %v, want the one under the bar", asked.Names)
	}
}

// drawBrowser paints a browser and reads it back, one string per row.
func drawBrowser(b *Browser, cols, rows int) []string {
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	b.Layout(ui.Size{Cols: cols, Rows: rows})
	b.Draw(g.View())
	out := make([]string, rows)
	for y := 0; y < rows; y++ {
		out[y] = rowText(g, y, cols)
	}
	return out
}

// The browser says which key does what along the bottom, the way
// Midnight Commander does. Without it there is nothing on screen saying
// that F5 copies.
func TestBrowserSaysWhatTheKeysDo(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()
	rows := drawBrowser(b, 80, 12)

	bar := rows[len(rows)-1]
	for _, want := range []string{"Tab", "Next", "2", "Rename", "5", "Copy", "6", "Move", "7", "Mkdir", "8", "Delete"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the bar reads %q, missing %q", bar, want)
		}
	}
	// And it is the bottom row alone: the panes keep the rest.
	if strings.Contains(rows[len(rows)-2], "Rename") {
		t.Errorf("the row above the bar reads %q", rows[len(rows)-2])
	}
}

// The bar takes a row from the panes rather than being drawn over them.
func TestTheBarTakesARowFromThePanes(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()
	b.Layout(ui.Size{Cols: 80, Rows: 12})
	// A pane has a header row and the rest is its listing, so eleven
	// rows of browser is ten rows of pane.
	rows := drawBrowser(b, 80, 12)
	if got := strings.TrimSpace(rows[0]); got == "" {
		t.Fatal("the pane header is not drawn")
	}
	if !strings.Contains(rows[11], "Copy") {
		t.Fatalf("the bar is on row %q", rows[11])
	}
}

// Clicking a key on the bar does what pressing it does. The bar is the
// help, so it has to work as one.
func TestClickingTheBarRunsTheKey(t *testing.T) {
	b, left, right := two(t)
	b.Style = styled()
	write(t, left, "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)

	var asked []Work
	b.OnCopy = func(w Work) { asked = append(asked, w) }
	b.Layout(ui.Size{Cols: 60, Rows: 12})

	// The third of six keys is Copy, so its cell starts a third of the
	// way along.
	took, err := b.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 11, Col: 60*2/6 + 1,
	})
	if err != nil {
		t.Fatalf("the click failed: %v", err)
	}
	if !took {
		t.Fatal("the click on the bar travelled on")
	}
	if len(asked) != 1 {
		t.Fatalf("the bar asked for %d copies", len(asked))
	}
	if asked[0].From.At() != left || asked[0].To.At() != right {
		t.Fatalf("the copy goes from %q to %q", asked[0].From.At(), asked[0].To.At())
	}
}

// A click on the bar never reaches the panes underneath, whatever the
// key it landed on does.
func TestAClickOnTheBarStaysOnIt(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()
	b.Layout(ui.Size{Cols: 60, Rows: 12})
	was := b.Here().At()

	// Nothing is wired to Rename, and nothing is picked out either.
	took, err := b.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 11, Col: 12,
	})
	if err != nil || !took {
		t.Fatalf("took %v, err %v", took, err)
	}
	if b.Here().At() != was {
		t.Fatalf("the click moved the keys to %q", b.Here().At())
	}
}

// Every column of the bar belongs to the key drawn there, all the way
// to the right edge.
func TestEveryColumnOfTheBarIsTheKeyItShows(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()
	for _, cols := range []int{40, 60, 61, 80, 97} {
		rows := drawBrowser(b, cols, 12)
		bar := rows[11]
		var swapped int
		b.OnCopy = func(Work) {}
		for col := 0; col < cols; col++ {
			i, ok := keyAt(col, cols, len(b.keys))
			if !ok {
				t.Fatalf("column %d of %d belongs to no key", col, cols)
			}
			start, end := keyCell(i, cols, len(b.keys))
			if col < start || col >= end {
				t.Fatalf("column %d of %d says key %d, drawn at %d..%d", col, cols, i, start, end)
			}
			if b.keys[i].Key == input.KeyTab {
				swapped++
			}
		}
		if swapped == 0 {
			t.Fatalf("no column of %q is the swap key", bar)
		}
	}
}

// A browser with no room for both the panes and the bar keeps the
// panes: a bar over one row of names helps nobody.
func TestABrowserTooShortForTheBar(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()
	rows := drawBrowser(b, 60, 3)
	for _, row := range rows {
		if strings.Contains(row, "Copy") {
			t.Fatalf("the bar is drawn in a browser three rows high: %v", rows)
		}
	}
	// And a click where the bar would have been reaches the panes.
	var asked int
	b.OnCopy = func(Work) { asked++ }
	b.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 2, Col: 25,
	})
	if asked != 0 {
		t.Fatal("a click in a browser with no bar ran a key on it")
	}
}

// A browser nobody is touching writes nothing, bar and all.
//
// The narrow width is the one that matters: there the names on the bar
// are longer than the room each key has, and a title allowed to run into
// the next key's cell would be written over by it every frame. A cell
// written twice in one frame is a cell that changed.
func TestAnIdleBrowserDirtiesNothing(t *testing.T) {
	for _, cols := range []int{36, 40, 60} {
		b, left, _ := two(t)
		b.Style = styled()
		write(t, left, "a.txt", "a")
		b.Here().Reload()

		g := grid.New(cols, 12, color.RGBA{}, color.RGBA{})
		b.Layout(ui.Size{Cols: cols, Rows: 12})
		b.Draw(g.View())
		g.ClearDirty()
		for i := 0; i < 5; i++ {
			b.Draw(g.View())
		}
		if g.AnyDirty() {
			var dirty []int
			for y := 0; y < 12; y++ {
				if g.RowDirty(y) {
					dirty = append(dirty, y)
				}
			}
			t.Fatalf("an idle browser %d columns wide dirtied rows %v", cols, dirty)
		}
	}
}

// The panes know how much room they have, which is the browser's height
// less the bar. A pane told it has a row it cannot draw puts the bar on
// a name nobody can see.
func TestTheBarDoesNotHideTheNameTheKeysAreOn(t *testing.T) {
	b, left, _ := two(t)
	b.Style = styled()
	for i := 0; i < 30; i++ {
		write(t, left, fmt.Sprintf("file%02d.txt", i), "x")
	}
	b.Here().Reload()
	b.Layout(ui.Size{Cols: 60, Rows: 12})

	// All the way to the last name, which is where a pane that thinks
	// it is a row taller than it is runs off the bottom.
	press(t, b, input.KeyEnd)
	on, ok := b.Here().Selected()
	if !ok {
		t.Fatal("nothing is selected")
	}

	rows := drawBrowser(b, 60, 12)
	var shown bool
	for _, row := range rows[:11] {
		if strings.Contains(row, on.Name) {
			shown = true
		}
	}
	if !shown {
		t.Fatalf("the keys are on %q, which is not on screen: %v", on.Name, rows)
	}
}

// A key with nothing behind it is still shown, so the bar says the same
// thing wherever it is, but it is shown without being offered.
func TestTheBarShowsAKeyWithNothingBehindIt(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()
	b.OnCopy = func(Work) {}

	g := grid.New(60, 12, color.RGBA{}, color.RGBA{})
	b.Layout(ui.Size{Cols: 60, Rows: 12})
	b.Draw(g.View())

	// Copy is wired and Delete is not, so the two read differently.
	copyAt, _ := keyCell(2, 60, len(b.keys))
	deleteAt, _ := keyCell(5, 60, len(b.keys))
	if got := g.At(copyAt+1, 11).BG; got != styled().SelectedBG {
		t.Errorf("a wired key is drawn on %v, want it marked out", got)
	}
	if got := g.At(deleteAt+1, 11).BG; got == styled().SelectedBG {
		t.Error("a key with nothing behind it is drawn as though it does something")
	}
}

// many returns a browser with n panes, each on a directory of its own.
func many(t *testing.T, n int) (*Browser, []string) {
	t.Helper()
	var dirs []string
	var panes []*Pane
	for i := 0; i < n; i++ {
		at := t.TempDir()
		dirs = append(dirs, at)
		panes = append(panes, here(t, at))
	}
	b := NewBrowser(panes...)
	b.Style = styled()
	b.Layout(ui.Size{Cols: 90, Rows: 12})
	b.SetFocus(true)
	return b, dirs
}

// Tab walks every pane in turn and comes back round, so a browser with
// five panes in it is one the user can reach all of.
func TestBrowserTabsThroughEveryPane(t *testing.T) {
	b, dirs := many(t, 4)
	for i := 0; i < len(dirs)*2; i++ {
		want := dirs[i%len(dirs)]
		if got := b.Here().At(); got != want {
			t.Fatalf("step %d is in %q, want %q", i, got, want)
		}
		press(t, b, input.KeyTab)
	}
}

// A copy goes from the pane with the keys to the next one along, and
// the last pane sends to the first.
func TestACopyGoesToTheNextPane(t *testing.T) {
	b, dirs := many(t, 3)
	for _, at := range dirs {
		write(t, at, "one.txt", "one")
	}
	var asked []Work
	b.OnCopy = func(w Work) { asked = append(asked, w) }

	for range dirs {
		b.Here().Reload()
		press(t, b, input.KeyDown)
		b.Here().Mark("one.txt", true)
		press(t, b, input.KeyF5)
		press(t, b, input.KeyTab)
	}
	if len(asked) != len(dirs) {
		t.Fatalf("%d copies asked for, want one per pane", len(asked))
	}
	for i, w := range asked {
		if w.From.At() != dirs[i] {
			t.Errorf("copy %d comes from %q, want %q", i, w.From.At(), dirs[i])
		}
		if want := dirs[(i+1)%len(dirs)]; w.To.At() != want {
			t.Errorf("copy %d goes to %q, want %q", i, w.To.At(), want)
		}
	}
}

// One pane has nowhere to send anything, so the keys that copy and move
// do nothing and say so on the bar.
func TestOnePaneHasNowhereToCopyTo(t *testing.T) {
	b, _ := many(t, 1)
	write(t, b.Here().At(), "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)
	b.Here().Mark("one.txt", true)

	var asked int
	b.OnCopy = func(Work) { asked++ }
	took, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyF5})
	if err != nil {
		t.Fatalf("F5: %v", err)
	}
	if took {
		t.Error("the one pane swallowed the copy key")
	}
	if asked != 0 {
		t.Fatal("a copy was asked for with nowhere to put it")
	}
	if b.wired(input.KeyF5) {
		t.Error("the bar offers a copy with one pane open")
	}
	// And Tab is not taken either: there is nowhere to go.
	if took, _ := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyTab}); took {
		t.Error("the one pane swallowed Tab")
	}
}

// Every column of the browser belongs to the pane drawn there, with a
// divider in the column between two of them.
func TestEveryColumnBelongsToAPane(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5} {
		b, _ := many(t, n)
		for _, cols := range []int{40, 60, 81, 90} {
			b.Layout(ui.Size{Cols: cols, Rows: 12})
			seen := make([]bool, cols)
			for i := range b.Panes() {
				start, end := b.paneCell(i, cols)
				if i > 0 && seen[start-1] {
					t.Fatalf("%d panes in %d columns: no divider before pane %d", n, cols, i)
				}
				for x := start; x < end; x++ {
					if seen[x] {
						t.Fatalf("%d panes in %d columns: column %d is in two panes", n, cols, x)
					}
					seen[x] = true
				}
			}
			// Everything but the divider columns is somebody's.
			var missing int
			for _, taken := range seen {
				if !taken {
					missing++
				}
			}
			if missing != n-1 {
				t.Fatalf("%d panes in %d columns leave %d columns spare, want %d dividers",
					n, cols, missing, n-1)
			}
		}
	}
}

// The dividers are drawn where the sharing left room for them.
func TestTheDividersAreDrawn(t *testing.T) {
	b, _ := many(t, 3)
	g := grid.New(90, 12, color.RGBA{}, color.RGBA{})
	b.Layout(ui.Size{Cols: 90, Rows: 12})
	b.Draw(g.View())
	for i := 1; i < 3; i++ {
		start, _ := b.paneCell(i, 90)
		for y := 0; y < 11; y++ {
			if got := g.At(start-1, y).Rune; got != divider {
				t.Fatalf("row %d of the column before pane %d holds %q", y, i, got)
			}
		}
	}
}

// A click puts the keys on the pane it landed in, which is the other way
// of choosing where a copy comes from.
func TestClickingAPaneTakesTheKeys(t *testing.T) {
	b, dirs := many(t, 3)
	b.Layout(ui.Size{Cols: 90, Rows: 12})
	start, _ := b.paneCell(2, 90)

	if _, err := b.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: start + 1, Row: 3,
	}); err != nil {
		t.Fatalf("the click failed: %v", err)
	}
	if got := b.Here().At(); got != dirs[2] {
		t.Fatalf("the keys are in %q, want the pane that was clicked", got)
	}
}

// Taking a pane out leaves the browser standing while it has one left,
// and says it is finished when it has none.
func TestRemovingPanes(t *testing.T) {
	b, _ := many(t, 3)
	panes := b.Panes()

	stands, ok := b.Remove(panes[0])
	if !ok || stands != ui.Widget(b) {
		t.Fatalf("Remove = %v, %v, want the browser to carry on", stands, ok)
	}
	stands, ok = b.Remove(panes[1])
	if !ok || stands != ui.Widget(b) {
		t.Fatalf("with one pane left Remove = %v, %v, want the browser", stands, ok)
	}
	// The keys did not go with them.
	if got := b.Here(); got != panes[2] {
		t.Fatalf("the keys are on %v, want the pane still open", got)
	}
	if !panes[2].Focused() {
		t.Error("the pane left does not know it has the keys")
	}

	stands, ok = b.Remove(panes[2])
	if !ok || stands != nil {
		t.Fatalf("with nothing left Remove = %v, %v, want nothing", stands, ok)
	}
	if b.Here() != nil {
		t.Error("an empty browser says it has a pane")
	}
	if _, ok := b.Remove(panes[0]); ok {
		t.Error("Remove accepted a pane that is not in the browser")
	}
}

// A browser with several panes nobody is touching writes nothing.
func TestAnIdleBrowserOfManyPanesDirtiesNothing(t *testing.T) {
	b, dirs := many(t, 4)
	for _, at := range dirs {
		write(t, at, "a.txt", "a")
	}
	b.Reload()

	g := grid.New(90, 12, color.RGBA{}, color.RGBA{})
	b.Layout(ui.Size{Cols: 90, Rows: 12})
	b.Draw(g.View())
	g.ClearDirty()
	for i := 0; i < 5; i++ {
		b.Draw(g.View())
	}
	if g.AnyDirty() {
		var dirty []int
		for y := 0; y < 12; y++ {
			if g.RowDirty(y) {
				dirty = append(dirty, y)
			}
		}
		t.Fatalf("an idle browser dirtied rows %v", dirty)
	}
}

// ChildArea says where a pane is, and it is the area Layout used: a
// container that works it out twice has two chances to disagree with
// itself.
func TestBrowserChildArea(t *testing.T) {
	b, _ := many(t, 3)
	b.Layout(ui.Size{Cols: 90, Rows: 12})
	for i, p := range b.Panes() {
		area, ok := b.ChildArea(p)
		if !ok {
			t.Fatalf("pane %d has no area", i)
		}
		start, end := b.paneCell(i, 90)
		want := ui.Rect{X: start, Cols: end - start, Rows: 11}
		if area != want {
			t.Fatalf("pane %d is at %+v, want %+v", i, area, want)
		}
	}
	if _, ok := b.ChildArea(here(t, t.TempDir())); ok {
		t.Error("a pane that is not in the browser has an area in it")
	}
}
