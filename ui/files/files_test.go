package files

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
		HeaderFG: white, PathFG: white, DirFG: white, LinkFG: white,
		MarkedFG: white, ClipFG: white,
		NoteFG:  color.RGBA{R: 90, G: 90, B: 90, A: 255},
		ErrorFG: white,
		// The bar: a key is read on the pane's own ground, and one with
		// nothing behind it on a ground between the two. Told apart from
		// NoteFG, or a test could not say which one the bar used.
		KeyFG: white, OffBG: color.RGBA{R: 60, G: 60, B: 60, A: 255},
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
	for y := range rows {
		var b strings.Builder
		for x := range cols {
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
	// And it says so on screen, short: the reason itself is behind the
	// row rather than trimmed into it.
	lines := drawn(p, 40, 8)
	if !strings.Contains(strings.Join(lines, "\n"), unreadable) {
		t.Fatalf("the pane does not say the read failed:\n%s", strings.Join(lines, "\n"))
	}
	// With nobody wired up to show the reason, the row does not offer it:
	// a click that did nothing would be worse than no offer at all.
	if strings.Contains(strings.Join(lines, "\n"), "click to see why") {
		t.Errorf("the row offers a reason nothing can show:\n%s", strings.Join(lines, "\n"))
	}
}

// A move that fails leaves the pane where it was, so the next name
// opened is joined onto the directory on screen.
func TestAFailedMoveLeavesThePaneWhereItWas(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub/two.txt", "two")

	p := alone(t, dir)
	p.Open(filepath.Join(dir, "nowhere-at-all"))

	if p.At() != dir {
		t.Fatalf("the pane says it is in %q, want %q", p.At(), dir)
	}
	if got := names(p); len(got) != 1 || got[0] != "sub" {
		t.Fatalf("it shows %v, want what it had", got)
	}
	// Opening a row is where the wrong directory used to show up: the
	// name was joined onto the path that failed.
	press(t, p, input.KeyDown)
	press(t, p, input.KeyEnter)
	if want := filepath.Join(dir, "sub"); p.At() != want {
		t.Fatalf("opening a row went to %q, want %q", p.At(), want)
	}
}

// The reason names the directory that could not be read, which is not
// where the pane is once a move has failed.
func TestTheReasonNamesTheDirectoryThatFailed(t *testing.T) {
	dir := t.TempDir()
	p := alone(t, dir)
	var at string
	p.OnError = func(path string, _ error) { at = path }

	nowhere := filepath.Join(dir, "nowhere-at-all")
	p.Open(nowhere)
	if at != nowhere {
		t.Fatalf("the reason was about %q, want %q", at, nowhere)
	}
}

// A move that fails keeps the marks. They are about what is in front of
// the user, and that has not changed.
func TestAFailedMoveKeepsTheMarks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")

	p := alone(t, dir)
	p.Mark("one.txt", true)
	p.Open(filepath.Join(dir, "nowhere-at-all"))

	if got := p.Marked(); len(got) != 1 || got[0] != "one.txt" {
		t.Fatalf("after a move that failed it offers %v", got)
	}
	// Marked falls back to the name under the bar when nothing is picked
	// out, so the mark itself is what is checked: it is drawn in front of
	// the name.
	if got := strings.Join(drawn(p, 40, 8), "\n"); !strings.Contains(got, "*one.txt") {
		t.Fatalf("the mark is not on screen:\n%s", got)
	}
}

// A reload that lands while a move is waiting is left out, so whoever
// asked for the move is not answered with the directory being left.
//
// A file job finishing reloads every pane on its filesystem, and it does
// that whatever else is going on.
func TestAReloadDoesNotCutInOnAMoveThatIsWaiting(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub/two.txt", "two")

	p := alone(t, dir)
	// Reads are held here rather than answered, which is how a move on a
	// slow machine sits while something else happens.
	var held []func([]vfs.Entry, error)
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) {
		held = append(held, then)
	}

	var answered []error
	p.OpenThen(filepath.Join(dir, "sub"), func(err error) {
		answered = append(answered, err)
	})
	p.Reload()
	if len(held) != 1 {
		t.Fatalf("%d reads went out, want the move on its own", len(held))
	}

	// The move's own answer is the one that reaches it.
	held[0](nil, nil)
	if len(answered) != 1 || answered[0] != nil {
		t.Fatalf("the move was answered %v, want once and with nothing wrong", answered)
	}
	if want := filepath.Join(dir, "sub"); p.At() != want {
		t.Fatalf("the pane is in %q, want %q", p.At(), want)
	}
	// And a reload is welcome again once the move has landed.
	p.Reload()
	if len(held) != 2 {
		t.Fatalf("%d reads went out in all, want the reload as well", len(held))
	}
}

// A pane on its way somewhere says where it is going, so a slow
// directory says what is being waited for.
func TestAPaneSaysWhereItIsGoing(t *testing.T) {
	dir := t.TempDir()
	p := alone(t, dir)
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) {}

	sub := filepath.Join(dir, "sub")
	p.Open(sub)
	if got := strings.Join(drawn(p, 140, 8), "\n"); !strings.Contains(got, sub+" …") {
		t.Fatalf("the pane does not say it is going to %q:\n%s", sub, got)
	}
}

// A pane whose very first read fails still has a directory to read
// again: it had nothing to keep, so it moved.
func TestAPaneWhoseFirstReadFailsCanTryAgain(t *testing.T) {
	dir := t.TempDir()
	want := errors.New("the machine went away")
	reads := 0

	p := New(vfs.NewLocal())
	p.Style = styled()
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) {
		reads++
		then(nil, want)
	}
	p.Layout(ui.Size{Cols: 40, Rows: 12})

	p.Open(dir)
	if p.At() != dir {
		t.Fatalf("the pane is in %q, want %q", p.At(), dir)
	}
	p.Reload()
	if reads != 2 {
		t.Fatalf("it read %d times, want the reload to have gone out too", reads)
	}
}

// The reason is shown at once in the pane the user is working in, and it
// is shown once: they asked for that directory and are waiting for it.
func TestPaneWithTheKeysShowsWhyAReadFailedStraightAway(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")

	p := alone(t, dir)
	var shown []error
	p.OnError = func(_ string, err error) { shown = append(shown, err) }

	want := errors.New("the machine went away")
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) { then(nil, want) }
	p.Reload()

	if len(shown) != 1 || !errors.Is(shown[0], want) {
		t.Fatalf("the pane showed %v, want the reason once", shown)
	}
	// And the row now offers it again.
	lines := drawn(p, 40, 8)
	if !strings.Contains(strings.Join(lines, "\n"), unreadableHint) {
		t.Errorf("the row does not offer the reason:\n%s", strings.Join(lines, "\n"))
	}
}

// A new failure is a new reason, and it is shown even though the last
// one already was.
func TestPaneShowsASecondFailureToo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")

	p := alone(t, dir)
	var shown []error
	p.OnError = func(_ string, err error) { shown = append(shown, err) }

	first := errors.New("the machine went away")
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) { then(nil, first) }
	p.Reload()
	second := errors.New("and it is still away")
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) { then(nil, second) }
	p.Reload()

	if len(shown) != 2 || !errors.Is(shown[1], second) {
		t.Fatalf("the pane showed %v, want both reasons in turn", shown)
	}
}

