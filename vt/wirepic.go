package vt

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// WirePicBudget is how many bytes of picture one repaint carries.
//
// A pane may hold sixty-four pictures of sixteen megabytes each, and a
// whole screen is sent every time a window starts watching. The
// pictures past the budget are left out, and the watcher sees the text
// with a gap where they were.
const WirePicBudget = 4 << 20

// setWirePic takes OSC 1338, which is how one gridterm window hands a
// picture to another along with the screen it is on.
//
// "clear" forgets the pictures the pane is holding.
// "place;row;col;cols;rows;<base64>" puts one at a row of the screen
// and leaves the cursor alone: a screen sent over the wire is a picture
// of a terminal, and the cursor in it is put where the program left it
// once everything else has been drawn.
//
// Only inline pictures ever come this way, so the same caps apply as to
// the sequence a program sends: the same decoder, the same size limit
// and the same count.
func (t *Terminal) setWirePic(params [][]byte) {
	if len(params) < 2 {
		return
	}
	switch string(params[1]) {
	case "clear":
		clear(t.images)
		t.images = t.images[:0]
	case "place":
		t.placeWirePic(params)
	}
}

// placeWirePic puts one picture at the row the sender said it was on.
func (t *Terminal) placeWirePic(params [][]byte) {
	if len(params) < 7 {
		return
	}
	row, ok := wireNum(params[2])
	col, ok2 := wireNum(params[3])
	cols, ok3 := wireNum(params[4])
	rows, ok4 := wireNum(params[5])
	if !ok || !ok2 || !ok3 || !ok4 {
		return
	}
	if col < 0 || cols <= 0 || rows <= 0 {
		return
	}
	// The row is turned back into a line number here, so the picture
	// keeps its place as the screen scrolls on from what was sent. A
	// row above the top of history names no line.
	line := int64(t.scr.gone) + int64(row)
	if line < 0 {
		return
	}
	img, raw, ok := decodeImage(string(params[6]))
	if !ok {
		return
	}
	t.holdImage(Image{
		Wire: true,
		Line: uint64(line),
		Col:  min(col, max(t.scr.cols-1, 0)),
		Cols: min(cols, t.scr.cols),
		Rows: min(rows, t.scr.rows),
		Img:  img,
		Raw:  raw,
	})
}

// wireNum reads one whole number off the wire.
func wireNum(b []byte) (int, bool) {
	n, err := strconv.Atoi(string(b))
	return n, err == nil
}

// writeImages writes the pictures on a screen, after saying to forget
// whatever pictures the far end was holding.
//
// The clear goes out even when there are none: a repaint replaces the
// screen, and a picture from the screen before it would otherwise sit
// on a line that now says something else.
func writeImages(b *strings.Builder, placed []Placement) {
	b.WriteString("\x1b]1338;clear\x07")
	left := WirePicBudget
	for _, at := range placed {
		if len(at.Raw) == 0 || len(at.Raw) > left {
			continue
		}
		left -= len(at.Raw)
		fmt.Fprintf(b, "\x1b]1338;place;%d;%d;%d;%d;%s\x07",
			at.Top, at.Col, at.Cols, at.Rows,
			base64.StdEncoding.EncodeToString(at.Raw))
	}
}
