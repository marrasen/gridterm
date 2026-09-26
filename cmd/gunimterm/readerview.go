package main

import (
	"errors"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
)

// reader shows a file on gridterm's own reader: its lines, coloured by
// the file's kind, with find, going to a line, following, a hex view,
// a view of a JSON log as columns, a minimap along the side, selecting
// and copying, and saving what it shows; or a picture. It draws into a
// grid of its own, copied into the pane as a terminal's is, with the
// picture and the minimap painted over it.
//
// The file is read on the program's side. The reader asks for it, and
// the lines arrive in the window's state; a file followed arrives again
// each time it changes.
type reader struct {
	id    string
	w     *window
	r     *files.Reader
	cells *widget.CellGrid
	g     *grid.Grid
	row   []widget.Cell
	// seq is the read handed over last, fresh a read that has arrived
	// and not been handed over, and then and thenPic what the reader
	// waits to be handed.
	seq     int
	fresh   *Reader
	then    func([]string, bool, error)
	thenPic func(files.Pic, error)
	// thenSave is told how the save that is out went, once Saves passes
	// saves.
	thenSave func(error)
	saves    int
	// send asks the program for something, from the update the reader
	// was last brought up to date in.
	send func(gunim.Intent)
	ui   *gunim.UI
	// pic and mapPic are the picture and the minimap as the painter
	// holds them, made from picFrom and mapFrom.
	pic, mapPic      *paint.Image
	picFrom, mapFrom image.Image
	wheel            float32
	held             bool
}

func newReader(w *window, id string) *reader {
	rd := &reader{id: id, w: w, cells: widget.NewCellGrid(), g: grid.New(1, 1, color.RGBA{}, color.RGBA{})}
	rd.cells.Size = w.fontSize
	rd.cells.Faces = w.font.Faces
	return rd
}

// show takes the reader's state: the first read makes the reader, and
// each read after it is handed to the reader, which asked for it or,
// for a file followed, is opened again for it.
func (rd *reader) show(st Reader, u *gunim.UI) {
	rd.send = func(in gunim.Intent) { u.Send(rd.cells, in) }
	rd.ui = u
	if st.Saves != rd.saves {
		rd.saves = st.Saves
		if then := rd.thenSave; then != nil {
			rd.thenSave = nil
			var err error
			if st.SaveErr != "" {
				err = errors.New(st.SaveErr)
			}
			then(err)
			rd.sync()
		}
	}
	if st.Seq == 0 || st.Seq == rd.seq {
		if rd.r != nil && rd.r.Busy() && st.SoFar > 0 {
			rd.r.ReadSoFar(st.SoFar)
			rd.sync()
		}
		return
	}
	rd.seq = st.Seq
	fresh := st
	rd.fresh = &fresh
	if rd.r == nil {
		rd.make(st)
	}
	switch {
	case rd.then != nil:
		then := rd.then
		rd.then = nil
		rd.handOver(then)
	case rd.thenPic != nil:
		then := rd.thenPic
		rd.thenPic = nil
		rd.handOverPic(then)
	default:
		rd.r.Open()
	}
	if st.Line > 0 && rd.seq == 1 {
		rd.r.GoToLine(st.Line)
	}
	rd.sync()
}

// make makes the reader, at the first read.
func (rd *reader) make(st Reader) {
	name := st.Name
	if name == "" {
		name = filepath.Base(st.Path)
	}
	r := files.NewReader(name, st.Path)
	r.Read = func(then func([]string, bool, error)) {
		if rd.fresh != nil {
			rd.handOver(then)
			return
		}
		rd.then = then
		rd.send(ReadAgain{Pane: rd.id})
	}
	r.ReadPic = func(then func(files.Pic, error)) {
		if rd.fresh != nil {
			rd.handOverPic(then)
			return
		}
		rd.thenPic = then
		rd.send(ReadAgain{Pane: rd.id})
	}
	r.OnCopy = func(text string) { rd.ui.SetClipboard(text) }
	r.OnClose = func() { rd.send(ClosePane{Pane: rd.id}) }
	r.OnSave = func(at string, lines []string, then func(error)) {
		rd.thenSave = then
		rd.send(SaveLines{Pane: rd.id, Path: at, Lines: lines})
	}
	r.SaveAs = filepath.Join("~", name)
	r.Scrolls = []string{"view.scrollUp", "view.scrollDown"}
	rd.r = r
	r.Follow(st.Follow)
	if st.Find {
		defer r.AskFind()
	}
}