// A pane without the keys waits: a dialog on top of what somebody is
// doing in the other pane is the window getting in their way.
func TestPaneWithoutTheKeysWaitsForThemToShowWhyAReadFailed(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")

	p := here(t, dir)
	var shown []error
	p.OnError = func(_ string, err error) { shown = append(shown, err) }

	want := errors.New("the machine went away")
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) { then(nil, want) }
	p.Reload()

	if len(shown) != 0 {
		t.Fatalf("a pane nobody is looking at showed %v", shown)
	}

	p.SetFocus(true)
	if len(shown) != 1 || !errors.Is(shown[0], want) {
		t.Fatalf("taking the keys showed %v, want the reason once", shown)
	}
	// Going away and coming back is not another failure.
	p.SetFocus(false)
	p.SetFocus(true)
	if len(shown) != 1 {
		t.Errorf("the reason was shown %d times, want once for one failure", len(shown))
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
	// The machine on top, then the directory under it.
	if !strings.HasPrefix(lines[0], p.FS().Name()) {
		t.Errorf("the first line is %q, want the machine", lines[0])
	}
	if !strings.HasSuffix(strings.TrimRight(lines[1], " "), vfs.Base(p.FS(), dir)) {
		t.Errorf("the second line is %q, want where it is", lines[1])
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
	b := NewBrowser(here(t, left), here(t, right))
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

	// Copy picks the name out and asks for nothing yet: where it goes is
	// decided by where the user is when they paste.
	press(t, b, input.KeyF5)
	if what != "" {
		t.Fatalf("copy asked for %q before anything was pasted", what)
	}
	press(t, b, input.KeyTab)
	press(t, b, input.KeyF7)
	if what != "copy" {
		t.Fatalf("paste asked for %q", what)
	}
	if asked.At != left || asked.To.At() != right {
		t.Fatalf("it copies from %q to %q", asked.At, asked.To.At())
	}
	if len(asked.Names) != 1 || asked.Names[0] != "one.txt" {
		t.Fatalf("it copies %v", asked.Names)
	}

	// Cut is the same, and a move when it lands.
	press(t, b, input.KeyTab)
	press(t, b, input.KeyDown)
	press(t, b, input.KeyF6)
	press(t, b, input.KeyTab)
	press(t, b, input.KeyF7)
	if what != "move" || asked.To == nil {
		t.Fatalf("pasting a cut asked for %q with To=%v", what, asked.To)
	}

	press(t, b, input.KeyTab)
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
	press(t, b, input.KeyTab)
	press(t, b, input.KeyF7)
	if asked {
		t.Fatal("it asked to copy the way up")
	}
	if !b.Clip().Empty() {
		t.Fatalf("the way up went on the clipboard: %v", b.Clip().Names)
	}
}

// Making a directory is about the pane rather than about what is picked
// out in it.
func TestBrowserMkdirIsAboutThePane(t *testing.T) {
	b, left, _ := two(t)
	var asked Work
	got := false
	b.OnMkdir = func(w Work) { asked, got = w, true }

	press(t, b, input.KeyF9)
	if !got {
		t.Fatal("F9 asked for nothing")
	}
	if asked.At != left {
		t.Fatalf("it would make one in %q", asked.At)
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
	for y := range 6 {
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
	for x := range cols {
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
	for range 5 {
		p.Draw(g.View())
	}
	if g.AnyDirty() {
		var rows []int
		for y := range 8 {
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
	for i := range 12 {
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

	// Copy and cut are left out: they fill the browser's own clipboard,
	// so they do something whether or not anything is wired behind them.
	for _, key := range []input.Key{
		input.KeyF2, input.KeyF7, input.KeyF8, input.KeyF9, input.KeyF10,
	} {
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
	for y := range rows {
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
	// Wide enough for every name. The bar divides what it has between
	// the keys, so a narrow one cuts the names -- see the test below.
	rows := drawBrowser(b, 120, 12)

	bar := rows[len(rows)-1]
	for _, want := range []string{
		"Tab", "Next", "F2", "Rename", "F3", "View", "F4", "Tail",
		"^C", "Copy", "^X", "Cut",
		"^V", "Paste", "F8", "Delete", "F9", "Mkdir", "^D", "Close",
	} {
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

	// Copy, then the next pane, then paste: three clicks on the bar, the
	// same three keys.
	clickKey := func(shown string) {
		t.Helper()
		i := -1
		for at, key := range b.keys {
			if key.Shown == shown {
				i = at
			}
		}
		if i < 0 {
			t.Fatalf("%q is not on the bar", shown)
		}
		start, _ := keyCell(i, 60, len(b.keys))
		took, err := b.HandleMouse(input.MouseEvent{
			Kind: input.MousePress, Button: input.MouseLeft, Row: 11, Col: start,
		})
		if err != nil {
			t.Fatalf("the click failed: %v", err)
		}
		if !took {
			t.Fatal("the click on the bar travelled on")
		}
	}
	clickKey("^C")
	clickKey("Tab")
	clickKey("^V")

	if len(asked) != 1 {
		t.Fatalf("the bar asked for %d copies", len(asked))
	}
	if asked[0].At != left || asked[0].To.At() != right {
		t.Fatalf("the copy goes from %q to %q", asked[0].At, asked[0].To.At())
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

// wiredKey reports whether the bar offers the key it spells this way.
func wiredKey(b *Browser, shown string) bool {
	for _, k := range b.keys {
		if k.Shown == shown {
			return b.wired(k)
		}
	}
	return false
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
		for col := range cols {
			i, ok := keyAt(col, cols, len(b.keys))
			if !ok {
				t.Fatalf("column %d of %d belongs to no key", col, cols)
			}
			start, end := keyCell(i, cols, len(b.keys))
			if col < start || col >= end {
				t.Fatalf("column %d of %d says key %d, drawn at %d..%d", col, cols, i, start, end)
			}
			if b.keys[i].Chord.Key == input.KeyTab {
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
	mouseTo(t, b, input.MouseEvent{
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
		for range 5 {
			b.Draw(g.View())
		}
		if g.AnyDirty() {
			var dirty []int
			for y := range 12 {
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
	for i := range 30 {
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

// barKey is where a key sits on the bar, found by the way the bar spells
// it, so a test does not have to count the keys before it.
func barKey(t *testing.T, b *Browser, shown string) int {
	t.Helper()
	for i, k := range b.keys {
		if k.Shown == shown {
			return i
		}
	}
	t.Fatalf("the bar has no %s: %v", shown, b.keys)
	return -1
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

	// Copy is wired and Delete is not, so the two read differently. The
	// one with nothing behind it is still lit enough to read: dimmer
	// than a working key and brighter than the bar's own ground.
	copyAt, _ := keyCell(barKey(t, b, "^C"), 60, len(b.keys))
	deleteAt, _ := keyCell(barKey(t, b, "F8"), 60, len(b.keys))
	if got := g.At(copyAt+2, 11).BG; got != styled().SelectedBG {
		t.Errorf("a wired key is drawn on %v, want it marked out", got)
	}
	off := g.At(deleteAt+2, 11).BG
	if off == styled().SelectedBG {
		t.Error("a key with nothing behind it is drawn as though it does something")
	}
	if off == styled().BG {
		t.Error("a key with nothing behind it is drawn on the bar's own ground")
	}
	if off != styled().OffBG {
		t.Errorf("a key with nothing behind it is drawn on %v", off)
	}
}

// many returns a browser with n panes, each on a directory of its own.
func many(t *testing.T, n int) (*Browser, []string) {
	t.Helper()
	var dirs []string
	var panes []*Pane
	for range n {
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
		press(t, b, input.KeyF7)
	}
	if len(asked) != len(dirs) {
		t.Fatalf("%d copies asked for, want one per pane", len(asked))
	}
	for i, w := range asked {
		if w.At != dirs[i] {
			t.Errorf("copy %d comes from %q, want %q", i, w.At, dirs[i])
		}
		if want := dirs[(i+1)%len(dirs)]; w.To.At() != want {
			t.Errorf("copy %d goes to %q, want %q", i, w.To.At(), want)
		}
	}
}

// One pane has nowhere else to go, so Tab does nothing. Copying still
// works: the names go on the clipboard, and a pane opened later is
// somewhere to paste them.
func TestOnePaneHasNowhereToTabTo(t *testing.T) {
	b, _ := many(t, 1)
	write(t, b.Here().At(), "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)
	b.Here().Mark("one.txt", true)

	if took, _ := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyTab}); took {
		t.Error("the one pane swallowed Tab")
	}
	if wiredKey(b, "Tab") {
		t.Error("the bar offers the next pane with one pane open")
	}

	var asked int
	b.OnCopy = func(Work) { asked++ }
	press(t, b, input.KeyF5)
	if b.Clip().Empty() {
		t.Fatal("copy put nothing on the clipboard")
	}
	if asked != 0 {
		t.Fatal("a copy was asked for before anything was pasted")
	}
	// And into the same pane, which is what "duplicate here" is.
	press(t, b, input.KeyF7)
	if asked != 1 {
		t.Fatalf("pasting asked for %d copies", asked)
	}
}

// Every column of the browser belongs to the pane drawn there, with a
// divider in the column between two of them.
//
// Read off what was drawn rather than out of paneCell: a test that asks
// the arithmetic what the arithmetic says agrees with it however wrong
// it is. The panes are each on a directory with one file whose name
// says which pane it is, so a column can be traced back to its pane.
func TestEveryColumnBelongsToAPane(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5, 7} {
		b, dirs := many(t, n)
		for i, at := range dirs {
			write(t, at, fmt.Sprintf("pane%d", i), "x")
		}
		b.Reload()

		for _, cols := range []int{20, 40, 41, 60, 81, 90, 121} {
			g := grid.New(cols, 12, color.RGBA{}, color.RGBA{})
			b.Layout(ui.Size{Cols: cols, Rows: 12})
			b.Draw(g.View())

			// Row 1 is the first name in each listing, which is "..".
			// Row 0 is the path, and the divider runs down both.
			var dividers int
			for x := range cols {
				if g.At(x, 0).Rune == divider {
					dividers++
					if got := g.At(x, 1).Rune; got != divider {
						t.Fatalf("%d panes in %d columns: the divider at %d is one row deep, not %q",
							n, cols, x, got)
					}
				}
			}
			if dividers != n-1 {
				t.Fatalf("%d panes in %d columns drew %d dividers, want %d",
					n, cols, dividers, n-1)
			}
			// And nothing is left blank: every other column was written
			// by the pane it belongs to.
			for x := range cols {
				if g.At(x, 0).Rune == 0 {
					t.Fatalf("%d panes in %d columns: column %d of the top row was never written",
						n, cols, x)
				}
			}
		}
	}
}

// The dividers stand between the panes rather than inside one, which is
// what the column each pane is laid out with has to leave room for.
func TestTheDividersStandBetweenThePanes(t *testing.T) {
	b, _ := many(t, 3)
	g := grid.New(90, 12, color.RGBA{}, color.RGBA{})
	b.Layout(ui.Size{Cols: 90, Rows: 12})
	b.Draw(g.View())

	var at []int
	for x := range 90 {
		if g.At(x, 0).Rune == divider {
			at = append(at, x)
		}
	}
	if len(at) != 2 {
		t.Fatalf("%d dividers drawn, want one between each pair", len(at))
	}
	// A pane on each side of each of them, each as wide as it was laid
	// out for: the widths and the rules have to add up to the box.
	widths := []int{at[0], at[1] - at[0] - 1, 90 - at[1] - 1}
	for i, p := range b.Panes() {
		if got := p.Size().Cols; got != widths[i] {
			t.Fatalf("pane %d was laid out %d columns wide, and %d are drawn for it",
				i, got, widths[i])
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
	for range 5 {
		b.Draw(g.View())
	}
	if g.AnyDirty() {
		var dirty []int
		for y := range 12 {
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
	at := 0
	for i, p := range b.Panes() {
		area, ok := b.ChildArea(p)
		if !ok {
			t.Fatalf("pane %d has no area", i)
		}
		// The size Layout gave the pane, asked of the pane: a container
		// that works its answer out twice has two chances to disagree
		// with itself.
		if got := p.Size(); area.Size() != got {
			t.Fatalf("pane %d is laid out %+v and reported at %+v", i, got, area)
		}
		if area.X != at {
			t.Fatalf("pane %d starts at %d, want %d", i, area.X, at)
		}
		// The next one starts a divider along from the end of this.
		at = area.X + area.Cols + 1
	}
	if _, ok := b.ChildArea(here(t, t.TempDir())); ok {
		t.Error("a pane that is not in the browser has an area in it")
	}
}

// focusedPanes returns which panes believe they have the keys.
func focusedPanes(b *Browser) []int {
	var on []int
	for i, p := range b.Panes() {
		if p.Focused() {
			on = append(on, i)
		}
	}
	return on
}

// The first pane put in a browser that already has the keys is told it
// has them.
//
// It is how the file manager opens: the empty manager goes into the tree
// and is given the keys, and the pane arrives after. A pane that is not
// told draws no selected row, so the whole thing looks dead.
func TestTheFirstPaneOfABrowserWithTheKeysIsToldSo(t *testing.T) {
	b := NewBrowser()
	b.Style = styled()
	b.SetFocus(true)
	b.Layout(ui.Size{Cols: 40, Rows: 12})

	p := here(t, t.TempDir())
	b.Add(p)
	if b.Here() != p {
		t.Fatalf("the keys are on %v, want the pane just added", b.Here())
	}
	if !p.Focused() {
		t.Fatal("the pane does not know it has the keys, so it draws no selected row")
	}
}

// Taking away a pane to the left of the one with the keys leaves the
// keys where they were.
//
// The panes after it shift along, so a browser holding a position rather
// than a pane would move the keys to the pane next door -- and leave the
// one that had them still believing it did.
func TestRemovingAPaneLeftOfTheKeysLeavesThemAlone(t *testing.T) {
	b, dirs := many(t, 4)
	b.Focus(b.Panes()[2])
	was := b.Here()
	if was.At() != dirs[2] {
		t.Fatalf("the keys are in %q, want the third pane", was.At())
	}

	if _, ok := b.Remove(b.Panes()[1]); !ok {
		t.Fatal("Remove refused")
	}
	if b.Here() != was {
		t.Fatalf("the keys moved to %q, want %q", b.Here().At(), was.At())
	}
	if got := focusedPanes(b); len(got) != 1 {
		t.Fatalf("panes %v believe they have the keys, want exactly one", got)
	}
	if !was.Focused() {
		t.Fatal("the pane with the keys does not know it")
	}
}

// And taking away the one with the keys hands them to whichever pane
// takes its place.
func TestRemovingThePaneWithTheKeysHandsThemOn(t *testing.T) {
	b, dirs := many(t, 3)
	b.Focus(b.Panes()[1])

	if _, ok := b.Remove(b.Panes()[1]); !ok {
		t.Fatal("Remove refused")
	}
	if got := b.Here().At(); got != dirs[2] {
		t.Fatalf("the keys went to %q, want the pane that took its place", got)
	}
	if got := focusedPanes(b); len(got) != 1 || got[0] != 1 {
		t.Fatalf("panes %v believe they have the keys", got)
	}

	// And the last pane hands them to the one before it.
	b.Focus(b.Panes()[1])
	if _, ok := b.Remove(b.Panes()[1]); !ok {
		t.Fatal("Remove refused")
	}
	if got := b.Here().At(); got != dirs[0] {
		t.Fatalf("the keys went to %q, want the pane before it", got)
	}
	if got := focusedPanes(b); len(got) != 1 {
		t.Fatalf("panes %v believe they have the keys", got)
	}
}

// A browser nobody is touching writes nothing, at every shape it can be.
//
// The one geometry the other idle test uses says nothing about the
// widths where the panes divide unevenly, and the panes are what the
// user adds and removes.
func TestNoShapeOfBrowserDirtiesAnIdleFrame(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5, 7} {
		b, dirs := many(t, n)
		for i, at := range dirs {
			write(t, at, fmt.Sprintf("pane%d", i), "x")
		}
		b.Reload()
		for _, cols := range []int{9, 13, 20, 37, 40, 41, 63, 80, 81, 90, 121, 200} {
			for _, rows := range []int{3, 4, 5, 12, 24} {
				g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
				b.Layout(ui.Size{Cols: cols, Rows: rows})
				b.Draw(g.View())
				g.ClearDirty()
				b.Draw(g.View())
				b.Draw(g.View())
				if g.AnyDirty() {
					t.Fatalf("%d panes in %dx%d dirtied an idle frame", n, cols, rows)
				}
			}
		}
	}
}

// A pane already in the browser is refused, and so is nothing at all.
//
// The same pane twice would give Remove two answers and leave the
// browser holding a ghost, and a caller that has put a row on a sidebar
// for it has to hear that it is not there.
func TestAddRefusesAPaneTwice(t *testing.T) {
	b, _ := many(t, 2)
	p := b.Panes()[0]

	if b.Add(p) {
		t.Fatal("Add took a pane it already had")
	}
	if got := len(b.Panes()); got != 2 {
		t.Fatalf("the browser holds %d panes", got)
	}
	if b.Add(nil) {
		t.Fatal("Add took nothing")
	}
	if got := len(b.Panes()); got != 2 {
		t.Fatalf("the browser holds %d panes after being given nothing", got)
	}
	// And one it has never seen goes in.
	if !b.Add(here(t, t.TempDir())) {
		t.Fatal("Add refused a new pane")
	}
}

// A copy stays on the clipboard, so it can be pasted into one pane
// after another.
func TestACopyCanBePastedSeveralTimes(t *testing.T) {
	b, dirs := many(t, 3)
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)

	var to []string
	b.OnCopy = func(w Work) { to = append(to, w.To.At()) }
	b.OnMove = func(Work) { t.Error("a copy asked for a move") }

	press(t, b, input.KeyF5)
	for i := 1; i < 3; i++ {
		press(t, b, input.KeyTab)
		press(t, b, input.KeyF7)
	}
	if len(to) != 2 || to[0] != dirs[1] || to[1] != dirs[2] {
		t.Fatalf("it pasted into %v, want %v and %v", to, dirs[1], dirs[2])
	}
	if b.Clip().Empty() {
		t.Fatal("the copy came off the clipboard after being pasted")
	}
}

// A cut can be pasted once. After that the names are somewhere else, so
// there is nothing left to paste.
func TestACutIsPastedOnce(t *testing.T) {
	b, dirs := many(t, 3)
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)

	var moves int
	b.OnMove = func(Work) { moves++ }
	b.OnCopy = func(Work) { t.Error("a cut asked for a copy") }

	press(t, b, input.KeyF6)
	if b.Clip().Empty() || !b.Clip().Cut {
		t.Fatal("cut put nothing on the clipboard")
	}
	press(t, b, input.KeyTab)
	press(t, b, input.KeyF7)
	if moves != 1 {
		t.Fatalf("pasting a cut asked for %d moves", moves)
	}
	if !b.Clip().Empty() {
		t.Fatal("the cut is still on the clipboard after being pasted")
	}
	press(t, b, input.KeyTab)
	if took, _ := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyF7}); took {
		t.Error("pasting with an empty clipboard was taken")
	}
	if moves != 1 {
		t.Fatalf("the cut was pasted %d times", moves)
	}
}

// Paste is shown without being offered while there is nothing to paste.
func TestPasteIsNotOfferedWithAnEmptyClipboard(t *testing.T) {
	b, dirs := many(t, 2)
	b.OnCopy = func(Work) {}
	if wiredKey(b, "^V") {
		t.Fatal("the bar offers paste with nothing on the clipboard")
	}
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)
	press(t, b, input.KeyF5)
	if !wiredKey(b, "^V") {
		t.Fatal("the bar does not offer paste with something on the clipboard")
	}
}

// The names waiting to be pasted are marked in the pane they came from,
// and only while that pane is still showing that directory.
func TestTheClipboardIsShownInThePaneItCameFrom(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	write(t, dirs[0], "sub/two.txt", "two")
	b.Here().Reload()

	press(t, b, input.KeyDown) // past ".."
	press(t, b, input.KeyDown) // onto one.txt
	on, _ := b.Here().Selected()
	if on.Name != "one.txt" {
		t.Fatalf("the bar is on %q", on.Name)
	}
	press(t, b, input.KeyF5)

	from := b.Panes()[0]
	if !from.isClipped("one.txt") {
		t.Fatal("the name it came from is not marked")
	}
	if b.Panes()[1].isClipped("one.txt") {
		t.Fatal("a pane the copy did not come from marks the name")
	}
	// And it is drawn as marked, not only recorded.
	g := grid.New(90, 12, color.RGBA{}, color.RGBA{})
	b.Layout(ui.Size{Cols: 90, Rows: 12})
	b.Draw(g.View())
	var found bool
	for y := range 11 {
		if strings.Contains(rowText(g, y, 40), "·one.txt") {
			found = true
		}
	}
	if !found {
		t.Fatal("the name waiting to be pasted is not marked on screen")
	}

	// The pane moves on, and the mark goes with the directory it was
	// about. The clipboard keeps the names: they are still there to
	// paste.
	from.Open(dirs[0] + string(from.FS().Sep()) + "sub")
	if from.isClipped("one.txt") {
		t.Fatal("the mark followed the pane to another directory")
	}
	if b.Clip().Empty() {
		t.Fatal("moving the pane emptied the clipboard")
	}
}

// A copy is pasted from the directory it was taken in, whatever the pane
// is showing by the time it lands.
func TestAPasteComesFromWhereItWasCopied(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	write(t, dirs[0], "sub/two.txt", "two")
	b.Here().Reload()
	press(t, b, input.KeyDown)
	press(t, b, input.KeyDown)

	var asked Work
	b.OnCopy = func(w Work) { asked = w }
	press(t, b, input.KeyF5)

	// The pane it came from is used to look somewhere else.
	b.Panes()[0].Open(dirs[0] + string(b.Panes()[0].FS().Sep()) + "sub")
	press(t, b, input.KeyTab)
	press(t, b, input.KeyF7)

	if asked.At != dirs[0] {
		t.Fatalf("the copy comes from %q, want %q", asked.At, dirs[0])
	}
	if len(asked.Names) != 1 || asked.Names[0] != "one.txt" {
		t.Fatalf("it copies %v", asked.Names)
	}
}

// Taking away the pane a copy came from takes the copy with it: there is
// nowhere left to read the names from.
func TestClosingThePaneAClipCameFromEmptiesIt(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)
	press(t, b, input.KeyF5)
	if b.Clip().Empty() {
		t.Fatal("copy put nothing on the clipboard")
	}

	if _, ok := b.Remove(b.Panes()[0]); !ok {
		t.Fatal("Remove refused")
	}
	if !b.Clip().Empty() {
		t.Fatal("the clipboard outlived the pane it came from")
	}
}

// Shift+Tab walks the panes the other way, so a browser with five of
// them is one the user can get back through.
func TestShiftTabGoesBackAPane(t *testing.T) {
	b, dirs := many(t, 4)
	for i := range slices.Backward(dirs) {
		if got := b.Here().At(); got != dirs[(i+1)%len(dirs)] {
			t.Fatalf("step %d is in %q", i, got)
		}
		if _, err := b.HandleKey(input.Event{
			Kind: input.KeyPress, Key: input.KeyTab, Mods: input.ModShift,
		}); err != nil {
			t.Fatalf("shift+tab: %v", err)
		}
	}
	if got := b.Here().At(); got != dirs[0] {
		t.Fatalf("back round to %q, want %q", got, dirs[0])
	}
}

// The bar says how to close a pane, and the key says which one.
func TestTheBarClosesAPane(t *testing.T) {
	b, dirs := many(t, 3)
	if wiredKey(b, "^D") {
		t.Fatal("the bar offers to close a pane with nothing wired to it")
	}

	var closed []*Pane
	b.OnClose = func(p *Pane) { closed = append(closed, p) }
	if !wiredKey(b, "^D") {
		t.Fatal("the bar does not offer to close a pane")
	}
	press(t, b, input.KeyTab)
	if _, err := b.HandleKey(input.Event{
		Kind: input.KeyPress, Key: input.KeyD, Mods: input.ModCtrl,
	}); err != nil {
		t.Fatalf("ctrl+d: %v", err)
	}
	if len(closed) != 1 || closed[0].At() != dirs[1] {
		t.Fatalf("it asked to close %v, want the pane with the keys", closed)
	}
	// The browser does not take it out itself: the tree it sits in owns
	// that, and it may have to let go of a filesystem on the way.
	if len(b.Panes()) != 3 {
		t.Fatalf("the browser took the pane out itself: %d left", len(b.Panes()))
	}
}

// The bar puts a blank between a key and what it does, so "Tab" and
// "Next" do not run into one another.
func TestTheBarSpacesTheKeyFromItsName(t *testing.T) {
	b, _ := many(t, 2)
	b.Style = styled()
	rows := drawBrowser(b, 100, 12)
	bar := rows[11]
	for _, want := range []string{"Tab Next", "F2 Rename", "^C Copy", "^V Paste", "^D Close"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the bar reads %q, want %q in it", bar, want)
		}
	}
}

// Copying takes the marks off: they said what the next key would act on,
// and that key has been pressed. Waiting to be pasted is marked its own
// way, and a name that was still marked would be drawn as marked
// instead.
func TestCopyingTakesTheMarksOff(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	write(t, dirs[0], "two.txt", "two")
	b.Here().Reload()

	press(t, b, input.KeyDown)
	press(t, b, input.KeySpace)
	press(t, b, input.KeySpace)
	here := b.Here()
	if got := len(here.Marked()); got != 2 {
		t.Fatalf("%d names marked, want both", got)
	}

	press(t, b, input.KeyF5)
	if got := b.Clip().Names; len(got) != 2 {
		t.Fatalf("the clipboard holds %v, want both", got)
	}
	// Marked() falls back to the row under the bar, so the test asks
	// about the names rather than the count.
	for _, name := range []string{"one.txt", "two.txt"} {
		if !here.isClipped(name) {
			t.Fatalf("%q is not marked as waiting to be pasted", name)
		}
	}
	g := grid.New(90, 12, color.RGBA{}, color.RGBA{})
	b.Layout(ui.Size{Cols: 90, Rows: 12})
	b.Draw(g.View())
	var clipped int
	for y := range 11 {
		row := rowText(g, y, 44)
		if strings.Contains(row, "·one.txt") || strings.Contains(row, "·two.txt") {
			clipped++
		}
		if strings.Contains(row, "*one.txt") || strings.Contains(row, "*two.txt") {
			t.Fatalf("row %d still reads as picked out: %q", y, row)
		}
	}
	if clipped != 2 {
		t.Fatalf("%d names are drawn as waiting to be pasted, want 2", clipped)
	}
}

// Paste is not offered when the one that would do the work is not wired,
// whichever that is for what is on the clipboard.
func TestPasteIsNotOfferedWithoutTheOneThatDoesIt(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)

	// A cut needs the mover, and only the copier is wired.
	b.OnCopy = func(Work) {}
	press(t, b, input.KeyF6)
	if wiredKey(b, "^V") {
		t.Fatal("the bar offers to paste a cut with nothing to move it")
	}
	if took, _ := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyF7}); took {
		t.Fatal("pasting a cut was taken with nothing to move it")
	}
	// And the other way round.
	b.OnMove, b.OnCopy = func(Work) {}, nil
	if !wiredKey(b, "^V") {
		t.Fatal("the bar does not offer to paste a cut with a mover wired")
	}
}

// Copy, cut and paste are the chords they are everywhere else.
func TestTheClipboardChords(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)

	var copied, moved int
	b.OnCopy = func(Work) { copied++ }
	b.OnMove = func(Work) { moved++ }

	ctrl := func(k input.Key) {
		t.Helper()
		took, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: k, Mods: input.ModCtrl})
		if err != nil {
			t.Fatalf("ctrl+%v: %v", k, err)
		}
		if !took {
			t.Fatalf("ctrl+%v travelled on", k)
		}
	}

	ctrl(input.KeyC)
	if b.Clip().Empty() || b.Clip().Cut {
		t.Fatal("ctrl+c put no copy on the clipboard")
	}
	press(t, b, input.KeyTab)
	ctrl(input.KeyV)
	if copied != 1 {
		t.Fatalf("ctrl+v asked for %d copies", copied)
	}

	press(t, b, input.KeyTab)
	press(t, b, input.KeyDown)
	ctrl(input.KeyX)
	if !b.Clip().Cut {
		t.Fatal("ctrl+x put no cut on the clipboard")
	}
	press(t, b, input.KeyTab)
	ctrl(input.KeyV)
	if moved != 1 {
		t.Fatalf("ctrl+v asked for %d moves", moved)
	}
}

// Escape empties the clipboard: one the user has forgotten about is one
// that pastes something they did not mean.
func TestEscapeClearsTheClipboard(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)

	// With nothing on it, Escape is not the browser's to take.
	if took, _ := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape}); took {
		t.Error("Escape was taken with an empty clipboard")
	}

	press(t, b, input.KeyF5)
	if b.Clip().Empty() {
		t.Fatal("copy put nothing on the clipboard")
	}
	took, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape})
	if err != nil || !took {
		t.Fatalf("Escape took %v, err %v", took, err)
	}
	if !b.Clip().Empty() {
		t.Fatal("Escape left the clipboard alone")
	}
	// And the marks it put on the pane go with it.
	if b.Panes()[0].isClipped("one.txt") {
		t.Fatal("the name is still marked as waiting to be pasted")
	}
}

// The F keys a two-pane browser has always used still work, even though
// the bar names the chords.
func TestTheOldFunctionKeysStillWork(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)

	var copied int
	b.OnCopy = func(Work) { copied++ }
	press(t, b, input.KeyF5)
	press(t, b, input.KeyTab)
	press(t, b, input.KeyF7)
	if copied != 1 {
		t.Fatalf("the old keys asked for %d copies", copied)
	}
}

// A key on the bar is read on the pane's own ground, so it is written in
// something brighter than the dim colour a note beside a file uses.
func TestTheBarDrawsItsKeysBrightly(t *testing.T) {
	b, _ := many(t, 2)
	b.Style = styled()
	g := grid.New(100, 12, color.RGBA{}, color.RGBA{})
	b.Layout(ui.Size{Cols: 100, Rows: 12})
	b.Draw(g.View())

	// The first cell of the first key is the "T" of Tab.
	start, _ := keyCell(0, 100, len(b.keys))
	cell := g.At(start, 11)
	if cell.Rune != 'T' {
		t.Fatalf("the bar starts with %q", cell.Rune)
	}
	if cell.FG != styled().KeyFG {
		t.Fatalf("the key is drawn in %v, want the bright colour %v",
			cell.FG, styled().KeyFG)
	}
	if cell.FG == styled().NoteFG {
		t.Fatal("the key is drawn in the dim colour a note uses")
	}
}

// The bar says F2 rather than 2, so a key with a name has its whole name
// on it.
func TestTheBarNamesTheFunctionKeys(t *testing.T) {
	b, _ := many(t, 2)
	b.Style = styled()
	bar := drawBrowser(b, 100, 12)[11]
	for _, want := range []string{"F2 Rename", "F8 Delete", "F9 Mkdir"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the bar reads %q, want %q in it", bar, want)
		}
	}
	// And not the bare number, which read as a count rather than a key.
	if strings.Contains(bar, " 2 Rename") {
		t.Errorf("the bar reads %q, want the F on it", bar)
	}
}

// A chord the bar never offered does nothing.
//
// Ctrl+Shift+X is not Ctrl+X. Taking it would cut files on a key the
// user pressed for something else, and the bar would never have said it
// was live.
func TestTheBrowserTakesOnlyTheChordsItOffers(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[0], "one.txt", "one")
	b.Here().Reload()
	press(t, b, input.KeyDown)
	b.OnClose = func(*Pane) { t.Error("a pane was closed by a chord the bar does not offer") }

	for _, mods := range []input.Mods{
		input.ModCtrl | input.ModShift,
		input.ModCtrl | input.ModAlt,
		input.ModAlt,
	} {
		for _, k := range []input.Key{input.KeyC, input.KeyX, input.KeyV, input.KeyD} {
			took, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: k, Mods: mods})
			if err != nil {
				t.Fatalf("%v+%v: %v", mods, k, err)
			}
			if took {
				t.Errorf("the browser took %v+%v, which the bar does not offer", mods, k)
			}
		}
	}
	if !b.Clip().Empty() {
		t.Fatal("a chord the bar does not offer filled the clipboard")
	}
}

