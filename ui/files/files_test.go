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
