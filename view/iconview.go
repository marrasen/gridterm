package view

import (
	"strings"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/ui/files"
	"github.com/marrasen/kakel/vfs"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/widget"
)

// A file pane shows its folder as a table of details or as a grid of
// icons, Ctrl+1 and Ctrl+2 as in gunim's file manager. In the icons, a
// picture shows as a thumbnail of itself, made in the background from
// the file on whichever machine it is.

// tileSize is an icon view's tile.
var tileSize = geom.Sz(112, 104)

// filesBody holds a file pane's two views, and shows the one asked for.
type filesBody struct {
	b *browser
}

// Children implements [gunim.Composite].
func (fb *filesBody) Children() []gunim.Node { return []gunim.Node{fb.b.table, fb.b.grid} }

// Layout implements [gunim.Node]: the view shown fills the body, and
// the other is laid out empty, building nothing.
func (fb *filesBody) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	shown, hidden := kids.At(0), kids.At(1)
	if fb.b.icons {
		shown, hidden = hidden, shown
	}
	shown.Layout(gunim.Tight(c.Max))
	shown.Place(geom.Point{})
	hidden.Layout(gunim.Tight(geom.Size{}))
	hidden.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (fb *filesBody) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	if fb.b.icons {
		kids.At(1).Paint(p)
	} else {
		kids.At(0).Paint(p)
	}
}

// newGrid makes the pane's icon view.
func (b *browser) newGrid() {
	g := widget.NewTileGrid(tileSize)
	g.Tile = func(i int) gunim.Node { return &fileTile{b: b, i: i} }
	g.OnActivate = func(i int) gunim.Intent {
		if i < 0 || i >= len(b.order) {
			return nil
		}
		if k := b.order[i]; k != up {
			return app.EnterEntry{Pane: b.id, Name: string(k)}
		}
		return app.GoUp{Pane: b.id}
	}
	g.OnView = func(first, count int) gunim.Intent {
		// Kept, to ask again for a folder listed in the same place;
		// hidden, the grid asks for nothing.
		b.inView = [2]int{first, count}
		if !b.icons {
			return nil
		}
		return b.wantThumbs(first, count)
	}
	g.DragTiles = func(sel [][2]int, at geom.Point) (any, gunim.Node, geom.Point) {
		return b.dragRows(b.keysIn(sel), at)
	}
	b.grid = g
}

// typeAt finds the tile named by what is typed at the icon view: text
// typed within a second of the last adds up, as in the table.
func (b *browser) typeAt(e gi.TextInput, u *gunim.UI) {
	if e.Time.Sub(b.typedAt) > time.Second {
		b.typed = ""
	}
	b.typed += e.Text
	b.typedAt = e.Time
	_, from := b.grid.Selected()
	i := widget.FindTyped(b.typed, len(b.order), from, func(i int) string { return string(b.order[i]) })
	if i >= 0 {
		b.grid.SetSelected([][2]int{{i, i + 1}}, i, u)
		b.grid.ShowTile(i, u)
	}
}

// keysIn returns the keys of the tiles in runs sel.
func (b *browser) keysIn(sel [][2]int) []widget.Key {
	var out []widget.Key
	for _, r := range sel {
		for i := r[0]; i < r[1] && i < len(b.order); i++ {
			out = append(out, b.order[i])
		}
	}
	return out
}

// wantThumbs asks for the thumbnails of the pictures among the tiles
// from first on, count of them, that are not made yet.
func (b *browser) wantThumbs(first, count int) gunim.Intent {
	var names []string
	for i := first; i < first+count && i < len(b.order); i++ {
		k := b.order[i]
		e, ok := b.byName[k]
		if !ok || e.IsDir() || !files.IsImage(e.Name) {
			continue
		}
		if _, done := files.Thumbnails.Get(b.thumbKey(e)); !done {
			names = append(names, e.Name)
		}
	}
	if len(names) == 0 || b.st.Archive {
		return nil
	}
	return app.NeedThumbs{Pane: b.id, Names: names}
}

// thumbKey is the key of entry e's thumbnail.
func (b *browser) thumbKey(e vfs.Entry) files.ThumbKey {
	return files.ThumbKey{Machine: string(b.w.filesKeyOf(b.id)), Path: b.joined(b.st.Path, e.Name), Mod: e.Mod, Size: e.Size}
}