// Typing a name moves to it, the way a file list does everywhere else.
func TestTypingANameMovesToIt(t *testing.T) {
	p := alone(t, dirWith(t, "alpha", "gamma", "gazebo", "zulu"))

	typeName(t, p, "ga")
	if got := selectedName(t, p); got != "gamma" {
		t.Fatalf("it is on %q, want gamma", got)
	}
	typeName(t, p, "z")
	if got := selectedName(t, p); got != "gazebo" {
		t.Fatalf("it is on %q, want gazebo", got)
	}
	if got := p.Finding(); got != "gaz" {
		t.Errorf("it is looking for %q, want gaz", got)
	}

	// Backspace takes a letter back rather than going up a directory.
	was := p.At()
	keyTo(t, p, input.Event{Kind: input.KeyPress, Key: input.KeyBackspace})
	if p.At() != was {
		t.Fatalf("it left %q for %q on a backspace while typing", was, p.At())
	}
	if got := p.Finding(); got != "ga" {
		t.Errorf("it is looking for %q, want ga", got)
	}

	// Escape gives up on the name, so backspace goes up again.
	keyTo(t, p, input.Event{Kind: input.KeyPress, Key: input.KeyEscape})
	if got := p.Finding(); got != "" {
		t.Errorf("it is still looking for %q", got)
	}
}

