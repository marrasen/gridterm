package glyph

import (
	"image"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
)

// A family with a face for every style fakes nothing.
func TestAFamilyWithEveryStyleFakesNothing(t *testing.T) {
	full := Fonts{
		Regular: gomono.TTF, Bold: gomonobold.TTF,
		Italic: gomono.TTF, BoldItalic: gomonobold.TTF,
	}

	_, faked, err := buildFaces(full, 15, 96)

	if err != nil {
		t.Fatalf("buildFaces: %v", err)
	}
	for s := range numStyles {
		if faked[s] != 0 {
			t.Errorf("style %d fakes %d, and the family has a face for it", s, faked[s])
		}
	}
}

// A family with only a regular face fakes each of the other three from
// it: a smear for bold, a shear for italic, and both together.
func TestAFamilyWithOneFaceFakesTheRest(t *testing.T) {
	_, faked, err := buildFaces(Fonts{Regular: gomono.TTF}, 15, 96)

	if err != nil {
		t.Fatalf("buildFaces: %v", err)
	}
	want := [numStyles]Style{Regular: 0, Bold: Bold, Italic: Italic, BoldItalic: BoldItalic}
	if faked != want {
		t.Errorf("it fakes %v, want %v", faked, want)
	}
}

// A family with a bold face but no italic one fakes only the shear, and
// takes the bold from the face it has.
func TestOnlyWhatIsMissingIsFaked(t *testing.T) {
	_, faked, err := buildFaces(Fonts{Regular: gomono.TTF, Bold: gomonobold.TTF}, 15, 96)

	if err != nil {
		t.Fatalf("buildFaces: %v", err)
	}
	if got := faked[Bold]; got != 0 {
		t.Errorf("bold fakes %d, and the family has a bold face", got)
	}
	if got := faked[Italic]; got != Italic {
		t.Errorf("italic fakes %d, want the shear alone", got)
	}
	// Bold italic borrows the bold face, so only the shear is missing.
	if got := faked[BoldItalic]; got != Italic {
		t.Errorf("bold italic fakes %d, want the shear alone: it borrows the bold face", got)
	}
}

// A smeared glyph is wider than the one it came from, and covers every
// pixel that one did.
func TestEmboldeningWidensAndKeepsWhatWasThere(t *testing.T) {
	src := maskOf([]string{
		".#.",
		"###",
		".#.",
	})

	got := embolden(src)

	if want := src.Rect.Dx() + emboldenBy; got.Rect.Dx() != want {
		t.Errorf("the smeared mask is %d wide, want %d", got.Rect.Dx(), want)
	}
	if got.Rect.Dy() != src.Rect.Dy() {
		t.Errorf("the smeared mask is %d tall, want the %d it was", got.Rect.Dy(), src.Rect.Dy())
	}
	for y := range src.Rect.Dy() {
		for x := range src.Rect.Dx() {
			if at(src, x, y) != 0 && at(got, x, y) == 0 {
				t.Errorf("%d,%d was covered and the smear lost it", x, y)
			}
		}
	}
	// And it is actually wider ink, not just a wider box.
	if before, after := ink(src), ink(got); after <= before {
		t.Errorf("the smear covers %d pixels, want more than the %d it started with", after, before)
	}
}

// A sheared glyph leans: what is above the baseline moves right, what
// hangs below it moves left, and the baseline itself stays put.
func TestSlantingLeansAboutTheBaseline(t *testing.T) {
	// A single column the height of a glyph at a readable size, with the
	// baseline where a descender would hang below it.
	rows := make([]string, 20)
	for i := range rows {
		rows[i] = "#"
	}
	src := maskOf(rows)
	const baseline = 14

	got, moved := slant(src, baseline)

	if moved > 0 {
		t.Errorf("the mask's left edge moved %d, and a shear can only grow it leftwards", moved)
	}
	// The baseline row is where it was, allowing for the growth to the
	// left of it.
	if x := onlyInkAt(t, got, baseline); x != -moved {
		t.Errorf("the baseline row is at %d, want %d: a shear turns about the baseline", x, -moved)
	}
	if top, base := onlyInkAt(t, got, 0), onlyInkAt(t, got, baseline); top <= base {
		t.Errorf("the top row is at %d and the baseline at %d, want the top further right", top, base)
	}
	if low, base := onlyInkAt(t, got, 19), onlyInkAt(t, got, baseline); low >= base {
		t.Errorf("the row below the baseline is at %d and the baseline at %d, want it further left", low, base)
	}
}

