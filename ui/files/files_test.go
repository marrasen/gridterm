package files

import (
	"errors"
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