// A letter that matches nothing starts a new name rather than being
// thrown away, and a pause does too.
func TestTypingStartsAgainWhenNothingMatches(t *testing.T) {
	p := alone(t, dirWith(t, "alpha", "gamma"))

	typeName(t, p, "al")
	typeName(t, p, "g")
	if got := selectedName(t, p); got != "gamma" {
		t.Fatalf("it is on %q, want gamma", got)
	}

	// And a pause between letters is two searches, not one.
	now := time.Now()
	p.clock = func() time.Time { return now }
	typeName(t, p, "a")
	now = now.Add(2 * findPause)
	typeName(t, p, "g")
	if got := selectedName(t, p); got != "gamma" {
		t.Fatalf("it is on %q, want the name the second letter starts", got)
	}
}

// dirWith makes a directory holding empty files with these names.
func dirWith(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// typeName types letters into a pane the way the platform delivers them.
func typeName(t *testing.T, p *Pane, text string) {
	t.Helper()
	for _, r := range text {
		keyTo(t, p, input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}
}

// selectedName is the name the bar is on.
func selectedName(t *testing.T, p *Pane) string {
	t.Helper()
	e, ok := p.Selected()
	if !ok {
		t.Fatal("nothing is selected")
	}
	return e.Name
}

// Going somewhere by name works from the bar and from the key.
//
// The bar is the only advertisement the key has, so a cell that looks
// live and does nothing when it is clicked is worse than no cell.
func TestGoToIsOnTheBarAndOnTheKey(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()

	at := barKey(t, b, "^G")
	asked := 0
	b.Panes()[0].OnGoTo = func() { asked++ }

	took, err := b.HandleKey(input.Event{
		Kind: input.KeyPress, Key: input.KeyG, Mods: input.ModCtrl,
	})
	if err != nil {
		t.Fatalf("the key failed: %v", err)
	}
	if !took || asked != 1 {
		t.Fatalf("the key was taken %v and asked %d times", took, asked)
	}

	// And the cell on the bar, which is what says the key is there.
	b.Layout(ui.Size{Cols: 60, Rows: 12})
	start, _ := keyCell(at, 60, len(b.keys))
	took, err = b.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Row: 11, Col: start,
	})
	if err != nil {
		t.Fatalf("the click failed: %v", err)
	}
	if !took || asked != 2 {
		t.Fatalf("the click was taken %v and asked %d times in all", took, asked)
	}
}

