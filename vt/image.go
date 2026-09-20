package vt

import (
	"bytes"
	"encoding/base64"
	"image"
	"strconv"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// MostImages is how many inline pictures a pane keeps, and
// MostImageBytes the largest one it will decode.
//
// A program can send as many as it likes, and each one is pixels held
// for as long as the line it sits on is. Past the count the oldest
// goes, which is the one furthest up the scrollback and the least
// likely to be looked at.
const (
	MostImages     = 64
	MostImageBytes = 16 << 20
)

// Image is a picture a program put in the output, and where it sits.
//
// Line is the line it starts on, counted the way LineNumber counts,
// so it names the same place however far the screen has scrolled
// since. Cols and Rows are the cells it was given, which is what the
// text flowed around.
type Image struct {
	Line uint64
	Col  int
	Cols int
	Rows int
	Img  image.Image
}

// Images are the pictures the pane is holding, oldest first.
//
// The slice is the terminal's. A caller may read it and must not
// write to it or keep it past the next write to the pane.
func (t *Terminal) Images() []Image { return t.images }

// setImage takes OSC 1337, which is how iTerm2 and the terminals that
// followed it put a picture in the output.
//
// The payload is "File=key=value;key=value:<base64>". Only inline
// pictures are taken: the same sequence is used to send a file to the
// machine the terminal is on, which is not something a pane should be
// able to make this window do.
func (t *Terminal) setImage(params [][]byte) {
	if len(params) < 2 {
		return
	}
	// The parser cuts on semicolons and the payload holds them, so
	// what was sent is the parameters joined back up.
	body := string(bytes.Join(params[1:], []byte(";")))
	head, encoded, found := strings.Cut(body, ":")
	if !found {
		return
	}
	args := imageArgs(head)
	if !strings.EqualFold(args["__name"], "File") || args["inline"] != "1" {
		// Not an inline picture. A File= without inline=1 asks the
		// terminal to save a file, which this does not do.
		return
	}
	img, ok := decodeImage(encoded)
	if !ok {
		return
	}
	t.placeImage(img, args)
}

// imageArgs reads "File=inline=1;width=20;height=10" into its parts,
// with the word in front under "__name".
func imageArgs(head string) map[string]string {
	out := map[string]string{}
	name, rest, found := strings.Cut(head, "=")
	if !found {
		out["__name"] = strings.TrimSpace(head)
		return out
	}
	out["__name"] = strings.TrimSpace(name)
	for _, pair := range strings.Split(rest, ";") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return out
}

// decodeImage reads the base64 a program sent and decodes the picture
// in it.
func decodeImage(encoded string) (image.Image, bool) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(MostImageBytes) {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	if len(raw) > MostImageBytes {
		return nil, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, false
	}
	if int64(cfg.Width)*int64(cfg.Height) > mostImagePixels {
		return nil, false
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil || img.Bounds().Empty() {
		return nil, false
	}
	return img, true
}

// mostImagePixels is the largest picture decoded, which is what stops
// a small file holding a very large one.
const mostImagePixels = 16 << 20

// placeImage puts a picture where the cursor is and moves the cursor
// past it, so what the program prints next lands below rather than
// on top.
func (t *Terminal) placeImage(img image.Image, args map[string]string) {
	cols, rows := t.imageCells(img, args)
	if cols <= 0 || rows <= 0 {
		return
	}
	at := Image{
		Line: t.scr.LineNumber(t.scr.cursor.Y),
		Col:  t.scr.cursor.X,
		Cols: cols,
		Rows: rows,
		Img:  img,
	}
	t.images = append(t.images, at)
	if len(t.images) > MostImages {
		// The oldest goes: it is furthest up the history and the least
		// likely to be looked at again.
		clear(t.images[:len(t.images)-MostImages])
		t.images = append(t.images[:0], t.images[len(t.images)-MostImages:]...)
	}
	// The rows the picture sits on are left blank and stepped over, so
	// the program's next line lands under it. Written as line feeds so
	// the screen scrolls and the picture's line number stays right.
	for range rows {
		t.scr.LineFeed()
	}
	t.scr.CarriageReturn()
}

// imageCells is how many cells a picture was given.
//
// A program may say in cells, in pixels or as a share of the screen.
// Saying nothing means as big as the picture is, at the cell size
// terminals are usually asked about, and capped at the screen.
func (t *Terminal) imageCells(img image.Image, args map[string]string) (cols, rows int) {
	b := img.Bounds()
	cols = imageSide(args["width"], t.scr.cols, b.Dx(), guessCellWidth)
	rows = imageSide(args["height"], t.scr.rows, b.Dy(), guessCellHeight)
	return min(max(cols, 1), t.scr.cols), min(max(rows, 1), t.scr.rows)
}

// guessCellWidth and guessCellHeight are the pixels a cell is taken to
// be when a program asks for a size in pixels.
//
// The emulator does not know how big a cell is drawn: that belongs to
// whatever is painting it. These are the ordinary sort of numbers, and
// a program that cares says in cells instead.
const (
	guessCellWidth  = 8
	guessCellHeight = 16
)

// imageSide reads one of iTerm2's size arguments: "20" is cells, "20px"
// is pixels, "20%" is a share of the screen, and "auto" or nothing is
// as big as the picture is.
func imageSide(arg string, screen, pixels, perCell int) int {
	arg = strings.ToLower(strings.TrimSpace(arg))
	switch {
	case arg == "", arg == "auto":
		return (pixels + perCell - 1) / perCell
	case strings.HasSuffix(arg, "px"):
		n, err := strconv.Atoi(strings.TrimSuffix(arg, "px"))
		if err != nil || n <= 0 {
			return 0
		}
		return (n + perCell - 1) / perCell
	case strings.HasSuffix(arg, "%"):
		n, err := strconv.Atoi(strings.TrimSuffix(arg, "%"))
		if err != nil || n <= 0 {
			return 0
		}
		return screen * min(n, 100) / 100
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// dropOldImages forgets the pictures whose lines have fallen out of
// history, because nothing can scroll back to them any more.
func (t *Terminal) dropOldImages() {
	oldest := t.scr.gone - uint64(min(t.scr.History(), int(t.scr.gone)))
	keep := t.images[:0]
	for _, at := range t.images {
		if at.Line+uint64(at.Rows) > oldest {
			keep = append(keep, at)
		}
	}
	clear(t.images[len(keep):])
	t.images = keep
}

// Placement is where a picture sits on the screen right now: the row
// its top is on, counted from the top of the view, and which row of
// the picture that is.
//
// Top may be negative and Rows may run past the bottom: a picture
// half scrolled off is drawn in part, and the caller clips it.
type Placement struct {
	Image

	// Top is the screen row the picture's first row would be on.
	Top int
}

// Placed is where each picture the pane holds sits on the screen as
// it stands, leaving out the ones scrolled entirely out of sight.
//
// The alternate screen has none: a full-screen program has its own
// picture of the world and the lines these sit on are not on it.
func (t *Terminal) Placed() []Placement {
	if t.scr.OnAltBuffer() {
		return nil
	}
	t.dropOldImages()
	var out []Placement
	for _, at := range t.images {
		// The row a line is on now: lines that have gone off the top
		// are what LineNumber counts from, and the view may be
		// scrolled back into them.
		top := int(int64(at.Line)-int64(t.scr.gone)) + t.scr.scrollOff
		if top >= t.scr.rows || top+at.Rows <= 0 {
			continue
		}
		out = append(out, Placement{Image: at, Top: top})
	}
	return out
}
