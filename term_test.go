package main

import (
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/winkeys"
)

// On a Swedish keyboard the key marked + sits where a US one has -.
// Ctrl and that key makes the font bigger, as the key says.
func TestPunctuationIsTheKeyItTypes(t *testing.T) {
	for _, c := range []struct {
		press gi.KeyPress
		want  input.Key
	}{
		{gi.KeyPress{Key: gi.KeyMinus, Char: '+'}, input.KeyPlus},
		{gi.KeyPress{Key: gi.KeySlash, Char: '-'}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyMinus, Char: '-'}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyMinus}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyA, Char: 'a'}, input.KeyA},
	} {
		ev, ok := winkeys.Event(c.press)
		if !ok || ev.Key != c.want {
			t.Errorf("%+v reads as %v, %v; want %v", c.press, ev.Key, ok, c.want)
		}
	}
	ev, _ := winkeys.Event(gi.KeyPress{Key: gi.KeyMinus, Char: '+', Mods: gi.ModControl})
	if id, _ := shortcuts().Lookup(ui.ChordOf(ev)); id != "font.increase" {
		t.Fatalf("Ctrl and the key marked + runs %q", id)
	}
}

// A pane too small for a usable screen still resizes the shell, once it
// has stayed so a moment.
func TestATinyPaneResizesItsShellOnceSettled(t *testing.T) {
	_, tm := termStage(t, geom.Sz(900, 75))
	// Until the sidebar has slid into place.
	frames(90)
	cols, rows := tm.cells.Fit()
	if cols >= leastCols && rows >= leastRows {
		t.Fatalf("the pane fits %dx%d, which is not small", cols, rows)
	}
	if size := tm.sh.T.Size(); size.Cols != cols || size.Rows != rows {
		t.Fatalf("settled, the shell is %dx%d, want %dx%d", size.Cols, size.Rows, cols, rows)
	}
}

// A folder a pane says it is in starts a new shell there only when it is
// a folder on this computer; one from another system starts it where it
// would have started, rather than stopping it starting.
func TestANewShellStartsOnlyInAFolderThatIsHere(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	dir := t.TempDir()
	if got := a.localDir("p1", dir); got != dir {
		t.Fatalf("a folder here gave %q", got)
	}
	if got := a.localDir("p1", "/mnt/d/no/such/folder"); got != "" {
		t.Fatalf("a folder from elsewhere gave %q", got)
	}
}