// A cell on the bar is drawn live only when there is something behind
// it, and "Go to" has something behind it.
func TestTheBarSaysGoToIsThere(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()

	if b.wired(b.keys[barKey(t, b, "^G")]) {
		t.Error("the bar offers Go to with nothing behind it")
	}
	b.Panes()[0].OnGoTo = func() {}
	if !b.wired(b.keys[barKey(t, b, "^G")]) {
		t.Error("the bar shows Go to as dead when it works")
	}
}

// Escape unwinds the innermost thing first.
//
// A name half typed to jump to it is closer to the user than what is on
// the clipboard: Escape has to take back the one they are in the middle
// of, and leave the other alone.
func TestEscapeTakesBackTheFindBeforeTheClipboard(t *testing.T) {
	b, left, _ := two(t)
	b.Style = styled()
	b.OnCopy = func(Work) {}
	write(t, left, "apple.txt", "one")
	b.Here().Reload()

	// Something on the clipboard, and a name being typed on top of it.
	here := b.Here()
	here.SetFocus(true)
	b.setClip(Clipboard{From: here, Names: []string{"one.txt"}})
	if _, err := b.HandleKey(input.Event{Kind: input.Text, NormalText: true, Rune: 'a'}); err != nil {
		t.Fatalf("typing: %v", err)
	}
	if here.Finding() == "" {
		t.Fatal("the pane is not in the middle of finding a name")
	}

	if _, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape}); err != nil {
		t.Fatalf("escape: %v", err)
	}
	if here.Finding() != "" {
		t.Error("Escape left the name being typed standing")
	}
	if b.clip.Empty() {
		t.Error("Escape emptied the clipboard as well as the find")
	}

	// And again, with nothing half typed, empties the clipboard.
	if _, err := b.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape}); err != nil {
		t.Fatalf("escape: %v", err)
	}
	if !b.clip.Empty() {
		t.Error("Escape left the clipboard standing with nothing else to take back")
	}
}

