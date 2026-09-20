package files

import "github.com/marrasen/gridterm/grid"

// mapLeast is the narrowest pane that still gets a strip. Below it the
// column the strip takes is a column the file needed more.
const mapLeast = 24

// mapSample is how many lines of a band are looked at. A band of a
// large file is thousands of lines, and what the strip draws is the
// shape of the file rather than a measurement of it.
const mapSample = 8

// mapShades are the blocks a band is drawn with, emptiest first. A
// band with nothing in it is left blank, so the strip shows where a
// file stops as well as what is in it.
var mapShades = []rune{' ', '░', '▒', '▓', '█'}

// band is one row of the strip: how much text the lines it covers
// hold, and the worst thing any of them said.
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
	r.mapFor = -1
}

// MapShowing reports whether the strip is on, whether or not the pane
// is wide enough to draw it.
func (r *Reader) MapShowing() bool { return r.showMap }

// mapWidth is how many columns the strip takes, and none when it is
// off or the pane is too narrow for it.
//
// Two columns for a log: one for the shape of the text and one for how
// bad it got, which is what a log is read for. One for everything
// else.
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

// bands are the rows of the strip, worked out once for a file and a
// height rather than on every frame.
func (r *Reader) bands(rows int) []band {
	if rows <= 0 {
		return nil
	}
	if r.mapFor == rows && r.mapLines == len(r.shown) && r.mapBands != nil {
		return r.mapBands
	}
	wide := float64(max(r.bodyCols(), 1))
	out := make([]band, rows)
	for y := range out {
		from, to := r.bandRange(y, rows)
		out[y] = r.bandOf(from, to, wide)
	}
	r.mapBands, r.mapFor, r.mapLines = out, rows, len(r.shown)
	return out
}

// bandRange is the lines one row of the strip stands for.
func (r *Reader) bandRange(y, rows int) (from, to int) {
	n := len(r.shown)
	from = y * n / rows
	to = (y + 1) * n / rows
	if to <= from {
		// More rows than lines: every line gets a row of its own and
		// the rows past the end stand for nothing.
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

// paintMap draws the strip down the right of the file.
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
	bands := r.bands(body)
	first, last := r.viewBand(body)
	x := cols - w
	for y, b := range bands {
		bg := r.Style.BG
		if y >= first && y < last {
			// Where the pane is in the file, as a box rather than a
			// line: a bar that stands for a screenful should look like
			// a screenful.
			bg = r.Style.OffBG
		}
		v.Set(x, y+1, grid.Cell{
			Rune: mapShades[min(int(b.ink*float64(len(mapShades))), len(mapShades)-1)],
			FG:   r.Style.NoteFG, BG: bg, Width: 1,
		})
		if w < 2 {
			continue
		}
		mark := ' '
		if b.worst != colourPlain {
			mark = '▌'
		}
		v.Set(x+1, y+1, grid.Cell{
			Rune: mark, FG: r.colourOf(b.worst), BG: bg, Width: 1,
		})
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

// mapPress is a press on the strip: the file goes to the band that was
// pointed at, with the pane's own height centred on it.
//
// It reports whether the press was on the strip at all, so a press
// anywhere else goes on to pick text out.
func (r *Reader) mapPress(col, row, rows int) bool {
	w := r.mapWidth()
	body := rows - readerChrome
	if w == 0 || body <= 0 || col < r.size.Cols-w || row < 1 || row > body {
		return false
	}
	at := (row - 1) * len(r.shown) / body
	r.top = at - r.rows()/2
	r.clampTop()
	r.follow = false
	return true
}