// setIcons shows the pane as icons, or back as details, with the
// cursor and the marks where they were.
func (b *browser) setIcons(on bool, u *gunim.UI) {
	if on == b.icons {
		return
	}
	cursor, _ := b.cursor()
	picked := b.pickedKeys()
	b.icons = on
	if on {
		b.selectTiles(picked, cursor, u)
		b.grid.ShowTile(b.indexOf(cursor), u)
		u.Focus(b.grid)
		b.askThumbs(u)
	} else {
		b.table.SetCursor(cursor, u)
		// What was picked among the tiles, and nothing else.
		b.table.SetMarked(picked)
		u.Focus(b.table)
	}
	b.w.tickSwitch("files.icons", on)
	u.Invalidate()
}

// selectTiles selects the tiles of keys, with the keyboard on cursor's.
func (b *browser) selectTiles(keys []widget.Key, cursor widget.Key, u *gunim.UI) {
	want := make(map[widget.Key]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	var runs [][2]int
	for i, k := range b.order {
		if !want[k] {
			continue
		}
		if n := len(runs); n > 0 && runs[n-1][1] == i {
			runs[n-1][1] = i + 1
		} else {
			runs = append(runs, [2]int{i, i + 1})
		}
	}
	b.grid.SetSelected(runs, b.indexOf(cursor), u)
}

// askThumbs asks for the thumbnails of the tiles in view, as a folder
// is listed, or the icons come back.
func (b *browser) askThumbs(u *gunim.UI) {
	if !b.icons {
		return
	}
	if in := b.wantThumbs(b.inView[0], max(b.inView[1], 1)); in != nil {
		u.Send(b, in)
	}
}

// indexOf is where key k is among the pane's rows, 0 when it is not.
func (b *browser) indexOf(k widget.Key) int {
	for i, o := range b.order {
		if o == k {
			return i
		}
	}
	return 0
}

// cursor is the row or tile the keyboard is on.
func (b *browser) cursor() (widget.Key, bool) {
	if !b.icons {
		return b.table.Cursor()
	}
	_, i := b.grid.Selected()
	if i < 0 || i >= len(b.order) {
		return "", false
	}
	return b.order[i], true
}

// pickedKeys are the rows marked, or the tiles selected.
func (b *browser) pickedKeys() []widget.Key {
	if !b.icons {
		return b.table.Marked()
	}
	sel, _ := b.grid.Selected()
	return b.keysIn(sel)
}

// clearPicked unmarks the rows, or leaves only the tile the keyboard is
// on selected.
func (b *browser) clearPicked(u *gunim.UI) {
	if !b.icons {
		b.table.ClearMarks()
		return
	}
	_, i := b.grid.Selected()
	var runs [][2]int
	if i >= 0 {
		runs = [][2]int{{i, i + 1}}
	}
	b.grid.SetSelected(runs, i, u)
}

// focusable is the pane's view that takes the keyboard.
func (b *browser) focusable() gunim.Node {
	if b.icons {
		return b.grid
	}
	return b.table
}

// fileTile is one tile of the icon view: a thumbnail, or its kind's
// icon, over its name.
type fileTile struct {
	b    *browser
	i    int
	name text.Run
	was  string
}

// Layout implements [gunim.Node].
func (t *fileTile) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Max
}

// Paint implements [gunim.Node].
func (t *fileTile) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	b := t.b
	if t.i >= len(b.order) {
		return
	}
	th := f.Theme
	k := b.order[t.i]
	e, isEntry := b.byName[k]
	kind := fileKind{icon.FolderUp, &fileFolder}
	label := ".."
	if isEntry {
		kind, label = kindOf(e), e.Name
	}
	if label != t.was {
		t.was = label
		t.name = text.Default().Shape(label, smallText.Get(th))
	}
	pic := geom.Rc((box.W-64)/2, 8, 64, 64)
	drew := false
	if isEntry && !e.IsDir() && files.IsImage(e.Name) {
		if th, ok := files.Thumbnails.Get(b.thumbKey(e)); ok && th.Image != nil {
			w, h := th.Image.Size()
			if w > 0 && h > 0 {
				s := min(pic.Size().W/float32(w), pic.Size().H/float32(h))
				sz := geom.Sz(float32(w)*s, float32(h)*s)
				r := geom.Rc(pic.Center().X-sz.W/2, pic.Center().Y-sz.H/2, sz.W, sz.H)
				p.Image(th.Image, r, paint.ImageOpts{Radius: 4, Opacity: 1})
				drew = true
			}
		}
	}
	if !drew {
		drawIcon(p, kind.icon, pic.Inset(geom.Uniform(6)), kind.ink.Get(th), 1.2)
	}
	ink := widget.Ink.Get(th)
	if strings.HasPrefix(label, ".") && label != ".." {
		ink.A /= 2
	}
	room := box.W - 8
	x := (box.W - min(t.name.Advance, room)) / 2
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rc(4, 76, room, box.H-76), Opacity: 1, Clip: true})()
	t.name.Paint(p, geom.Pt(x, 78), ink)
}