// underRoot puts a browser in a root, so a test drives the mouse the way
// the window does: press, move and release all go in at the top.
func underRoot(b *Browser, cols, rows int) *ui.Root {
	r := &ui.Root{}
	r.SetWidget(b)
	r.Layout(ui.Rect{Cols: cols, Rows: rows})
	return r
}

// widths is how wide each pane was laid out, left to right.
func widths(b *Browser) []int {
	panes := b.Panes()
	out := make([]int, len(panes))
	for i, p := range panes {
		out[i] = p.Size().Cols
	}
	return out
}

// checkSpread is the browser's invariant: every pane keeps a cell, and
// the panes and the dividers between them fill the box, so the last pane
// ends at the right edge.
func checkSpread(t *testing.T, b *Browser, cols int) {
	t.Helper()
	got := widths(b)
	total := len(got) - 1
	for i, w := range got {
		if w < 1 {
			t.Fatalf("pane %d is %d columns wide, want at least one: %v", i, w, got)
		}
		total += w
	}
	if total != cols {
		t.Fatalf("the panes and dividers come to %d of %d columns: %v", total, cols, got)
	}
	// And what the browser says is where a pane sits agrees with it.
	last := b.Panes()[len(got)-1]
	area, ok := b.ChildArea(last)
	if !ok {
		t.Fatal("the last pane has nowhere to be drawn")
	}
	if at := area.X + area.Cols; at != cols {
		t.Fatalf("the last pane ends at column %d, want the right edge at %d", at, cols)
	}
}

// pressCol, moveCol and releaseCol are the three events of a drag, on a
// row of the panes rather than the bar.
func pressCol(col int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: 3}
}

func moveCol(col int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: col, Row: 3}
}

func releaseCol(col int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: col, Row: 3}
}

// dividerCol is the column the rule between two panes is drawn in, read
// off the drawing rather than out of the arithmetic.
func dividerCol(t *testing.T, b *Browser, cols, rows, which int) int {
	t.Helper()
	g := grid.New(cols, rows, color.RGBA{}, color.RGBA{})
	b.Draw(g.View())
	var at []int
	for x := range cols {
		if g.At(x, 0).Rune == divider {
			at = append(at, x)
		}
	}
	if which >= len(at) {
		t.Fatalf("%d dividers are drawn, want one at %d", len(at), which)
	}
	return at[which]
}

// A divider is dragged where the user drags it, and the panes each side
// take the room. The whole gesture goes in through the root, the way it
// does in the window.
func TestBrowserDividerCanBeDragged(t *testing.T) {
	for _, tc := range []struct {
		name  string
		panes int
		cols  int
		// which divider is grabbed, and the column it is dropped in.
		which, to int
		want      []int
	}{
		{name: "two panes", panes: 2, cols: 91, which: 0, to: 20, want: []int{20, 70}},
		{name: "three panes, the first divider", panes: 3, cols: 92, which: 0, to: 20,
			want: []int{20, 40, 30}},
		{name: "three panes, the second divider", panes: 3, cols: 92, which: 1, to: 70,
			want: []int{30, 39, 21}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := many(t, tc.panes)
			r := underRoot(b, tc.cols, 12)
			at := dividerCol(t, b, tc.cols, 12, tc.which)

			took, err := r.HandleMouse(pressCol(at))
			if err != nil {
				t.Fatalf("the press failed: %v", err)
			}
			if !took {
				t.Fatal("the press on the divider was not taken, so no drag can follow")
			}
			mouseTo(t, r, moveCol(tc.to))
			mouseTo(t, r, releaseCol(tc.to))

			if got := widths(b); !equalInts(got, tc.want) {
				t.Fatalf("the panes are %v wide, want %v", got, tc.want)
			}
			checkSpread(t, b, tc.cols)
			if got := dividerCol(t, b, tc.cols, 12, tc.which); got != tc.to {
				t.Fatalf("the rule is drawn in column %d, want where it was dropped, %d", got, tc.to)
			}

			// And a move once the button is up is nothing to do with it.
			was := widths(b)
			mouseTo(t, r, moveCol(tc.to+10))
			if got := widths(b); !equalInts(got, was) {
				t.Fatalf("the panes are %v wide after the button came up, want %v", got, was)
			}
		})
	}
}

// equalInts reports whether two lists of widths are the same.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A divider dragged off the end of the box leaves every pane on screen,
// and one dragged into its neighbour stops there rather than pushing it
// out of the way.
func TestBrowserDragKeepsACellForEveryPane(t *testing.T) {
	b, _ := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)

	mouseTo(t, r, pressCol(at))
	mouseTo(t, r, moveCol(-99))
	if got := widths(b)[0]; got != 1 {
		t.Fatalf("dragged off the left the first pane is %d wide, want 1", got)
	}
	checkSpread(t, b, 92)

	mouseTo(t, r, moveCol(999))
	// It stops at the divider beside it, which keeps its own pane's cell.
	if got := widths(b)[1]; got != 1 {
		t.Fatalf("dragged off the right the middle pane is %d wide, want 1", got)
	}
	checkSpread(t, b, 92)
	mouseTo(t, r, releaseCol(999))

	// And the last divider dragged off the right edge leaves the pane
	// beyond it on screen: there is nothing further right to stop it.
	last := dividerCol(t, b, 92, 12, 1)
	mouseTo(t, r, pressCol(last))
	mouseTo(t, r, moveCol(999))
	if got := widths(b)[2]; got != 1 {
		t.Fatalf("dragged off the right the last pane is %d wide, want 1", got)
	}
	checkSpread(t, b, 92)
	mouseTo(t, r, releaseCol(999))
}

// A divider dragged into the one beside it stops there rather than
// shoving it along: every divider is where the user put it, and only the
// one being dragged moves.
func TestBrowserDividerStopsAtItsNeighbour(t *testing.T) {
	b, _ := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)

	mouseTo(t, r, pressCol(at))
	mouseTo(t, r, moveCol(999))
	mouseTo(t, r, releaseCol(999))

	// The middle pane is down to its cell and the last one is where it
	// was, one column along to make room for it.
	if got, want := widths(b), []int{60, 1, 29}; !equalInts(got, want) {
		t.Fatalf("the panes are %v wide, want %v", got, want)
	}
	checkSpread(t, b, 92)
}

// A drag whose release never comes must not leave the divider stuck to
// the pointer.
func TestBrowserCancelGestureEndsADrag(t *testing.T) {
	b, _ := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)
	was := widths(b)

	mouseTo(t, r, pressCol(at))
	b.CancelGesture()
	mouseTo(t, r, moveCol(10))

	if got := widths(b); !equalInts(got, was) {
		t.Fatalf("the panes are %v wide, want the drag to have stopped at %v", got, was)
	}
}

// Scrolling mid-drag is not a move: a wheel notch has no release, and
// acting on one would put the divider where the pointer is not.
func TestBrowserWheelDuringADragDoesNotMoveIt(t *testing.T) {
	b, _ := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)
	was := widths(b)

	mouseTo(t, r, pressCol(at))
	mouseTo(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseWheelUp, Col: 10, Row: 3,
	})

	if got := widths(b); !equalInts(got, was) {
		t.Fatalf("the panes are %v wide, want the notch ignored and %v", got, was)
	}
	// The drag is still on: the notch did not end it either.
	mouseTo(t, r, moveCol(20))
	if got := widths(b)[0]; got != 20 {
		t.Fatalf("the first pane is %d wide, want the drag to carry on after the notch", got)
	}
}

// The handle is the divider and nothing more: the column beside it
// belongs to the pane, and a press there puts the keys on it.
func TestBrowserPressBesideADividerReachesThePane(t *testing.T) {
	b, dirs := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)
	was := widths(b)

	mouseTo(t, r, pressCol(at-1))
	mouseTo(t, r, moveCol(10))
	mouseTo(t, r, releaseCol(10))

	if got := b.Here().At(); got != dirs[0] {
		t.Fatalf("the keys are in %q, want the pane beside the divider", got)
	}
	if got := widths(b); !equalInts(got, was) {
		t.Fatalf("the panes are %v wide, want a press beside the divider to leave them at %v", got, was)
	}
}