// handOver gives the reader the read that arrived.
func (rd *reader) handOver(then func([]string, bool, error)) {
	f := rd.fresh
	rd.fresh = nil
	var err error
	if f.Err != "" {
		err = errors.New(f.Err)
	}
	then(f.Lines, f.Cut, err)
}

// handOverPic gives the reader the picture that arrived.
func (rd *reader) handOverPic(then func(files.Pic, error)) {
	f := rd.fresh
	rd.fresh = nil
	var err error
	if f.Err != "" {
		err = errors.New(f.Err)
	}
	var pic files.Pic
	if f.Pic != nil {
		pic = *f.Pic
	}
	then(pic, err)
}

// sync draws the reader and copies the rows that changed into the
// pane.
func (rd *reader) sync() {
	if rd.r == nil {
		return
	}
	cols, rows := rd.g.Size()
	rd.r.Draw(rd.g.View())
	rd.cells.Resize(cols, rows)
	for y := range rows {
		if !rd.g.RowDirty(y) {
			continue
		}
		rd.row = rd.row[:0]
		for x := range cols {
			rd.row = append(rd.row, cellOf(rd.g, x, y))
		}
		rd.cells.SetRow(y, rd.row)
	}
	rd.g.ClearDirty()
	rd.cells.SetCursor(widget.Cursor{})
}

// Children implements [gunim.Composite].
func (rd *reader) Children() []gunim.Node { return []gunim.Node{rd.cells} }

// Focusable implements [gunim.Focusable].
func (rd *reader) Focusable() bool { return true }

// Layout implements [gunim.Node]: the reader takes as many cells as fit,
// in the theme's colours.
func (rd *reader) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	size := k.Layout(c)
	k.Place(geom.Point{})
	if rd.r == nil {
		return size
	}
	rd.r.Style = readerStyle(f)
	cols, rows := rd.cells.Fit()
	cols, rows = max(cols, 1), max(rows, 1)
	if w, h := rd.g.Size(); w != cols || h != rows {
		rd.g.Resize(cols, rows)
	}
	rd.r.Layout(ui.Size{Cols: cols, Rows: rows})
	rd.sync()
	return size
}

// rgba is a theme colour as a grid takes it.
func rgba(c color.NRGBA) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }

// readerStyle is the reader's colours, from the theme.
func readerStyle(f gunim.Frame) files.Style {
	th := f.Theme
	fg, bg := rgba(widget.Ink.Get(th)), rgba(termBackground.Get(th))
	accent, dim := rgba(widget.Accent.Get(th)), rgba(faint.Get(th))
	return files.Style{
		FG: fg, BG: bg, SelectedFG: bg, SelectedBG: accent,
		HeaderFG: accent, PathFG: fg, DirFG: accent, LinkFG: accent, MarkedFG: accent, ClipFG: accent,
		KeyFG: fg, OffBG: rgba(widget.FieldFill.Get(th)), NoteFG: dim,
	}
}

