package files

import (
	"image"
	"image/color"
	"math"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
)

// mapLeast is the narrowest pane that still gets a strip. Below it the
// column the strip takes is a column the file needed more.
const mapLeast = 24

// mapSample is how many lines of a band are looked at. A band of a
// large file is thousands of lines, and what the strip draws is the
// shape of the file rather than a measurement of it.
const mapSample = 8

// mapGapFrom is how many pixels a line has to be given on the strip
// before a pixel of it is left out, so lines standing one under the
// next read as lines rather than as one block.
const mapGapFrom = 3

// band is one row of pixels of the strip: how much text the lines it
// covers hold, and the worst thing any of them said.
type band struct {
	// ink is 0 to 1, the share of the pane's width the lines fill.
	ink float64

	// worst is the colour of the loudest line in the band, and
	// colourPlain for a band with nothing to say.
	worst colour
}

// ShowMap turns the strip beside the file on or off.
func (r *Reader) ShowMap(on bool) {
	r.showMap = on
	r.mapPic = nil
	r.mapDrag = false
}

// MapShowing reports whether the strip is on, whether or not the pane
// is wide enough to draw it.
func (r *Reader) MapShowing() bool { return r.showMap }

// mapWidth is how many columns the strip takes, and none when it is
// off or the pane is too narrow for it.
//
// Two columns for a log: room for how bad it got beside the shape of
// the text, which is what a log is read for. One for everything else.
func (r *Reader) mapWidth() int {
	if !r.showMap || r.isPic || r.err != nil || len(r.shown) == 0 {
		return 0
	}
	if r.size.Cols < mapLeast {
		return 0
	}
	if r.log != nil {
		return 2
	}
	return 1
}

// bodyCols is how wide the file itself is drawn, which is the pane
// less the strip.
func (r *Reader) bodyCols() int { return max(r.size.Cols-r.mapWidth(), 0) }

// MapRoom is where the strip goes, in the reader's own cells, and empty
// when there is none. The window draws MapPicture there.
func (r *Reader) MapRoom() ui.Rect {
	w := r.mapWidth()
	body := r.size.Rows - readerChrome
	if w == 0 || body <= 0 {
		return ui.Rect{}
	}
	return ui.Rect{X: r.size.Cols - w, Y: 1, Cols: w, Rows: body}
}