// A click lands in the pane the drag left under it: the browser routes
// by the cells it now draws, not the ones it started with.
func TestBrowserClicksFollowADraggedDivider(t *testing.T) {
	b, dirs := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)

	mouseTo(t, r, pressCol(at))
	mouseTo(t, r, moveCol(10))
	mouseTo(t, r, releaseCol(10))

	// Column 20 was the first pane's before the drag and is the second
	// pane's after it.
	if at <= 20 {
		t.Fatalf("the divider started at column %d, so column 20 was never the first pane's", at)
	}
	mouseTo(t, r, pressCol(20))

	if got := b.Here().At(); got != dirs[1] {
		t.Fatalf("the click put the keys in %q, want the pane the drag moved under it", got)
	}
}

// Opening another pane shares the room out evenly again, which is the
// rule: a boundary the user dragged is not the same boundary once the
// panes have changed.
func TestAddingAPaneSharesTheRoomAgain(t *testing.T) {
	b, _ := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)
	mouseTo(t, r, pressCol(at))
	mouseTo(t, r, moveCol(10))
	mouseTo(t, r, releaseCol(10))

	if !b.Add(here(t, t.TempDir())) {
		t.Fatal("Add refused a new pane")
	}

	got := widths(b)
	checkSpread(t, b, 92)
	for i, w := range got {
		if w < 21 || w > 23 {
			t.Fatalf("pane %d is %d wide, want the room shared evenly: %v", i, w, got)
		}
	}
	// And taking one away shares it out again too.
	if _, ok := b.Remove(b.Panes()[0]); !ok {
		t.Fatal("Remove refused a pane that was there")
	}
	checkSpread(t, b, 92)
}

// A pane that closed mid-drag ends the drag with it: the boundary the
// pointer was holding is gone.
func TestRemovingAPaneDuringADragEndsIt(t *testing.T) {
	b, _ := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)
	mouseTo(t, r, pressCol(at))

	if _, ok := b.Remove(b.Panes()[2]); !ok {
		t.Fatal("Remove refused a pane that was there")
	}
	was := widths(b)
	mouseTo(t, r, moveCol(5))

	if got := widths(b); !equalInts(got, was) {
		t.Fatalf("the panes are %v wide, want the drag to have ended at %v", got, was)
	}
}

// The drag repaints the panes it moved, and the frame after it writes
// nothing: a divider that is not moving must not repaint the window.
func TestBrowserDragRedrawsAndThenSettles(t *testing.T) {
	b, dirs := many(t, 3)
	for _, at := range dirs {
		write(t, at, "a.txt", "a")
	}
	b.Reload()
	r := underRoot(b, 92, 12)
	g := grid.New(92, 12, color.RGBA{}, color.RGBA{})
	b.Draw(g.View())
	g.ClearDirty()

	at := dividerCol(t, b, 92, 12, 0)
	mouseTo(t, r, pressCol(at))
	mouseTo(t, r, moveCol(20))
	mouseTo(t, r, releaseCol(20))
	b.Draw(g.View())

	if !g.AnyDirty() {
		t.Fatal("the drag moved the panes and dirtied nothing")
	}
	if got := dividerCol(t, b, 92, 12, 0); got != 20 {
		t.Fatalf("the rule is drawn in column %d, want 20", got)
	}

	g.ClearDirty()
	b.Draw(g.View())
	if g.AnyDirty() {
		var dirty []int
		for y := range 12 {
			if g.RowDirty(y) {
				dirty = append(dirty, y)
			}
		}
		t.Fatalf("an idle frame after the drag dirtied rows %v", dirty)
	}
}

// blank is a widget that draws nothing and records what it is handed,
// for putting a browser somewhere other than the top of the tree.
type blank struct {
	size ui.Size
	seen []input.MouseEvent
}

func (w *blank) Layout(size ui.Size) { w.size = size }
func (w *blank) Draw(grid.View)      {}
func (w *blank) HandleMouse(ev input.MouseEvent) (bool, error) {
	w.seen = append(w.seen, ev)
	return true, nil
}

// Only the button that started the drag ends it. Another one coming up
// proves nothing: the first may still be down.
func TestBrowserDragIgnoresAnotherButtonComingUp(t *testing.T) {
	b, _ := many(t, 3)
	r := underRoot(b, 92, 12)
	at := dividerCol(t, b, 92, 12, 0)

	mouseTo(t, r, pressCol(at))
	mouseTo(t, r, moveCol(25))
	mouseTo(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseRight, Col: 25, Row: 3,
	})
	mouseTo(t, r, input.MouseEvent{
		Kind: input.MouseRelease, Button: input.MouseRight, Col: 25, Row: 3,
	})
	mouseTo(t, r, moveCol(20))

	if got := widths(b)[0]; got != 20 {
		t.Fatalf("the first pane is %d wide, want the drag to carry on to 20", got)
	}
	// And the left button still ends it.
	mouseTo(t, r, releaseCol(20))
	mouseTo(t, r, moveCol(40))
	if got := widths(b)[0]; got != 20 {
		t.Fatalf("the first pane is %d wide after the button came up, want 20", got)
	}
}

// A browser below the top of the tree keeps the pointer all the same:
// the root holds it for whoever took the press, so a drag that wanders
// out of the browser still moves its divider.
func TestBrowserDragCarriesOnOutsideItsOwnArea(t *testing.T) {
	b, _ := many(t, 3)
	beside := &blank{}
	r := &ui.Root{}
	r.SetWidget(ui.NewSplit(ui.Columns, beside, b))
	r.Layout(ui.Rect{Cols: 185, Rows: 12})

	area, shown := r.AreaOf(b)
	if !shown {
		t.Fatal("the browser is not on screen")
	}
	if area.X <= 0 {
		t.Fatalf("the browser starts at column %d, so there is nothing to its left", area.X)
	}
	at := dividerCol(t, b, area.Cols, area.Rows, 0)

	mouseTo(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: area.X + at, Row: 3,
	})
	if got := r.Holding(); got != ui.Widget(b) {
		t.Fatalf("the pointer is held by %v, want the browser", got)
	}
	// Right out of the browser and into the widget beside it.
	mouseTo(t, r, input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: 0, Row: 3,
	})

	if got := widths(b)[0]; got != 1 {
		t.Fatalf("the first pane is %d wide, want the drag clamped to the browser's edge", got)
	}
	if len(beside.seen) != 0 {
		t.Fatalf("the widget beside the browser saw %d events of the drag", len(beside.seen))
	}
}

// A browser too narrow to give every pane a cell leaves the panes it
// squeezed out at the size they had, the way a split does. Telling a
// pane it has no columns is not the same as telling it nothing.
func TestBrowserLeavesASqueezedPaneAlone(t *testing.T) {
	b, _ := many(t, 5)
	was := widths(b)

	b.Layout(ui.Size{Cols: 4, Rows: 12})

	if got := widths(b); !equalInts(got, was) {
		t.Fatalf("the panes are %v wide, want the %v they had: there is no room to share", got, was)
	}
}

// A browser built without NewBrowser is idle to begin with: nothing is
// dragged until a press on a divider says so.
func TestABrowserIsNotDraggingUntilAPressSaysSo(t *testing.T) {
	b := &Browser{}
	b.Add(here(t, t.TempDir()))
	b.Add(here(t, t.TempDir()))
	b.Layout(ui.Size{Cols: 91, Rows: 12})
	was := widths(b)

	if _, err := b.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove, Button: input.MouseLeft, Col: 10, Row: 3,
	}); err != nil {
		t.Fatalf("the move failed: %v", err)
	}

	if got := widths(b); !equalInts(got, was) {
		t.Fatalf("the panes are %v wide, want the %v they had: nobody has pressed a divider", got, was)
	}
}

// A bar too narrow for the names still names every key.
//
// The names are what a bar is cut down to, and the chord is what the
// user has to know: a bar reading "F3 Vie" still says which key views a
// file, and one missing F3 altogether does not.
func TestANarrowBarStillNamesEveryKey(t *testing.T) {
	b, _, _ := two(t)
	b.Style = styled()

	rows := drawBrowser(b, 80, 12)

	bar := rows[len(rows)-1]
	for _, k := range BrowserKeys() {
		if !strings.Contains(bar, k.Shown) {
			t.Errorf("the bar reads %q, missing the key %q", bar, k.Shown)
		}
	}
}

// clickRow presses the left button on a row of a pane's listing,
// counted from the top of the listing.
func clickRow(t *testing.T, p *Pane, row int) {
	t.Helper()
	if _, err := p.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: 3, Row: p.head() + row,
	}); err != nil {
		t.Fatalf("click row %d: %v", row, err)
	}
}

// A pane whose clock the test moves by hand.
func clocked(t *testing.T, at string) (*Pane, *time.Time) {
	t.Helper()
	p := alone(t, at)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	p.clock = func() time.Time { return now }
	return p, &now
}