// maskOf builds a coverage mask from rows of '#' and '.'.
func maskOf(rows []string) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, len(rows[0]), len(rows)))
	for y, row := range rows {
		for x, c := range row {
			if c != '#' {
				continue
			}
			for i := range 4 {
				m.Pix[y*m.Stride+x*4+i] = 0xff
			}
		}
	}
	return m
}

// at is the coverage of one pixel.
func at(m *image.RGBA, x, y int) byte { return m.Pix[y*m.Stride+x*4] }

// ink is how many pixels a mask covers at all.
func ink(m *image.RGBA) int {
	n := 0
	for y := range m.Rect.Dy() {
		for x := range m.Rect.Dx() {
			if at(m, x, y) != 0 {
				n++
			}
		}
	}
	return n
}

// onlyInkAt is the one covered column of a row, and fails if there is
// not exactly one.
func onlyInkAt(t *testing.T, m *image.RGBA, y int) int {
	t.Helper()
	found := -1
	for x := range m.Rect.Dx() {
		if at(m, x, y) == 0 {
			continue
		}
		if found >= 0 {
			t.Fatalf("row %d covers both %d and %d, and it started as one column", y, found, x)
		}
		found = x
	}
	if found < 0 {
		t.Fatalf("row %d covers nothing", y)
	}
	return found
}

// A family with only a regular face still draws bold and italic as
// something of their own, rather than as the regular glyph again.
//
// The mask is what the size says, so a smear is wider and a shear is
// wider still, and neither is the shape the regular face gave.
func TestOneFaceStillGivesBoldAndItalicShapesOfTheirOwn(t *testing.T) {
	a, err := NewAtlas(Fonts{Regular: gomono.TTF}, 15, 96)
	if err != nil {
		t.Fatalf("atlas: %v", err)
	}

	plain := a.Get('M', Regular)
	bold := a.Get('M', Bold)
	italic := a.Get('M', Italic)
	both := a.Get('M', BoldItalic)

	if plain.Empty {
		t.Fatal("the regular face drew no M, so this proves nothing")
	}
	if got, want := bold.Rect.Dx(), plain.Rect.Dx()+emboldenBy; got != want {
		t.Errorf("bold is %d wide and regular %d, want %d: bold is the smear",
			got, plain.Rect.Dx(), want)
	}
	if italic.Rect.Dx() <= plain.Rect.Dx() {
		t.Errorf("italic is %d wide and regular %d, want wider: italic is the shear",
			italic.Rect.Dx(), plain.Rect.Dx())
	}
	if both.Rect.Dx() <= italic.Rect.Dx() {
		t.Errorf("bold italic is %d wide and italic %d, want wider: it is both",
			both.Rect.Dx(), italic.Rect.Dx())
	}
	// A shear turns about the baseline, so a letter that hangs below it
	// leans the other way and reaches further left than the upright one.
	// An M sits on the baseline and has nothing to lean left, so this
	// needs a descender.
	tail, tailItalic := a.Get('p', Regular), a.Get('p', Italic)
	if tailItalic.Offset.X >= tail.Offset.X {
		t.Errorf("an italic p starts at %d and an upright one at %d, want it further left: "+
			"what hangs below the baseline leans that way", tailItalic.Offset.X, tail.Offset.X)
	}
}

// A family that has the faces draws them, and fakes nothing.
func TestAFamilyWithABoldFaceDrawsItRatherThanASmear(t *testing.T) {
	a, err := NewAtlas(Fonts{Regular: gomono.TTF, Bold: gomonobold.TTF}, 15, 96)
	if err != nil {
		t.Fatalf("atlas: %v", err)
	}

	plain := a.Get('M', Regular)
	bold := a.Get('M', Bold)

	if got := bold.Rect.Dx(); got == plain.Rect.Dx()+emboldenBy {
		t.Errorf("bold is %d wide, exactly the smear of the regular %d: the bold face was not used",
			got, plain.Rect.Dx())
	}
}