// MapPicture is the strip drawn w by h pixels, and nil when there is no
// strip.
//
// Pixels rather than characters. The grid is for text, and a strip of
// block characters could only say how full a line was in five steps and
// a band in one row of text: this has a row of pixels for every few
// lines and a bar as long as they are. The ground is left clear, so the
// box the strip's own cells draw, saying where the pane is, shows
// through.
//
// The same image comes back until the lines or the size change, so the
// window builds a texture only then.
func (r *Reader) MapPicture(w, h int) *image.RGBA {
	if r.mapWidth() == 0 || w <= 0 || h <= 0 {
		return nil
	}
	if p := r.mapPic; p != nil && p.Bounds().Dx() == w && p.Bounds().Dy() == h {
		return p
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	wide := float64(max(r.bodyCols(), 1))
	// A log keeps a quarter of the strip, and never less than two
	// pixels, for how bad each band got: a band with one error in it
	// is found by its colour, however short its lines are.
	mark := 0
	if r.log != nil {
		mark = max(w/4, 2)
	}
	room := max(w-mark-1, 1)
	n := len(r.shown)
	for y := range h {
		from, to := r.bandRange(y, h)
		if from >= n {
			break
		}
		if n <= h/mapGapFrom && y+1 < h {
			// Every line has a few rows to itself: the last of them is
			// left clear so one line is told from the next.
			if next, _ := r.bandRange(y+1, h); next != from {
				continue
			}
		}
		b := r.bandOf(from, to, wide)
		if b.ink > 0 {
			// A pixel at least, so a line with anything on it shows.
			fill(img, 0, y, max(int(math.Round(b.ink*float64(room))), 1), r.Style.NoteFG)
		}
		if mark > 0 && b.worst != colourPlain {
			fill(img, w-mark, y, mark, r.colourOf(b.worst))
		}
	}
	r.mapPic = img
	return img
}

// fill paints a run of one row of pixels.
func fill(img *image.RGBA, x, y, n int, c color.RGBA) {
	for i := range n {
		img.SetRGBA(x+i, y, c)
	}
}

// bandRange is the lines one row of the strip stands for, of rows.
func (r *Reader) bandRange(y, rows int) (from, to int) {
	n := len(r.shown)
	from = y * n / rows
	to = (y + 1) * n / rows
	if to <= from {
		// More rows than lines: a line is drawn on every row that
		// falls inside it.
		to = min(from+1, n)
	}
	return from, to
}

// bandOf measures one band: how full its lines are, and the worst
// thing any of them said.
func (r *Reader) bandOf(from, to int, wide float64) band {
	if from >= to || from >= len(r.shown) {
		return band{}
	}
	step := max((to-from)/mapSample, 1)
	var total float64
	var n int
	var worst colour
	for i := from; i < to && i < len(r.shown); i += step {
		total += float64(len(r.shown[i]))
		n++
	}
	if sev := r.worstIn(from, to); sev > worst {
		worst = sev
	}
	if n == 0 {
		return band{worst: worst}
	}
	return band{ink: min(total/float64(n)/wide, 1), worst: worst}
}

// worstIn is the loudest level any line of a band was written at, for
// a file the log view has read. Every other file has none.
func (r *Reader) worstIn(from, to int) colour {
	if r.log == nil {
		return colourPlain
	}
	var worst colour
	for i := from; i < to && i < len(r.log.marks); i++ {
		if sev := r.log.marks[i].sev; sev > worst {
			worst = sev
		}
	}
	return worst
}

// paintMap draws the ground of the strip down the right of the file:
// blank cells, with the rows the pane is showing on a ground of their
// own. What is in the file is drawn over them in pixels, by whatever
// puts MapPicture on screen.
//
// In the place a code editor puts its minimap and doing the same job:
// showing the shape of the whole file at once, so a run of errors is
// found by looking rather than by scrolling.
func (r *Reader) paintMap(v grid.View, cols, rows int) {
	w := r.mapWidth()
	body := rows - readerChrome
	if w == 0 || body <= 0 {
		return
	}
	first, last := r.viewBand(body)
	x := cols - w
	for y := range body {
		bg := r.Style.BG
		if y >= first && y < last {
			// Where the pane is in the file, as a box rather than a
			// line: a bar that stands for a screenful should look like
			// a screenful.
			bg = r.Style.OffBG
		}
		for i := range w {
			v.Set(x+i, y+1, grid.Cell{Rune: ' ', FG: r.Style.NoteFG, BG: bg, Width: 1})
		}
	}
}

// viewBand is the rows of the strip the pane is showing, the second
// one past the end.
//
// Never fewer than one row: a screenful of a very long file is a
// fraction of a row, and a box nobody can see is not a box.
func (r *Reader) viewBand(rows int) (first, last int) {
	n := len(r.shown)
	if n == 0 || rows <= 0 {
		return 0, 0
	}
	first = min(r.top*rows/n, rows-1)
	last = max((r.top+r.rows())*rows/n, first+1)
	return first, min(last, rows)
}

// onMap reports whether a cell of the pane is on the strip.
func (r *Reader) onMap(col, row int) bool {
	room := r.MapRoom()
	return !room.Empty() && room.Contains(col, row)
}

// mapTo moves the file to the line a row of the strip stands for, with
// the pane's own height centred on it. A row past either end goes to
// that end, so a drag carried off the strip keeps going the way it was.
func (r *Reader) mapTo(row int) {
	room := r.MapRoom()
	if room.Empty() {
		return
	}
	y := min(max(row-room.Y, 0), room.Rows-1)
	at := y * len(r.shown) / room.Rows
	r.top = at - r.rows()/2
	r.clampTop()
	r.follow = false
}

// mapPress is a press on the strip: the file goes to the band that was
// pointed at, and a drag from there carries on until the button comes
// up.
//
// It reports whether the press was on the strip at all, so a press
// anywhere else goes on to pick text out.
func (r *Reader) mapPress(col, row int) bool {
	if !r.onMap(col, row) {
		return false
	}
	r.mapTo(row)
	r.mapDrag = true
	return true
}