// A click points at a name and a double click opens it. One click used
// to open, which took the user into a directory they had only meant to
// pick out.
func TestAClickPointsAtANameAndADoubleClickOpensIt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sub/two.txt", "two")
	write(t, dir, "one.txt", "one")
	p, now := clocked(t, dir)

	// Row 0 is the way up, then sub, then one.txt.
	clickRow(t, p, 1)
	if p.At() != dir {
		t.Fatalf("one click went into %q", p.At())
	}
	if e, ok := p.Selected(); !ok || e.Name != "sub" {
		t.Fatalf("one click left the bar on %v, want sub", e.Name)
	}

	*now = now.Add(ui.DoubleClickTime / 2)
	clickRow(t, p, 1)
	if want := filepath.Join(dir, "sub"); p.At() != want {
		t.Fatalf("a double click left the pane in %q, want %q", p.At(), want)
	}
}

// A double click on a file opens it.
func TestADoubleClickOnAFileOpensIt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")
	p, _ := clocked(t, dir)
	var opened []string
	p.OnOpen = func(e vfs.Entry) { opened = append(opened, e.Name) }

	clickRow(t, p, 1)
	if len(opened) != 0 {
		t.Fatalf("one click opened %v", opened)
	}
	clickRow(t, p, 1)
	if len(opened) != 1 || opened[0] != "one.txt" {
		t.Fatalf("a double click opened %v, want one.txt once", opened)
	}
}

// Two clicks too far apart, or on two names, are two clicks.
func TestTwoClicksThatAreNotADoubleClickOpenNothing(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")
	write(t, dir, "two.txt", "two")
	p, now := clocked(t, dir)
	var opened []string
	p.OnOpen = func(e vfs.Entry) { opened = append(opened, e.Name) }

	clickRow(t, p, 1)
	*now = now.Add(ui.DoubleClickTime + time.Millisecond)
	clickRow(t, p, 1)
	if len(opened) != 0 {
		t.Fatalf("two slow clicks opened %v", opened)
	}

	*now = now.Add(ui.DoubleClickTime + time.Millisecond)
	clickRow(t, p, 1)
	clickRow(t, p, 2)
	if len(opened) != 0 {
		t.Fatalf("clicks on two names opened %v", opened)
	}
	if e, _ := p.Selected(); e.Name != "two.txt" {
		t.Errorf("the bar is on %q, want the name clicked last", e.Name)
	}
}

// A pane with nothing to show says it is reading while its first
// listing is on the way, so a slow machine does not look like an empty
// directory. Once the listing is in, an empty directory says nothing.
func TestAPaneSaysItIsReadingUntilItsFirstListingArrives(t *testing.T) {
	dir := t.TempDir()
	p := New(vfs.NewLocal())
	p.Style = styled()
	var answer func()
	p.Read = func(f vfs.FS, path string, then func([]vfs.Entry, error)) {
		answer = func() { then(f.ReadDir(path)) }
	}
	p.Layout(ui.Size{Cols: 40, Rows: 12})

	p.Open(dir)
	if got := strings.Join(drawn(p, 40, 12), "\n"); !strings.Contains(got, stillReading) {
		t.Fatalf("the pane says nothing while it reads:\n%s", got)
	}
	answer()
	if got := strings.Join(drawn(p, 40, 12), "\n"); strings.Contains(got, stillReading) {
		t.Fatalf("the pane still says it is reading once it has read:\n%s", got)
	}
}

// A pane that has a listing keeps it while it moves, and says nothing
// over it: the path line says where it is going.
func TestAPaneMovingOnSaysNothingOverWhatItHas(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.txt", "one")
	write(t, dir, "sub/two.txt", "two")
	p := alone(t, dir)
	p.Read = func(_ vfs.FS, _ string, _ func([]vfs.Entry, error)) {}

	p.Open(filepath.Join(dir, "sub"))
	got := strings.Join(drawn(p, 40, 12), "\n")
	if strings.Contains(got, stillReading) {
		t.Errorf("the pane says it is reading over the listing it has:\n%s", got)
	}
	if !strings.Contains(got, "one.txt") {
		t.Errorf("the pane dropped the listing it had:\n%s", got)
	}
}

// failing makes a pane's reads fail from now on and counts the reasons
// it shows.
func failing(p *Pane) *[]error {
	var shown []error
	p.OnError = func(_ string, err error) { shown = append(shown, err) }
	p.Read = func(_ vfs.FS, _ string, then func([]vfs.Entry, error)) {
		then(nil, errors.New("the machine went away"))
	}
	return &shown
}

// A click on the row saying a pane without the keys could not be read
// shows why once. Gaining the keys shows it, and the same press landing
// on the row as well showed it again.
func TestClickingAFailedPaneShowsWhyOnce(t *testing.T) {
	b, _, _ := two(t)
	p := b.panes[1]
	shown := failing(p)
	p.Reload()
	if len(*shown) != 0 {
		t.Fatalf("a pane without the keys showed %v", *shown)
	}
	start, _ := b.paneCell(1, 80)
	errorPress := input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: start + 1, Row: errorRow,
	}

	mouseTo(t, b, errorPress)
	if len(*shown) != 1 {
		t.Fatalf("the click showed the reason %d times, want once", len(*shown))
	}
	// And the row still shows it again when asked.
	mouseTo(t, b, errorPress)
	if len(*shown) != 2 {
		t.Errorf("a second click showed the reason %d times in all, want twice", len(*shown))
	}
}

// The same through a split, where the press that moves the keys to the
// browser is the split's to hand on or keep.
func TestClickingAFailedPaneBesideShowsWhyOnce(t *testing.T) {
	b, _ := many(t, 2)
	r := &ui.Root{}
	r.SetWidget(ui.NewSplit(ui.Columns, &blank{}, b))
	r.Layout(ui.Rect{Cols: 185, Rows: 12})
	p := b.panes[1]
	shown := failing(p)
	p.Reload()
	if len(*shown) != 0 {
		t.Fatalf("a pane without the keys showed %v", *shown)
	}
	area, _ := r.AreaOf(b)
	start, _ := b.paneCell(1, area.Cols)

	mouseTo(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: area.X + start + 1, Row: errorRow,
	})
	if len(*shown) != 1 {
		t.Fatalf("the click showed the reason %d times, want once", len(*shown))
	}
}

// A double click on a name in a browser without the keys opens it: the
// press that moves the keys there is the first of the two.
func TestADoubleClickOnABrowserWithoutTheKeysOpens(t *testing.T) {
	b, dirs := many(t, 2)
	write(t, dirs[1], "one.txt", "one")
	b.panes[1].Reload()
	r := &ui.Root{}
	r.SetWidget(ui.NewSplit(ui.Columns, &blank{}, b))
	r.Layout(ui.Rect{Cols: 185, Rows: 12})
	var opened []string
	b.panes[1].OnOpen = func(e vfs.Entry) { opened = append(opened, e.Name) }
	area, _ := r.AreaOf(b)
	start, _ := b.paneCell(1, area.Cols)
	// The way up is the first row of the listing, and one.txt the next.
	click := input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + start + 1, Row: b.panes[1].head() + 1,
	}

	mouseTo(t, r, click)
	mouseTo(t, r, click)
	if len(opened) != 1 || opened[0] != "one.txt" {
		t.Fatalf("a double click opened %v, want one.txt", opened)
	}
}

// Two reads answered out of order leave an empty directory saying
// nothing, rather than saying it is still reading.
func TestReadsAnsweredOutOfOrderLeaveNoReadingLine(t *testing.T) {
	slow, empty := t.TempDir(), t.TempDir()
	write(t, slow, "one.txt", "one")
	p := New(vfs.NewLocal())
	p.Style = styled()
	answers := map[string]func(){}
	p.Read = func(f vfs.FS, path string, then func([]vfs.Entry, error)) {
		answers[path] = func() { then(f.ReadDir(path)) }
	}
	p.Layout(ui.Size{Cols: 40, Rows: 12})

	p.Open(slow)
	p.Open(empty)
	answers[empty]()
	answers[slow]()

	if got := strings.Join(drawn(p, 40, 12), "\n"); strings.Contains(got, stillReading) {
		t.Fatalf("the pane says it is reading after both answers:\n%s", got)
	}
}

// A click on one pane of a browser without the keys hands them to that
// pane, not to the one the browser had last. That one could not be
// read, and handed the keys on the way it showed why, for a click that
// was not on it.
func TestAClickOnABrowserWithoutTheKeysGoesToThatPane(t *testing.T) {
	b, _ := many(t, 2)
	r := &ui.Root{}
	r.SetWidget(ui.NewSplit(ui.Columns, &blank{}, b))
	r.Layout(ui.Rect{Cols: 185, Rows: 12})
	b.SetFocus(false)
	last := b.panes[0]
	shown := failing(last)
	last.Reload()
	if len(*shown) != 0 {
		t.Fatalf("a pane without the keys showed %v", *shown)
	}
	area, _ := r.AreaOf(b)
	start, _ := b.paneCell(1, area.Cols)

	mouseTo(t, r, input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + start + 1, Row: b.panes[1].head(),
	})
	if b.Here() != b.panes[1] {
		t.Fatal("the keys are not in the pane that was clicked")
	}
	if len(*shown) != 0 {
		t.Errorf("a click on the pane beside it showed %v", *shown)
	}
}