// Paint implements [gunim.Node]: the cells, and over them the picture
// and the minimap.
func (rd *reader) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
	if rd.r == nil {
		return
	}
	cell := rd.cells.CellSize()
	room := func(r ui.Rect) geom.Rect {
		return geom.Rc(float32(r.X)*cell.W, float32(r.Y)*cell.H, float32(r.Cols)*cell.W, float32(r.Rows)*cell.H)
	}
	if rd.r.ShowsAPicture() {
		if img := rd.r.Picture(); img != nil {
			if img != rd.picFrom {
				rd.picFrom, rd.pic = img, paint.NewImage(img)
			}
			at := room(rd.r.PictureRoom())
			if at.Size().W > 0 {
				p.Image(rd.pic, fitted(at, img.Bounds().Size()), paint.ImageOpts{Opacity: 1})
			}
		}
	}
	if rd.r.MapShowing() {
		at := room(rd.r.MapRoom())
		if w, h := int(at.Size().W), int(at.Size().H); w > 0 && h > 0 {
			if img := rd.r.MapPicture(w, h); img != nil {
				if image.Image(img) != rd.mapFrom {
					rd.mapFrom, rd.mapPic = img, paint.NewImage(img)
				}
				p.Image(rd.mapPic, at, paint.ImageOpts{Opacity: 1})
			}
		}
	}
}

// fitted is the largest rectangle of a picture's shape that fits in
// room, in its middle.
func fitted(room geom.Rect, size image.Point) geom.Rect {
	if size.X <= 0 || size.Y <= 0 {
		return room
	}
	s := min(room.Size().W/float32(size.X), room.Size().H/float32(size.Y), 1)
	w, h := float32(size.X)*s, float32(size.Y)*s
	c := room.Center()
	return geom.Rc(c.X-w/2, c.Y-h/2, w, h)
}

// Handle implements [gunim.Handler]: keys and the mouse go to the
// reader; the wheel scrolls it.
func (rd *reader) Handle(e gi.Event, u *gunim.UI) bool {
	if rd.r == nil {
		return false
	}
	switch e := e.(type) {
	case gi.FocusGained, gi.FocusLost:
		_, on := e.(gi.FocusGained)
		rd.r.SetFocus(on)
	case gi.KeyPress:
		if e.Typed {
			return true
		}
		ev, ok := keyEvent(e)
		if !ok {
			return false
		}
		if id, bound := rd.w.keys.Lookup(ui.ChordOf(ev)); bound && !rd.r.ClaimsChord(ev, id) {
			return false
		}
		if took, _ := rd.r.HandleKey(ev); !took {
			return false
		}
	case gi.TextInput:
		for _, r := range e.Text {
			_, _ = rd.r.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
		}
	case gi.Scroll:
		if e.Mods&gi.ModControl != 0 {
			// Ctrl and the wheel size the font, which the window does.
			return false
		}
		h := rd.cells.CellSize().H
		if h <= 0 {
			return true
		}
		rd.wheel -= e.Delta.Y / h
		lines := int(math.Trunc(float64(rd.wheel)))
		rd.wheel -= float32(lines)
		rd.r.Scroll(lines)
	case gi.PointerDown:
		at := rd.cellAt(e.Pos)
		rd.held = true
		_, _ = rd.r.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: mouseButton(e.Button), Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)})
	case gi.PointerMove:
		at := rd.cellAt(e.Pos)
		button := input.MouseNone
		if rd.held {
			button = input.MouseLeft
		}
		_, _ = rd.r.HandleMouse(input.MouseEvent{Kind: input.MouseMove, Button: button, Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)})
	case gi.PointerUp:
		at := rd.cellAt(e.Pos)
		rd.held = false
		_, _ = rd.r.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: mouseButton(e.Button), Col: at.X, Row: at.Y, Mods: mouseMods(e.Mods)})
	default:
		return false
	}
	rd.sync()
	u.Invalidate()
	// A read that takes a while shows how far it has got.
	if rd.r.Busy() {
		u.After(100*time.Millisecond, func(u *gunim.UI) { rd.sync(); u.Invalidate() })
	}
	return true
}

func (rd *reader) cellAt(p geom.Point) grid.Point {
	col, row := rd.cells.CellAt(p)
	return grid.Point{X: col, Y: row}
}
